#!/usr/bin/env bash
# Build, upload and restart the API. Add --admin to also ship the maintenance CLI.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

build_args=()
names=(sidequestz-server)
args=()
for arg in "$@"; do
  case "$arg" in
    --admin) build_args=(--admin); names=(sidequestz-server sidequestz-admin) ;;
    -h|--help) echo "Usage: ./deploy.sh [host] [user] [--admin]"; exit 0 ;;
    --*) echo "Unknown option: $arg" >&2; exit 1 ;;
    *) args+=("$arg") ;;
  esac
done
[[ ${#args[@]} -le 2 ]] || { echo "Expected at most host and user." >&2; exit 1; }

# Read DEPLOY_* values only; shell environment takes precedence, then root .env.
for file in ../.env .env; do
  [[ -f "$file" ]] || continue
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ "$line" =~ ^[[:space:]]*(DEPLOY_[A-Z_]+)[[:space:]]*=[[:space:]]*(.*[^[:space:]])?[[:space:]]*$ ]] || continue
    key="${BASH_REMATCH[1]}"; value="${BASH_REMATCH[2]}"
    value="${value#\"}"; value="${value%\"}"; value="${value#\'}"; value="${value%\'}"
    [[ -n "${!key:-}" ]] || export "$key=$value"
  done < "$file"
done

host="${args[0]:-${DEPLOY_HOST:-}}"
user="${args[1]:-${DEPLOY_USER:-root}}"
remote_dir="${DEPLOY_REMOTE_DIR:-/opt/backend}"
service="${DEPLOY_SERVICE:-sidequestz}"
[[ -n "$host" ]] || { echo "Set DEPLOY_HOST or pass a host." >&2; exit 1; }
[[ "$remote_dir" =~ ^/[a-zA-Z0-9_./-]+$ && "$service" =~ ^[a-zA-Z0-9_-]+$ ]] || {
  echo "Invalid DEPLOY_REMOTE_DIR or DEPLOY_SERVICE." >&2; exit 1;
}

# Use SSH keys, or reuse the same password helper for ssh and scp.
auth=()
if [[ -n "${DEPLOY_PASSWORD:-}" ]]; then
  if command -v sshpass >/dev/null 2>&1; then
    export SSHPASS="$DEPLOY_PASSWORD"
    auth=(sshpass -e)
  else
    askpass=$(mktemp)
    trap 'rm -f "$askpass"' EXIT
    printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$DEPLOY_PASSWORD"\n' > "$askpass"
    chmod 700 "$askpass"
    export SSH_ASKPASS="$askpass" SSH_ASKPASS_REQUIRE=force DISPLAY="${DISPLAY:-:0}"
  fi
fi
ssh_options=(-o StrictHostKeyChecking=accept-new)

# Check the remote configuration before building or uploading.
${auth[@]+"${auth[@]}"} ssh "${ssh_options[@]}" "$user@$host" "test -s '$remote_dir/.env'" || {
  echo "Remote connection failed or $remote_dir/.env is missing or empty." >&2; exit 1;
}
GOOS=linux GOARCH=amd64 bash ./build.sh ${build_args[@]+"${build_args[@]}"}
for name in "${names[@]}"; do
  ${auth[@]+"${auth[@]}"} scp "${ssh_options[@]}" "bin/$name" "$user@$host:$remote_dir/$name.new"
done
${auth[@]+"${auth[@]}"} scp "${ssh_options[@]}" backend.service "$user@$host:$remote_dir/backend.service.new"

${auth[@]+"${auth[@]}"} ssh "${ssh_options[@]}" "$user@$host" "bash -s -- '$remote_dir' '$service' ${names[*]}" <<'REMOTE'
set -euo pipefail
remote_dir=$1; service=$2; shift 2
cd "$remote_dir"
for name in "$@"; do
  chmod 755 "$name.new"
  [[ ! -f "$name" ]] || mv -f "$name" "$name.prev"
  mv -f "$name.new" "$name"
done
sed "s|/opt/backend|$remote_dir|g" backend.service.new > "/etc/systemd/system/$service.service"
rm backend.service.new
id -u sidequestz >/dev/null 2>&1 || useradd --system --home "$remote_dir" --shell /usr/sbin/nologin sidequestz
chown sidequestz:sidequestz .env
chmod 600 .env
systemctl daemon-reload
systemctl enable "$service"
systemctl restart "$service"
for attempt in {1..15}; do
  if systemctl is-active --quiet "$service" && curl -fsS --max-time 2 http://127.0.0.1:8080/healthz; then
    exit 0
  fi
  sleep 2
done
systemctl status "$service" --no-pager
exit 1
REMOTE

echo "Deployed. Rollback: ssh $user@$host 'cd $remote_dir && mv -f sidequestz-server.prev sidequestz-server && systemctl restart $service'"
