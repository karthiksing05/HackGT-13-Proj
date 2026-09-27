# The demo

Everything a judge sees comes from one seeded account in a fictional city. This page lists what is
seeded, the three-minute walkthrough, what is deliberately simulated, and how to reset; the one-page
brief for the account is [DEMO_ACCOUNT.md](DEMO_ACCOUNT.md). The seed is
`sidequestz-admin seed-demo` (`Backend/cmd/sidequestz-admin/seed.go`); the city is in
`dataingestion/demo/`.

## Sandy Byte

| | |
|---|---|
| Account | **Sandy Byte**, `@sandybyte`, `demo@sidequestz.tech`; password = the server's `DEMO_PASSWORD` (not in the repo) |
| Profile | avatar sage, status open, born 2003-06-14 (adult), school "Saltlight Harbor College", setup complete |
| Catalog and city | `demo_activities`, `saltlight`; home base **Seaside Market Square** (31.3680, −81.4250) |
| Likes (1–5) | outdoors 5, long walks 5, live music 4, food 4, early mornings 4, museums 3, sports 3, shopping 2, nightlife 2, big crowds 1 |
| Preferences | small group, balanced pace, under $15, a bit over is ok, split equally, prefers free; agentic checkout off (Account › Payments; stored as `instant_checkout`) |
| Answers | perfect afternoon: "A long walk along the water, a snack from the market, then live music somewhere small while the sun goes down." · never: "Packed clubs, huge crowds, or anything that only gets going after midnight." · plans around: "Sunrise swims, the Saturday market, and whoever's free to wander." |
| Taste vectors | rebuilt through the ML service at seed time; her texts are shown in [EMBEDDINGS.md](EMBEDDINGS.md#text-formats). The seed reports "not refreshed" when the service is down, and her next preferences save rebuilds them |

The sign-in link: with `SQ_DEMO_PASSWORD` set at build time (or `-SQDemoPassword` at launch) the
sign-in screen shows **Use the demo account** under "Create an account"; it fills both fields and signs
in. Sandy's phone is wherever the judge is: the Create flow starts and ends at her home base, and the
planner moves any start more than 60 km from Saltlight Harbor back to it.

## What is seeded

Times hang off the seed run in `DEMO_TZ` (default `America/New_York`), so "tomorrow" stays tomorrow
after a reseed. The demo accounts live on a fixed demo date, the server's `DEMO_DATE=2026-09-27`: Sunday, the
busiest day in the catalog (17 events, from an 11 AM brunch crawl to an 8 PM bioluminescence paddle,
with 22 more through Oct 2). "Today" is that date at the real time of day, on any real day. The bots
have random passwords nobody knows.

| Item | Detail |
|---|---|
| **Marin Okafor** (`@marinokafor`, `marin@bots.sidequestz.tech`, bot) | Sandy's friend (for 30 days). Hosts **"Golden hour by the market"**, an open plan **tomorrow 5:30–8 PM**: a walk from Seaside Market Square through the three places nearest it (Seaside Market Hall, Fish Box Karaoke and Salvage and Sons Vintage) and back, at most 6 people, joining locks at 5 PM, members Marin and Theo, so the Forum shows 4 spots left. Also a friends-only **free-now post**, "Free until … near Seaside Market", lasting until 9 PM or three hours after the seed run, whichever is later |
| **Theo Park** (`@theopark`, `theo@bots.sidequestz.tech`, bot) | a **pending friend request** to Sandy: "Met at the market" |
| **"Saturday market crew"** (last Saturday, 9:30 AM) | hosted by Sandy with Marin and Theo: Seaside Market Hall (75 min), then Driftwood Coffee Roasters (45 min), walked from and back to the square. Sandy rated the market 5 stars ("Great people", "Would go again") and the coffee 4 stars ("Good value"). Finished, so not on the Forum |
| **"Heron Creek wander"** (yesterday, 4 PM) | Sandy alone on the Heron Creek Greenway (90 min), **not rated yet**, so Home shows the "past events to rate" card |
| Group chat "Saturday market crew" | three messages ("Coffee's on me today, it's in Splits." · "Thank you! I added the bus fares." · "Best Saturday in ages. Same time next week?") and two expenses split three ways: Coffee $24.00 paid by Marin, Bus fares $9.00 paid by Sandy. Sandy's balance is **"You owe $2.00"**: she owes Marin $5.00 and Theo owes her $3.00 |
| DM Sandy ↔ Marin | "That market morning was the best. Let's do it again soon!" and Marin's **unread** "I'm hosting a golden hour walk tomorrow at 5:30. Join if you're free!" |
| Payment method | a demo Visa •••• 4242, the default card (nothing real behind it) |

Running the seed again rewrites the seeded documents without duplicating them, re-times them to the new
run, and undoes what a walkthrough changes in them: Sandy's rating of the Heron Creek stop, an accepted
friendship with Theo, anyone who joined Marin's plan, unread counts, her default card, and her profile,
preferences, password and taste tags. Everything the seed did not write stays (new plans, sent messages,
added expenses, the group chat created by a join) until a [reset](#reset). The users keep fixed `5eed…`
ObjectIDs (an existing `demo@sidequestz.tech` account is adopted instead); the other documents have
fixed `seed-…` ids.

The catalog: 100 Saltlight Harbor activities, 45 events from Saturday Sep 26 to Friday Oct 2, 2026 (New York time) and 55 places
(trails, parks, gardens, markets, cafés, restaurants, bars, museums, galleries, rec venues, tours and
workshops), all with vectors once `embed_missing` has run. Neighbourhoods: Lighthouse Point, Marina Row,
The Shipyard, Deepwater Quarter, Seaside Market, Kelp Hollow, Tidepool Heights, Lanternfall Beach.
Details in [DATA.md](DATA.md).

## The three-minute walkthrough

Sign-in screen → **Use the demo account**. Home loads from the live server; a skeleton shows first and
the S logo appears only if the request takes more than 2 s.

1. **Home** (0:00). The "past events to rate" card at the top; no active sidequest yet, just "+ New
   sidequest". Tap **Past**: the rated Saturday stops, and "Heron Creek Greenway" with a **Rate** pill;
   rate it (this nudges her taste profile). Back to **Sidequests**.
2. **Create** (0:30). Tap **+**. *Where*: start and end are already **Seaside Market Square**, her home
   base, with "End where I start" on; range **Walkable**; **No ride**. *When*: tomorrow 12–4 PM (or this
   evening; the demo events run to Oct 2). *Vibe*: type or dictate "chill and outside, then live music",
   quick picks **Outdoors** and **Music**, budget **$**, **Friends only**.
3. **Review** (1:00). The S loader plays while the planner runs (under a second on the live server), then
   three options, "Best match" first, each with its stops and a meta line such as "~$ · 1.2 mi walking ·
   3 stops" (typical stop counts are in [PLANNER.md](PLANNER.md#measured-on-the-live-server)). Drag the
   ☰ handle to reorder:
   "Recalculating transit…" then updated times; if a fixed start breaks, the route header says "Some
   stops would be late" and the stop reads "Late for a fixed start". Press and hold a stop → **Swap for
   something similar**: up to five alternatives with a reason ("Also time outside · 0.4 mi away"); pick
   one and the route re-times. **Start this sidequest**.
4. **Home again** (1:45). The new sidequest is selected, with its timeline of walks and stops. **Tap a
   block**: the Event sheet with the time, place, description, **Getting there** (walk / MARTA /
   rideshare estimates), Website, notes, and "Rate it after".
5. **Forum** (2:10). The area defaults to her home base, Seaside Market Square. Marin's "Golden hour by
   the market" for tomorrow shows "4 of 6 spots left" and a lock label like "Locks Sun 5 PM"; **Request
   to join** answers instantly with "You're in · Open chat": the plan appears on Home for tomorrow and its
   group chat exists. Marin's
   free-now post (friends only) shows **Plan together**, which opens their DM with "Saw your post. Want
   to plan something together?" already sent.
6. **Groups** (2:35). The list shows "Saturday market crew" and, after the groups, the DM with Marin
   (one unread). Open the crew › **Splits**: the headline shows her net "You owe $2.00" (the Groups row's chip
   reads "You owe $2"), and the rows read "You owe Marin $5.00" and "Theo owes you $3.00", over two
   expenses split equally, and the button reads **Settle up $5.00** (what she owes, paid with the demo Visa).
   **+ Add an expense** ("Pizza", $40, split 3 ways) shows the cent-exact
   preview ($13.34 / $13.33 / $13.33) and, once saved, the banner "Everyone was notified".
7. **Account** (2:50). Her taste profile bars, **Home base** (Seaside Market Square, Saltlight Harbor),
   the demo Visa, and **Friends** with Marin and Theo's pending request ("Met at the market") to accept.

`LiveSmokeUITests` walks the same path automatically (sign-in, every tab, Create up to Review) and
attaches screenshots ([DEPLOY.md](DEPLOY.md#runbook), step 5). If the server is unreachable,
`-SQAPIMode mock` runs the same screens offline on the app's built-in Atlanta demo data (user Jordan
Lee), without the live planner.

## Known limits

- **Simulated on purpose** (labelled `// Simulated:` in the code; see the [roadmap](ROADMAP.md)): the
  agent checkout (a timer-driven state machine; "Approve purchase" never charges anything, the ticket is
  a demo page), the card page (it saves only a brand and the last four digits), calendar connect (marks
  the calendar connected and reads nothing), password-reset delivery (the code goes to the server log)
  and push notifications.
- **Agentic checkout is a sandbox.** "Let Muse get your tickets" buys from our own merchant
  (`events.sidequestz.tech`) with Stripe test-mode payment tokens, so nothing real is charged; it offers
  only stops whose ticket page is on that merchant (Saltlight's 25 ticketed events), and its endpoints
  answer 503 until the server has the Stripe and merchant settings ([AGENTIC_CHECKOUT.md](AGENTIC_CHECKOUT.md)).
- **The bots do not answer.** Marin and Theo are seeded records; messages to them stay unanswered, and
  nobody else will join a judge's open plan unless a second account does.
- **The demo events run from Sep 26 to Oct 2, 2026 (New York time).** After that the planner only finds places in
  Saltlight; regenerate the snapshot (`python -m demo.generate` after moving `DAY0` in
  `dataingestion/demo/generate.py`) and reimport it to move the events.
- **Travel times are straight-line estimates** (4.5 km/h walking, 20 km/h transit plus 5 min, 30 km/h
  driving plus 5 min), and "MARTA" is simply the transit label; Saltlight has no transit data. Transit
  and anywhere days still spread over 8–12 miles.
- **Facebook** works only for accounts added as testers of the Meta app while it is in Development
  mode; the demo does not go through it.
- **One catalog per account.** Sandy sees Saltlight; a new account sees the real Atlanta catalog (which
  has no restaurants or cafés yet) and gets no seeded friends.
- **One shared account.** Two judges signing in as Sandy at once share her plans and realtime events; she
  can hold 5 live sockets, and a sixth connection closes the oldest.

## Reset

On the VPS:

```sh
A="/opt/backend/sidequestz-admin --env-file /opt/backend/.env"
$A reset-app-data --yes          # every app collection except the catalogs and the users
$A ensure-indexes
$A drop-ttl demo_activities
$A seed-demo                     # recreates Sandy's world and prints what it wrote
```

A full wipe that also removes the accounts judges created is `reset-app-data --yes --users`, then
`ensure-indexes` (dropping a collection drops its indexes), then `seed-demo`, which recreates Sandy and
the bots. Locally the same commands are `go
run ./cmd/sidequestz-admin …` from `Backend/` with the environment from the
[local loop](DEPLOY.md#local-development-loop). To reset only the app on a phone, relaunch with
`-SQResetSession YES`.
