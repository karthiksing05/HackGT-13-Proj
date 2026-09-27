# Backend rebuild: the app-facing layer

Owners: **backend-foundation** (Phase 0), then agents **backend-A** (auth/me/preferences/devices/calendar
stub/payments stub/photos), **backend-B** (itineraries, calendar days, events, transit, past, ratings,
insights, search, places), **backend-C** (forum, join, threads, messages, groups, friends, invites,
presence, realtime emitters), **backend-D** (checkout + seed), **backend-E** (Facebook). Paths relative to
`Backend/`. The contract is `frontend/API_CONTRACT.md`; the Swift models
(`frontend/SideQuestz/Models/*.swift`, `Decoding.swift`) are the exact field list; the app's mock
(`frontend/SideQuestz/Services/Mock/MockAPIClient.swift`, `MockData.swift`, `MockLedger.swift`) is the
behavioral reference for every endpoint.

## 0. Decisions

| Topic | Decision |
|---|---|
| Old app-facing code | **Delete** `pkg/handlers/*` (the planner handlers are replaced by `pkg/api/planning` from the planner design), the app-facing structs in `pkg/models/models.go`, `pkg/store/store.go`, `pkg/handlers/handlers_test.go`, `pkg/util/ics.go`, `pkg/util/pagination.go`. Rewrite `pkg/realtime/ws.go`, `pkg/router/routes.go`, `main.go`, `pkg/env/env.go`. Keep `pkg/itinerary`, `pkg/travel`, `pkg/ml` (rewritten by the ml/planner agents), `pkg/util/{argon,jwt,uid,verification}.go`, `pkg/middleware/{auth,cors,response}.go` (extended), `pkg/datastore/mongo.go` (hardened). |
| Wire types | New `pkg/contract` mirrors the Swift models field-for-field, snake_case tags, RFC 3339 UTC times. Handlers never serialize a Mongo document. |
| Mongo docs | `pkg/models` = Mongo document schemas only: `Activity` (unchanged; the planner/ML depend on it), a rewritten `User`, and new docs per collection. No `json` tags on docs. |
| Multi-account | Every store query takes the caller's user id; ownership/membership checked in the store layer (`ErrNotFound`/`ErrForbidden`/`ErrConflict`). No in-memory fallbacks; Mongo down → process exits at startup / 503 on `/healthz`. |
| IDs | Users: ObjectID hex. Everything else: UUIDv7 strings as `_id`. The app only ever sees `id`. |
| Simulated pieces (labelled in code + docs) | checkout agent (timer-driven state machine), hosted card page, calendar connect pages, password-reset "delivery" (server log + `DEV_RESET_CODES=1`). |
| Photos | JPEG bytes in a `photos` collection (≤ 2 MB), served publicly at `GET /photos/{id}` (unguessable UUID, immutable cache). |
| Join model | Auto-accept (contract): `joined` + group thread created on first join; `full` / `closed` computed; `requested` never returned. |
| Lists | Bare arrays with server caps everywhere, except `GET /forum/posts` and `GET /me/past-events` (`{items, next_cursor}`, page 50). `GET /threads/{id}/messages` must stay a bare array. |

## 1. Architecture

### 1.1 Package layout

```
main.go                          config → datastore.MustConnect → store.EnsureIndexes → hub → checkout agent → http.Server (timeouts, graceful shutdown)
pkg/config/config.go             typed Config from env (§8); Validate() refuses a default/placeholder JWT secret unless APP_ENV=dev
pkg/env/env.go                   thin wrappers kept for the planner
pkg/contract/                    auth.go people.go preferences.go itinerary.go planning.go social.go checkout.go insights.go facebook.go time.go enums.go
pkg/httpx/                       decode.go (1 MB body) · write.go (JSON/NoContent/Error) · tz.go (X-Time-Zone) · page.go (cursors) · fmt.go (time/money/label formatting) · ratelimit.go · recover.go · reqlog.go
pkg/middleware/                  auth.go (BearerAuth, OptionalAuth), cors.go, response.go ({error,message})
pkg/models/                      activity.go (moved, unchanged) · user.go · itinerary.go · social.go · checkout.go · facebook.go · tokens.go · planner.go (planner-owned)
pkg/store/                       store.go (Store{db}, collections, EnsureIndexes, errors) · users.go · tokens.go · itineraries.go · item_state.go · ratings.go · checkout.go · payments.go · photos.go · forum.go · threads.go · messages.go · expenses.go · friends.go · invites.go · devices.go · facebook.go · catalog.go · planstore.go (planner-owned)
pkg/realtime/                    hub.go (rewritten) · publisher.go (interface, Noop, Recorder) · events.go (typed emitters)
pkg/api/deps.go                  Deps{Store, Hub, Cfg, Now, Planner, ML}
pkg/api/auth/                    handlers.go tokens.go reset.go
pkg/api/me/                      me.go preferences.go taste.go devices.go photo.go
pkg/api/integrations/            calendar.go payments.go pages.go (hosted HTML pages)
pkg/api/photos/                  serve.go
pkg/api/itineraries/             handlers.go view.go create.go retime.go calendar.go events.go transit.go past.go insights.go search.go places.go
pkg/api/social/                  forum.go join.go threads.go messages.go groups.go expenses.go friends.go invites.go presence.go view.go
pkg/api/checkout/                handlers.go agent.go
pkg/api/facebook/                handlers.go graph.go crypto.go mapping.go signed_request.go
pkg/api/planning/                handlers.go (thin adapters; the planner agent owns pkg/planner)
pkg/router/routes.go             composes areas; route ordering (facebook before {provider})
pkg/testutil/                    mongo.go (test DB per run) · server.go (httptest + helpers) · fixtures.go · events.go
docs/api/examples/*.json         generated from the app's ContractTests (ios-3); the Go contract test reads ../docs/api/examples
scripts/{smoke.sh, test-db.sh}
Makefile                         test, test-db, examples, smoke, build, deploy
```

### 1.2 Handler conventions

- Each area exposes `func Register(r *mux.Router, d *api.Deps)`; handlers are methods on `type H struct{ d *api.Deps }`.
- `httpx.Decode(r, &req)` (1 MB `MaxBytesReader`, unknown keys allowed, 400 `"Check the details and try again."` on syntax errors).
- `httpx.JSON(w, 200, contract.X{...})`, `httpx.NoContent(w)`, `httpx.Error(w, status, sentence)` → `{"error": "...", "message": "<sentence>"}`. Every 400/409 message is a user-facing sentence (the app shows it).
- Store errors: `ErrNotFound → 404`, `ErrForbidden → 403`, `ErrConflict → 409`, else 500 (logged with a request id). Unowned resources return **404** except host-only itinerary edits by a member → **403**.
- `httpx.TZ(r)` from `X-Time-Zone` (default `America/New_York`, `_ "time/tzdata"`, LRU-cached). `httpx.Now(d)` for testable clocks.
- View builders (`view.go` per area) convert `models.*` → `contract.*` **per viewer** (`is_host`, `unread`, `sender_name: "You"`, `join_status`). Realtime payloads with viewer-dependent shapes are rendered per recipient.

### 1.3 `pkg/api/deps.go`

```go
type Deps struct {
    Store   *store.Store
    Hub     realtime.Publisher      // Send(userID, type, data) / SendMany / Broadcast
    Cfg     *config.Config
    Now     func() time.Time
    Planner planner.Service         // owned by the planner agent
    ML      *ml.Client              // best-effort profile refresh
}
```

## 2. Persistence and multi-account

### 2.1 Collections (database `freetime`)

