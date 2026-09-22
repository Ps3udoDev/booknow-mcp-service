#!/usr/bin/env bash
# Renders deploy/cloudrun/service.yaml for one environment and prints it to stdout.
#
#   deploy/cloudrun/render.sh staging "$IMAGE" > /tmp/service.yaml
#   gcloud run services replace /tmp/service.yaml --project "$PROJECT_ID" --region "$REGION"
#
# IMAGE must be pinned by digest (…@sha256:…): what was smoke-tested is exactly what gets promoted.
set -euo pipefail

usage() { echo "usage: $0 <staging|production> <image@sha256:digest>" >&2; exit 2; }

[[ $# -eq 2 ]] || usage
env_name=$1
image=$2
dir=$(cd "$(dirname "$0")" && pwd)
env_file="$dir/$env_name.env"

[[ -f $env_file ]] || { echo "unknown environment: $env_name" >&2; usage; }
[[ $image =~ @sha256:[0-9a-f]{64}$ ]] || { echo "image must be pinned by digest (…@sha256:<64 hex>): $image" >&2; exit 1; }

# The env file is data, not code: KEY=value lines, read literally (no sourcing, no expansion).
while IFS= read -r line || [[ -n $line ]]; do
  line=${line%$'\r'}
  [[ -z $line || $line == \#* ]] && continue
  [[ $line =~ ^([A-Z_][A-Z0-9_]*)=(.*)$ ]] || { echo "$env_file: invalid line: $line" >&2; exit 1; }
  export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"
done < "$env_file"
export IMAGE=$image

vars=(SERVICE REGION APP_ENV LOG_LEVEL RUNTIME_SA SUPABASE_URL MCP_PUBLIC_URL MCP_ALLOWED_ORIGINS
  MAX_INSTANCES DB_MAX_CONNS DATABASE_URL_SECRET DATABASE_URL_SECRET_VERSION IMAGE)

for v in "${vars[@]}"; do
  [[ ${!v+x} ]] || { echo "$env_file: $v is not set" >&2; exit 1; }
  [[ ${!v} != *"<"* ]] || { echo "$env_file: $v still has a <placeholder>: ${!v}" >&2; exit 1; }
done

# Only the listed variables are substituted, so a stray "$" in the template can never pull in the caller's env.
# shellcheck disable=SC2016 # the literal ${NAME} list is envsubst's SHELL-FORMAT argument
envsubst "$(printf '${%s} ' "${vars[@]}")" < "$dir/service.yaml"
