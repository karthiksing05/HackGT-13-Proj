# `pkg/api`: the seams every area package uses

Spec: `docs/design/backend-contract.md`; wire truth: `docs/api/examples/*.json` (the Go contract test
round-trips every file). Read this before adding a handler.

## Packages and ownership

| Package | Owner | Notes |
|---|---|---|
| `pkg/api/auth`, `pkg/api/me` (GET/PATCH), `pkg/api/planning`, `pkg/api` root, `pkg/api/view`, `pkg/router`, `pkg/realtime`, `pkg/httpx`, `pkg/store/{store,users,tokens,websessions,photos}.go`, `pkg/testutil` | foundation | Do not edit; ask for a change in your report. |
| `pkg/api/me` (rest), `pkg/api/integrations`, `pkg/api/photos` | backend-A | `store/devices.go`, `store/payments.go`, `store/users_prefs.go` … |
| `pkg/api/itineraries` | backend-B | `store/itineraries.go`, `item_state.go`, `ratings.go`, `catalog.go` |
| `pkg/api/social` | backend-C | `store/forum.go`, `threads.go`, `messages.go`, `expenses.go`, `friends.go`, `invites.go` |
| `pkg/api/checkout` | backend-D | `store/checkout.go` |
| `pkg/api/facebook` | backend-E | `store/facebook.go` |
| `pkg/planner`, `store/planstore.go`, `models/planner*.go` | planner | implements `api.Planner` |

Rules: each package owns its files; `pkg/models` gets one file per area (`itinerary.go`, `social.go`,
`checkout.go`, `facebook.go`); the store gets one file per collection. Never edit another agent's file.

## Registering routes

`pkg/router/routes.go` calls every area's `func Register(r *mux.Router, d *api.Deps)` and is never edited.
Handlers are methods on `type H struct{ d *api.Deps }`. Every §4 route already exists as a 501 stub inside
its area package (`api.Stub(r, d, "GET", "/itineraries", true)` → `{"error":"not_implemented","message":"Not
built yet"}`). To build one, delete the stub line and register the handler:

```go
r.Handle("/itineraries", d.Protect(h.List)).Methods("GET")      // bearer required (401 otherwise)
r.Handle("/photos/{id}", http.HandlerFunc(h.Serve)).Methods("GET") // public
r.Handle("/auth/logout", d.Optional(h.Logout)).Methods("POST")  // user attached when the token is valid
```

Order matters only for literal-vs-`{id}` siblings; `facebook.Register` runs before `integrations.Register`
so `/integrations/facebook…` beats `{provider:google|outlook}`.

## Inside a handler

```go
var req contract.ItineraryUpdate
if !httpx.Decode(w, r, &req) { return }          // 1 MB cap, unknown keys ok, 400 "Check the details and try again."
user, err := h.d.CurrentUser(r)                  // *models.User of the token (401 when gone); api.UserID(r) for just the id
if err != nil { api.Fail(w, r, err); return }
tz := httpx.TZ(r)                                // *time.Location from X-Time-Zone (default America/New_York)
now := h.d.BusinessNow(r.Context())              // the account's time (see "Two clocks"); never time.Now() in handlers or stores
…
httpx.JSON(w, http.StatusOK, view)               // httpx.NoContent(w) for 204
httpx.Error(w, http.StatusBadRequest, "Give your sidequest a name.")   // {"error":"bad_request","message":…}
```

`api.Fail(w, r, err)` maps `store.ErrNotFound → 404`, `ErrForbidden → 403`, `ErrConflict → 409`
(`ErrEmailTaken` / `ErrUsernameTaken` get their sentences), `*httpx.StatusError` → its status and
sentence, anything else → 500 logged with the request id. Unowned resources are 404; host-only itinerary
edits by a member are 403. Every 400/409 message is a sentence the app shows verbatim; unknown enum
values are 400 (`contract.Visibility("x").Valid()`). `httpx.DecodeOptional` for bodies that may be empty.

## Two clocks

With `DEMO_DATE` set (YYYY-MM-DD, `pkg/democlock`), accounts of the demo catalog (`demo_activities`: Sandy
and her bots) live on that date in `DEMO_TZ` at the real time of day, on any real day; everyone else, and
everything with `DEMO_DATE` unset, lives in real time. `d.Protect` / `d.Optional` put the signed-in
account's clock on the request context (a cached catalog lookup, only while `DEMO_DATE` is set).

- **Business time** (`h.d.BusinessNow(ctx)`, `s.BusinessNow(ctx)` in stores) for whatever decides or shows
  what is upcoming, past or now: plan windows, active/past, calendar "today", past events, the Forum's
  labels and started/locked checks, free-now `until`, presence, chat times, and the `createdAt` / `sentAt`
  of what people see (plans, messages, posts, expenses, ratings, friendships, photos, checkout intents).
  Realtime payloads render with the acting request's business time.
