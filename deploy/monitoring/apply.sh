#!/usr/bin/env bash
# Creates the log-based metrics, the e-mail notification channel and the alert policies of booknow-mcp.
# Idempotent: anything that already exists (same name or displayName) is left as is.
#
#   PROJECT_ID=agendia-mcp ALERT_EMAIL=ops@example.com SERVICE=booknow-mcp deploy/monitoring/apply.sh
#
# Uses the REST APIs with `gcloud auth print-access-token`, so the gcloud alpha/beta components are not needed.
# GCLOUD overrides the gcloud binary (on Windows Git Bash: GCLOUD=gcloud.cmd).
set -euo pipefail

project=${PROJECT_ID:?PROJECT_ID is required}
email=${ALERT_EMAIL:?ALERT_EMAIL is required}
service=${SERVICE:-booknow-mcp}
gcloud_bin=${GCLOUD:-gcloud}
dir=$(cd "$(dirname "$0")" && pwd)

token=$("$gcloud_bin" auth print-access-token | tr -d '\r')
logging_api="https://logging.googleapis.com/v2/projects/$project"
monitoring_api="https://monitoring.googleapis.com/v3/projects/$project"

api() { # method url [body-file]
  local args=(-sS --fail-with-body -X "$1" -H "Authorization: Bearer $token" -H 'Content-Type: application/json')
  [[ $# -eq 3 ]] && args+=(--data-binary "@$3")
  curl "${args[@]}" "$2"
}

for file in "$dir"/metrics/*.json; do
  name=$(basename "$file" .json)
  if api GET "$logging_api/metrics/$name" >/dev/null 2>&1; then
    echo "exists   metric $name"
  else
    api POST "$logging_api/metrics" "$file" >/dev/null
    echo "created  metric $name"
  fi
done

channel_name="booknow-mcp alerts ($email)"
channels=$(api GET "$monitoring_api/notificationChannels?filter=type%3D%22email%22")
channel=$(python -c '
import json, sys
wanted = sys.argv[1]
for c in json.load(sys.stdin).get("notificationChannels", []):
    if c.get("labels", {}).get("email_address") == wanted:
        print(c["name"]); break
' "$email" <<<"$channels")

if [[ -n $channel ]]; then
  echo "exists   channel $channel_name"
else
  body=$(mktemp)
  python -c 'import json, sys; print(json.dumps({"type": "email", "displayName": sys.argv[1], "labels": {"email_address": sys.argv[2]}}))' \
    "$channel_name" "$email" > "$body"
  channel=$(api POST "$monitoring_api/notificationChannels" "$body" | python -c 'import json, sys; print(json.load(sys.stdin)["name"])')
  rm -f "$body"
  echo "created  channel $channel_name"
fi

existing=$(api GET "$monitoring_api/alertPolicies?pageSize=200" | python -c '
import json, sys
print("\n".join(p["displayName"] for p in json.load(sys.stdin).get("alertPolicies", [])))')

export SERVICE=$service CHANNEL=$channel
for file in "$dir"/policies/*.json; do
  rendered=$(mktemp)
  # shellcheck disable=SC2016 # the literal ${NAME} list is envsubst's SHELL-FORMAT argument
  envsubst '${SERVICE} ${CHANNEL}' < "$file" > "$rendered"
  display=$(python -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["displayName"])' "$rendered")
  if grep -qxF "$display" <<<"$existing"; then
    echo "exists   policy $display"
  else
    api POST "$monitoring_api/alertPolicies" "$rendered" >/dev/null
    echo "created  policy $display"
  fi
  rm -f "$rendered"
done