| Collection | `_id` | Key fields (bson) | Notes |
|---|---|---|---|
| `users` | ObjectID | `email` (lower), `passwordHash`, `name`, `nameLower`, `username`, `usernameLower`, `photoId?`, `avatarColor`, `status` (open/friends_only/busy), `birthDate?`, `school?`, `setupComplete`, `catalog` (activities/demo_activities), `city`, `homeBase?` {name, lat, lng}, `prefs` (contract shape, snake keys), `taste` {tags map, ratingCount}, `integrations` {google:{connected,at}, outlook:…}, `positiveText`, `negativeText`, `positiveEmbedding`, `negativeEmbedding`, `embeddingModel`, `profileTextHash`, `profileUpdatedAt`, `facebookInterests`, `lastLocation?`, `roles` [bot/demo], `createdAt`, `updatedAt`, `lastActiveAt` | Embedding fields keep today's bson names. Age bracket computed at read time. |
| `refresh_tokens` | uuid | `tokenHash` (sha256), `userId`, `familyId`, `expiresAt`, `revokedAt?`, `replacedBy?`, `createdAt` | Rotation with family-reuse detection. |
| `reset_codes` | email | `codeHash`, `attempts`, `expiresAt` (10 min), `verifiedAt?` | |
| `web_sessions` | token (32 random bytes, base64url) | `userId`, `purpose` (payment_setup/calendar_connect/facebook_state/fb_deletion), `provider?`, `rerequest?`, `expiresAt`, `usedAt?` | One-time tokens for hosted pages and OAuth `state`. |
| `devices` | uuid | `userId`, `token` (unique), `platform`, `createdAt` | |
| `payment_methods` | uuid | `userId`, `brand`, `last4`, `isDefault`, `demoToken`, `createdAt` | Simulated cards. |
| `photos` | uuid | `ownerId`, `kind` (avatar/group), `groupId?`, `contentType`, `bytes` (≤ 2 MB), `size`, `createdAt` | |
| `itineraries` | uuid | `hostId`, `memberIds` [incl. host], `title`, `dateKey` (YYYY-MM-DD in `tz`), `tz`, `date`, `start`, `backBy`, `startPlace`, `endPlace`, `visibility`, `lockAt?`, `maxGroupSize?`, `items` [ItemDoc], `plan` (raw contract.PlanRequest), `optionId`, `runId`, `routeMode`, `status` (active/past/deleted), `threadId?`, `postId?`, `createdAt`, `updatedAt` | ItemDoc: `id`, `kind` (stop/transit), `title`, `place`, `start`, `end`, `description`, `websiteUrl?`, `bookable`, `priceCents?`, `activityId?`, `stopId`, `durationMin`, `sharedNotes?`, `sharedNotesBy?`, `legMode?`, `legMinutes?`. |
| `item_states` | `userId|itemId` | `userId`, `itemId`, `itineraryId`, `notes?`, `notesScope?`, `transitMode?`, `ticket?` {id, quantity, totalCents, confirmation, url} | Per-viewer state folded into `ItineraryItem`. |
| `ratings` | `userId|itemId` | `userId`, `itemId`, `itineraryId`, `stars`, `tags`, `note?`, `createdAt` | |
| `checkout_intents` | uuid | `userId`, `itemId`, `itineraryId`, `itemTitle`, `quantity`, `paymentMethodId?`, `cardBrand`, `cardLast4`, `state`, `steps` [{text,done}], `subtotalCents?`, `feesCents?`, `totalCents?`, `instant`, `failureReason?`, `nextTransitionAt?`, `ticketId?`, `createdAt`, `updatedAt` | |
| `forum_posts` | uuid | `type` (plan/free_now), `authorId`, `visibility` (friends/everyone), `location` (GeoJSON), `areaLabel?`, `radiusMi?`, `text?`, `until?`, `itineraryId?`, `startsAt?`, `endsAt?`, `tags`, `priceTier`, `planTogetherBy` [userIds], `expiresAt` (TTL), `createdAt` | Plan posts mirror the itinerary. |
| `join_requests` | uuid | `postId`, `itineraryId`, `userId`, `status` (accepted/cancelled), `createdAt` | Record only. |
| `threads` | uuid | `isGroup`, `title`, `memberIds`, `itineraryId?` (unique sparse), `dmKey?` (`a|b` sorted, unique sparse), `createdBy`, `lastMessageText`, `lastSenderId`, `lastMessageAt`, `unread` {userId:n}, `readAt` {userId:t}, `createdAt`, `updatedAt` | Group id == thread id. |
| `messages` | uuid | `threadId`, `senderId`, `text`, `clientId?`, `sentAt` | |
| `expenses` | uuid | `groupId`, `what`, `amountCents`, `payerId`, `splitAmong`, `shares`, `createdBy`, `settlement` bool, `createdAt` | |
| `friendships` | `a|b` (sorted) | `userIds` [a,b], `createdAt` | |
| `friend_requests` | uuid | `fromId`, `toId`, `note`, `status` (pending/accepted/declined/cancelled), `createdAt` | |
| `invites` | code (8 chars, Crockford base32) | `userId`, `expiresAt` (30 d), `uses`, `createdAt` | |
| `facebook_accounts` | userId | `fbUserId` (unique sparse), `accessTokenEnc`, `tokenExpiresAt`, `grantedScopes`, `declinedScopes`, `needsReconnect`, `name`, `connectedAt` | |
| `facebook_imports` | userId | `importedAt`, `likedPages`, `pages` [{id,name,category,categoryList,likedAt}], `city?`, `friendFbIds`, `suggestedRatings`, `interests` | Replaced on each import. |
| `plan_pools`, `plan_runs` | run id | planner-owned (see `planner.md`) | |
| `activities`, `demo_activities` | existing | read-only here (`store/catalog.go`: `SearchPlaces(catalog, q, near, limit)`, `Nearest(catalog, pt, maxM)`, `GetActivity(catalog, id)`) | `user.catalog` selects the collection. |

### 2.2 Indexes (`store.EnsureIndexes`)

```
users:             {email:1} unique · {usernameLower:1} unique · {nameLower:1} · {roles:1}
refresh_tokens:    {tokenHash:1} unique · {userId:1} · {familyId:1} · {expiresAt:1} TTL(0)
reset_codes:       {expiresAt:1} TTL(0)
web_sessions:      {expiresAt:1} TTL(0)
devices:           {token:1} unique · {userId:1}
payment_methods:   {userId:1, createdAt:1}
photos:            {ownerId:1} · {groupId:1, createdAt:-1} sparse
itineraries:       {memberIds:1, start:-1} · {hostId:1, start:-1} · {"items.id":1} · {status:1, backBy:1} · {postId:1} sparse · {threadId:1} sparse
item_states:       {userId:1, itemId:1} unique · {itineraryId:1}
ratings:           {userId:1, itemId:1} unique · {userId:1, createdAt:-1}
checkout_intents:  {userId:1, createdAt:-1} · {state:1, nextTransitionAt:1}
forum_posts:       {location:"2dsphere"} · {authorId:1} · {type:1, startsAt:1} · {itineraryId:1} unique sparse · {expiresAt:1} TTL(0)
join_requests:     {postId:1, userId:1} unique · {userId:1}
threads:           {memberIds:1, lastMessageAt:-1} · {itineraryId:1} unique sparse · {dmKey:1} unique sparse
messages:          {threadId:1, sentAt:-1} · {threadId:1, senderId:1, clientId:1} unique partial(clientId exists)
expenses:          {groupId:1, createdAt:1}
friendships:       {userIds:1}
friend_requests:   {toId:1, status:1} · {fromId:1, status:1} · {fromId:1, toId:1} unique partial(status:"pending")
invites:           {userId:1} · {expiresAt:1} TTL(0)
facebook_accounts: {fbUserId:1} unique sparse
plan_pools / plan_runs: {expiresAt:1} TTL(0), plan_runs {userId:1, createdAt:-1}
activities, demo_activities: {location:"2dsphere"} (create if missing) · {city:1, kind:1, start:1} · {name:1}
```
`EnsureIndexes` uses fixed names; "exists with different options" is fatal at startup. It never touches TTL
indexes on the catalog collections; remove unwanted TTL indexes directly in MongoDB.

### 2.3 Fail loudly

`datastore.MustConnect(ctx, cfg)` = `Connect` + `Ping` (10 s) or `log.Fatal`. `IsConnected()` removed; every
store method returns errors (no `_ =` on writes; a grep test asserts that). `GET /healthz` → ping → 200 /
503. No seeded catalog, no in-memory maps, no `GlobalStore`.