- **Real time** (`h.d.Clock()`, `s.Now()`) for tokens, rate limits, reset codes, web sessions, invites,
  Facebook, the checkout agent's step timing, account metadata, logs, and every TTL `expiresAt` (MongoDB
  expires documents by the wall clock). A TTL expiry derived from a business deadline is stored at its
  real instant (`s.RealAt(ctx, until)`).
- The owner's `User` (`h.d.UserView(user)`: GET/PATCH /me, sign-up, log-in) carries `demo_date` for a demo
  account, so the app puts its "today" there too. Background work for a user takes their clock with
  `d.ForUser(ctx, userID)`.

## Contract types and time

`pkg/contract` mirrors the Swift models: snake_case keys, typed enums with `Valid()`, optional fields are
pointers with `omitempty`, list fields are never nil (`[]` on the wire). `contract.Time` writes RFC 3339 UTC
and reads RFC 3339 (fractions, offsets) or `"2006-01-02"` (UTC midnight; re-anchor day-only values with
`httpx.LocalMidnight(t, tz)`); required times are values, optional ones `*contract.Time`
(`contract.Ptr`, `contract.PtrOrNil`, `.StdPtr()`). `Preferences.ratings` / `answers` keys are canonical
snake_case (`contract.Ratings`, `contract.Answers`, `CanonicalKey`). `contract.Page[T]` is the
`{items, next_cursor}` shape; cursors are opaque (`httpx.EncodeCursor` / `httpx.Cursor(r)` /
`httpx.NextCursor`). Labels: `httpx.Clock`, `ClockShort`, `TimeRange`, `MonthDay`, `Weekday`, `DayLabel`,
`Dollars`, `Plural`, `Miles`; `contract.TravelMode.Label()`.

## Views, per viewer

Handlers never serialize a Mongo document. `view.PersonRef(u, cfg.PublicBaseURL)` and
`view.User(u, cfg.PublicBaseURL, now)` render people identically everywhere (initials, avatar hex from the
app palette, `photo_url = <PUBLIC_BASE_URL>/photos/<id>`, age bracket). Each area adds its own `view.go`
that converts `models.*` → `contract.*` for one viewer (`is_host`, `unread`, `sender_name: "You"`,
`join_status`); realtime payloads with viewer-dependent shapes are rendered per recipient.

## Store

