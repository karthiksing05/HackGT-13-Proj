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
now := h.d.Clock()                               // the testable clock; never time.Now() in handlers or stores
…
httpx.JSON(w, http.StatusOK, view)               // httpx.NoContent(w) for 204
httpx.Error(w, http.StatusBadRequest, "Give your sidequest a name.")   // {"error":"bad_request","message":…}
```

`api.Fail(w, r, err)` maps `store.ErrNotFound → 404`, `ErrForbidden → 403`, `ErrConflict → 409`
(`ErrEmailTaken` / `ErrUsernameTaken` get their sentences), `*httpx.StatusError` → its status and
sentence, anything else → 500 logged with the request id. Unowned resources are 404; host-only itinerary
edits by a member are 403. Every 400/409 message is a sentence the app shows verbatim; unknown enum
values are 400 (`contract.Visibility("x").Valid()`). `httpx.DecodeOptional` for bodies that may be empty.

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
(`users_prefs.go`: `func (u Users) SavePrefs(…)`). Use `s.Now()` for timestamps, `store.NewID()` for
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

`api.Planner` (`Generate`, `More`, `Route`, `Alternatives`, `ResolveStop → *api.StopDetail`) is set on
`Deps.Planner` in `main.go` by the planner agent; `pkg/api/planning` answers 503 "Planning is warming up."
while it is nil. `POST /itineraries` (backend-B) enriches stops through `d.Planner.ResolveStop` when the
planner is present and falls back to `option.stops` otherwise.

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
`Planner`, `FB*`, `Demo*`, `DevResetCodes`, `CheckoutStepDelay` (0 in tests), `TrustProxy`,
`MaxPhotoBytes`, `MaxJSONBytes`, `DisableRateLimits`. `sidequestz-admin --env-file` reads the same names.