### 2.4 Auth tokens

- Access JWT (HS256, `JWT_SECRET` ≥ 32 bytes, required): claims `sub`=userId, `jti`, `exp` = now+1h, `typ:"access"`. `expires_at` in responses = access expiry.
- Refresh: 32 random bytes → base64url; only `sha256` stored. `POST /auth/refresh`: find by hash → unrevoked & unexpired → insert a new token in the same `familyId`, mark old `revokedAt`+`replacedBy`. A revoked token presented again revokes the family → 401.
- Reset token: JWT `typ:"reset"`, `sub`=userId, 15 min, single use.
- `POST /auth/logout {refresh_token}`: public route (works with an expired access token); 204.
- Password reset revokes all refresh tokens of the user.

### 2.5 `X-Time-Zone`

Applied via `httpx.TZ(r)` in: `GET /calendar/days` (parse `from`/`to`, day keys), `POST /itineraries`
(`date` = local midnight of `plan.date`), `PATCH /itineraries/{id}` (day shifts), `GET /forum/posts`
(`day`, `when`, `lock_label`), `POST /forum/posts` (`"Free until 6:30 PM near …"`), `GET /threads*`
(`last_time`, group `subtitle`), `GET /friends` (status lines), `GET /me/insights` (time buckets), `GET
/me/past-events`. Tests pass `X-Time-Zone: America/New_York`.

## 3. Contract structs (`pkg/contract`)

`contract.Time` wraps `time.Time`: marshals `t.UTC().Format(time.RFC3339)`, unmarshals RFC3339 /
RFC3339Nano / `"2006-01-02"` (UTC midnight; handlers re-anchor day-only values in the request tz). Enums are
typed strings with `Valid()`; unknown input enum values → 400. Key normalization (`enums.go`):
`Preferences.ratings` / `suggested_ratings` keys are canonical snake_case (`live_music`, `big_crowds`,
`early_mornings`, `long_walks`); camelCase input (`liveMusic`) is accepted. `answers` keys likewise
(`perfect_afternoon`, `never_do`, `plan_around`). `PlanRequest.modes` is emitted sorted.

