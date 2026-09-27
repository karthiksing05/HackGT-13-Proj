# iOS app changes, documentation set, deployment runbook

Owners: **ios-1** (§1A), **ios-2** (§1B), **ios-3** (§1C–1E), **docs** (§2), the coordinator (§3).
`$F` = `frontend/`.

## 0. Facts

- App defaults today: `Info.plist` `SQAPIMode=mock`, `SQAPIBaseURL=http://127.0.0.1:8000`,
  `SQWebSocketURL=ws://127.0.0.1:8000/ws`, `SQMockDelay=5`; `AppEnvironment.makeDefault()` hard-codes the same
  fallbacks and passes `fixedDelay` into `MockAPIClient.simulate()` and `scheduleCheckoutFinish()`.
- Three loading containers: `LoadableView` (`DesignSystem/Components/StateViews.swift`), `AuthLoadable`
  (`Features/Auth/AuthComponents.swift`: Account, Friends, Setup), `HomeLoadable`
  (`Features/Home/HomeMotion.swift`: Itineraries, Past, Search). Forum, Groups, Chat, Album, Splits, Swap,
  Checkout, Facebook and Setup switch on `Loadable` by hand. Pull-to-refresh keeps content and uses
  `.sqRefreshing()` (dots pill), never re-entering `.loading`.
- The backend's `AuthResponse`, planner shapes and websocket auth are being rebuilt to the contract
  (`docs/design/backend-contract.md`, `planner.md`); the app only adds optional decoding.
- Demo city data: `dataingestion/demo/saltlight_harbor.json` (city `saltlight`); Sandy Byte's home base is
  Seaside Market Square (31.3680, -81.4250).
- Mac: Xcode 26.6 at `/Applications/Xcode-26.6.app` (`DEVELOPER_DIR`), Go toolchain in the session
  scratchpad, Docker Desktop with `sq-mongo` (mongo:7, seeded), python3 3.13, simulator `iPhone 17e`
  (`SQ-Main`), device build already proven (`-destination id=00008140-001A68AE1A82801C
  -allowProvisioningUpdates`, team `RBKLNCY6HZ`, bundle `com.karthiksing05.SideQuestz`).
- Root `.env` (values never printed): `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_PASSWORD`, plus `HF_TOKEN`, `TYPESAFE_API_KEY`, `GOOGLE_APPLICATION_CREDENTIALS`, `DEMO_PASSWORD`.

## 1. iOS change list

### 1A. Live by default, no 5 s delay, demo sign-in link (ios-1)

- **`Info.plist`**: `SQAPIMode` → `live`; `SQAPIBaseURL` → `https://api.sidequestz.tech`; `SQWebSocketURL` → `wss://api.sidequestz.tech/ws`; delete `SQMockDelay`; add `SQDemoPassword` = `$(SQ_DEMO_PASSWORD)`.
- **`project.pbxproj`** (app target, Debug + Release): `SQ_DEMO_PASSWORD = "";` (supplied per build: `xcodebuild … SQ_DEMO_PASSWORD='…'`, or `-SQDemoPassword …` at launch).
- **`App/AppEnvironment.swift`**: mode fallback `"live"`; delete `delayText`/`fixedDelay`; base-URL/socket fallbacks match; `let demoPassword: String?` parsed by a pure `static func demoPassword(launch:info:) -> String?` (trim; nil when empty; launch arg first); `static let demoEmail = "demo@sidequestz.tech"`; `preview()` gives the preview user a `homeBase`.
- **`Services/Mock/MockAPIClient.swift`**: remove `fixedDelay` (property, init param, `simulate`, `scheduleCheckoutFinish` → `1.2 * latencyScale`).
- **`App/Router.swift`** `applyLaunch`: in live mode when not signed in, land on `.auth`, never `.main`.
- **`Features/Auth/LoginView.swift`**: "Use the demo account" link (`accessibilityIdentifier("login.demo")`) between "Create an account" and the terms line, shown when `env.demoPassword != nil && !env.isMock`; fills the fields and calls `signIn()`.
- **`$F/README.md`** and **`API_CONTRACT.md`** ("Pointing the app at your server"): new defaults; remove `SQMockDelay` and the "5 seconds on purpose" sentence; add `-SQAPIMode mock`, `-SQDemoPassword`, `-SQSlowLoadingAfter`; note that `-SQRoute` deep links need mock mode.
- Tests: UI tests already pass `-SQAPIMode mock -SQMockLatency 0`; add unit test `demoPasswordParsing`.

