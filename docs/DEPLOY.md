# Deployment

The API and the ML service run on one VPS (Vultr, Debian 12) behind Cloudflare; MongoDB runs on the
same machine. This page is the operator's view: layout, services, scripts, the runbook, rollback,
logs, secrets, Cloudflare specifics, the local loop and troubleshooting. Design:
[design/app-docs-deploy.md](design/app-docs-deploy.md) §3.

## Topology

```mermaid
flowchart LR
    app["iPhone app"] -- "HTTPS / WSS" --> cf["Cloudflare (proxied)<br/>api.sidequestz.tech"]
    cf --> nginx["nginx :443<br/>Let's Encrypt"]
    subgraph vps["the VPS"]
        nginx -- "/ and /ws → 127.0.0.1:8080" --> go["backend.service<br/>sidequestz-server"]
        go --> mongo[("mongod<br/>127.0.0.1:27017")]
        go --> ml["ml.service<br/>uvicorn, 2 workers, 127.0.0.1:8000"]
        timer["ml-embed-missing.timer"] --> ml
        timer --> mongo
    end
```

Only 80 and 443 are reachable from outside; `ufw` blocks 8080, 8000 and 27017, and all three services
bind to loopback. There is no public route to the ML service: `/v1/…` from the internet is a 404 at
nginx. The nginx site is `Backend/sidequestz.tech` (`client_max_body_size 20M` for photo uploads, HTTP/1.1
upgrade on `/ws`, long proxy timeouts on the socket).

## Layout on the server

| Path | Contents |
|---|---|
| `/opt/backend/sidequestz-server` | the Go binary (static linux/amd64); `sidequestz-server.prev` is the previous one |
| `/opt/backend/sidequestz-admin` | the admin tool: `ensure-indexes`, `seed-demo`, `drop-ttl <collection>`, `reset-app-data --yes [--users]` |
| `/opt/backend/.env` | the API's environment, mode 0600 (see below) |
| `/opt/ml/` | the `ml/` tree (without `.env`, `.cache`, `.venv`, `datagen/`, `data/`, sbatch files, `wandb/`); `/opt/ml/.venv`; `/opt/ml/.cache` (model weights, the embedding cache); `/opt/ml.prev` is the previous tree |
| `/opt/ml/.env`, `/opt/ml/gcp-sa.json` | the ML service's secrets, mode 0600 |
| `/etc/systemd/system/backend.service` (alias `sidequestz.service`), `ml.service`, `ml-embed-missing.service`, `ml-embed-missing.timer` | units, installed by the deploy scripts |
| `/etc/nginx/sites-available/sidequestz.tech` | the site, managed by Certbot for TLS |

## Services and environment

| Unit | Runs | Notes |
|---|---|---|
| `backend.service` | `/opt/backend/sidequestz-server` as user `sidequestz`, `EnvironmentFile=/opt/backend/.env`, `Restart=always`, `RestartSec=3`, `LimitNOFILE=65536`, `NoNewPrivileges`, `ProtectSystem=full`, `ProtectHome`, `PrivateTmp` | no inline `Environment=` lines; startup pings Mongo (10 s) and exits on failure; `EnsureIndexes` runs at startup and is fatal when an index exists with different options |
| `ml.service` | `/opt/ml/.venv/bin/uvicorn api.main:app --host 127.0.0.1 --port 8000 --workers 2` with `HF_HOME=/opt/ml/.cache EMBED_PROVIDER=auto EMBED_CACHE_PATH=/opt/ml/.cache/embeddings.sqlite EMBED_LOCAL_THREADS=8 OMP_NUM_THREADS=8 HF_HUB_DISABLE_TELEMETRY=1 RERANK_TOP_K=12`, `EnvironmentFile=-/opt/ml/.env`, `TimeoutStartSec=300`, `ReadWritePaths=/opt/ml/.cache` | the first local model load takes 20–60 s; the warmup thread never blocks startup |
| `ml-embed-missing.timer` → `.service` | `python -m tools.embed_missing` 2 min after boot and every 15 min | fills vectors for activities that have text but no current vector |

`/opt/backend/.env` (created once, never uploaded by the scripts):