```go
// people.go
type Coordinate struct { Lat float64 `json:"lat"`; Lng float64 `json:"lng"` }
type Place struct { Name string `json:"name"`; Coordinate *Coordinate `json:"coordinate,omitempty"` }
type User struct {
    ID string `json:"id"`; Name string `json:"name"`; Username *string `json:"username,omitempty"`; Email string `json:"email"`
    PhotoURL *string `json:"photo_url,omitempty"`; AvatarColor string `json:"avatar_color"` // ink|sage|clay|forest|sand
    Status string `json:"status"` // open|friends_only|busy
    AgeBracket string `json:"age_bracket"` // under_13|teen|under_21|adult
    School *string `json:"school,omitempty"`; SetupComplete bool `json:"setup_complete"`
    HomeBase *Place `json:"home_base,omitempty"`; City *string `json:"city,omitempty"`
}
type PersonRef struct { ID, Name, Initials, ColorHex string; PhotoURL *string `json:"photo_url,omitempty"`; Username *string `json:"username,omitempty"` }
type Friend struct { Person PersonRef `json:"person"`; StatusLine string `json:"status_line"`; Activity string `json:"activity"` } // free|on_sidequest|busy|new
type FriendRequest struct { ID string; Person PersonRef; Note string; Outgoing bool }
type UserSearchResult struct { Person PersonRef; Relation string; RequestID *string `json:"request_id,omitempty"` } // none|friend|outgoing|incoming

// auth.go
type Tokens struct { AccessToken string `json:"access_token"`; RefreshToken string `json:"refresh_token"`; ExpiresAt Time `json:"expires_at"` }
type AuthResponse struct { User User `json:"user"`; Tokens Tokens `json:"tokens"` }
type SignupRequest struct { Name, Email, Password string; Username *string; DateOfBirth *Time `json:"date_of_birth"` }
type UserPatch struct { Name, Username *string; DateOfBirth *Time `json:"date_of_birth"`; Status *string }
type RefreshResponse struct { AccessToken string `json:"access_token"`; RefreshToken string `json:"refresh_token"`; ExpiresAt Time `json:"expires_at"` }

// preferences.go
type Preferences struct {
    Ratings map[string]int `json:"ratings"`; Company string `json:"company"`; Pace string `json:"pace"`; Spend string `json:"spend"`
    Flexibility string `json:"flexibility"`; SplitStyle string `json:"split_style"`; PreferFree bool `json:"prefer_free"`
    Answers map[string]string `json:"answers"` // never nil
    InstantCheckout bool `json:"instant_checkout"`; InstantCheckoutLimitCents int `json:"instant_checkout_limit_cents"`
}
type TasteBar struct { Label string `json:"label"`; Value float64 `json:"value"` }
type TasteProfile struct { Bars []TasteBar `json:"bars"` }
type Integration struct { Provider string `json:"provider"`; Connected bool `json:"connected"` }
type PaymentMethod struct { ID, Brand, Last4 string; IsDefault bool `json:"is_default"` }

// itinerary.go
type Rating struct { Stars int; Tags []string; Note *string `json:"note,omitempty"` }
type Ticket struct { ID string; Quantity int; TotalCents *int `json:"total_cents,omitempty"`; Confirmation *string `json:"confirmation,omitempty"`; URL *string `json:"url,omitempty"` }
type ItineraryItem struct {
    ID, Kind /* busy|sidequest|transit|group */, Title string; Place *Place `json:"place,omitempty"`; Start, End Time
    Description *string `json:"description,omitempty"`; WebsiteURL *string `json:"website_url,omitempty"`
    Bookable bool; PriceCents *int `json:"price_cents,omitempty"`; People, Interested []PersonRef; ExtraGoing int `json:"extra_going"`
    Notes *string `json:"notes,omitempty"`; NotesScope *string `json:"notes_scope,omitempty"` /* private|shared */
    Rating *Rating `json:"rating,omitempty"`; TransitMode *string `json:"transit_mode,omitempty"`; Ticket *Ticket `json:"ticket,omitempty"`
    ActivityID *string `json:"activity_id,omitempty"`
}
type Itinerary struct {
    ID, Title string; Date, Start, BackBy Time; StartPlace, EndPlace Place; Visibility string /* just_me|friends|open */
    LockAt *Time `json:"lock_at,omitempty"`; MaxGroupSize *int `json:"max_group_size,omitempty"`
    Items []ItineraryItem; GoingCount int `json:"going_count"`; IsHost bool `json:"is_host"`
}
type ItineraryUpdate struct { Title *string; Date, Start, BackBy *Time; Visibility *string; StopOrder []string `json:"stop_order"` }
type ItemNotesPatch struct { Notes string; NotesScope *string `json:"notes_scope"` }
type TransitOption struct { Mode string; Minutes int; CostCents *int `json:"cost_cents,omitempty"` }
type CalendarItem struct { ID, Kind, Title string; Start, End Time; People, Interested []PersonRef; ItineraryID *string `json:"itinerary_id,omitempty"` }
type CalendarDay struct { ID string; Date Time; Items []CalendarItem }
type PastEvent struct { ID, Title, Place, Company string; Date Time; Kind string; Rating *Rating `json:"rating,omitempty"` }

// insights.go
type PastInsight struct { ID, Title, Value string; Detail *string `json:"detail,omitempty"`; Symbol *string `json:"symbol,omitempty"` }
type PastInsights struct { Headline string; Highlights []PastInsight; TopTags []string `json:"top_tags"`; BasedOn int `json:"based_on"` }
type SearchResults struct { Sidequests []Itinerary; People []UserSearchResult; Places []Place; Posts []ForumPost }

// social.go
type ForumPost struct {
    ID, Type /* plan|free_now */ string; Author PersonRef; IsFriend bool `json:"is_friend"`; FriendsOnly bool `json:"friends_only"`
    Title *string `json:"title,omitempty"`; Text *string `json:"text,omitempty"`; Meta string; When *string `json:"when,omitempty"`; Route *string `json:"route,omitempty"`
    StartsInMinutes int `json:"starts_in_minutes"`; Day string; DistanceMi float64 `json:"distance_mi"`; PriceTier int `json:"price_tier"`
    Tags []string; SpotsLeft *int `json:"spots_left,omitempty"`; Capacity *int `json:"capacity,omitempty"`; LockLabel *string `json:"lock_label,omitempty"`
    Going []PersonRef; GoingCount int `json:"going_count"`; InterestedCount int `json:"interested_count"`; PostedMinutesAgo int `json:"posted_minutes_ago"`
    JoinStatus string `json:"join_status"` /* none|requested|joined|full|closed */; PlanTogetherSent bool `json:"plan_together_sent"`; ThreadID *string `json:"thread_id,omitempty"`
}
type JoinResult struct { Status string; ItineraryID *string `json:"itinerary_id,omitempty"`; ThreadID *string `json:"thread_id,omitempty"` }
type MyFreePost struct { ID, Visibility, Text string; Until *Time `json:"until,omitempty"`; AreaLabel *string `json:"area_label,omitempty"`; RadiusMi *int `json:"radius_mi,omitempty"` }
type NewForumPost struct { Type, Visibility string; Until *Time; Lat, Lng *float64; AreaLabel *string `json:"area_label"`; RadiusMi *int `json:"radius_mi"` }
type ChatThread struct {
    ID string; IsGroup bool `json:"is_group"`; Title, Subtitle string; Members, Faces []PersonRef; LastMessage string `json:"last_message"`; LastTime string `json:"last_time"`
    Chips []string; Unread int; AlbumTitle *string `json:"album_title,omitempty"`; AlbumSubtitle *string `json:"album_subtitle,omitempty"`
}
type Message struct { ID string; SenderID string `json:"sender_id"`; SenderName string `json:"sender_name"`; Text string; SentAt Time `json:"sent_at"`; ClientID *string `json:"client_id,omitempty"` }
type GroupPhoto struct { ID string; ByName string `json:"by_name"`; UploaderID *string `json:"uploader_id,omitempty"`; CreatedAt *Time `json:"created_at,omitempty"`; URL *string `json:"url,omitempty"`; PlaceholderHex *string `json:"placeholder_hex,omitempty"` }
type Expense struct { ID, What string; AmountCents int `json:"amount_cents"`; PayerID string `json:"payer_id"`; SplitAmong []string `json:"split_among"`; Shares []int; CreatedBy *string `json:"created_by,omitempty"` }
type NewExpense struct { What string; AmountCents int `json:"amount_cents"`; PayerID string `json:"payer_id"`; SplitAmong []string `json:"split_among"` }
type Balance struct { UserID string `json:"user_id"`; NetCents int `json:"net_cents"` }
type SettleRequest struct { AmountCents int `json:"amount_cents"`; PaymentMethodID *string `json:"payment_method_id"` }

// checkout.go
type CheckoutStep struct { Text string; Done bool }
type CheckoutIntent struct {
    ID string; ItemID string `json:"item_id"`; ItemTitle string `json:"item_title"`; Steps []CheckoutStep
    SubtotalCents, FeesCents, TotalCents *int (omitempty); CardBrand string `json:"card_brand"`; CardLast4 string `json:"card_last4"`
    State string // preparing|awaiting_approval|processing|booked|cancelled|failed
    Quantity int; PaymentMethodID *string `json:"payment_method_id,omitempty"`; FailureReason *string `json:"failure_reason,omitempty"`; Instant bool
}
type CreateCheckoutIntent struct { ItemID string `json:"item_id"`; Quantity int; PaymentMethodID *string `json:"payment_method_id"`; Instant bool }

// facebook.go
type FacebookImport struct { ImportedAt Time `json:"imported_at"`; LikedPages int `json:"liked_pages"`; SuggestedRatings map[string]int `json:"suggested_ratings"`; Interests []string; HomeArea *string `json:"home_area,omitempty"`; FriendsOnApp []UserSearchResult `json:"friends_on_app"` }
type FacebookConnection struct { Connected bool; NeedsReconnect bool `json:"needs_reconnect"`; Name *string `json:"name,omitempty"`; DeclinedScopes []string `json:"declined_scopes"`; LastImport *FacebookImport `json:"last_import,omitempty"` }
type FacebookConnectRequest struct { Rerequest bool }
type DataDeletionResponse struct { URL string `json:"url"`; ConfirmationCode string `json:"confirmation_code"` }

// planning.go (wire shapes; the planner maps them internally — see planner.md §6.2 for the additive fields)
type PlanRequest struct { Start, End Place; Date, StartTime, BackBy Time; Range, Ride string; OpenSeats *int; MoodText string; Tags []string; Budget int; Who, Pace string; Modes []string }
type PlanStop struct { ID, Title, Subtitle string; Place Place; DurationMinutes int `json:"duration_minutes"`; /* + optional extras */ }
type PlanOption struct { ID, Name, Tag, Meta string; Stops []PlanStop /* + optional extras */ }
type PlanBatch struct { Options []PlanOption; Cursor *string `json:"cursor,omitempty"`; Done bool; Reason *string `json:"reason,omitempty"` }
type PlanAlternative struct { Stop PlanStop; Reason string }
type Leg struct { Mode string; Minutes int }
type StopWindow struct { Start, End Time }
type RouteRequest struct { OptionID string `json:"option_id"`; StopOrder []string `json:"stop_order"`; Start, End Place; StartTime, BackBy Time; Ride string; Modes []string }
type RouteResult struct { Legs []Leg; StopTimes []StopWindow `json:"stop_times"`; Arrival Time; MinutesLate int `json:"minutes_late"`; BrokenAt int `json:"broken_at"` }
type AlternativesRequest struct { OptionID string `json:"option_id"`; StopID string `json:"stop_id"`; StopOrder []string `json:"stop_order"` }
type CreateItineraryRequest struct { Plan PlanRequest; Option PlanOption; StopOrder []string `json:"stop_order"`; Route RouteResult; Visibility string; LockAt *Time `json:"lock_at,omitempty"`; MaxGroupSize *int `json:"max_group_size,omitempty"` }
```

## 4. Endpoint specification by work package

Legend: **Auth** = bearer required unless noted; **owner/member/host** = store-enforced.

### Package A: auth, me, preferences, devices, calendar stub, payments stub, photos

