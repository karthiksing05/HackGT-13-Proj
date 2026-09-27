# Handoff: the Atlanta events website and Stripe

For whoever builds the ticket website for the Atlanta pitch catalog and sets up its payments. State
as of 2026-09-27 (pitch day), production verified that morning. Background on the catalog:
[REPORT.md](REPORT.md).

## The goal

At the pitch, anyone who signs up for SideQuests plans from `freetime.pitch_activities`: 250 synthetic
Atlanta activities on real venues. Only the demo account, Sandy Byte, plans in the fictional Saltlight
Harbor. **150 of the 250 are paid**, and the demo shows buying them in two ways, both on Stripe test mode:

1. **Agentic checkout:** in the app, "Buy tickets for this plan" lets the SideQuests agent buy every
   paid stop, paying with a Stripe Shared Payment Token (SPT).
2. **By hand:** the stop's ticket link opens the website, and the buyer pays on Stripe Checkout.

Both go through the existing ticket site in `Events/` (`events.sidequestz.tech`, today branded
"Saltlight Tickets"). **What's missing is the 150 Atlanta listings.** Every paid pitch item links to
`https://events.sidequestz.tech/{slug}/tickets`, none of those slugs exists on the site yet, and so
every purchase of an Atlanta item currently fails with a 404. Saltlight's 25 listings work.

## What's already done

| Piece | State |
|---|---|
| Catalog | `pitch_activities`: 250 items with texts and vectors; the 150 paid ones carry `ticketUrl` |
| Backend routing | deployed: every account except the demo cast plans from `pitch_activities` |
| Ticket site | live at `events.sidequestz.tech` (`/opt/events`, port 8085, `events.service`), with 25 Saltlight listings |
| Stripe | configured in **test mode** on both sides (next section); a test order already went through |
| Listing data | `dataingestion/pitch/out/ticket_listings.json`: the 150 listings in the site's own format |

## Stripe: how it's set up

Two **separate** Stripe test accounts are needed, because Stripe won't let one account issue a
payment token to itself.

| Where | Setting | Role | State in production |
|---|---|---|---|
| Backend `/opt/backend/.env` | `STRIPE_SECRET_KEY` | the **agent** account (SideQuests): issues an SPT per purchase | set, `sk_test_…` |
| | `STRIPE_SELLER_PROFILE` | the **merchant** account's test profile (`profile_test_…`) that SPTs are issued for | set |
| | `PAYMENTS_MODE=sandbox`, `MERCHANT_HOST`, `MERCHANT_BASE_URL` | the agent only buys from `events.sidequestz.tech` | set |
| | `TAP_AGENT_KEY` | the agent's Ed25519 signing key (Visa Trusted Agent Protocol) | set |
| Ticket site `/opt/events/.env` | `STRIPE_MERCHANT_SECRET_KEY` | the **merchant** account: confirms PaymentIntents (agent orders) and creates Checkout Sessions (web orders) | set, `sk_test_…` |
| | `TAP_AGENT_PUBLIC_KEY` | verifies the agent's signatures; must pair with the backend's `TAP_AGENT_KEY` | set |
| | `PAYMENTS_MODE=sandbox`, `DEMO_KEY` | sandbox only; the key guards `POST /_demo/scenario` | set |

What that means for you:

- **You don't need to change Stripe to add the Atlanta listings.** Payments don't depend on the
  listing: a new slug is sellable as soon as it exists on the site.
- **Stay in test mode.** Both services refuse live keys. Pay by hand with the test card
  `4242 4242 4242 4242`, any future expiry and any CVC. The agent uses a saved demo card in the app.
- **Web checkout** creates a Stripe Checkout Session whose success URL is
  `https://events.sidequestz.tech/orders/complete?session_id={CHECKOUT_SESSION_ID}`. The site issues
  the ticket on that return, so no webhook is configured or needed.
