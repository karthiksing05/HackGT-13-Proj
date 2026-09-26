# SideQuests — backend API endpoints (derived from the mock UI)

REST + JSON over HTTPS, bearer token auth (except the auth routes). Money in integer cents. Times in ISO 8601 with time zone. List endpoints use cursor pagination (`?cursor=&limit=`). One WebSocket for live updates.

Rule of thumb: the server owns anything that must be correct for everyone — equal-split math, transit times, age filtering, join limits, ratings → taste profile.

## System Health & Test
- GET /hello — interactive browser test dashboard when opened in a browser; JSON system health & endpoint map when requested with `Accept: application/json`

## Auth
- POST /auth/signup — email, password, name → account + tokens (profile setup step 1)
- POST /auth/login — email, password → tokens
- POST /auth/refresh — refresh token → new access token
- POST /auth/logout
- POST /auth/password/forgot — email → sends 6-digit code (always 200, don't reveal whether the email exists)
- POST /auth/password/verify — email + code → short-lived reset token
- POST /auth/password/reset — reset token + new password → signs out other sessions
- POST /auth/password/resend — resend code

## Me / profile
- GET /me — name, username, email, photo, status, age bracket
- PATCH /me — name, username, date of birth, status (open / online / not free)
- POST /me/photo — upload image → URL; DELETE /me/photo
- PATCH /me/avatar — initials color (when no photo)
- GET /me/preferences, PUT /me/preferences — 1–5 trip-type ratings, company, pace, money prefs (spend, flexibility, split style, prefer free), open-ended answers (typed, or spoken via Apple Speech on the phone)
- GET /me/taste-profile — the learned bars shown in Account
- POST /me/devices — APNs push token

## Integrations + payments
- GET /integrations — which calendars are connected
- POST /integrations/{google|outlook}/connect — returns OAuth URL
- GET /integrations/{provider}/callback — OAuth redirect
- DELETE /integrations/{provider}
- GET /me/payment-methods, POST /me/payment-methods (tokenized Visa card), DELETE /me/payment-methods/{id}

## Calendar (RFC 5545 .ics Feed & Universal Subscription)
- GET /calendar/feed/{token}.ics — public unauthenticated RFC 5545 iCalendar feed for external calendar apps (Google Calendar, Apple Calendar, Outlook, Thunderbird)
- GET /calendar/link — authenticated endpoint returning user's public feed URL, `webcal://` subscription URL, one-click Google Calendar add link, and setup instructions
- POST /calendar/link/regenerate — rotates the user's secret calendar token and invalidates previous subscription links
- GET /calendar/export.ics — authenticated direct `.ics` download of active sidequests, stops, and schedules
- GET /calendar/days?from=&to= — merged view per day: busy blocks (free/busy only), sidequests, group events, plus the user's `calendar_link` subscription object

## Activities, Places & Events Catalog (Freetime Database)
- GET /activities?kind=&near=&radius=&from=&to=&tags=&price=&age_ok= — full activities catalog (places & events from `freetime.activities`)
- GET /activities/search?q=&near=&kind= — search activities, places, and trails
- GET /activities/recommendations?near=&radius=&tags=&price=&limit=&q= — personalized, ML compatibility-ranked activities
- GET /activities/{id} — activity details (including RFC 5545 times, trail geometry, weekly hours, duration model)
- GET /places/search?q=&near= — search for start/end pins and suggestion chips
- GET /places/reverse?lat=&lng= — 2dsphere reverse geocoding to nearest place / dropped pin
- GET /events?near=&radius=&from=&to=&tags=&price=&age_ok=&rank= — filtered events catalog (with optional `rank=true` ML ordering)
- GET /events/recommendations?near=&radius=&tags=&price=&limit=&q= — personalized, ML compatibility-ranked events
- GET /events/{id} — event details, website URL, ticket availability

## Planning (Create flow)
- POST /plans/generate — start + end location, date, start time, back-by time, range, ride choice (+ open seats), mood text, tags, budget, who's coming, pace, travel modes → first 3 ML-ranked options + cursor
- POST /plans/generate/more — cursor → 2 more ML-ranked options ("Load more options"); returns done flag when out
- POST /plans/route — option id + stop order (+ ride, modes) → recalculated legs, stop times, arrival, late flag (after drag-to-reorder)
- POST /itineraries — save the chosen option + visibility (just me / friends / open), lock time, max group size

## Itineraries (Home)
- GET /itineraries?status=active — cards + timelines
- GET /itineraries/{id}
- PATCH /itineraries/{id} — reorder, visibility, lock time, max size
- DELETE /itineraries/{id}
- PATCH /itineraries/{id}/items/{itemId} — notes (private or shared with group)
- GET /itineraries/{id}/items/{itemId}/transit — walk / MARTA / rideshare options for "Getting there"

## Past events + ratings
- GET /me/past-events?unrated= — Home › Past, "N to rate" card
- PUT /ratings/{itemId} — stars, tags, note (updates taste profile and calls ML service to update user preference vectors)

## Agent checkout (Visa)
- POST /checkout/intents — item id, quantity → agent finds tickets, returns quote (price, fees, total, steps)
- GET /checkout/intents/{id} — status (also pushed over WebSocket)
- POST /checkout/intents/{id}/approve — user approval, the only step that spends money
- POST /checkout/intents/{id}/cancel

## Forum
- GET /forum/posts?lat=&lng=&radius=&scope=&type=&when=&max_dist=&cost=&tags=&open_only=&sort= — feed with sort + filters
- POST /forum/posts — "I'm free" post (visibility: friends / everyone, until time); open plans are published from POST /itineraries
- DELETE /forum/posts/{id} — take down
- POST /forum/posts/{id}/join-requests — request to join; DELETE to cancel
- POST /forum/posts/{id}/plan-together — opens a DM with the poster
- GET /itineraries/{id}/join-requests; POST /join-requests/{id}/approve | decline — host side (if approval is required)

## Threads (group chats + DMs)
- GET /threads — groups list + DMs, last message, badges
- GET /threads/{id}/messages?before=; POST /threads/{id}/messages
- POST /threads/dm — start a DM with a user

## Group album
- GET /groups/{id}/photos; POST /groups/{id}/photos (upload); DELETE /groups/{id}/photos/{photoId}

## Group splits (equal)
- GET /groups/{id}/expenses
- POST /groups/{id}/expenses — what, amount (cents), payer, split between → server returns each share (with 1¢ rounding) and notifies members
- DELETE /groups/{id}/expenses/{expenseId}
- GET /groups/{id}/balances — per-person net ("Maya owes you $2.50")
- POST /groups/{id}/settle — pay what you owe (Visa)

## Friends
- GET /friends — with live status
- GET /users/search?q= — name or @handle
- GET /friends/requests; POST /friends/requests; POST /friends/requests/{id}/accept | decline
- DELETE /friends/{id}
- POST /invites — shareable invite link

## Realtime
- WS /ws — new messages, join requests, friend status changes, forum updates, checkout status, transit delays

## Not public API (backend jobs)
- Scraper + LLM tool-calling pipeline that keeps the event catalog fresh
- Taste-profile updates from setup answers and ratings
- Calendar free/busy sync