`d.Store.Users()`, `.Tokens()`, `.Resets()`, `.WebSessions()`, `.Photos()` are namespaces (value types
with a `*Store` inside). Add yours in your own file: `type Itineraries struct{ s *Store }` +
`func (s *Store) Itineraries() Itineraries`; add methods to an existing namespace in your own file
(`users_prefs.go`: `func (u Users) SavePrefs(…)`). Use `s.BusinessNow(ctx)` or `s.Now()` for timestamps ([two
clocks](#two-clocks)), `store.NewID()` for
`_id`s, return the sentinels (wrap with `%w`), never `_ =` a write (a test greps for it). Indexes: add
rows to `appIndexes` only through the foundation agent; `EnsureIndexes` runs at startup and in
`sidequestz-admin ensure-indexes`. Users: `ByID`, `ByIDs` (map), `ByEmail`, `ByUsername`,
`Search(q, excludeID, limit)`, `Update(id, bson.M)`, `Unset`, `SetPassword`, `SetPhoto`, `Touch`.
WebSessions: `Create(userID, purpose, provider, ttl)` / `Insert(sess, ttl)` (extra fields), `Peek`,
`Consume(token, purpose)` (single use). Photos: `Put`, `Get`, `Delete`, `ListForGroup` (no bytes),
`CountForGroup`.

## Realtime

`h.d.Publish()` is the `realtime.Publisher` (`Send`, `SendMany`, `Broadcast`; `Noop` when unset). Use the
typed helpers in `pkg/realtime/events.go`: `MessageNew`, `ThreadUpdated`, `ThreadRead`, `JoinRequest`,
`JoinUpdate`, `FriendStatus`, `FriendRequest`, `ForumUpdate`, `CheckoutStatus`, `ItineraryUpdated`,
`ItineraryRemoved`, `ExpenseAdded`, `PhotoAdded`, `TransitDelay`. Envelope: `{"type": …, "data": {…}}`.

## Planner seam

`api.Planner` (`Generate`, `More`, `Route`, `Alternatives`, `ResolveStop → *api.StopDetail`) is
`planner.Service` (`pkg/planner/service.go`), built in `main.go` by `plannerwire.Planner(ctx, db, deps.ML)`:
catalogs, `plan_pools` (6 h) and `plan_runs` (72 h) through `pkg/planner/mongosource`, scores and search
vectors through the ML client, knobs from `PLANNER_*`. If it cannot start, `main` logs and leaves it nil,
and `pkg/api/planning` answers 503 "Planning is warming up." Only `pkg/contract` fields go out: options
carry `late_flag` and `total_cost_cents` (sum of the known prices), stops `arrive_time`, `depart_time`,
`kind`, `flexible`, `activity_id`. An empty batch is a 200 with `reason` (`no_candidates_fit_window`,
`no_feasible_itinerary`, `invalid_request: …`, and for the must-see picks of `must_include`
`must_include_unavailable: <title>` or `must_include_no_fit`; more than 10 picks is a 400).
`GET /activities/search` (`pkg/api/itineraries`, the must-see search) reads the caller's catalog with
`Store.Catalog().SearchActivities` and leaves what can be a pick, and the order, to `planner.ActivityHits`
(docs/PLANNER.md, Must-see picks). `GET /activities/{id}` (Review's stop pane) reads one document of that
catalog with `Store.Catalog().Activity` (another catalog's id is a 404) and renders it with
`planner.ActivityDetail`: the stops' category label, a price label and the plan day's hours line.
`broken_at` is the first stop that no longer works (a fixed start reached late, or a place outside its
hours) and `minutes_late` counts both. Expired or other users' plans are 404 "This plan expired.
Generate again."; stop ids the plan does not know are 400 "That plan
changed. Go back and try again." Ids: option `<runId>-<n>`, stop `stop_<activityId>_<i>` or
`alt_<activityId>_<slot>`, cursor `dag_<runId>_<offset>`. `POST /itineraries` (backend-B) enriches stops
through `d.Planner.ResolveStop` (newest live pool holding the id, else the catalog activity it names;
unknown ids are `store.ErrNotFound`) and falls back to `option.stops` otherwise. Tests wire the real planner
with `srv.Deps.Planner = planner.NewService(p)` over the test database (`pkg/planner/http_test.go`);
`pkg/planner` also has fakes for every seam (`FakeSource`, `FakeScorer`, `MemPoolStore`). Design:
`docs/design/planner.md`.

## Profiles seam (taste vectors)

Handlers never call the ML client. `api.Profiles` (`Refresh(ctx, userID)`, `Rated(ctx, userID, activityID,
stars)`) is set on `Deps.Profiles` in `main.go` by `pkg/profiles` once the ML rewrite lands; until then
it is nil and every helper is a no-op. Use the nil-safe helpers: `d.RefreshProfile(ctx, userID, 15*time.Second)`
(synchronous: `PUT /me/preferences`, Facebook import, the demo seed; log the error, never fail the
request), `d.RefreshProfileAsync(userID, 20*time.Second)` (sign-up), `d.RatedAsync(userID, activityID,
stars, 10*time.Second)` (`PUT /ratings/{itemId}` for a stop with an `activityId`). Tests:
`p := &testutil.ProfilesRecorder{}; srv := testutil.New(t, testutil.WithProfiles(p))`, then
`p.WaitRefreshes(t, userID, 1, time.Second)` / `p.WaitRated(t, 1, time.Second)`; set `p.Err` to prove a
failing ML service does not fail the request.

## Indexes and collections for new files

`appIndexes` and `AppCollections` in `store.go` are package-level slices: a new collection's store file
registers its own indexes (and adds itself to what `reset-app-data` drops) from an `init()` in that file,
e.g. `func init() { appIndexes = append(appIndexes, indexSpec{coll: collPlanTogether, keys: bson.D{{Key:
"postId", Value: 1}, {Key: "userId", Value: 1}}, name: "postId_userId_unique", unique: true});
AppCollections = append(AppCollections, collPlanTogether) }`. Never edit `store.go` itself.

## Tests

```go
srv := testutil.New(t)                                   // fresh DB, router, Recorder, frozen clock
a := srv.Signup(t, "Alice")                              // *Session: UserID, Access, Refresh, Email, Password, User
res := srv.Do(t, "POST", "/itineraries", body, a).Expect(t, 201); res.JSON(t, &out)  // X-Time-Zone always set
srv.WaitEvent(t, a.UserID, realtime.EventItineraryUpdated, time.Second); srv.Events.For(a.UserID)
srv.Clock.Set(testutil.Fixed) / Advance(d)               // keep pinned times near the present: TTL indexes use the wall clock
conn, _, _ := srv.Dial(t, a.Access, false); testutil.ExpectEvent(t, conn, "connected", time.Second)
```

Options: `testutil.WithPlanner(p)`, `WithConfig(func(*config.Config))`, `WithNow(t)`. Tests skip without
Mongo unless `CI=1` (`make test-db` runs everything against `sq-mongo` with `-race`). Append your
cross-user cases to `scopes` in `pkg/api/scoping_test.go`.

## Config

`d.Cfg` (`pkg/config`): `PublicBaseURL`, `JWTSecret`, token TTLs, `MLServiceURL` + `ML*` timeouts,
`Planner`, `FB*`, `Demo*` (`DemoDate`: `DEMO_DATE`, `Cfg.DemoClock()`), `DevResetCodes`, `CheckoutStepDelay` (0 in tests), `TrustProxy`,
`MaxPhotoBytes`, `MaxJSONBytes`, `DisableRateLimits`. `sidequestz-admin --env-file` reads the same names.