- **Keys:** never commit them, and never paste them in chat. They live only in the two `.env` files
  (mode 600).
- **If you change a key:**
  - Changing `STRIPE_SECRET_KEY` or the merchant account means `STRIPE_SELLER_PROFILE` must name the
    new merchant account's profile. `Backend/scripts/stripe-spt-smoke.sh` prints it and proves the SPT
    path end to end.
  - Regenerating the TAP key means `TAP_AGENT_PUBLIC_KEY` must match it
    (`sidequestz-admin tap-keygen`).
  - Restart both services afterwards (`systemctl restart sidequestz events`).
- **Worth fixing:** `/opt/events/.env` has `APP_ENV=dev`, which lets the site fall back to the repo's
  demo TAP key. With `TAP_AGENT_PUBLIC_KEY` set that key isn't used, but `APP_ENV=prod` is safer.

## What to build

### 1. Add the 150 listings (required)

The site reads listings from MongoDB `sidequestz_events.merchant_events` (25 today). At startup it
seeds them idempotently from `DefaultDemoEvents` in `Events/pkg/store/seed_data.go`. There are two
ways to add Atlanta:

- **Seed from code (recommended):** generate a `seed_atlanta.go` from `ticket_listings.json` and seed
  it alongside Saltlight. It's reproducible and survives a database reset.
- **Insert into `merchant_events`:** quicker, but lost if that database is rebuilt.

Each JSON object maps onto `models.Event`:

| JSON field | `models.Event` | bson |
|---|---|---|
| `slug`, `title`, `description`, `summary`, `category`, `tags` | same names | same names |
| `venue`, `address` | `Venue`, `Address` | `venue`, `address` |
| `starts_at`, `ends_at` (UTC) | `Start`, `End` | `start`, `end` |
| `unit_cents`, `max_unit_cents` | `UnitCents`, `MaxUnitCents` | `unitCents`, `maxUnitCents` |
| `capacity`, `remaining` | `Capacity`, `Remaining` | `capacity`, `remaining` |
| `image_url` (empty) | `ImageURL` | `imageUrl` |

Fields the site should honour:

- **Keep every `slug` exactly**, including the date suffix on weekly series (`bluegrass-brunch-sep-27`,
  `bluegrass-brunch-oct-4`). The app links to them.
- **`unit_cents` must equal the catalog's minimum price.** The app budgets a stop at `price.min`, and
  the agent's quote has to match. `max_unit_cents` is the top of the range.
- `listing_type` is `event` (97), `reservation` (23 restaurants and cafés, a prepaid amount toward the
  bill), `admission_pass` (23 museums, venues and tours) or `cover` (7 bars and clubs).
  - Places have no showtime. Their window spans Sep 27 – Oct 10; show "Valid any day, Sep 27 – Oct 10".
- `age_21_plus`: show a 21+ badge and ask the buyer to confirm their age.
- `series`, for example "Fridays at 9 PM, Oct 2 – Oct 9, 2026", and `attendance`: `fixed_start`
  (arrive on time) or `drop_in` (come and go).

The file is gitignored because it carries addresses from Google Places. Regenerate it with
`cd dataingestion && .venv/bin/python -m pitch.tickets` (it reads `pitch/out/pitch_activities.json`),
or ask for a copy.

### 2. Design (the site is yours to shape)

- **Atlanta first.** Rebrand from "Saltlight Tickets" to something covering both cities, or show
  Atlanta on `/` with Saltlight as a second tab. Lead with the pitch day, Sun Sep 27: 16 ticketed
  events plus the day's passes.
- **Home:** listings grouped by day, with filter chips for music, nightlife, food and drink, classes,
  stage, outdoors and culture, plus search. Show price ranges, e.g. "$15–$20".
- **Listing page:** title, venue and address, Atlanta time (`America/New_York`) or "valid any day",
  price or range, 21+ badge, description and a Buy button.
