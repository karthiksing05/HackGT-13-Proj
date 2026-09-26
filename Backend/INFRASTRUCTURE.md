# Infrastructure

Moved to [`docs/DEPLOY.md`](../docs/DEPLOY.md): the VPS layout, the three services with their units and
environment files, `build.sh`, `deploy.sh` and `ml/deploy.sh`, the ordered runbook, rollback, logs and
health checks, secrets handling, Cloudflare notes, the local development loop and troubleshooting. The
shape described here before still holds: nginx (`sidequestz.tech`) terminates TLS for
`api.sidequestz.tech` and proxies `/` and `/ws` to the Go API on `127.0.0.1:8080`; the ML service
(`127.0.0.1:8000`) and MongoDB (`127.0.0.1:27017`) are loopback-only and reachable from a Mac only
through an SSH tunnel (`ssh -N -L 27017:127.0.0.1:27017 <user>@<host>`). What changed: the API reads its
configuration from `/opt/backend/.env` (mode 0600) instead of `Environment=` lines in the unit, the
hostname is proxied by Cloudflare, and the deploy scripts never upload secrets.
