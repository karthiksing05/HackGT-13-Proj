# The demo account

**Sandy Byte** (`@sandybyte`) is the shared account for showing SideQuests. She lives in Saltlight Harbor,
the fictional seaside city made for HackGT, and was originally populated from its `demo_activities` catalog (100 activities,
45 of them events on Sep 26 – Oct 2, 2026; [DATA.md](DATA.md#the-demo-snapshot)). Full walkthrough: [DEMO.md](DEMO.md).

## Signing in

- **Email:** `demo@sidequestz.tech`, on the live API `https://api.sidequestz.tech` (the app's default).
- **Password:** never in git. It is the `DEMO_PASSWORD` entry in the repo-root `.env` (gitignored); the
  app uses it for demo sign-in; the account password is already stored as a hash in MongoDB.
- **Use the demo account:** a link under "Create an account" on the sign-in screen that fills both fields
  and signs in. It shows only in live mode, when the build has the `SQ_DEMO_PASSWORD` build setting or the
  launch has `-SQDemoPassword` ([how](../frontend/README.md#pointing-the-app-at-a-server)).
- Anyone can sign up for their own account. Demo/bot roles use `demo_activities`; other accounts use `pitch_activities`. Everyone signed in as Sandy shares her data.

## Sandy at a glance

| | |
|---|---|
| Home base | Seaside Market Square, Saltlight Harbor. Create starts there as a round trip, wherever the phone is |
| Profile | Saltlight Harbor College, status "Open to all", adult (born 2003-06-14) |
| Likes (1–5) | outdoors 5, long walks 5, live music 4, food 4, early mornings 4, museums 3, sports 3, shopping 2, nightlife 2, big crowds 1 |
| How she plans | small group, balanced pace, under $15 (a bit over is OK), prefers free, splits equally |
| In her words | a walk along the water, a market snack, small live music at sundown; never packed clubs, huge crowds or after-midnight plans; plans around sunrise swims and the Saturday market |
| Taste vectors | built by the live ML service from her preferences and rated stops when preferences or ratings change |

## What's already there

The demo accounts live on the server's `DEMO_DATE` (2026-09-27, a Sunday: the catalog's busiest day,
17 events), at the real time of day in `DEMO_TZ` (`America/New_York`), whatever the real date is. Seed
times hang off that date: Marin's plan is Mon Sep 28, 5:30–8 PM, and the crew outing and the solo walk
were on Sat Sep 26. Expired fixture posts must be recreated through the app or restored from a fixture backup.

| Tab | What she sees |
|---|---|
| Home | "1 past event to rate": yesterday's solo **Heron Creek wander** (Heron Creek Greenway, 4 PM). **Past** also holds **Saturday market crew**, last Saturday morning with Marin and Theo, already rated (Seaside Market Hall 5★, Driftwood Coffee Roasters 4★). Nothing upcoming. |
| Forum | Marin's free-now post near Seaside Market (friends only): "Free until 9 PM", or 3 hours after the seed if later; it disappears then. His open plan **Golden hour by the market**: tomorrow 5:30–8 PM, a round-trip walk from the square to Seaside Market Hall, Salvage and Sons Vintage and Fish Box Karaoke; Marin and Theo are in, 4 of 6 spots left, joining locks at 5 PM. |
| Groups | **Saturday market crew**: 3 messages; Splits has Coffee $24 (Marin paid) and Bus fares $9 (Sandy paid), each split three ways, so "You owe $2" (she owes Marin $5, Theo owes her $3). A DM from Marin, 1 unread: "I'm hosting a golden hour walk tomorrow at 5:30. Join if you're free!" |
| Account | Taste profile, Home base, a demo Visa •••• 4242 (default; nothing real behind it), and Friends: Marin, plus Theo's pending request ("Met at the market"). |

**Marin Okafor** (`@marinokafor`, `marin@bots.sidequestz.tech`) and **Theo Park** (`@theopark`,
`theo@bots.sidequestz.tech`) are bots with random, unknown passwords: nobody signs in as them; they never reply.

## A 60-second showcase

1. Tap **Use the demo account**. On Home tap "1 past event to rate", then **Rate** the greenway and save.
2. Tap **+** (it starts at Seaside Market Square), keep today (Sunday, 17 events) with a window a few hours ahead, tap **Outdoors** and **Music**.
3. On Review, hold a stop › **Swap for something similar**, then **Start this sidequest**.
4. **Forum**: Marin's plan › **Request to join** → "You're in · Open chat"; it is now on Home for tomorrow.
5. **Groups** › Saturday market crew › **Splits**: "You owe $2.00", and who owes whom.
6. **Account**: the taste bars, Home base, and **Friends** to accept Theo.

## Resetting

The backend no longer includes seed/reset tooling. Restore a fixture database backup to reset
these records. Existing accounts, password hashes and app data are unaffected by removing the tool.