- **Keep the machine-readable parts the agent relies on:**
  - JSON-LD `Event` (or `Offer` for passes) on the listing page;
  - the `<link rel="agent-checkout">` tag;
  - the checkout form at `/{slug}/tickets`.
  - The agent reads the page text, JSON-LD and that link (`Backend/pkg/agent/merchant.go`), then
    calls `GET /api/events/{slug}/offer` and `POST /api/orders`. Don't change those paths or
    payloads.
- **Checkout and ticket:** as today. Pick a quantity, pay on Stripe Checkout (test mode), and get a
  ticket page with a barcode at `/t/{id}`.
- **Honesty line:** the events and prices are invented and the venues are real, so add a footer such
  as "Demo listings for SideQuests: not real tickets".

### 3. Ship it

`Events/deploy.sh` builds and restarts `events.service` in `/opt/events`. Then check:

```sh
# every listing answers (expect 150 × 200)
jq -r '.listings[].slug' dataingestion/pitch/out/ticket_listings.json | while read s; do
  curl -s -o /dev/null -A curl -w "%{http_code}\n" "https://events.sidequestz.tech/$s/tickets"; done | sort | uniq -c
# the listing price is the catalog's minimum
curl -s -A curl "https://events.sidequestz.tech/beer-biscuits-brunch" | grep -o '\$[0-9.]*' | head -3
```

Then do an end-to-end run:
1. Sign up in the app with any new account; it plans in Atlanta.
2. Plan a Sunday afternoon with a paid stop and save it.
3. Tap "Buy tickets" and watch the agent buy the stop.
4. Also buy one by hand from the ticket link with the 4242 card.

Scenarios for the booth (`POST /_demo/scenario` with `X-Demo-Key`): `sold_out`, `price_bump`,
`overcharge` and `slow` show how the agent handles failures. Reset with `normal`.

## All 150 listings

### Events (97)