### 1B. Loading policy (ios-2)

New **`DesignSystem/Components/SlowLoading.swift`**:

```swift
enum SlowLoading {
    static let defaultThreshold: Duration = .seconds(2)
    static let threshold: Duration = parse(UserDefaults.standard.string(forKey: "SQSlowLoadingAfter"))   // 0 = at once, large = never
    static func parse(_ text: String?) -> Duration
    static func showsLogo(isLoading: Bool, hasContent: Bool, elapsed: Duration, threshold: Duration = defaultThreshold) -> Bool {
        isLoading && !hasContent && elapsed >= threshold
    }
}
struct SlowLoadingModifier: ViewModifier {   // content keeps its height; after `after` the skeleton cross-fades to LoadingStateView(lines:logoSize:)
    let isLoading: Bool; var after: Duration = SlowLoading.threshold; var lines: [String] = []; var logoSize: CGFloat = 40
}
extension View { func sqSlowLoading(_ isLoading: Bool = true, after: Duration = SlowLoading.threshold, lines: [String] = [], logoSize: CGFloat = 40) -> some View }
```
Wire the three containers (each gains `slowAfter`, `slowLines`, `slowLogoSize` and wraps its skeleton
branch): `LoadableView`, `AuthLoadable` (pass `isLoading: shimmers` so an off-screen tab doesn't start the
timer), `HomeLoadable`.

| Screen | Site | Change |
|---|---|---|
| Home first load | `ItinerariesView` `HomeLoadable` | automatic; lines "Loading your sidequests…", logo 44 |
| Home Calendar | `CalendarDaysView` skeleton | `.sqSlowLoading(lines: ["Loading your calendar…"])` |
| Past + insights | `PastView` `HomeLoadable`; `HomeInsights` skeleton | automatic; insights "Looking at what you rated…", logo 32 |
| Forum feed | `ForumView.feed` `.loading` | `.sqSlowLoading(lines: ["Finding posts near \(query.area.name)…"])` |
| Groups | `GroupsView.content` `.loading` | `.sqSlowLoading(lines: ["Loading your groups…"])` |
| Chat / Album / Splits / Thread header | their skeletons | `.sqSlowLoading()` |
| Account cards | `AuthLoadable` sites | automatic; logo 32 |
| Search results | `HomeSearch` `HomeLoadable` | automatic; "Searching…"; a new query while results show keeps `sqRefreshing` |
| Swap alternatives | `SwapSheet.content` `.loading` | `.sqSlowLoading(lines: ["Finding similar spots…"])` |
| Facebook import | `FacebookModel.isImporting`; sheet shows the S with lines while importing; Setup step 2 caption "Still reading your Pages…" after 2 s | |
| Checkout preparing | `CheckoutSheet` receipt skeleton → `LogoLoadingView(size: 32)` + "The agent is preparing your order…" | |
| Event sheet | detail/transit skeletons | `.sqSlowLoading()` |
| Create Review | `CreatePlanLoader` | unchanged (immediate S with lines) |

Invariant: reloads and merges keep the loaded value, so `.loading` is never re-entered while content exists
(the only way back to a skeleton is a retry after a failed first load). `DesignGallery` gets a "Slow loading"
tile; README table gets `-SQSlowLoadingAfter`. Unit tests (`SideQuestzTests/LoadingPolicyTests.swift`):
`showsLogo` truth table; `parse`.

### 1C. Planner extras (ios-3)

