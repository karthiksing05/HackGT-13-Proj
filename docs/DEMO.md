# The demo

Everything a judge sees comes from one seeded account in a fictional city. This page lists what is
seeded, the three-minute walkthrough, what is deliberately simulated, and how to reset. The seed lives in
`Backend/cmd/sidequestz-admin` (`seed-demo`); the city in `dataingestion/demo/`.

## Sandy Byte

| | |
|---|---|
| Account | **Sandy Byte**, `@sandybyte`, `demo@sidequestz.tech`; password = the server's `DEMO_PASSWORD` (not in the repo) |
| Profile | avatar sage, status open, born 2003-06-14 (adult), school "Saltlight Harbor College", setup complete |
| Catalog and city | `demo_activities`, `saltlight`; home base **Seaside Market Square** (31.3680, −81.4250) |
| Likes (1–5) | outdoors 5, long walks 5, live music 4, food 4, early mornings 4, museums 3, sports 3, shopping 2, nightlife 2, big crowds 1 |
| Preferences | small group, balanced pace, under $15, a bit over is ok, split equally, prefers free |
| Answers | perfect afternoon: "A long walk along the water, a snack from the market, then live music somewhere small while the sun goes down." · never: "Packed clubs, huge crowds, or anything that only gets going after midnight." · plans around: "Sunrise swims, the Saturday market, and whoever's free to wander." |
| Embeddings | computed through the ML service at seed time when it is reachable (logged otherwise) |

The sign-in link: with `SQ_DEMO_PASSWORD` set at build time (or `-SQDemoPassword` at launch) the
sign-in screen shows **Use the demo account** under "Create an account"; it fills both fields and signs
in. Sandy's phone is wherever the judge is; the planner snaps a start point more than 60 km from
Saltlight back to the Seaside Market, and the Create flow starts at her home base anyway.

## What is seeded

Times are relative to the seed run (`DEMO_TZ`, default `America/New_York`), so "tomorrow" stays
tomorrow after a reseed.

| Item | Detail |
|---|---|
| **Marin Okafor** (`@marinokafor`, bot) | Sandy's friend. Hosts an **open plan tomorrow 17:30–20:00** built from the three demo places nearest the market: `max_group_size 6`, joining locks at 17:00, members Marin and Theo, so it is on the Forum with spots left. Also has a **free-now post** for today |
| **Theo Park** (`@theopark`, bot) | a **pending friend request** to Sandy, note "Met at the market" |
| Past group sidequest (last Saturday) | Sandy, Marin and Theo; two stops rated by Sandy: 5 stars "Great people", "Would go again" and 4 stars "Good value" |
| Past solo sidequest | one **unrated** stop, so Home shows "1 past event to rate" and Past shows a Rate pill |
| Group thread **"Saturday market crew"** | 3 messages; expenses Marin $24.00 "Coffee" and Sandy $9.00 "Bus fares", split three ways, so Splits shows Sandy owing Marin $5.00 and Theo owing both |
| DM Sandy ↔ Marin | 2 messages, 1 unread |
| Payment method | a demo Visa •••• 4242 (nothing real behind it) |

The catalog: 100 Saltlight Harbor activities (45 events dated 2026-09-26 to 2026-10-03, 55 places:
trails, parks, a market, cafés, restaurants, bars, a museum, galleries, rec venues, tours, workshops),
all with vectors. Neighbourhoods: Lighthouse Point, Marina Row, The Shipyard, Deepwater Quarter, Seaside
Market, Kelp Hollow, Tidepool Heights, Lanternfall Beach. Details in [DATA.md](DATA.md).

## The three-minute walkthrough

Sign-in screen → **Use the demo account**. Home loads from the live server; a skeleton shows first and
the S logo appears only if the request takes more than 2 s.

1. **Home** (0:00). "1 past event to rate" at the top; no active sidequest yet, just "+ New sidequest".
   Tap **Past** to show the rated Saturday stops and the unrated one; tap **Rate**, pick stars, save
   (this nudges her taste profile). Back to **Sidequests**.