| Endpoint | Behavior | Store / events | Auth | Mock reference |
|---|---|---|---|---|
| `POST /auth/signup` | `SignupRequest` → 201 `AuthResponse`. name ≥ 2, valid email, password ≥ 8 with a digit; username normalized (`@` stripped, lower, `^[a-z0-9_.]{3,30}$`) or generated `<name>_<4hex>`; `date_of_birth` < 13 y → 400 "You need to be 13 or older to use SideQuests."; duplicates → 409 "An account with that email already exists." / "That username is taken." Defaults: `avatar_color: ink`, `status: open`, `setup_complete: false`, `catalog: activities`, `city: atlanta`, `school` from the email domain (gatech.edu→"Georgia Tech", emory.edu→"Emory", gsu.edu→"Georgia State"). | `Users.Create`, `Tokens.Issue`; async profile refresh (ml) | public, rate-limited | `signup` |
| `POST /auth/login` | → `AuthResponse`; wrong → 401 "That email and password don't match." (constant-time dummy verify when the user is missing) | `Users.ByEmail`, `Tokens.Issue` | public, rate-limited | `login` |
| `POST /auth/refresh` | `{refresh_token}` → `RefreshResponse` (rotated); invalid/revoked → 401 | `Tokens.Rotate` | public | contract |
| `POST /auth/logout` | `{refresh_token?}` → 204 | `Tokens.Revoke` | public (bearer optional) | `logout` |
| `POST /auth/password/forgot`, `…/resend` | `{email}` → 200 `{}` always (+ `{"code"}` only when `DEV_RESET_CODES=1`). 6-digit code, 10 min; logged at INFO. | `Resets.Create` | public, rate-limited | `forgotPassword` |
| `POST /auth/password/verify` | `{email, code}` → `{reset_token}`; wrong/expired/5 attempts → 400 "That code didn't work. Check the email or resend a new one." | `Resets.Verify` | public | `verifyResetCode` |
| `POST /auth/password/reset` | `{reset_token, new_password}` → 204 | `Users.SetPassword`, `Tokens.RevokeAll` | public | `resetPassword` |
| `GET /me` | → `User` (with `home_base`, `city`) | | | `me` |
| `PATCH /me` | `UserPatch` → `User`; username conflict 409; `status` enum | `Users.Update`; `friend.status` to friends on status change | | `updateMe` |
| `POST /me/photo` | multipart `photo` (jpeg/png by magic bytes), ≤ 2 MB else 413 → `{"url": "<PUBLIC_BASE_URL>/photos/<id>"}` | `Photos.Put`, `Users.SetPhoto` (deletes the previous) | | `uploadPhoto` |
| `DELETE /me/photo` | → 204 | | | `deletePhoto` |
| `PATCH /me/avatar` | `{color}` (enum) → 204 | | | `setAvatarColor` |
| `GET /me/preferences` | → `Preferences` (defaults when unset: small_group, balanced, under_15, bit_over_ok, equally, prefer_free true, answers {}, instant false, limit 5000) | | | `preferences` |
| `PUT /me/preferences` | `Preferences` → 200 `Preferences`; validates enums/ratings 1–5; sets `setup_complete: true`; then `ml.UserProfile` refresh (sync 15 s; on error keep old vectors, clear the hash) | `Users.SavePrefs`, `Users.SetEmbeddings` | | `savePreferences` |
| `GET /me/taste-profile` | → `TasteProfile` bars `Outdoors, Food, Art, Social, Nightlife`: `value = clamp(0.6·pref/5 + 0.4·tasteTag)`; Art←museums, Social←mean(company≠solo?0.7:0.3, big_crowds/5); default 0.5 | | | `tasteProfile` |
| `POST /me/devices`, `DELETE /me/devices/{token}` | `{push_token, platform}` → 204 | `Devices.Upsert/Delete` | | no-ops in mock |
| `GET /integrations` | → `[Integration]` exactly two rows (google, outlook) | | | `integrations` |
| `POST /integrations/{provider:google\|outlook}/connect` | → `{url: PUBLIC_BASE_URL/integrations/{provider}/start?t=<web_session>}` | `WebSessions.Create` | | `connectIntegration` |
| `GET /integrations/{provider}/start?t=` | **Honest stub**: consumes the token, marks connected, 302 → `sidequestz://integrations/{provider}/done?status=connected`. The page says no calendar data is read. | `WebSessions.Consume`, `Users.SetIntegration` | public (token) | `completeIntegration` |
| `DELETE /integrations/{provider}` | → 204 | | | `disconnectIntegration` |
| `GET /me/payment-methods` | → `[PaymentMethod]` | | | `paymentMethods` |
| `POST /me/payment-methods/setup` | → `{url: PUBLIC_BASE_URL/pay/setup?t=<web_session>}` | | | `paymentSetupURL` |
| `GET /pay/setup?t=`, `POST /pay/setup` | **Simulated card page** (server-rendered HTML: "Demo card" button + optional test number accepting `4242…`, `5454…`, `1881…`); adds a card, 302 → `sidequestz://payments/done`. No PAN stored. | `Payments.Add` | public (token) | `addPaymentMethod` demo cards |
| `POST /me/payment-methods` | `{token}` → 201 `PaymentMethod` (`tok_<brand>_<last4>` parsed; else Visa 4242); first card is default | | | `addPaymentMethod` |
| `DELETE /me/payment-methods/{id}` | → 204; promotes the next card | | | `deletePaymentMethod` |
| `GET /photos/{id}` | bytes, `Cache-Control: public, max-age=31536000, immutable`, ETag | | **public** | — |
| `GET /healthz` | `{ok:true}` / 503 | ping | public | — |

### Package B: itineraries, calendar, events, transit, past, ratings, insights, search, places

Shared `itineraries/view.go` builds `contract.Itinerary` per viewer: `is_host`, `going_count = len(memberIds)`;
item `kind` = `transit` for legs, `group` when `len(memberIds) > 1` else `sidequest`; `people` = first 3
members, `extra_going = max(0, len−3)`, `interested = []`; fold `item_states` (notes/scope/transit_mode/ticket)
and `ratings` for the viewer; without a private note, shared notes are returned with `notes_scope=shared`.

