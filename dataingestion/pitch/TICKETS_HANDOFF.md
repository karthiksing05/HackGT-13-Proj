# Handoff: ticket listings for the Atlanta pitch catalog

For whoever builds the ticket site that sells the pitch catalog's paid items. State as of
2026-09-27. The catalog itself: [REPORT.md](REPORT.md) and [HANDOFF.md](HANDOFF.md).

## What's needed

`freetime.pitch_activities` (250 synthetic Atlanta activities for the pitch) now has **150 paid items**,
and each one carries a `ticketUrl`:

```
https://events.sidequestz.tech/{slug}/tickets
```

The app shows those items as bookable, and the checkout agent buys them from that URL. **None of the 150
slugs exists on the ticket site yet**, so every purchase fails until the site has a listing per slug.
The site today holds only the 25 Saltlight listings. The 100 free items have no ticket link and need
nothing.

The simplest route is to add the 150 listings to the existing `Events/` service. The backend only buys
from `MERCHANT_HOST` (`events.sidequestz.tech`), and the agent already speaks that service's API. A
separate site would need its own host added to the backend config.

## The contract the site must keep

The backend and the checkout agent depend on these; see `Backend/pkg/agent/merchant.go`.

- **Paths.**
  - `GET /{slug}` is the listing page.
  - `GET /{slug}/tickets` is the checkout page, the URL stored in the catalog.
  - `GET /api/events/{slug}/offer?quantity=N` returns a quote.
  - `POST /api/orders` places the order.
  - The agent reads the page text, its JSON-LD and `<link rel="agent-checkout">`, exactly as for
    Saltlight.
- **Slugs are fixed.** Each is lowercase ASCII with hyphens. Every date of a weekly series has its own
  slug, for example `bluegrass-brunch-sep-27` and `bluegrass-brunch-oct-4`. None collides with a Saltlight
  slug or a reserved one (`api`, `t`, `events`, `orders`, ...); the catalog validator checks both.
- **Price.**
  - The app charges a stop's `price.min`, so the ticket's `unit_cents` must equal the listing's minimum
    price, or the agent's quote won't match the plan's budget.
  - `max_unit_cents` is the top of the range, for a premium tier if the site wants one.
  - Fees are the site's own (`models.CalculateFees`), as today.
- **Times** are UTC in the data. Show them in Atlanta time (`America/New_York`).
- **Dates** run from Sun Sep 27 (the pitch day) to Sat Oct 10, 2026. Past listings shouldn't sell.

## The data

`dataingestion/pitch/out/ticket_listings.json` has one object per listing. Regenerate it with
`cd dataingestion && .venv/bin/python -m pitch.tickets`. The file is gitignored like the rest of
`pitch/out/`, because it carries addresses from Google Places records, so share it through the handoff
bundle and not through git.

Its fields map one to one onto `Events/pkg/models.Event`: `slug`, `title`, `description`, `summary`,
`category`, `tags`, `venue`, `address`, `starts_at`, `ends_at`, `unit_cents`, `max_unit_cents`, `capacity`,
`remaining` and `image_url` (empty; no approved images exist). A few extra fields help render the listing
correctly:

| Field | Meaning |
|---|---|
| `listing_type` | `event` (97), `reservation` (23 restaurants and cafés: a prepaid amount toward the bill), `admission_pass` (23 museums, venues and tours) or `cover` (7 bars and clubs) |
| `age_21_plus` | show a 21+ notice and ask the buyer to confirm |
| `series` | for a weekly series, e.g. "Fridays at 9 PM, Oct 2 – Oct 9, 2026" |
| `attendance` | `fixed_start` (arrive on time) or `drop_in` (come any time in the window) |
| `catalog_id`, `ticket_url` | the `pitch_activities` document and the exact URL the app will open |

**Places have no single date.** A reservation, pass or cover is valid on any day of the range during the
venue's opening hours. Their `starts_at`/`ends_at` span the whole range so that `models.Event` stays
valid. The page should say "Valid any day, Sep 27 – Oct 10" rather than show a showtime.

`capacity` is a rough room size per category for events and 1000 for places. `remaining` starts equal to it.

## What the site should look like

- **Home:** "Atlanta" listings next to Saltlight's, with the pitch day (Sun Sep 27) first. Group them by
  day and filter by category (music, nightlife, food, classes, stage, outdoors, culture).
- **Listing page:** title, venue and address, the Atlanta date and time or "valid any day", price or price
  range, 21+ badge, description and a Buy button. Keep the JSON-LD (`Event` or `Offer`) and the
  agent-checkout link that the Saltlight pages already have.
- **Checkout page:** as today. A quantity, the Stripe (test mode) payment step and the confirmation with a
  ticket and barcode.
- **Say it's a demo.** The events and prices are invented and the venues are real, so a footer line such
  as "Demo listings for SideQuests: not real tickets" keeps it honest.

## Checking it

```sh
# every listing answers (expect 150 lines of 200)
jq -r '.listings[].slug' dataingestion/pitch/out/ticket_listings.json | while read s; do
  curl -s -o /dev/null -w "%{http_code} $s\n" "https://events.sidequestz.tech/$s/tickets"; done | sort | uniq -c | sort -rn | head
# the quote matches the catalog price (unit_cents = price.min)
curl -s "https://events.sidequestz.tech/api/events/beer-biscuits-brunch/offer?quantity=2" | jq
```

Then make an end-to-end purchase from the app with an account on the `pitch_activities` catalog.

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