2. **Create** (0:30). Tap **+**. *Where*: the start is already **Seaside Market Square**, her home base;
   "End where I start" on; range **Walkable**; **No ride**. *When*: today, a window that ends this
   evening (or tomorrow afternoon; the demo events run to Oct 3). *Vibe*: type or dictate "chill and
   outside, then live music", quick picks **Outdoors** and **Music**, budget **$**, **Friends only**.
3. **Review** (1:00). The S loader plays while the planner runs, then three options: "Best match" first,
   each with its stops and "~$ · 1.2 mi walking" meta. Drag the ☰ handle to reorder: "Recalculating
   transit…" then updated times; if a fixed-start event ends up late, the route header says "Some stops
   would be late" and the stop reads "Late for a fixed start". Press and hold a stop → **Swap for
   something similar**: 3–5 alternatives with a reason ("Also time outside · 0.4 mi away"); pick one and
   the route re-times. **Start this sidequest**.
4. **Home again** (1:45). The new sidequest is selected, with its timeline of walks and stops. **Tap a
   block**: the Event sheet with the time, place, description, **Getting there** (walk / MARTA /
   rideshare estimates), Website, notes, and "Rate it after".
5. **Forum** (2:10). The area defaults to Saltlight Harbor around the market. Marin's open plan for
   tomorrow shows "2 of 6 spots left · Locks 5:00 PM"; **Request to join** answers instantly with
   "Joined": the plan appears on Home for tomorrow and a group chat exists. Marin's free-now post shows
   **Plan together**, which opens a DM with a first message already sent.
6. **Groups › Splits** (2:35). "Saturday market crew": Chat with the seeded messages, Album, and Splits
   showing "You owe $5.00" to Marin with the two expenses split equally. **+ Add an expense** ("Pizza",
   $40, split 3 ways) shows the cent-exact preview ($13.34 / $13.33 / $13.33) and, once saved, the
   banner "Everyone was notified".
7. **Account** (2:50). Her taste profile bars, **Home base** (Seaside Market Square, Saltlight Harbor),
   the connected demo Visa, and **Friends** with Theo's pending request ("Met at the market") to accept.

If the server is unreachable, `-SQAPIMode mock` runs the same screens offline on the app's built-in
Atlanta demo data (user Jordan Lee), without the live planner.

## Known limits

- **Simulated on purpose** (labelled in code and on the [roadmap](ROADMAP.md)): the agent checkout and
  its card page (a timer-driven state machine; "Approve purchase" never charges anything), calendar
  connect (marks connected, reads no calendar), password-reset delivery (the code goes to the server
  log), push notifications.
- **The bots do not answer.** Marin and Theo are seeded records; messages to them stay unanswered, and
  nobody else will join a judge's open plan unless a second account does.
- **The demo events are dated 2026-09-26 to 2026-10-03.** After that the planner only finds places in
  Saltlight; regenerate the snapshot (`python -m demo.generate` after moving `DAY0` in
  `dataingestion/demo/generate.py`) and reimport it to move the events.
- **Travel times are straight-line estimates** (4.5 km/h walking, 20 km/h transit plus 5 min, 30 km/h
  driving plus 5 min), and "MARTA" is simply the transit label; Saltlight has no transit data.
- **Facebook** works only for accounts added as testers of the Meta app while it is in development
  mode; the demo does not go through it.
- **One catalog per account.** Sandy sees Saltlight; a new account sees the real Atlanta catalog and
  gets no seeded friends.
- Two judges signing in as Sandy at once share the same account; each has up to 5 live sockets.

## Reset

On the VPS (the admin tool reads `/opt/backend/.env`):

```sh
set -a; . /opt/backend/.env; set +a
/opt/backend/sidequestz-admin reset-app-data --yes      # every app collection except the catalogs and users
/opt/backend/sidequestz-admin ensure-indexes
/opt/backend/sidequestz-admin drop-ttl demo_activities
/opt/backend/sidequestz-admin seed-demo                  # recreates Sandy, the bots, the plans, threads, expenses
```

`seed-demo` uses fixed `seed-` ids, so running it twice changes nothing; `reset-app-data --yes --users`
also removes accounts judges created. Locally the same commands are `go run ./cmd/sidequestz-admin …`
from `Backend/` with the environment from the [local loop](DEPLOY.md#local-development-loop). To reset only
the app on a phone, relaunch with `-SQResetSession YES`.