```
APP_ENV=prod
HTTP_ADDR=127.0.0.1:8080
PUBLIC_BASE_URL=https://api.sidequestz.tech
MONGO_URI=mongodb://127.0.0.1:27017
MONGO_DB=freetime
ML_SERVICE_URL=http://127.0.0.1:8000
JWT_SECRET=<openssl rand -base64 48>
PLANNER=dag
TRUST_PROXY=1
FB_APP_ID=<Meta app id>
FB_APP_SECRET=<Meta app secret>
DEMO_PASSWORD=<the demo account's password>
```

Optional: `ML_*` timeouts, `PLANNER_*` knobs ([PLANNER.md](PLANNER.md)), `FB_GRAPH_VERSION` (`v26.0`),
`FB_TOKEN_KEY` (32-byte hex; default derived from `JWT_SECRET`), `DEMO_TZ` (`America/New_York`),
`DEV_RESET_CODES` (`0`), `CHECKOUT_STEP_DELAY` (`1500ms`), `ACCESS_TOKEN_TTL` (`1h`), `REFRESH_TOKEN_TTL`
(`720h`), `MAX_PHOTO_BYTES` (2 MB), `MAX_JSON_BYTES` (1 MB). `PORT` is accepted in place of `HTTP_ADDR`.

`/opt/ml/.env`: `HF_TOKEN`, `TYPESAFE_API_KEY`, `GOOGLE_APPLICATION_CREDENTIALS=/opt/ml/gcp-sa.json`, and
any `EMBED_*` / `VERTEX_*` override ([EMBEDDINGS.md](EMBEDDINGS.md)).

## Deploy scripts

Both scripts read the root `.env` on the Mac for `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_PASSWORD`,
`DEPLOY_REMOTE_DIR` and `DEPLOY_SERVICE`, authenticate non-interactively (`sshpass` or `SSH_ASKPASS`),
and never print the values.

| Script | What it does |
|---|---|
| `Backend/build.sh [--amd64\|--arm64\|--native] [--skip-tests] [--clean]` | `go mod download`, `go test ./...` (unless skipped), then a static, stripped build (`CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`) to `Backend/bin/sidequestz-server` |
| `Backend/deploy.sh` | runs `build.sh --amd64`; pre-flight `test -s /opt/backend/.env` on the server; uploads the binary as `.new` and renames it (no `ETXTBSY`), keeping the previous binary as `sidequestz-server.prev`; syncs `backend.service`; `systemctl daemon-reload && restart`; checks it is active. `--seed` runs `sidequestz-admin ensure-indexes` after the restart. It never uploads `.env` |
| `ml/deploy.sh [--skip-tests]` | runs the unit tests locally; `cp -a /opt/ml /opt/ml.prev`; rsyncs `ml/` excluding `.env`, `.cache`, `.venv`, `datagen/`, `data/`, `*.sbatch`, `wandb/`; directories 755, files 644, `.env` and `gcp-sa.json` 600; creates the venv and installs `requirements-serve.txt` (CPU torch wheels); prefetches the Qwen weights into `/opt/ml/.cache`; installs `ml.service` and the timer units; restarts; loops on `/healthz` and then `/healthz?probe=1` and fails loudly if either does not come up |

## Runbook

**0. On the Mac.** Go toolchain, Docker with `sq-mongo` running (for the tests), the root `.env`,
`DEVELOPER_DIR=/Applications/Xcode-26.6.app/Contents/Developer`.

**1. Server prep (once).** Create `/opt/backend`, `/opt/ml/.cache`, write `/opt/backend/.env` and
`/opt/ml/.env` with `umask 077` (values from the root `.env`, never echoed), copy `gcp-sa.json` to
`/opt/ml/` with mode 600, and confirm `ufw status` still blocks 8080, 8000 and 27017. Both `.env` files
must show `-rw-------`.

**2. ML service.**

```sh
cd ml && ./deploy.sh
ssh <host> 'systemctl is-active ml; curl -s 127.0.0.1:8000/healthz | jq .embedding.provider; journalctl -u ml -n 30 --no-pager'
```

**3. Backend.**

```sh
cd Backend && ./build.sh --amd64 && file bin/sidequestz-server     # ELF 64-bit, x86-64, statically linked
./deploy.sh
ssh <host> 'systemctl is-active backend; ss -ltnp | grep 8080; curl -s 127.0.0.1:8080/healthz; journalctl -u backend -n 30 --no-pager'
```

