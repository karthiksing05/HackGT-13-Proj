# The demo

This page describes the existing demo fixture account and the three-minute walkthrough.
The account brief is [DEMO_ACCOUNT.md](DEMO_ACCOUNT.md); city data lives in `dataingestion/demo/`.
The backend no longer includes the demo account's seed/reset tooling; `Backend/cmd/seed` only adds
[showcase data](#showcase-data) around it. `store.CatalogFor` in
`Backend/pkg/store/catalog.go` selects `demo_activities` for demo/bot roles and `pitch_activities`
for other accounts.

## Sandy Byte

| | |
|---|---|
| Account | **Sandy Byte**, `@sandybyte`, `demo@sidequestz.tech`; password = the existing account password (not in the repo) |
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
   stops would be late" and the stop reads "Late for a fixed start". Tap a stop for its details (timing, map, description, price, links) with **Swap for something
   similar** and **Remove stop**; press and hold does the same from a menu. **Swap for
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

## Showcase data

`Backend/cmd/seed` fills the live database with made-up people and sidequests, so the app is lively the
moment anyone opens it. Roles decide who sees which of its two worlds (`store.CatalogFor`):

- **Atlanta**, what every regular account sees: eight students from Georgia Tech, Emory, Georgia State,
  SCAD Atlanta, Morehouse and Spelman (role `showcase`: neither the demo cast nor bots, so they share the
  pitch catalog, People for you and the Forum with real sign-ups), with preferences, taste vectors (for
  People for you and the Forum's match %), presence and friendships among them; five plans on today's and
  tomorrow's evenings around Midtown, Tech Square and the BeltLine (open or friends only, real
  `pitch_activities` places, 2–3 people each), each with a group chat; three "free now" posts near Tech
  Square that end tonight.
- **Saltlight**, what Sandy sees on the demo date: Marin Okafor and Theo Park (reused as they are when
  they exist) and Juno Reyes, Kai Nakamura and Rosa Delgado; friendships with Sandy, Rosa's pending friend
  request, three open plans near Seaside Market Square, "Bowling night" that Sandy is in (2 unread
  messages in its chat) and two "free now" posts. Sandy's own account is never changed. With it, the
  walkthrough above shows more: Home has "Bowling night" tomorrow (not "no active sidequest yet"), Groups
  has its chat, the Forum has four more plans and two more "free now" posts, and Account ›
  Friends has Juno and Kai, with Rosa's request next to Theo's.

Every document it writes is tagged `seed: "showcase-v1"` and has a fixed id, so running it again rewrites
the same documents with fresh times (Atlanta from now, Saltlight from `DEMO_DATE`), and anyone who joined
a showcase plan stays in it. `--remove` deletes exactly what it wrote, plus what exists only inside its
plans and chats (as when a host deletes a plan). How to run it: [DEPLOY.md](DEPLOY.md#showcase-data).

**History for real accounts** (`--history @handle,…`): gives named regular accounts a believable past, one
story each: *outdoors* (parks, the BeltLine, food halls), *nightlife* (live music, nights out, late bites)
or *arts* (museums, galleries, long walks), whichever best fits what they already like unless named
(`@handle=arts`). Each gets five finished sidequests over the last three weeks from real
`pitch_activities` places (some with showcase people), ratings of some stops made through the app's own
rating handler (so taste and insights pick them up), at least five stops left for Home's "past events to
rate", and liked categories raised or filled to match (never lowered). Their taste vectors are then rebuilt
like any account's. Everything is tagged `seed: "history-v1"`; the fields it changes (preferences, taste,
vectors) are saved once in `seed_backups` first, and `--history … --remove --apply` deletes the history and
puts those fields back exactly, except what the person changed themselves since. Demo, bot and showcase
accounts are refused. Each person also gets two **default upcoming sidequests** (`seed: "baseline-v1"`),
so Home is never empty: one they host with a showcase friend or two and a short chat, one a showcase person
hosts that they are in, in the next one to four days and clear of their classes; every `--apply` and
`--reset` makes them again with fresh dates.

**Class calendars** (`--calendar @handle,…`): a Georgia Tech fall 2026 semester per person in
`calendar_events` (busy blocks), matching their story: four classes with rooms, a recitation, homework
blocks, one or two extras (band practice, run club and climbing, studio and gallery shifts) and a few
one-offs (midterms, a due date, office hours), weekly from Aug 17 to Dec 11 without Labor Day, fall break
or Thanksgiving. Afternoons, most evenings and weekends stay free. Tagged `seed: "calendar-v1"`, fixed ids.

**Reset to the baseline** (`Backend/scripts/reset-user.sh @handle,…`): for practicing. It deletes what
the person made since the seeds (every sidequest they host that no seed wrote, their own from before the
seeds included, with its chat, notes, tickets, ratings, joins and checkouts; their places in other
people's plans; their joins to showcase plans; their "free now" posts, extra ratings and calendar events),
then puts back the history, interests and taste as seeded, the calendar and fresh default upcoming
sidequests. Their account, friendships and requests, direct messages, payment methods and other people's
own data stay; the dry run lists every item and who loses a plan of theirs.

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
- **Catalogs by role.** Demo/bot accounts use Saltlight; other accounts use the Atlanta pitch catalog and have no fixture friends.
- **One shared account.** Two judges signing in as Sandy at once share her plans and realtime events; she
  can hold 5 live sockets, and a sixth connection closes the oldest.

## Reset

Restore a fixture database backup to reset the demo records. To reset only the app session,
relaunch with `-SQResetSession YES`.