| When (Atlanta time) | Listing | Venue | Price | Slug |
|---|---|---|---|---|
| Sun Sep 27, 11 AM–1 PM | Beer & Biscuits Brunch | Monday Night Brewing - The Grove | $25–$30 | `beer-biscuits-brunch` |
| Sun Sep 27, 11:30 AM–1:30 PM | Bluegrass Brunch | The EARL | $20–$30 | `bluegrass-brunch-sep-27` |
| Sun Sep 27, 11:30 AM–1:30 PM | Sunday Jazz Brunch | Park Bar | $25–$35 | `sunday-jazz-brunch` |
| Sun Sep 27, 12 PM–1 PM | Family Puppet Show | Center For Puppetry Arts | $20–$25 | `family-puppet-show` |
| Sun Sep 27, 12:30 PM–2:30 PM | Drag Brunch | Lips Drag Queen Show Palace, Restaurant & Bar | $35–$45 | `drag-brunch-sep-27` |
| Sun Sep 27, 1:30 PM–2:30 PM | Create-A-Puppet Workshop | Center For Puppetry Arts | $15 | `create-a-puppet-workshop` |
| Sun Sep 27, 2 PM–3 PM | Make a Glass Paperweight | Decatur Glassblowing | $85–$95 | `make-a-glass-paperweight` |
| Sun Sep 27, 2 PM–4:30 PM | Shakespeare Comedy Matinee | Shakespeare Tavern Playhouse | $25–$45 | `shakespeare-comedy-matinee` |
| Sun Sep 27, 4 PM–9 PM | Amapiano Sunset Session (21+) | High Note Rooftop Bar | $15–$20 | `amapiano-sunset-session` |
| Sun Sep 27, 6 PM–8:30 PM | Sunday Supper: Beer Pairing Dinner (21+) | Monday Night Brewing - The Garage | $65–$75 | `sunday-supper-beer-pairing-dinner` |
| Sun Sep 27, 7 PM–10 PM | Soul & Funk Revue | Terminal West | $25–$35 | `soul-funk-revue` |
| Sun Sep 27, 7 PM–8:30 PM | Sunday Stand-Up Showcase | Laughing Skull Lounge | $20–$25 | `sunday-stand-up-showcase` |
| Sun Sep 27, 7:30 PM–10 PM | R&B Showcase | Buckhead Theatre | $20–$25 | `r-b-showcase` |
| Sun Sep 27, 8 PM–10:30 PM | Indie Rock Triple Bill | Aisle 5 | $15–$18 | `indie-rock-triple-bill` |
| Sun Sep 27, 8 PM–11 PM | Punk & Garage Night (21+) | The EARL | $12–$15 | `punk-garage-night` |
| Sun Sep 27, 10 PM–3 AM | Sunday Soul & Disco Night (21+) | Royal Peacock Lounge | $15 | `sunday-soul-disco-night` |
| Mon Sep 28, 6 PM–9 PM | Learn to Play: Tabletop RPG Night | Oxford Comics & Games | $10 | `learn-to-play-tabletop-rpg-night` |
| Mon Sep 28, 8 PM–10:30 PM | Alt-Rock Double Bill | The Masquerade - Altar | $18–$22 | `alt-rock-double-bill` |
| Mon Sep 28, 8 PM–9:30 PM | Monday Night Showcase | Laughing Skull Lounge | $15 | `monday-night-showcase` |
| Tue Sep 29, 6 PM–7:30 PM | Beginner Tennis Clinic | Piedmont Park | $25 | `beginner-tennis-clinic` |
| Tue Sep 29, 6:30 PM–8 PM | Cocktail Class: Classic Sours (21+) | Ranger Station | $50 | `cocktail-class-classic-sours` |
| Tue Sep 29, 7 PM–9:30 PM | Garden Harvest Dinner | Atlanta Botanical Garden | $95–$125 | `garden-harvest-dinner` |
| Tue Sep 29, 8 PM–10:30 PM | Americana Night | Variety Playhouse | $25–$35 | `americana-night` |
| Wed Sep 30, 5:30 PM–7 PM | Street-Art Photography Walk | Krog Street Tunnel | $30 | `street-art-photography-walk` |
| Wed Sep 30, 7 PM–9 PM | Personal Essay Writing Workshop | Charis Books & More | $25 | `personal-essay-writing-workshop` |
| Wed Sep 30, 7 PM–9 PM | Wine & Cheese Pairing Dinner (21+) | Side Saddle Wine Saloon & Bar | $55–$65 | `wine-cheese-pairing-dinner` |
| Wed Sep 30, 10 PM–2 AM | Midweek Dance Night (21+) | The Heretic Atlanta | $10 | `midweek-dance-night` |
| Thu Oct 1, 6 PM–7:30 PM | Curator-Led Gallery Tour After Hours | High Museum of Art | $25–$30 | `curator-led-gallery-tour-after-hours` |
| Thu Oct 1, 6:30 PM–8 PM | Wine Tasting 101 (21+) | Taste Wine Bar and Market | $40 | `wine-tasting-101` |
| Thu Oct 1, 7 PM–9 PM | Pickleball Round-Robin Night | Painted Pickle | $20 | `pickleball-round-robin-night` |
| Thu Oct 1, 7 PM–9:30 PM | Rooftop Supper Club | The Roof at Ponce City Market | $85–$95 | `rooftop-supper-club` |
| Thu Oct 1, 8 PM–9:30 PM | Long-Form Improv Night | Whole World Improv Theatre | $15–$20 | `long-form-improv-night` |
| Fri Oct 2, 12 PM–1:30 PM | Lunchtime Architecture Walk | Woodruff Park | $20 | `lunchtime-architecture-walk` |
| Fri Oct 2, 5 PM–6 PM | Behind-the-Scenes Aquarium Tour | Georgia Aquarium | $60–$70 | `behind-the-scenes-aquarium-tour` |
| Fri Oct 2, 8 PM–9:45 PM | Headliner Stand-Up | The Punchline Comedy Club | $25–$35 | `headliner-stand-up` |
| Fri Oct 2, 8 PM–11 PM | Hip-Hop Showcase | Tabernacle | $30–$45 | `hip-hop-showcase` |
| Fri Oct 2, 8 PM–12 AM | Silent Disco on the Roof | The Roof at Ponce City Market | $20 | `silent-disco-on-the-roof` |
| Fri Oct 2, 9 PM–1 AM | Salsa & Bachata Fridays (21+) | Havana Nightclub ATL | $15 | `salsa-bachata-fridays-oct-2` |
| Fri Oct 2, 11 PM–4 AM | House & Disco Friday (21+) | MJQ Concourse | $15–$20 | `house-disco-friday` |
| Sat Oct 3, 8 AM–11 AM | Community Pancake Breakfast | Decatur Square | $10 | `community-pancake-breakfast` |
| Sat Oct 3, 8 AM–9:30 AM | Fall 5K Fun Run | Piedmont Park | $35–$40 | `fall-5k-fun-run` |
| Sat Oct 3, 9 AM–10:30 AM | Latte Art Basics | Café Belli, Coffee & Cocktail Bar | $35 | `latte-art-basics` |
| Sat Oct 3, 10 AM–12 PM | Donut & Coffee Walking Tour | Krog Street Tunnel | $35–$40 | `donut-coffee-walking-tour-oct-3` |
| Sat Oct 3, 10 AM–12 PM | Wheel-Throwing Taster | Callanwolde Fine Arts Center | $55 | `wheel-throwing-taster-oct-3` |
| Sat Oct 3, 12 PM–10 PM | Oktoberfest Party (21+) | Halfway Crooks Beer | $15–$20 | `oktoberfest-party` |
| Sat Oct 3, 5 PM–6:15 PM | Family Magic Show | Atlanta Magic Theater | $25–$30 | `family-magic-show` |
| Sat Oct 3, 5 PM–7 PM | Guided Sunset Kayak on the Chattahoochee | Paces Mill | $55–$65 | `guided-sunset-kayak-on-the-chattahoochee` |
| Sat Oct 3, 6 PM–9 PM | Murder Mystery Dinner Show | The Dinner Detective | $70–$90 | `murder-mystery-dinner-show` |
| Sat Oct 3, 6:30 PM–9 PM | Low-Country Boil on the Patio | Wild Heaven Beer: West End Brewery & Gardens | $45–$55 | `low-country-boil-on-the-patio` |
| Sat Oct 3, 8 PM–10:30 PM | Broadway Hits in Concert | Fox Theatre | $45–$125 | `broadway-hits-in-concert` |
| Sat Oct 3, 8 PM–10:30 PM | Latin Jazz Orchestra | Buckhead Theatre | $30–$55 | `latin-jazz-orchestra` |
| Sat Oct 3, 9 PM–1 AM | Country Night & Line Dancing Lesson (21+) | PBR Atlanta | $10 | `country-night-line-dancing-lesson` |
| Sat Oct 3, 10 PM–3 AM | Warehouse Techno Night (21+) | Pisces | $15–$25 | `warehouse-techno-night` |
| Sun Oct 4, 9:30 AM–11:30 AM | Guided BeltLine Bike Tour | Beltline Shed at Ponce City Market | $45 | `guided-beltline-bike-tour` |
| Sun Oct 4, 10 AM–12 PM | Watercolor in the Garden | Atlanta Botanical Garden | $55 | `watercolor-in-the-garden` |
| Sun Oct 4, 11 AM–1:30 PM | Rooftop Brunch | The Roof at Ponce City Market | $35–$45 | `rooftop-brunch` |
| Sun Oct 4, 11:30 AM–1:30 PM | Bluegrass Brunch | The EARL | $20–$30 | `bluegrass-brunch-oct-4` |
| Sun Oct 4, 12 PM–2 PM | Drag Bingo Brunch | The Midway Pub | $20–$25 | `drag-bingo-brunch` |
| Sun Oct 4, 12:30 PM–2:30 PM | Drag Brunch | Lips Drag Queen Show Palace, Restaurant & Bar | $35–$45 | `drag-brunch-oct-4` |
| Sun Oct 4, 1 PM–2:30 PM | Terrarium Workshop | Woodlands Garden | $40 | `terrarium-workshop` |
| Sun Oct 4, 3 PM–5:30 PM | Contemporary Play Matinee | Horizon Theatre Company | $35–$50 | `contemporary-play-matinee` |
| Sun Oct 4, 3 PM–9 PM | Sunday Day Party on the Patio (21+) | Westside Motor Lounge | $20–$30 | `sunday-day-party-on-the-patio` |
| Sun Oct 4, 5 PM–7:30 PM | Pig Roast & Bluegrass Supper | SweetWater Brewing Company | $40–$50 | `pig-roast-bluegrass-supper` |
| Sun Oct 4, 7 PM–9:30 PM | Folk Singer-Songwriter Night | Variety Playhouse | $25–$30 | `folk-singer-songwriter-night` |
| Mon Oct 5, 8 PM–11 PM | Metal Triple Bill | The Masquerade - Hell | $20–$25 | `metal-triple-bill` |
| Mon Oct 5, 8 PM–10 PM | Singer-Songwriter Showcase | Vinyl | $15 | `singer-songwriter-showcase` |
| Tue Oct 6, 6:30 PM–7:30 PM | Rooftop Pilates | Skyline Park | $25 | `rooftop-pilates` |
| Tue Oct 6, 8 PM–11 PM | Jam Band Night | Terminal West | $20–$25 | `jam-band-night` |
| Tue Oct 6, 8 PM–9:30 PM | Sketch Comedy Night | Vinyl | $15 | `sketch-comedy-night` |
| Wed Oct 7, 6 PM–8:30 PM | Screen-Printing Workshop | MET Atlanta | $60 | `screen-printing-workshop` |
| Wed Oct 7, 7 PM–9 PM | Acoustic Duo in the Listening Room | Eddie's Attic | $15–$20 | `acoustic-duo-in-the-listening-room` |
| Wed Oct 7, 7 PM–9:30 PM | Italian Wine Dinner (21+) | Taste Wine Bar and Market | $60–$75 | `italian-wine-dinner` |
| Wed Oct 7, 8 PM–10:30 PM | Jazz-Funk Fusion Night | Aisle 5 | $15–$20 | `jazz-funk-fusion-night` |
| Thu Oct 8, 5 PM–7 PM | Design Lab: Intro to 3D Printing | MODA (Museum of Design Atlanta) | $35–$45 | `design-lab-intro-to-3d-printing` |
| Thu Oct 8, 6:30 PM–8:30 PM | Glass Mosaic Workshop | Callanwolde Fine Arts Center | $45 | `glass-mosaic-workshop` |
| Thu Oct 8, 7 PM–9:30 PM | Chef Collaboration Dinner (21+) | Three Taverns Imaginarium | $70–$85 | `chef-collaboration-dinner` |
| Thu Oct 8, 7:30 PM–10 PM | New Play Premiere | Alliance Theatre | $35–$75 | `new-play-premiere` |
| Thu Oct 8, 8 PM–10:30 PM | Neo-Soul Night | Center Stage Theater | $25–$40 | `neo-soul-night` |
| Thu Oct 8, 8 PM–10 PM | Romantic Masterworks | Atlanta Symphony Hall | $35–$110 | `romantic-masterworks` |
| Thu Oct 8, 8 PM–11 PM | Synth-Pop Night | The Masquerade - Purgatory | $22–$30 | `synth-pop-night` |
| Thu Oct 8, 9 PM–1 AM | Thursday Deep House Session (21+) | Lore | $10 | `thursday-deep-house-session` |
| Fri Oct 9, 7 PM–10:30 PM | Pop-Punk Revival | The Masquerade - Heaven | $25–$35 | `pop-punk-revival` |
| Fri Oct 9, 7 PM–9:30 PM | Rooftop Oyster Roast (21+) | High Note Rooftop Bar | $50–$70 | `rooftop-oyster-roast` |
| Fri Oct 9, 7:30 PM–10 PM | Symphonic Film Music Concert | Fox Theatre | $45–$95 | `symphonic-film-music-concert` |
| Fri Oct 9, 8 PM–9:30 PM | Close-Up Magic Late Show | Atlanta Magic Theater | $35–$40 | `close-up-magic-late-show` |
| Fri Oct 9, 9 PM–1 AM | Salsa & Bachata Fridays (21+) | Havana Nightclub ATL | $15 | `salsa-bachata-fridays-oct-9` |
| Fri Oct 9, 10 PM–3 AM | Afrobeats Friday (21+) | Royal Peacock Lounge | $20 | `afrobeats-friday` |
| Fri Oct 9, 11:30 PM–1:30 AM | Midnight Cult Classic Screening | Plaza Theatre | $12–$15 | `midnight-cult-classic-screening` |
| Sat Oct 10, 10 AM–12 PM | Donut & Coffee Walking Tour | Krog Street Tunnel | $35–$40 | `donut-coffee-walking-tour-oct-10` |
| Sat Oct 10, 10 AM–12 PM | Wheel-Throwing Taster | Callanwolde Fine Arts Center | $55 | `wheel-throwing-taster-oct-10` |
| Sat Oct 10, 10:30 AM–12 PM | Sketching in the Galleries | High Museum of Art | $30–$35 | `sketching-in-the-galleries` |
| Sat Oct 10, 11 AM–1 PM | Improv for Absolute Beginners | Whole World Improv Theatre | $30 | `improv-for-absolute-beginners` |
| Sat Oct 10, 2 PM–4 PM | Homebrewing 101 (21+) | SweetWater Brewing Company | $35 | `homebrewing-101` |
| Sat Oct 10, 7 PM–9:30 PM | Candlelit Dinner at the Swan House | Swan House at Atlanta History Center | $110–$150 | `candlelit-dinner-at-the-swan-house` |
| Sat Oct 10, 7:30 PM–9:30 PM | Fall Dance Program | Rialto Center for the Arts at Georgia State University | $25–$45 | `fall-dance-program` |
| Sat Oct 10, 8 PM–11:30 PM | Indie Dance Night | The Eastern | $25 | `indie-dance-night` |
| Sat Oct 10, 11 PM–4 AM | Bass Music Night (21+) | Lunchbox | $10–$15 | `bass-music-night` |