The log must show Mongo connected and the listener on `127.0.0.1:8080` only.

**4. Data and the demo account** (on the server; the admin tool reads `/opt/backend/.env`):

```sh
set -a; . /opt/backend/.env; set +a
/opt/backend/sidequestz-admin reset-app-data --yes     # drops the app collections, keeps the catalogs and users
/opt/backend/sidequestz-admin ensure-indexes
/opt/backend/sidequestz-admin drop-ttl demo_activities  # the demo events must not expire
/opt/backend/sidequestz-admin seed-demo                 # idempotent; needs DEMO_PASSWORD
mongosh freetime --eval 'db.users.findOne({email:"demo@sidequestz.tech"},{name:1,homeBase:1,city:1,setupComplete:1,embeddingModel:1})'
```

`reset-app-data --users` also drops users; without it existing accounts survive a redeploy.

**5. Smoke test from the Mac.** `Backend/scripts/smoke.sh` with `BASE_URL=https://api.sidequestz.tech`:
a throwaway sign-up → preferences → a plan in Atlanta → route → save → list → calendar days → a free-now
post → `/forum/posts/mine` → `/healthz`, then Sandy's sign-in → a plan in Saltlight → forum → a
websocket `101`. Time the plan call: Cloudflare cuts requests at 100 s, the target is p95 under 30 s.
Measured on the VPS: to be recorded after the first deploy. <!-- verify-after-deploy -->

**6. The app against the live server.** Simulator first: build with the `SQ_DEMO_PASSWORD` setting,
tap "Use the demo account", Home loads. Then the phone:

```sh
xcodebuild -project frontend/SideQuestz.xcodeproj -scheme SideQuestz -configuration Debug \
  -destination id=<device id from `xcrun xctrace list devices`> -derivedDataPath /tmp/DD-device \
  -allowProvisioningUpdates SQ_DEMO_PASSWORD='…' build
xcrun devicectl device install app --device <device id> /tmp/DD-device/Build/Products/Debug-iphoneos/SideQuestz.app
```

## Rollback

| Part | Command |
|---|---|
| Backend | `mv -f /opt/backend/sidequestz-server.prev /opt/backend/sidequestz-server && systemctl restart backend` |
| ML | `rm -rf /opt/ml && mv /opt/ml.prev /opt/ml && systemctl restart ml` |
| Data | the seed is idempotent; rerun step 4 |
| App | reinstall the previous `.app`, or relaunch with `-SQAPIMode mock` to demo offline |

## Logs and health checks

```sh
journalctl -u backend -f -n 50          # request log with ids, planner timings, ML fallbacks
journalctl -u ml -f -n 50               # one INFO line per embed call, WARNING on provider fallback
curl -s 127.0.0.1:8080/healthz          # {ok:true} or 503 when Mongo does not answer the ping
curl -s 127.0.0.1:8000/healthz | jq     # status, provider, jev, model versions
curl -s 'https://api.sidequestz.tech/healthz'
systemctl list-timers ml-embed-missing.timer
mongosh freetime --eval 'db.plan_runs.find({}, {createdAt:1, "final.totalMs":1, "ml.mode":1}).sort({createdAt:-1}).limit(5)'
```

To inspect the database from a Mac, tunnel it (Mongo is loopback-only) and connect Compass or `mongosh`
to `mongodb://127.0.0.1:27017`:

```sh
ssh -N -L 27017:127.0.0.1:27017 <user>@<host>
```

## Secrets

- The only copies are the root `.env` on a developer's Mac (gitignored, never in a worktree),
  `/opt/backend/.env`, `/opt/ml/.env` and `/opt/ml/gcp-sa.json` on the server, all mode 0600.
- `JWT_SECRET` is generated once with `openssl rand -base64 48`; rotating it signs everyone out.
  Rotate `FB_TOKEN_KEY` only together with a Facebook reconnect for every user (tokens are encrypted
  with it).
- Units carry no secrets; the scripts never upload `.env` files; nothing prints a value. `journalctl`
  shows reset codes only when `DEV_RESET_CODES=1`, which the VPS does not set.