| Endpoint | Behavior | Store / events | Auth | Mock reference |
|---|---|---|---|---|
| `POST /itineraries` | `CreateItineraryRequest` → 201 `Itinerary`. Materialize like the mock: stops = `stop_order` over `option.stops` (unknown ids → 400 "That plan changed. Go back and try again."); for each `route.legs[i]`: transit item `"<ModeLabel> to <destination>"` (`walk→Walk, marta→MARTA, rideshare→Rideshare, drive→Drive, uber→Uber`), `place:{name: destination}`, description "Route options below. Times update live if you run late.", `start=cursor`, `end=cursor+minutes`; then the stop item with `route.stop_times[i]`. Title `option.name`; `date` = local midnight (tz) of `plan.date`; `start`/`back_by` from the plan; enrich stops via `Planner.ResolveStop` (activity id, price, website, bookable). `back_by <= start_time` → 400. | `Itineraries.Insert`; if `visibility != just_me` → `Forum.UpsertPlanPost`; `forum.update` broadcast | | `createItinerary` |
| `GET /itineraries?status=active` | → `[Itinerary]` where viewer ∈ `memberIds`, `status=active` (lazily flip active→past when `backBy < now`), sorted `start asc`, cap 100 | `Itineraries.ListForUser` | | `activeItineraries` |
| `GET /itineraries/{id}` | → `Itinerary`; non-member → 404 | | member | `itinerary` |
| `PATCH /itineraries/{id}` | `ItineraryUpdate` → `Itinerary`. Host only else 403 "Only the host can edit this sidequest." Title empty → 400 "Give your sidequest a name."; empty `stop_order` → 400 "A sidequest needs at least one stop."; date change shifts every non-busy item by whole days (tz-aware); then **re-time** (`retime.go`, port of the mock's `retimed`: kept stops in order, each preceded by a walk leg, leg minutes = `travel.Estimate` when both coordinates exist else 15, stepping around `busy` items); visibility change → create/delete the forum post. | `Itineraries.UpdateHost`; `itinerary.updated` per member; `forum.update` | host | `updateItinerary`, `retimed` |
| `DELETE /itineraries/{id}` | → 204. Host only (member → 403 "You joined this sidequest, so you can leave it but not delete it.") | `Itineraries.DeleteHost`, `Forum.DeletePlanPost`, `JoinRequests.DeleteForItinerary`, `Threads.Archive`; `itinerary.removed` to members; `forum.update` | host | `deleteItinerary` |
| `POST /itineraries/{id}/leave` | → 204; host → 400 "You host this sidequest. Delete it instead." | `Itineraries.RemoveMember`, `Threads.RemoveMember`, `JoinRequests.Cancel`; `itinerary.removed` to the leaver; `itinerary.updated` to the rest; `thread.updated`; `forum.update` | member | `leaveItinerary` |
| `PATCH /itineraries/{id}/items/{itemId}`, `PATCH /events/{id}` | `{notes, notes_scope?}` → 204; `shared` also writes `items.$.sharedNotes` | `ItemStates.Upsert`; `itinerary.updated` to others when shared | member | `updateItemNotes` |
| `GET …/items/{itemId}/transit`, `GET /events/{id}/transit` | → `[TransitOption]` from the previous non-transit item's place (or `start_place`) to this item: walk/`marta`/`rideshare` via `travel.Estimate`, costs `0 / 250 / null`; no coordinates → `18/12/8` | | member | `transitOptions` |
| `PUT …/transit`, `PUT /events/{id}/transit` | `{mode}` → 204 | `ItemStates.Upsert(transitMode)` | member | `selectTransit` |
| `GET /events/{id}` | → `ItineraryItem` for any item id in a viewer itinerary (index `items.id`) | `Itineraries.FindItemForMember` | member | `eventDetail` |
| `GET /calendar/days?from&to` | day-only strings in tz (default today..today+13; max 62 days) → `[CalendarDay]` for every day in range (empty `items: []` days included), items = the viewer's non-transit itinerary items (`people`, `itinerary_id`) grouped by tz day key. **No busy blocks.** | `Itineraries.ItemsBetween` | | `calendarDays` |
| `GET /me/past-events?unrated=` | → `{items:[PastEvent], next_cursor}`; stop items with `end < now` in viewer's itineraries; `company` = `"solo"` or `"with N others"`; `unrated=true` drops rated; date desc | | | `pastEvents` |
| `GET /me/insights` | → `PastInsights`: port of `MockAPIClient.pastInsights` (liked = ≥ 4; `mostCommon` with the same tie-break; time buckets `<12 Mornings, 12–17 Afternoons, else Evenings`; company; favorite area; top rated; `top_tags` by count then name; `based_on`; none → nudge headline, empty highlights) | | | `pastInsights` |
| `PUT /ratings/{itemId}` | `Rating` → 204; item must belong to a viewer itinerary | `Ratings.Upsert`, `Users.BumpTaste`; async `ML.UpdateUserEmbedding` (with the activity's vector; see embeddings.md) | member | `rate` |
| `GET /search?q=` | → `SearchResults`; empty q → all empty. sidequests: viewer's active itineraries whose title or a non-transit item title contains q; people: as `/users/search`; places: `Catalog.SearchPlaces(catalog, q, near home base, 5)`; posts: visible forum posts whose title/text/author name contains q | | | `search` |
| `GET /places/search?q&near` | → `[Place]` (≤ 10): q empty → `[home base] + 4 nearest catalog places`; else catalog name regex ordered by distance | `Catalog.SearchPlaces` | | `searchPlaces` |
| `GET /places/reverse?lat&lng` | → `Place`: nearest catalog place within 150 m else `{name: "Dropped pin", coordinate}` | `Catalog.Nearest` | | `reverseGeocode` |

### Package C: forum, threads, messages, groups, friends, invites, presence, realtime

Visibility: a post is visible to V if `authorId != V` and (`visibility == everyone` or V is the author's
friend). Scope `friends` requires author ∈ friends. Radius: `$geoWithin` circle (`radius` mi, default 2)
around `lat,lng` for scope `everyone`; friends' posts ignore radius. `max_dist` is a second filter.

Post view (tz-aware): `meta` = `"Hosting · 0.4 mi away"` / `"Free now · 0.3 mi away"`; `when` = `"Today ·
5:30–8 PM"` / `"Tomorrow · …"` / `"Sat · 6–10 AM"` / `"Oct 3 · …"`; `route` = `"N stops ·
walking|MARTA|driving"`; `day` = `today` / lowercase 3-letter weekday; `starts_in_minutes`; `price_tier` =
plan budget; `spots_left = maxGroupSize - len(members)` (nil without max); `capacity`; `lock_label = "Locks
5:00 PM"` / `"Locks Fri 9 PM"`; `going` = members minus host (≤ 3); `going_count`; `interested_count = 0`;
`join_status`: `joined` if V ∈ members, else `closed` if `lockAt < now` or started, else `full` if spots 0,
else `none`; `thread_id` when joined; `plan_together_sent`; `is_friend`; `friends_only`. Free-now: `text`,
`tags: []`, `price_tier 0`, `starts_in_minutes 0`, `day today`.

| Endpoint | Behavior | Store / events | Auth | Mock reference |
|---|---|---|---|---|
| `GET /forum/posts?…` | → `{items:[ForumPost], next_cursor}` (page 50). Filters/sort exactly as `MockAPIClient.forumPosts` (`type`, `scope`, `when` now≤60 min/today/weekend, `max_dist`, `cost` set, `tags` any-match, `open_only` = plan with spots>0; sort soonest/closest/spots/newest) | `Forum.Visible`, `Friends.IDs`, `Itineraries.ByIDs` | | `forumPosts` |
| `GET /forum/posts/mine` | → `MyFreePost` or **204** | `Forum.MyFreePost` | | `myFreePost` |
| `POST /forum/posts` | `NewForumPost` (`type` must be `free_now`) → 201 `MyFreePost`; `until` default now+3h; `text = "Free until <h:mm a> near <area_label|"you">"`; replaces an existing free post; `lat/lng` default home base | `Forum.ReplaceFreePost`; `forum.update`; `friend.status` to friends | | `postFreeNow` |
| `DELETE /forum/posts/{id}` | author only → 204; plan posts → 400 "Change the sidequest's visibility instead." | `Forum.DeleteOwn`; `forum.update`; `friend.status` | author | `deleteForumPost` |
| `POST /forum/posts/{id}/join-requests` | → 200 `JoinResult`. plan post only (free_now → 400 "Use Plan together for this post."); host → 400; already member → `joined`; `closed` if locked/started; `full` if no spots; else **auto-accept**: `AddMember`, `Threads.EnsureGroup(itin)` (title = itinerary title, members = itinerary members), `JoinRequests.Record` → `{joined, itinerary_id, thread_id}` | to joiner: `join.update`, `itinerary.updated`, `thread.updated`; to host: `join.request {post_id, from}`; to all members: `itinerary.updated`, `thread.updated`; `forum.update` | | contract (auto-accept) |
| `DELETE /forum/posts/{id}/join-requests` | → 204; if member → same as leave | | | `cancelJoinRequest` |
| `POST /forum/posts/{id}/plan-together` | → `ChatThread` (DM with the author); posts `"Saw your post. Want to plan something together?"` once per post | `Threads.EnsureDM`, `Messages.Insert`, `Forum.MarkPlanTogether`; `message.new` + `thread.updated` to the author | | `planTogether` |
| `GET /threads` | → `[ChatThread]` viewer's threads, `lastMessageAt desc`, cap 200 | | | `threads` |
| `GET /threads/{id}` | → `ChatThread`; non-member 404 | | member | `thread` |
| `GET /threads/{id}/messages?before=` | → **bare** `[Message]` oldest-first, page 30 ending before `before` | `Messages.PageBefore` | member | `messages` |
| `POST /threads/{id}/messages` | `{text, client_id?}` → 201 `Message`; empty → 400 "Type a message first."; same `client_id` → the existing message | `Messages.InsertIdempotent`, `Threads.Touch`; `message.new` (per-recipient `sender_name`, `"You"` for the sender), `thread.updated` | member | `sendMessage` |
| `POST /threads/{id}/read` | → 204 | `Threads.MarkRead`; `thread.read` to the viewer's other sockets | member | `markThreadRead` |
| `POST /threads/dm` | `{user_id}` → `ChatThread`; self → 400; unknown → 404 | `Threads.EnsureDM`; `thread.updated` to the other user on create | | `startDM` |
| `GET /groups/{id}/photos` | → `[GroupPhoto]` newest first (`url` = `/photos/{id}`) | | member | `groupPhotos` |
| `POST /groups/{id}/photos` | multipart `photo` ≤ 2 MB → 201 `GroupPhoto` | `Photos.Put`; `photo.added`; `thread.updated` | member | `uploadGroupPhoto` |
| `DELETE /groups/{id}/photos/{photoId}` | uploader only (403 "You can only delete photos you added.") → 204 | | member | `deleteGroupPhoto` |
| `GET /groups/{id}/expenses` | → `[Expense]` | | member | `ledger` |
| `POST /groups/{id}/expenses` | `NewExpense` → 201 `Expense`; validations "Add what the expense was for." / "Enter an amount above $0." / "Pick at least one person to split with."; payer and `split_among` must be members; `shares = equalShares` (floor, first `total mod n` get +1); `created_by = viewer` | `Expenses.Insert`; `expense.added`; `thread.updated` | member | `addExpense`, `SplitMath.equalShares` |
| `DELETE /groups/{id}/expenses/{expenseId}` | creator only (403) → 204 | | member | `deleteExpense` |
| `GET /groups/{id}/balances` | → `[Balance]` per other member (port of `MockLedger.balances`; + they owe me) | | member | `MockLedger.balances` |
| `POST /groups/{id}/settle` | `{amount_cents, payment_method_id?}` → 204. `owed = Σ(-net for net<0)`; mismatch → **409** "The balance changed. Check the new amount and try again."; no card → 400 "Add a card in Account first."; one settlement expense per creditor (`what = "Settled up with <Brand> •••• <last4>"`). **Simulated.** | `Expenses.InsertMany`; `expense.added`; `thread.updated` | member | `settleUp` |
| `GET /friends` | → `[Friend]` with `status_line`/`activity` from `presence.go` (active free post → "Free until 6:30 PM" `free`; on an itinerary now → "On a sidequest · <title>" `on_sidequest`; busy → "Busy"; friendship < 24 h → "Just added" `new`; else "Open to plans") | | | `friends` |
| `GET /users/search?q=` | → `[UserSearchResult]` (≤ 20; excludes self; relation via `Friends.Relations`) | | | `matchingPeople` |
| `GET /friends/requests` | → `[FriendRequest]` incoming (`outgoing:false`) + outgoing (`outgoing:true`) pending | | | `friendRequests` |
| `POST /friends/requests` | `{user_id}` → 201 `FriendRequest(outgoing:true)`; self → 400; already friends → 409 "You're already friends."; existing outgoing → return it; existing incoming from that user → accept it | `friend.request` to the recipient | | `sendFriendRequest` |
| `DELETE /friends/requests/{id}` | own outgoing only → 204 | | | `cancelFriendRequest` |
| `POST /friends/requests/{id}/accept` | recipient only → 204; friendship + DM thread ("Just added") | `friend.status` to both; `thread.updated` | | `acceptFriendRequest` |
| `POST /friends/requests/{id}/decline` | → 204 | | | `declineFriendRequest` |
| `DELETE /friends/{user_id}` | → 204 (DM kept) | `friend.status` | | `removeFriend` |
| `POST /invites` | → 201 `{url: "https://sidequests.app/invite/<code>"}` (one live code per user) | | | `createInvite` |
| `POST /invites/{code}/accept` | → `Friend` (the inviter); unknown/expired → 400 "That invite link didn't work. Ask for a new one."; own code → 400 | `friend.status`, `thread.updated` to the inviter | | `acceptInvite` |

ChatThread view: DM: `title` = other person's name, `subtitle` = their status line, `faces` = [other].
Group: `title` = itinerary title, `subtitle` = `"N people · Today 6 PM"`, `faces` = first two members ≠
viewer, `chips` = `["N people", <"You owe $9" | "You're owed $4" | "Settled up">, "N photos"]`,
`album_title` = `"<title> · Sep 25"`, `album_subtitle` = `"N photos · M people"`. `last_message` =
`"You: …"` / `"<First>: …"`; `last_time` = `"5:12 PM"` (today), weekday (< 7 days), else `"Sep 20"`;
`unread = unread[viewer]`.

### Package D: agent checkout (simulated, persisted state machine)

`checkout/agent.go`: goroutine (`agent.Run(ctx)`) ticking every 500 ms: `FindOneAndUpdate({state in
[preparing, processing], nextTransitionAt <= now}, {$set: next state})` per document (atomic), then
`checkout.status {intent_id, state}` to `userId`. `CHECKOUT_STEP_DELAY` (1.5 s; tests 0).

```
create ─ no card ──► failed ("Add a card in Account first.")
create ─ else ─► preparing ─(delay)─► awaiting_approval   steps: [Found tickets on the official site ✓, Filled in your name and email ✓, Waiting for your approval ✗]
create ─ instant && prefs.instant_checkout && subtotal != nil && subtotal <= limit ─► processing (instant:true, step 3 "Paying instantly (within your limit)") ─(delay)─► booked
approve (awaiting_approval only, else 400 "This checkout already finished.") ─► processing ─(delay)─► booked
booked: all steps done, ticket {id, quantity, total_cents, confirmation "SQ-XXXXX", url PUBLIC_BASE_URL/tickets/{id}} written to item_states[user,item].ticket
cancel: preparing|awaiting_approval ─► cancelled (204); processing|booked ─► 409 "This checkout already went through."
```

| Endpoint | Behavior | Auth | Mock reference |
|---|---|---|---|
| `POST /checkout/intents` | `CreateCheckoutIntent` → 201 `CheckoutIntent` (`preparing`, or `processing` when instant, or `failed`). Item must be the viewer's; `subtotal_cents = price_cents×qty` or null; card = given → default → first; `quantity` 1–10 | | `createCheckoutIntent` |
| `GET /checkout/intents/{id}` | → `CheckoutIntent` (owner else 404) | owner | `checkoutIntent` |
| `PATCH /checkout/intents/{id}` | `{payment_method_id}` → `CheckoutIntent`; only before `processing` | owner | `updateCheckoutIntent` |
| `POST /checkout/intents/{id}/approve` | → `CheckoutIntent` in `processing` | owner | `approveCheckout` |
| `POST /checkout/intents/{id}/cancel` | → 204 / 409 | owner | `cancelCheckout` |
| `GET /tickets/{id}` | tiny HTML ticket page | public (unguessable) | — |

### Package E: Facebook connector (real Graph API v26.0)

Config: `FB_APP_ID`, `FB_APP_SECRET` (dev fallback: repo-root `meta_app_id` / `meta_app_secret` when
`APP_ENV=dev`), `FB_GRAPH_VERSION=v26.0`, `FB_TOKEN_KEY` (32-byte hex for AES-256-GCM; default HKDF from
`JWT_SECRET`), redirect URI `PUBLIC_BASE_URL/integrations/facebook/callback`. Every Graph call carries
`appsecret_proof`. `graph.go` is an interface with a real client and a fake for tests.

| Endpoint | Behavior | Auth |
|---|---|---|
| `GET /integrations/facebook` | `FacebookConnection`; `last_import.friends_on_app` re-rendered with current relations | |
| `POST /integrations/facebook/connect` | `{rerequest}` → `{url}` = `https://www.facebook.com/v26.0/dialog/oauth?client_id&redirect_uri&state&response_type=code&scope=public_profile,user_likes,user_location,user_friends[&auth_type=rerequest]`; `state` = `web_sessions` token (10 min, single use) | |
| `GET /integrations/facebook/callback` | public; resolve `state` (unknown/used → 302 `…?status=error&message=That sign-in link expired. Try again.`); `error=access_denied` → `…?status=denied`; else code→short token→long-lived token→`/me?fields=id,name`+`/me/permissions`; store encrypted; 302 `sidequestz://integrations/facebook?status=connected`; failures → `status=error&message=<sentence>` | public |
| `POST /integrations/facebook/import` | not connected → 409 "Connect Facebook first."; Graph `me?fields=id,name,location`, `me/likes` (paged, ≤ 1000), `me/friends?fields=id,name`; store raw import; suggestions via `mapping.go` (category table; keep types with ≥ 3 pages; rating = `clamp(1,5, round(1 + 4·count/maxCount))`); `interests` = 3–8 humanized top categories; `friends_on_app` by `fbUserId`; Graph error 190 → `needsReconnect`, **409** "Facebook needs you to sign in again." (never 401); then `facebookInterests` feeds the profile refresh | |
| `DELETE /integrations/facebook` | Graph `DELETE /me/permissions` (best-effort), delete account + import, 204 | |
| `POST /integrations/facebook/deauthorize` | form `signed_request` → verify HMAC-SHA256 → delete token, mark disconnected; 200 | public (signature) |
| `POST /integrations/facebook/data-deletion` | verify → delete → `{url: PUBLIC_BASE_URL/integrations/facebook/deletion-status?code=<c>, confirmation_code}` | public |
| `GET /integrations/facebook/deletion-status?code=` | plain HTML | public |

Router: all `/integrations/facebook…` routes first; calendar routes constrained with `{provider:google|outlook}`.

### Package F: planning (router surface only)

```go
type Service interface {
    Generate(ctx, user *models.User, req contract.PlanRequest, tz *time.Location) (contract.PlanBatch, error)
    More(ctx, user *models.User, cursor string) (contract.PlanBatch, error)
    Route(ctx, user *models.User, req contract.RouteRequest) (contract.RouteResult, error)
    Alternatives(ctx, user *models.User, req contract.AlternativesRequest) ([]contract.PlanAlternative, error)
    ResolveStop(ctx, stopID string) (*StopDetail, error)   // StopDetail{ActivityID, PriceCents *int, WebsiteURL *string, Bookable bool, DurationMin int}
}
```
Routes: `POST /plans/generate`, `POST /plans/generate/more`, `POST /plans/route`, `POST /plans/alternatives`.

## 5. Realtime

Envelope: exactly `{"type": "<event>", "data": {…}}`. Hub: auth via `Authorization: Bearer` first,
`?token=` fallback; invalid → 401 before upgrade. Registry `map[userID]map[*client]struct{}` under one mutex;
no channel closed by a sender; `client.send` buffered (64); a full buffer closes that connection; `unregister`
runs once (`sync.Once`). Ping 25 s, read deadline 60 s, read limit 4 KB, max 5 sockets per user, write
deadline 10 s. `Publisher` interface (`Send`, `SendMany`, `Broadcast`), `Recorder` for tests, typed helpers
in `events.go`. On connect send `{"type":"connected","data":{}}`.

| `type` | `data` | Recipients | Emitted by |
|---|---|---|---|
| `message.new` | `{thread_id, message}` (per-recipient `sender_name`) | thread members | send message, plan together |
| `thread.updated` | `ChatThread` (per-recipient) | thread members | send/read, membership changes, expenses, photos, DM creation |
| `thread.read` | `{thread_id}` | the reader's other devices | mark read |
| `join.request` | `{post_id, from: PersonRef}` | host | join |
| `join.update` | `{post_id, result: JoinResult}` | joiner | join |
| `friend.status` | `{user_id, status_line}` | the user's friends | status patch, free post create/delete, accept/remove friend |
| `friend.request` | `FriendRequest` | recipient | send request |
| `forum.update` | `{}` | **broadcast** | itinerary create/patch/delete, join/leave, free post create/delete |
| `checkout.status` | `{intent_id, state}` | intent owner | checkout agent + handlers |
| `itinerary.updated` | `Itinerary` (per-recipient) | members | patch, join, leave, shared notes |
| `itinerary.removed` | `{itinerary_id}` | members / leaver | delete, leave, cancel join |
| `expense.added` | `{group_id, expense}` | group members | add expense, settle |
| `photo.added` | `{group_id, photo}` | group members | upload photo |
| `transit.delay` | `{itinerary_id, item_id, minutes}` | members | helper only |

## 6. Tests

- `pkg/contract/contract_test.go`: for every `docs/api/examples/<Name>.json` (generated by ios-3; until then the dump in the session scratchpad `contract-swap/` is a stand-in) decode into the mapped struct with `DisallowUnknownFields`, re-encode, and assert the recursive key set and canonical value are identical.
- `pkg/testutil/mongo.go`: `MONGO_TEST_URI` (default `mongodb://127.0.0.1:27017`), database `sq_test_<pid>_<rand>` dropped at the end; skip locally without a server, hard fail when `CI=1`. `pkg/testutil/server.go`: full router with `realtime.Recorder`, fixed `Now`, checkout delays 0; helpers `Signup`, `Do`, `X-Time-Zone` always set.
- Suites per area: `auth_test` (signup→me→refresh rotation→reuse detection→logout→reset flow with `DEV_RESET_CODES=1`, rules, duplicates), `scoping_test` (A's resources are 404 for B; member-vs-host 403s), `itineraries_test` (create from the contract example → items match the mock's materialization; patch reorder → re-timed; calendar days; past events + insights; ratings; search/places), `social_test` (free post; visibility/radius; join → joined/full/closed + thread + events; messages paging + `client_id`; unread/read; expenses 4000/3 → 1334,1333,1333; balances; settle 409; friends/requests/invites; photo upload/serve), `checkout_test` (transitions, instant path, guards, events), `facebook_test` (fake Graph; connect URL; callback states; import mapping; 190 → 409; signed_request vectors), `realtime_test` (real websocket: header auth, `?token=`, per-user delivery, slow-client eviction, `-race`).
- `scripts/smoke.sh` (`BASE_URL`, curl + jq): signup → preferences → `/plans/generate` → `/plans/route` → `/itineraries` → list → `/calendar/days` → free-now post → `/forum/posts/mine` → `/healthz`.

## 7. Database setup

The server creates required indexes at startup. Accounts are created through signup;
existing fixture records remain in MongoDB. There is no admin executable or seed/reset command.

## 8. Config and deployment

| Env | Required | Default / notes |
|---|---|---|
| `APP_ENV` | no | `prod`; `dev` relaxes JWT/FB checks and enables the repo-root FB file fallback |
| `HTTP_ADDR` | no | `127.0.0.1:8080` (`PORT` accepted as a fallback) |
| `PUBLIC_BASE_URL` | yes | `https://api.sidequestz.tech` |
| `MONGO_URI`, `MONGO_DB` | yes / no | `mongodb://127.0.0.1:27017`, `freetime` |
| `JWT_SECRET` | **yes** | ≥ 32 bytes; startup refuses the old placeholder or anything shorter unless `APP_ENV=dev` |
| `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL` | no | `1h`, `720h` |
| `ML_SERVICE_URL`, `ML_*` timeouts, `PLANNER`, `PLANNER_*` | no | see embeddings.md / planner.md |
| `FB_APP_ID`, `FB_APP_SECRET`, `FB_GRAPH_VERSION`, `FB_TOKEN_KEY` | for Facebook | |
| `DEMO_TZ` | timezone for `DEMO_DATE` | `America/New_York` |
| `DEV_RESET_CODES` | no | `0` |
| `CHECKOUT_STEP_DELAY` | no | `1500ms` |
| `TRUST_PROXY` | no | `1` on the VPS |
| `MAX_PHOTO_BYTES`, `MAX_JSON_BYTES` | no | `2097152`, `1048576` |

Server: `http.Server{ReadHeaderTimeout: 10s, ReadTimeout: 30s, WriteTimeout: 60s, IdleTimeout: 120s,
MaxHeaderBytes: 64<<10}`; graceful shutdown (10 s). Middleware: recover → request id/log → CORS → body limit
→ rate limit (`/auth/*` 10 req/min/IP burst 20; login/forgot 5/min/email) → auth.

`backend.service`: `User=sidequestz`, `EnvironmentFile=/opt/backend/.env` (0600), no inline `Environment=`
lines, `Restart=always`, `RestartSec=3`, `LimitNOFILE=65536`, hardening kept + `PrivateTmp=true`.
`Backend/deploy.sh`: build and upload the binary and unit, then restart the API.
The server's `.env` stays in place. Tests run separately; indexes are created at API startup.

## 9. Ordering and risks

Phase 0 (foundation) lands first: `pkg/config`, hardened `datastore`, `pkg/contract` + `contract_test`,
`pkg/httpx`, `pkg/models` rewrite, `pkg/store/{store,users,tokens}.go` + `EnsureIndexes`, `pkg/api/auth` +
`me` (GET/PATCH), rewritten `pkg/realtime`, `pkg/router` with every contract route registered (unbuilt ones
answer `501 {"message":"Not built yet"}`), `pkg/testutil`, deletion of the old
handlers/store/tests, `main.go`. Then A–E in parallel (C depends on B's `store/itineraries.go` for
`AddMember/RemoveMember`; D on A's payments and B's items). Risks: time zones and day keys (test around
midnight and DST); planner ↔ B handoff (persisted pools; fallback to `option.stops`); `answers` key casing
(server emits snake_case, accepts both); `GET /threads/{id}/messages` must be a bare array; the VPS's
old-shape data is reset once before seeding; Facebook needs the redirect URI whitelisted and testers added.
