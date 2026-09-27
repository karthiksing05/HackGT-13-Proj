#!/usr/bin/env bash
# Shared connection settings; sourced from each service directory.
for file in ../.env .env ../Backend/.env; do
  [[ -f "$file" ]] || continue
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ "$line" =~ ^[[:space:]]*(DEPLOY_HOST|DEPLOY_USER|DEPLOY_PASSWORD)[[:space:]]*=[[:space:]]*(.*[^[:space:]])?[[:space:]]*$ ]] || continue
    key="${BASH_REMATCH[1]}"; value="${BASH_REMATCH[2]}"
    value="${value#\"}"; value="${value%\"}"; value="${value#\'}"; value="${value%\'}"
    [[ -n "${!key:-}" ]] || export "$key=$value"
  done < "$file"
done

server="${2:-${DEPLOY_USER:-root}}@${1:-${DEPLOY_HOST:?Set DEPLOY_HOST in .env}}"

# Use the saved password when present, otherwise SSH handles authentication.
if [[ -n "${DEPLOY_PASSWORD:-}" ]]; then
  askpass=$(mktemp)
  trap 'rm -f "$askpass"' EXIT
  printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$DEPLOY_PASSWORD"\n' > "$askpass"
  chmod 700 "$askpass"
  export DEPLOY_PASSWORD SSH_ASKPASS="$askpass" SSH_ASKPASS_REQUIRE=force DISPLAY="${DISPLAY:-:0}"
fi