- **`Models/Planning.swift`**: `enum PlanStopKind: String, Codable { case event, place }`; `PlanStop` adds `arriveTime: Date?`, `departTime: Date?`, `kind: PlanStopKind?`, `flexible: Bool?`, `activityId: String?` (defaults keep the memberwise init used by `MockData`); `PlanOption` adds `lateFlag: Bool = false`, `totalCostCents: Int?`; `PlanBatch` adds `reason: PlanEmptyReason?` (`noCandidatesFitWindow`, `noFeasibleItinerary`, `invalidRequest(String)`, `other(String)`; `message`: "Nothing nearby fits this window yet. Try a longer window, a wider range, or another day." / "We couldn't fit stops into this window. Try a wider range or a later back-by time." / "Check your start, end and times, then try again." / the existing copy); `RouteResult` adds `brokenAt: Int = -1`; `TravelMode.marta.label` → "Transit"; lenient `init(from:)`: `transit|bus|train|subway` → `.marta`, unknown → `.walk`.
- **`Models/Decoding.swift`**: lenient inits with aliases for the backend's older spellings (`title ?? name`, `durationMinutes ?? durationMin`, `place ?? Place(name:title, coordinate: lat/lng)`, `name ?? title`, `meta ?? summary`, `tag ?? ""`, `legs ?? recalculatedLegs`, stop windows `{start,end}` or `{arrive_time,depart_time}`, `minutesLate ?? 0`); encode adds `broken_at`.
- UI: `ReviewStep` empty state uses `model.emptyReason?.message`; `CreateOptionCard` "Tight timing" chip (`Theme.dangerBg`/`dangerText`, 12pt) when `lateFlag`, hides an empty tag chip; `routeHeader` `.updatedLate` status ("Some stops would be late", danger) set by `refreshRoute` when `brokenAt >= 0`, with a VoiceOver announcement; `RouteCard` marks the stop at `brokenAt` late ("Late for a fixed start · "); `MockRouteEngine` keeps `brokenAt: -1`; `MockData` option E gets `lateFlag: true` for the demo route.

### 1D. Home base (ios-3)

- **`Models/People.swift`** `User`: `homeBase: Place?`, `city: String?`; `Decoding.swift` CodingKeys add `homeBase, city`.
- **`CreateModel.swift`**: `loadDefaultPlacesIfNeeded`: `if start == nil, let home = env.user?.homeBase { start = home }` synchronously, then still await the location for the "Current location" pill; pills = Current location → Home base → results (deduped, `prefix(4)`); `near` fallback → home base.
- **Forum**: `ForumQuery.defaultArea(homeBase:)`, `ForumArea.suggestions(homeBase:isMock:)`; `ForumView.loadPosts()` resolves the area once from the home base; `AreaSheet` uses the new suggestions.
- **Account**: "Home base" section (pin glyph, name, city) between Taste profile and Connected when set; the handle line shows `school ?? city`.
- Mock: `MockData.user()` keeps no home base; `AppEnvironment.preview()` sets one. Tests: `homeBaseDecodesAndDefaultsToNil`, `createStartsAtHomeBaseWhenSet`, `forumDefaultArea`.

### 1E. Contract examples as the shared artifact (ios-3)

- **`SideQuestzTests/ContractTests.swift`**: new dumps `UserHomeBase`, `PlanBatch.dag` (options with `late_flag`, `total_cost_cents`, stops with all extras, a `transit` leg), `PlanBatch.empty` (`reason: "no_candidates_fit_window"`), `RouteResult.dag` (`broken_at: 1`, `minutes_late: 25`); tests `plannerExtrasDecodeLeniently`, `userHomeBaseDecodes`.
- **`scripts/gen_contract_examples.py`**: `--examples-dir` (default `<repo>/docs/api/examples`), `--no-copy`; GROUPS additions; writes `<name>.json` (normalized) + `index.json`; deletes unlisted files; exits non-zero on missing names.
- Regeneration: `TEST_RUNNER_SQ_DUMP_CONTRACT=$DUMP xcodebuild test … -only-testing:SideQuestzTests/ContractTests` then `python3 scripts/gen_contract_examples.py $DUMP API_CONTRACT.md`.
- `docs/api/README.md`: what the contract is, the examples dir, regeneration, how Go tests consume them, the change protocol.