### Places (53)

| Place | Sells | Price | Slug |
|---|---|---|---|
| Activate Games | admission pass | $25–$35 | `activate-games` |
| Apex Museum | admission pass | $7–$10 | `apex-museum` |
| Atlanta Botanical Garden | admission pass | $22–$28 | `atlanta-botanical-garden` |
| Atlanta History Center | admission pass | $24–$28 | `atlanta-history-center` |
| Beat The Bomb Atlanta | admission pass | $35–$45 | `beat-the-bomb-atlanta` |
| Center For Puppetry Arts | admission pass | $15–$20 | `center-for-puppetry-arts` |
| Cherokee Trail at Stone Mountain Park | admission pass | $20 | `cherokee-trail-at-stone-mountain-park` |
| Cochran Shoals | admission pass | $5 | `cochran-shoals` |
| East Palisades | admission pass | $5 | `east-palisades` |
| Fernbank Museum | admission pass | $25–$30 | `fernbank-museum` |
| Georgia Aquarium | admission pass | $45–$55 | `georgia-aquarium` |
| High Museum of Art | admission pass | $19 | `high-museum-of-art` |
| Jimmy Carter Presidential Library and Museum | admission pass | $12 | `jimmy-carter-presidential-library-and-museum` |
| MODA (Museum of Design Atlanta) | admission pass | $12–$15 | `moda-museum-of-design-atlanta` |
| Michael C. Carlos Museum | admission pass | $8 | `michael-c-carlos-museum` |
| Midtown Bowl | admission pass | $10–$25 | `midtown-bowl` |
| Painted Pickle | admission pass | $15–$25 | `painted-pickle` |
| Puttshack Midtown | admission pass | $15–$25 | `puttshack-midtown` |
| Sandbox VR | admission pass | $40–$55 | `sandbox-vr` |
| Skyline Park | admission pass | $15–$20 | `skyline-park` |
| The Painted Duck | admission pass | $10–$25 | `the-painted-duck` |
| White Trail at Sweetwater Creek State Park | admission pass | $5 | `white-trail-at-sweetwater-creek-state-park` |
| Zoo Atlanta | admission pass | $30–$35 | `zoo-atlanta` |
| Blind Willie's (21+) | cover | $10–$20 | `blind-willie-s` |
| Lore (21+) | cover | $10–$50 | `lore` |
| MJQ Concourse (21+) | cover | $20–$30 | `mjq-concourse` |
| Northside Tavern (21+) | cover | $20–$30 | `northside-tavern` |
| PBR Atlanta (21+) | cover | $10–$50 | `pbr-atlanta` |
| Royal Peacock Lounge (21+) | cover | $20–$30 | `royal-peacock-lounge` |
| The Heretic Atlanta (21+) | cover | $20–$30 | `the-heretic-atlanta` |
| 42 Bar and Grill | reservation | $20–$60 | `42-bar-and-grill` |
| Bantam Pub | reservation | $20–$30 | `bantam-pub` |
| Brick Store Pub | reservation | $20–$30 | `brick-store-pub` |
| Café Belli, Coffee & Cocktail Bar | reservation | $10–$20 | `cafe-belli-coffee-cocktail-bar` |
| Cypress Street Pint & Plate | reservation | $20–$30 | `cypress-street-pint-plate` |
| Eleventh Street Pub | reservation | $20–$30 | `eleventh-street-pub` |
| Fado Irish Pub | reservation | $20–$30 | `fado-irish-pub` |
| Manny's - Grant Park | reservation | $10–$20 | `manny-s-grant-park` |
| Manuel's Tavern | reservation | $10–$20 | `manuel-s-tavern` |
| Marlow's Tavern | reservation | $20–$30 | `marlow-s-tavern` |
| Melton's App & Tap | reservation | $10–$20 | `melton-s-app-tap` |
| Milltown Arms Tavern | reservation | $10–$20 | `milltown-arms-tavern` |
| Moe's and Joe's | reservation | $10–$20 | `moe-s-and-joe-s` |
| Mr. C's Bar & Grill | reservation | $10–$20 | `mr-c-s-bar-grill` |
| New Realm Brewing Co. | reservation | $15–$35 | `new-realm-brewing-co` |
| North Highland Pub | reservation | $10–$30 | `north-highland-pub` |
| Steinbeck's | reservation | $10–$20 | `steinbeck-s` |
| Sugar Factory American Brasserie | reservation | $20–$40 | `sugar-factory-american-brasserie` |
| The Imperial | reservation | $20–$30 | `the-imperial` |
| The Porter Beer Bar | reservation | $20–$30 | `the-porter-beer-bar` |
| Thinking Man Tavern | reservation | $20–$30 | `thinking-man-tavern` |
| Three Arches Bar & Restaurant | reservation | $15–$30 | `three-arches-bar-restaurant` |
| Two Best Friends Cafe & Books | reservation | $5–$15 | `two-best-friends-cafe-books` |
