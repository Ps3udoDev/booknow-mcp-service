#!/usr/bin/env bash
# Smoke test of a deployed booknow-mcp-service. Read-only: it never creates drafts or appointments.
#
#   deploy/cloudrun/smoke.sh https://booknow-mcp-staging-123.us-west1.run.app
#
# Optional environment:
#   IAM_TOKEN           identity token for an IAM-private service (staging): `gcloud auth print-identity-token`.
#                       Sent as X-Serverless-Authorization so Authorization stays free for the MCP Bearer token.
#   SMOKE_ACCESS_TOKEN  Supabase OAuth access token of an MCP connection (MCP Inspector). Enables the MCP checks.
#   EXPECTED_RESOURCE   protected resource to expect in the metadata (default: <url>/mcp).
#   EXPECTED_ISSUER     authorization server to expect (default: production Supabase Auth).
# Tokens are never printed.
set -uo pipefail

[[ $# -eq 1 ]] || { echo "usage: $0 <service-url>" >&2; exit 2; }
base=${1%/}
resource=${EXPECTED_RESOURCE:-$base/mcp}
issuer=${EXPECTED_ISSUER:-https://rrnysepngbycvuciodoj.supabase.co/auth/v1}
failures=0

iam=()
[[ -n ${IAM_TOKEN:-} ]] && iam=(-H "X-Serverless-Authorization: Bearer $IAM_TOKEN")

check() { # name, expected, actual
  if [[ $3 == "$2" ]]; then
    echo "ok    $1"
  else
    echo "FAIL  $1: want $2, got $3"
    failures=$((failures + 1))
  fi
}

contains() { # name, needle, haystack
  if [[ $3 == *"$2"* ]]; then
    echo "ok    $1"
  else
    echo "FAIL  $1: missing $2"
    failures=$((failures + 1))
  fi
}

code() { curl -s -o /dev/null -w '%{http_code}' "${iam[@]}" "$@"; }

# /healthz is not checked from outside: Cloud Run's frontend reserves that path and answers 404 before the
# container (the liveness probe still reaches it inside the instance). /readyz proves the process and Postgres.
check "GET /readyz" 200 "$(code "$base/readyz")"

metadata=$(curl -s "${iam[@]}" "$base/.well-known/oauth-protected-resource/mcp")
contains "metadata resource" "\"resource\":\"$resource\"" "$metadata"
contains "metadata issuer" "\"authorization_servers\":[\"$issuer\"]" "$metadata"

mcp_headers=(-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream')
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}'

challenge=$(curl -s -o /dev/null -D - "${iam[@]}" "${mcp_headers[@]}" -X POST "$base/mcp" -d "$init" | tr -d '\r')
contains "POST /mcp without token → 401" " 401" "$(head -n1 <<<"$challenge")"
contains "401 challenge points to metadata" 'resource_metadata="' "$challenge"

check "POST /mcp from a foreign Origin → 403" 403 \
  "$(code "${mcp_headers[@]}" -H 'Origin: https://evil.example' -X POST "$base/mcp" -d "$init")"

if [[ -n ${SMOKE_ACCESS_TOKEN:-} ]]; then
  auth=(-H "Authorization: Bearer $SMOKE_ACCESS_TOKEN" -H 'Mcp-Protocol-Version: 2025-06-18')
  rpc() { curl -s "${iam[@]}" "${mcp_headers[@]}" "${auth[@]}" -X POST "$base/mcp" -d "$1"; }

  contains "initialize" '"serverInfo"' "$(rpc "$init")"

  tools=$(rpc '{"jsonrpc":"2.0","id":2,"method":"tools/list"}')
  for t in health get_business_snapshot get_schedule_summary list_available_slots list_appointments \
    search_customers create_appointment_draft confirm_appointment_draft; do
    contains "tools/list has $t" "\"name\":\"$t\"" "$tools"
  done
  if [[ $tools == *'"tenantId"'* || $tools == *'"tenant_id"'* ]]; then
    echo "FAIL  a tool exposes a tenant argument"
    failures=$((failures + 1))
  fi

  health=$(rpc '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"health","arguments":{}}}')
  contains "tools/call health" '"status":"ok"' "$health"

  # Stateless JSON transport: no SSE stream to open.
  check "GET /mcp → 405" 405 "$(code "${auth[@]}" -H 'Accept: text/event-stream' "$base/mcp")"
else
  echo "skip  MCP calls (SMOKE_ACCESS_TOKEN not set)"
fi

if ((failures > 0)); then
  echo "$failures check(s) failed"
  exit 1
fi

echo "all checks passed"