## 2. Docs set (docs agent)

- **`README.md`** (root): SideQuests in one paragraph ("Turn waiting into wandering.", HackGT 13); architecture diagram (mermaid: iPhone → Cloudflare → nginx → Go API → Mongo + ML service; embedding providers; data ingestion offline; Raven training → HF checkpoints); repo layout table (`frontend/`, `Backend/`, `ml/`, `dataingestion/`, `docs/`); quick start per part; tests (the three `xcodebuild test` lines, `go test ./...`, `python -m unittest discover`, `pytest`); deploy; demo account (Sandy Byte, `demo@sidequestz.tech`, password via `SQ_DEMO_PASSWORD`, the "Use the demo account" link); env/secrets table (names only); links; credits (JetBrains Mono OFL, "© OpenStreetMap contributors").
- **`docs/ARCHITECTURE.md`**: components and interfaces table (caller → callee, protocol, auth, defined where); token lifecycle; the per-user catalog and home base; sequence diagrams (sign-up → preferences → embeddings; plan generation with the iterative loop; swap; join); contract governance.
- **`docs/PLANNER.md`** (absorbs `Backend/ITINERARY_PLANNER.md`): guarantees, request mapping, retrieval, the loop, outputs (extras table), follow-ups, knobs, inspecting `plan_runs` (three `mongosh` queries), limitations.
- **`docs/EMBEDDINGS.md`** (absorbs `Backend/EMBEDDINGS.md`): providers and the parity gate, when we embed, text formats, storage fields, env, retraining pointer.
- **`docs/DEPLOY.md`**: VPS layout, services and env, deploy scripts, the runbook (§3), rollback, logs, health checks, secrets handling, Cloudflare notes (proxied WS ok; 100 s request cap), the local dev loop, troubleshooting.
- **`docs/DEMO.md`**: Sandy Byte, what is seeded, the 3-minute judge script (Use the demo account → Home → tap a block → + → Where shows the home base → When → Vibe → Review: S loader, options, drag, hold → Swap → Start → Forum → Groups › Splits → Account), known limits, reset.
- **`docs/ROADMAP.md`**: simulated pieces and what real integrations need; crawler deployment; routing provider; retraining on logged runs; Facebook App Review; host approval for joins; Vertex/HF token steps.
- **`docs/DATA.md`**: activities schema summary, categories, indexes and the demo TTL exception, sources, commands, demo snapshot, caches/quotas, attribution.
- Pointer files: `Backend/API_ENDPOINTS.md`, `Backend/ITINERARY_PLANNER.md`, `Backend/EMBEDDINGS.md`, `Backend/INFRASTRUCTURE.md` → one paragraph + link to `docs/`. Section updates in `frontend/README.md`, `ml/README.md`, `dataingestion/README.md`.

## 3. Deployment runbook (coordinator)

### 3.1 Mac prerequisites
Go toolchain in the scratchpad (`$SCR/go/bin`, `GOPATH=$SCR/gopath`, `GOCACHE=$SCR/gocache`); Docker `sq-mongo`; root `.env` present; `DEVELOPER_DIR=/Applications/Xcode-26.6.app/Contents/Developer`.

### 3.2 Server prep (once; values from `.env`, never echoed)
```
mkdir -p /opt/backend /opt/ml/.cache
[ -f /opt/backend/.env ] || { umask 077; cat > /opt/backend/.env <<EOF
APP_ENV=prod
HTTP_ADDR=127.0.0.1:8080
PUBLIC_BASE_URL=https://api.sidequestz.tech
MONGO_URI=mongodb://127.0.0.1:27017
MONGO_DB=freetime
ML_SERVICE_URL=http://127.0.0.1:8000
JWT_SECRET=$(openssl rand -base64 48)
PLANNER=dag
TRUST_PROXY=1
FB_APP_ID=<from meta_app_id>
FB_APP_SECRET=<from meta_app_secret>
EOF
}
chmod 600 /opt/backend/.env
# /opt/ml/.env (600): HF_TOKEN, TYPESAFE_API_KEY, GOOGLE_APPLICATION_CREDENTIALS=/opt/ml/gcp-sa.json ; copy gcp-sa.json (600)
```
Verify: both `.env` files are `-rw-------`; `ufw status` still blocks 8080/8000/27017.

