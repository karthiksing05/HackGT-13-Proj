# The demo account

**Sandy Byte** (`@sandybyte`) is the shared account for showing SideQuests. She lives in Saltlight Harbor,
the fictional seaside city made for HackGT, and plans only from its `demo_activities` catalog (100 activities,
45 of them events on Sep 26 – Oct 2, 2026; [DATA.md](DATA.md#the-demo-snapshot)). Full walkthrough: [DEMO.md](DEMO.md).

## Signing in

- **Email:** `demo@sidequestz.tech`, on the live API `https://api.sidequestz.tech` (the app's default).
- **Password:** never in git. It is the `DEMO_PASSWORD` entry in the repo-root `.env` (gitignored); the
  server's `/opt/backend/.env` has the same entry, and `seed-demo` sets her password from it.
- **Use the demo account:** a link under "Create an account" on the sign-in screen that fills both fields
  and signs in. It shows only in live mode, when the build has the `SQ_DEMO_PASSWORD` build setting or the
  launch has `-SQDemoPassword` ([how](../frontend/README.md#pointing-the-app-at-a-server)).
- Anyone can sign up for their own account (real Atlanta catalog). Everyone signed in as Sandy shares her data.

## Sandy at a glance

| | |
|---|---|
| Home base | Seaside Market Square, Saltlight Harbor. Create starts there as a round trip, wherever the phone is |
| Profile | Saltlight Harbor College, status "Open to all", adult (born 2003-06-14) |
| Likes (1–5) | outdoors 5, long walks 5, live music 4, food 4, early mornings 4, museums 3, sports 3, shopping 2, nightlife 2, big crowds 1 |
| How she plans | small group, balanced pace, under $15 (a bit over is OK), prefers free, splits equally |
| In her words | a walk along the water, a market snack, small live music at sundown; never packed clubs, huge crowds or after-midnight plans; plans around sunrise swims and the Saturday market |
| Taste vectors | built by the live ML service from her preferences and rated stops when the seed runs |

## What's already there

Times are relative to when `seed-demo` ran, in `DEMO_TZ` (default `America/New_York`). For example,
seeded Sat Sep 26 → Marin's plan is Sun Sep 27, 5:30–8 PM, the crew outing was Sat Sep 19 and the solo
walk Fri Sep 25. Re-seed on the morning of a demo so "tomorrow" and "free now" still hold.

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
2. Tap **+** (it starts at Seaside Market Square), pick a window later today, tap **Outdoors** and **Music**.
3. On Review, hold a stop › **Swap for something similar**, then **Start this sidequest**.
4. **Forum**: Marin's plan › **Request to join** → "You're in · Open chat"; it is now on Home for tomorrow.
5. **Groups** › Saturday market crew › **Splits**: "You owe $2.00", and who owes whom.
6. **Account**: the taste bars, Home base, and **Friends** to accept Theo.

## Resetting

On the VPS, as root. The API (`sidequestz.service`) keeps running; no restart is needed.

```sh
/opt/backend/sidequestz-admin --env-file /opt/backend/.env seed-demo
```

It needs `DEMO_PASSWORD` in that file and the Saltlight catalog, or it writes nothing. It is idempotent:
every record it writes has a fixed id, so nothing is ever duplicated. It re-times the world to the new run and:

- **resets** Sandy's profile, preferences, home base and password, clears her learned taste tags and
  refreshes her taste vectors (if the ML service is down, it still succeeds and says so);
- **rewrites** the 3 plans, both chats (unread counts too), the 5 seeded messages, the 2 expenses, her 2 crew
  ratings, the Marin friendship, Theo's request (pending again), the free-now post and the Visa (default again);
- **undoes** her greenway rating, an accepted friendship with Theo and anyone's place in Marin's plan;
- **leaves** everything else a demo made: new plans, sent messages, added expenses (still counted in
  Splits), photos, other cards and the chat created by joining Marin's plan.

A full wipe deletes **every account** and all app data, not just the demo (the catalogs stay):

```sh
/opt/backend/sidequestz-admin --env-file /opt/backend/.env reset-app-data --yes --users
/opt/backend/sidequestz-admin --env-file /opt/backend/.env ensure-indexes   # recreate the dropped indexes
/opt/backend/sidequestz-admin --env-file /opt/backend/.env seed-demo
```

Without `--users` accounts survive, but everyone's plans, chats and friends still go ([DEPLOY.md](DEPLOY.md#runbook)).