- The Meta app secret, the HF token and the TypeSafe key are revoked and reissued from their dashboards;
  after editing the file, `systemctl restart backend` or `ml`.

## Cloudflare notes

- The hostname is proxied (orange cloud). Proxied WebSockets work; the hub pings every 25 s and reads
  with a 60 s deadline, well under Cloudflare's idle timeout. `TRUST_PROXY=1` makes the API rate-limit
  on the forwarded client address instead of Cloudflare's.
- Requests are cut off after 100 s. Plan generation has a 9 s hard timeout and a 5 s soft budget, so
  a `524` from Cloudflare means the process, not the planner, is stuck.
- TLS is terminated twice: Cloudflare to nginx uses the Let's Encrypt certificate (set the SSL mode to
  Full (strict)); the API sees `X-Forwarded-Proto: https`.
- `GET /photos/{id}` answers with `Cache-Control: public, max-age=31536000, immutable`; Cloudflare may
  cache it, which is fine because photo ids are never reused.

## Local development loop

```sh
docker start sq-mongo                    # freetime holds the Atlanta sample and demo_activities (see docs/DATA.md)
cd ml && .venv/bin/uvicorn api.main:app --port 8000
cd Backend && APP_ENV=dev HTTP_ADDR=127.0.0.1:8080 MONGO_URI=mongodb://127.0.0.1:27017 MONGO_DB=freetime \
  ML_SERVICE_URL=http://127.0.0.1:8000 JWT_SECRET=dev-secret-dev-secret-dev-secret-dev PLANNER=dag \
  PUBLIC_BASE_URL=http://127.0.0.1:8080 DEMO_PASSWORD=demo \
  sh -c 'go run ./cmd/sidequestz-admin seed-demo && go run .'
# Simulator launch arguments: -SQAPIBaseURL http://127.0.0.1:8080 -SQWebSocketURL ws://127.0.0.1:8080/ws -SQDemoPassword demo
```

`APP_ENV=dev` relaxes the JWT-secret check, enables the repo-root `meta_app_id` / `meta_app_secret`
fallback for Facebook and allows `DEV_RESET_CODES=1`. The Go tests use `MONGO_TEST_URI` (default the
same local server) and create and drop `sq_test_*` databases.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `backend` exits at startup with a Mongo error | `mongod` is down or `MONGO_URI` is wrong; the API never runs without its database |
| startup log: index "exists with different options" | an old index with the same name; drop it (`db.<coll>.dropIndex("<name>")`) and restart |
| startup refuses `JWT_SECRET` | it is shorter than 32 bytes or still the placeholder; regenerate it |
| `GET /healthz` is 503 | the Mongo ping failed; check `systemctl status mongod` |
| `ml` takes minutes to become active, or `providers.local` says `loading` | the first model load; wait for `loaded`, keep `TimeoutStartSec=300`; the deploy's health loop waits too |
| `/healthz` says `degraded` and `provider` is `local` | the remote providers are off or failing (permissions, parity); local serving is the expected fallback ([EMBEDDINGS.md](EMBEDDINGS.md)) |
| plans come back with `"ml": {"mode": "fallback:…"}` in `plan_runs` | the classifier call timed out; check `ml` logs and `ML_RANK_TIMEOUT_MS` / `PLANNER_ML_TIMEOUT_MS` |
| Cloudflare `524` on `/plans/generate` | the request exceeded 100 s; look for a stuck Mongo or ML call in the backend log, not at the planner budget |
| `Text file busy` when copying the binary | copy to `.new` and rename, as `deploy.sh` does |
| the app on a phone cannot reach a local server | `127.0.0.1` only works in the Simulator; use the Mac's LAN address in `-SQAPIBaseURL` (plain `http://` is allowed for local networks only) |
| no "Use the demo account" link | the build had no `SQ_DEMO_PASSWORD` and no `-SQDemoPassword` argument, or the app is in mock mode |
| Facebook connect ends in `status=error` | the redirect URI `https://api.sidequestz.tech/integrations/facebook/callback` is not whitelisted, or the person is not a tester of the app in development mode |
| demo events vanished | a TTL index on `demo_activities`; `sidequestz-admin drop-ttl demo_activities`, then reimport the snapshot ([DATA.md](DATA.md)) |