### 3.3 Ordered steps
1. `cd ml && ./deploy.sh` → `systemctl is-active ml`; `curl -s 127.0.0.1:8000/healthz` shows the provider; `journalctl -u ml -n 30`.
2. `cd Backend && bash build.sh` (tests run separately) → `file bin/sidequestz-server` (static x86-64) → `./deploy.sh` → `systemctl is-active sidequestz`; log shows Mongo connected and `127.0.0.1:8080`; `ss -ltnp | grep 8080` loopback only; `curl -s 127.0.0.1:8080/healthz`.
3. On the server: verify the existing demo account: `mongosh freetime --eval 'db.users.findOne({email:"demo@sidequestz.tech"},{name:1,homeBase:1,city:1,setupComplete:1,embeddingModel:1})'`.
4. From the Mac: `scripts/smoke.sh` with `BASE_URL=https://api.sidequestz.tech` (throwaway signup → preferences → plan in Atlanta → route → save → list; Sandy sign-in → plan in Saltlight → forum → websocket 101). Time the plan call (Cloudflare caps requests at 100 s; target p95 < 30 s).
5. Simulator against live (`SQ_DEMO_PASSWORD` build setting) → demo link signs in, Home loads.
6. iPhone: `xcodebuild -project frontend/SideQuestz.xcodeproj -scheme SideQuestz -configuration Debug -destination id=00008140-001A68AE1A82801C -derivedDataPath $SCR/DD-device -allowProvisioningUpdates SQ_DEMO_PASSWORD="…" build`; `xcrun devicectl device install app --device 00008140-001A68AE1A82801C …/SideQuestz.app`; launch.
7. Redeploy earlier code: check out that revision and run the root `bash deploy.sh`. Deploy scripts do not create backups. App: reinstall the previous `.app` or relaunch with `-SQAPIMode mock`.

### 3.4 Local dev loop
```
docker start sq-mongo                       # freetime already holds the Atlanta sample + demo_activities
cd ml && .venv/bin/uvicorn api.main:app --port 8000
cd Backend && APP_ENV=dev HTTP_ADDR=127.0.0.1:8080 MONGO_URI=mongodb://127.0.0.1:27017 MONGO_DB=freetime ML_SERVICE_URL=http://127.0.0.1:8000 \
  JWT_SECRET=dev-secret-dev-secret-dev-secret-dev PLANNER=dag PUBLIC_BASE_URL=http://127.0.0.1:8080 go run .
# Simulator: -SQAPIBaseURL http://127.0.0.1:8080 -SQWebSocketURL ws://127.0.0.1:8080/ws -SQDemoPassword demo
```

## 4. Verification checklist
- App: unit + UI suites green on the mock; zero warnings; `grep -rn SQMockDelay frontend` empty; live values in `Info.plist`; `docs/api/examples` regenerated and reviewed.
- Live app on the phone: demo link signs in; Home skeleton → S only if > 2 s; pull-to-refresh never shows the S; Create starts at the home base; Review shows the S then options; a reorder that breaks a fixed start shows the late state; swap works; empty results show the reason copy; Forum area defaults to Saltlight Harbor; Account shows Home base; airplane mode shows error states; a revoked token lands on Login.
- Backend/ML/docs: as in the plan's §10.

## 5. Task split
ios-1 (A) · ios-2 (B) · ios-3 (C+D+E) · docs · coordinator (§3). Merge order ios-1 → ios-3 → ios-2 (ios-2 rebases on ios-3's `ForumView`/`AccountMeSection`); regenerate the contract examples once after ios-3; Go's contract tests then run against them.
