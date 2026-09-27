"""Build the fake HackGT 13 demo city, Saltlight Harbor, as an activities snapshot.

Everything here is invented: the city, venues, addresses and events. The theme follows
HackGT 13's seaside harbor (Oracle of the Deep, The Shipyard, The Lighthouse Laboratory,
A Marina's Mission, the Seaside Market). Output loads with:

    python -m demo.generate
    python -m ingest snapshot import demo/saltlight_harbor.json
"""

import hashlib
import json
import math
from datetime import datetime, timedelta, timezone
from pathlib import Path
from zoneinfo import ZoneInfo

from bson import ObjectId, json_util

from ingest.enrich.duration import EVENT_TIMES_SIGMA, MAX_FIXED_SPAN_MIN, prior
from ingest.enrich.price import price_from_range
from ingest.models import (
    Activity, Address, Duration, GeoPoint, HoursInterval, SourceRef, Trail,
)

CITY = "saltlight"
TZ = ZoneInfo("America/New_York")
TIERS = [0, 15, 40, 80]
DAY0 = datetime(2026, 9, 26, tzinfo=TZ)  # Saturday; offsets below are days from this
FETCHED = datetime(2026, 9, 26, 12, 0, tzinfo=timezone.utc)
OUT = Path(__file__).parent / "saltlight_harbor.json"

# neighborhood -> (lat, lng, street names used for fake addresses)
HOODS = {
    "Lighthouse Point": (31.395, -81.395, ["Lantern Ln", "Beacon Way"]),
    "Marina Row": (31.372, -81.418, ["Marina Row", "Mooring St"]),
    "The Shipyard": (31.362, -81.432, ["Keel St", "Rivet Ave"]),
    "Deepwater Quarter": (31.378, -81.440, ["Fathom St", "Sonar Ave"]),
    "Seaside Market": (31.368, -81.425, ["Stall St", "Brine Ave"]),
    "Kelp Hollow": (31.350, -81.410, ["Kelp Rd", "Heron Ct"]),
    "Tidepool Heights": (31.385, -81.455, ["Coral Rd", "Urchin Hill"]),
    "Lanternfall Beach": (31.360, -81.395, ["Dune Dr", "Gull Way"]),
}
POSTAL = {h: f"3199{i}" for i, h in enumerate(HOODS)}
DAYS = {"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}


def _hash(s: str) -> int:
    return int(hashlib.sha1(s.encode()).hexdigest(), 16)


def slug(name: str) -> str:
    return "".join(c if c.isalnum() else "-" for c in name.lower()).strip("-").replace("--", "-")


def spot(hood: str, name: str, spread: float = 0.006) -> tuple[GeoPoint, Address]:
    lat, lng, streets = HOODS[hood]
    h = _hash(name)
    lat += ((h % 1000) / 1000 - 0.5) * 2 * spread
    lng += (((h // 1000) % 1000) / 1000 - 0.5) * 2 * spread
    street = f"{10 + h % 890} {streets[h % len(streets)]}"
    addr = Address(
        formatted=f"{street}, Saltlight Harbor, GA {POSTAL[hood]}", street=street,
        locality="Saltlight Harbor", region="GA", postalCode=POSTAL[hood], countryCode="US",
    )
    return GeoPoint.at(round(lat, 6), round(lng, 6)), addr


def hours(spec: str) -> list[HoursInterval]:
    """'daily 09:00-17:00' or 'tue-sun 11:00-02:00' (close before open = past midnight)."""
    days, span = spec.split()
    o, c = [int(t[:2]) * 60 + int(t[3:]) for t in span.split("-")]
    if days == "daily":
        idx = range(7)
    else:
        a, b = (DAYS[d] for d in days.split("-"))
        idx = [d % 7 for d in range(a, b + 1 if b >= a else b + 8)]
    out = []
    for d in idx:
        start = d * 1440 + o
        end = d * 1440 + c + (1440 if c <= o else 0)
        out.append(HoursInterval(open=start, close=end % 10080))
    return out


def popularity(name: str, rating_count: int | None) -> float:
    base = math.log10((rating_count or 50) + 1) / 4
    return round(min(1.0, base + (_hash(name) % 20) / 100), 2)


# DEMO_BASE is where the fictional city's places live (no such site);
# MERCHANT_BASE is the sandbox ticket merchant (Events/, events.sidequestz.tech),
# which serves a page for every ticketed event and sells its tickets to the
# SideQuestz checkout agent.
DEMO_BASE = "https://saltlight.example"
MERCHANT_BASE = "https://events.sidequestz.tech"


def source(name: str, base: str = DEMO_BASE) -> dict:
    s = slug(name)
    url = f"{base}/{s}"
    return dict(url=url, sourceKeys=[f"demo:{s}"], sources=[SourceRef(name="demo", id=s, url=url, fetchedAt=FETCHED)])


# ---------------------------------------------------------------- events
# (name, category, tags, hood, venue, day offset, "HH:MM", minutes, (min, max) price, description)
EVENTS = [
    # Saturday Sep 26
    ("Oracle of the Deep: Late-Night Data Seance", "class_workshop", ["indoor", "free", "learning", "late_night", "group", "medium_energy"], "Deepwater Quarter", "The Abyss Lecture Hall", 0, "20:00", 120, (0, 0),
     "Local data scientists pull strange signals from Saltlight's tide gauges and whale-song archive, then hand the room a notebook to find the next anomaly. Laptops encouraged; pizza shaped like anglerfish provided."),
    ("Sunset Jazz on Pier Nine", "live_music", ["outdoor", "cheap", "music", "date", "drinks", "low_energy"], "Marina Row", "Pier Nine Bandstand", 0, "18:30", 150, (12, 20),
     "The Brass Barnacles quartet plays standards as the sun drops behind the moored sailboats. Bring a blanket; the pier bar pours local cider."),
    ("Barnacle Bash Silent Disco", "nightclub", ["indoor", "late_night", "music", "high_energy", "group", "21_plus", "drinks"], "The Shipyard", "Drydock No. 3", 0, "22:00", 240, (15, 25),
     "Three headphone channels, three DJs, one echoing drydock. Switch between sea-punk, deep house and 2000s throwbacks until 2 a.m."),
    ("Kelp Forest Kayak Cleanup", "community_event", ["outdoor", "free", "active", "nature", "group", "daytime", "high_energy"], "Kelp Hollow", "Kelp Hollow Launch", 0, "09:00", 180, (0, 0),
     "Paddle the kelp channels with A Marina's Mission volunteers, fishing out plastic and logging it for a debris-mapping project. Kayaks, gloves and snacks provided."),
    ("Saturday Seaside Market", "market", ["outdoor", "free", "food", "daytime", "family", "local_favorite", "low_energy"], "Seaside Market", "Seaside Market Square", 0, "08:00", 360, (0, 0),
     "Over sixty stalls under striped awnings: dayboat fish on ice, flower bundles, pickled everything, hand-thrown pottery and a guy who sharpens knives while you wait."),
    ("Seasick Standup at the Crow's Nest", "comedy", ["indoor", "cheap", "late_night", "drinks", "21_plus", "group", "medium_energy"], "Marina Row", "The Crow's Nest", 0, "21:00", 100, (10, 15),
     "Saltlight's rowdiest comedy night: five comics, one host in a captain's hat, and a rule that every set must mention a fish."),
    # Sunday Sep 27
    ("Lighthouse Laboratory Open House: VR Shipwreck Dive", "community_event", ["indoor", "free", "learning", "family", "touristy", "daytime", "low_energy"], "Lighthouse Point", "The Lighthouse Laboratory", 1, "12:00", 360, (0, 0),
     "Put on a headset and explore a 1890s schooner wreck rebuilt from sonar scans, then try the AR tide-pool table. Student XR projects demoed all afternoon."),
    ("Shipyard Robot Regatta", "sports_event", ["outdoor", "cheap", "family", "group", "learning", "daytime", "medium_energy"], "The Shipyard", "Slipway Basin", 1, "13:00", 150, (5, 10),
     "Home-built autonomous boats race a buoy course across the basin. Watch from the seawall or enter your own bot in the open heat at 2 p.m."),
    ("Tidepool Walk with a Marine Biologist", "tour", ["outdoor", "free", "nature", "learning", "family", "daytime", "low_energy"], "Tidepool Heights", "Urchin Rocks", 1, "14:00", 90, (0, 0),
     "Low tide peaks at 2:40. Dr. Nia Okafor leads a slow scramble over the rocks to find sea stars, anemones and the occasional octopus."),
    ("Harbor Dumpling Brunch Crawl", "tour", ["outdoor", "food", "group", "daytime", "medium_energy", "local_favorite"], "Seaside Market", "Meets at the Fish Box Clock", 1, "11:00", 180, (35, 45),
     "Four stops, four kinds of dumpling: crab soup dumplings, shrimp har gow, pierogi and a dessert bao. Walking pace, ends at the market."),
    ("Paint the Lighthouse: Sip and Paint", "class_workshop", ["indoor", "art", "date", "drinks", "21_plus", "low_energy", "daytime"], "Lighthouse Point", "Beacon Studio", 1, "15:00", 120, (38, 45),
     "Canvas, brushes and a glass of wine; an instructor walks you through painting the Old Saltlight Lighthouse from the studio window."),
    ("Saltlight Mariners vs. Port Coral FC", "sports_event", ["outdoor", "group", "high_energy", "drinks", "touristy"], "Deepwater Quarter", "Mariners Stadium", 1, "16:00", 120, (18, 60),
     "The Mariners host their coastal rivals in the last home match of the season. Supporters' section sings shanties the whole second half."),
    ("Sea Shanty Singalong at The Rusty Anchor", "live_music", ["indoor", "free", "music", "drinks", "group", "local_favorite", "medium_energy"], "Marina Row", "The Rusty Anchor", 1, "17:00", 120, (0, 0),
     "Lyric sheets on every table, a fiddler and a concertina. No experience needed, just volume."),
    ("Bioluminescence Night Paddle", "tour", ["outdoor", "nature", "date", "late_night", "active", "splurge", "medium_energy"], "Kelp Hollow", "Harbor Kayak and SUP", 1, "20:00", 120, (45, 55),
     "Glide through the marsh on a clear-bottom kayak while plankton light up every paddle stroke. Guided, beginner-friendly, limited to 12."),
    ("Sunday Beach Volleyball Open Play", "community_event", ["outdoor", "free", "active", "high_energy", "group", "daytime", "solo_friendly"], "Lanternfall Beach", "Lanternfall Courts", 1, "13:00", 180, (0, 0),
     "Show up, get put on a team. Six courts, all levels, rotating every 20 minutes. Bring sunscreen."),
    ("Harbor Stories: Film Under the Stars", "cinema", ["outdoor", "free", "family", "date", "low_energy"], "Lanternfall Beach", "Lanternfall Beach Lawn", 1, "19:30", 120, (0, 0),
     "A giant inflatable screen on the sand plays a classic ocean adventure. Popcorn cart and blankets; arrive early for the front rows."),
    ("Pop the Balloon: Harbor Speed-Friending", "community_event", ["indoor", "cheap", "group", "solo_friendly", "medium_energy", "drinks"], "Seaside Market", "Market Hall Mezzanine", 1, "18:00", 90, (5, 5),
     "New in town or just want more crew? Short rounds of conversation, then pop a balloon to reveal your next question. Mocktails included."),
    ("Sunday Makers' Swap Meet", "market", ["outdoor", "free", "cheap", "learning", "daytime", "solo_friendly", "low_energy"], "The Shipyard", "Shipyard Loading Dock", 1, "12:00", 240, (0, 0),
     "Trade old microcontrollers, 3D-printer filament, vintage radios and mystery cables. Free soldering station for quick repairs."),
    ("Poetry by the Tide", "community_event", ["outdoor", "free", "art", "low_energy", "solo_friendly", "daytime"], "Lanternfall Beach", "Driftwood Amphitheater", 1, "15:30", 75, (0, 0),
     "An open reading on the driftwood benches. Sign up for a five-minute slot or just listen to the waves and the verses."),
    ("AI for Good Roundtable", "class_workshop", ["indoor", "free", "learning", "daytime", "solo_friendly", "low_energy"], "Deepwater Quarter", "Harbor Library", 1, "14:00", 90, (0, 0),
     "Researchers and community organizers talk through using ML for flood forecasting and fisheries health, and where it goes wrong. Q&A after."),
    ("Drag Brunch Ahoy!", "theater", ["indoor", "food", "drinks", "21_plus", "group", "high_energy", "daytime"], "Marina Row", "Siren Song Lounge", 1, "12:00", 120, (35, 45),
     "Mermaid-themed drag numbers, bottomless mimosas and a crab-cake benedict. Reservations fill up; tips go to the queer youth sailing club."),
    ("Boardwalk Pickleball Round-Robin", "community_event", ["outdoor", "cheap", "active", "group", "daytime", "medium_energy"], "Lanternfall Beach", "Boardwalk Courts", 1, "14:30", 120, (8, 8),
     "Paddles provided, partners assigned. Fast games, a leaderboard, and lemonade for the winners."),
    ("Chess on the Boardwalk", "community_event", ["outdoor", "free", "low_energy", "solo_friendly", "learning", "daytime"], "Lanternfall Beach", "Boardwalk Pavilion", 1, "13:00", 180, (0, 0),
     "A dozen boards set up by the Saltlight Chess Club. Blitz, casual games or a lesson from a regular. Drop in any time."),
    # Monday Sep 28
    ("Marina's Mission Health-Tech Meetup", "class_workshop", ["indoor", "free", "learning", "group", "medium_energy"], "Marina Row", "Harbormaster's Hall", 2, "18:30", 120, (0, 0),
     "Clinicians from Saltlight General pitch real problems; builders pitch prototypes. Lightning demos, then a mixer with snacks."),
    ("Twenty Thousand Questions Under the Sea: Trivia", "community_event", ["indoor", "free", "drinks", "group", "21_plus", "late_night", "medium_energy"], "Marina Row", "The Rusty Anchor", 2, "20:00", 120, (0, 0),
     "Six rounds of trivia with an ocean-lore bonus round. Teams of up to six; the winners' names get carved into the bar."),
    ("Beginner Arduino: Build a Tide Sensor", "class_workshop", ["indoor", "cheap", "learning", "solo_friendly", "medium_energy"], "The Shipyard", "The Shipyard Makerspace", 2, "18:00", 150, (25, 25),
     "Wire up an ultrasonic sensor and log real water levels from the dock. Kit included and yours to keep. No experience needed."),
    ("Moonlight Yoga on Lanternfall Beach", "class_workshop", ["outdoor", "cheap", "low_energy", "solo_friendly", "nature"], "Lanternfall Beach", "Lanternfall Beach", 2, "19:30", 60, (10, 10),
     "A gentle flow on the sand under the rising moon. Mats available to borrow; ends with a guided breathing session to the sound of the surf."),
    # Tuesday Sep 29
    ("Coral Reef Restoration Dive", "community_event", ["outdoor", "active", "nature", "high_energy", "group", "daytime"], "Tidepool Heights", "Reef Station Dock", 3, "08:00", 240, (30, 30),
     "Certified divers help plant coral fragments on the Saltlight artificial reef. Snorkelers can help from the surface. Gear rental included."),
    ("Karaoke Night at the Fish Box", "bar", ["indoor", "cheap", "music", "drinks", "late_night", "group", "high_energy"], "Seaside Market", "Fish Box Karaoke", 3, "21:00", 180, (5, 10),
     "Main-stage karaoke with a light-up fish-crate stage. Sea-song duets get a free round."),
    ("Tech Talk: Sonar, Signals and Machine Learning", "class_workshop", ["indoor", "free", "learning", "daytime", "solo_friendly", "low_energy"], "Deepwater Quarter", "The Abyss Lecture Hall", 3, "12:00", 60, (0, 0),
     "A lunch talk on teaching models to tell whales from ship noise. Bring your lunch; slides and code shared afterward."),
    ("Chef's Table: Seven Courses of the Sea", "restaurant", ["indoor", "splurge", "food", "date", "drinks", "low_energy"], "Marina Row", "The Captain's Table", 3, "19:00", 150, (120, 150),
     "Chef Ines Marlow cooks seven courses from that morning's catch at a twelve-seat counter. Wine pairing optional."),
    # Wednesday Sep 30
    ("Wednesday Night Sailing Races", "sports_event", ["outdoor", "free", "active", "group", "nature", "medium_energy"], "Marina Row", "Saltlight Yacht Club", 4, "17:30", 150, (0, 0),
     "Watch from the breakwater or show up at the dock at 5 to crew for a skipper who needs hands. No experience needed, just grippy shoes."),
    ("Open Mic at Driftwood Coffee", "live_music", ["indoor", "free", "music", "solo_friendly", "low_energy"], "Seaside Market", "Driftwood Coffee Roasters", 4, "19:00", 120, (0, 0),
     "Singer-songwriters, spoken word and the occasional ukulele. Sign-up sheet opens at 6:30."),
    ("Kids' Shipwright Workshop: Cardboard Boats", "class_workshop", ["indoor", "cheap", "family", "learning", "daytime", "medium_energy"], "The Shipyard", "The Shipyard Makerspace", 4, "16:00", 90, (8, 8),
     "Kids design and tape together a cardboard boat, then test it in the float tank. Ages 6-12 with a grown-up."),
    ("XR Art Night: Paint in Virtual Waves", "class_workshop", ["indoor", "cheap", "art", "learning", "date", "medium_energy"], "Lighthouse Point", "The Lighthouse Laboratory", 4, "19:00", 120, (15, 15),
     "Sculpt glowing sea creatures in 3D with VR brushes and take home a video of your piece. Headsets provided."),
    # Thursday Oct 1
    ("Harbor Lights Art Walk", "gallery", ["outdoor", "free", "art", "date", "local_favorite", "low_energy"], "Lighthouse Point", "Barnacle Street Galleries", 5, "18:00", 180, (0, 0),
     "Galleries along Barnacle Street stay open late with new shows, live painting and lantern-lit murals. Start anywhere."),
    ("Oyster Shucking 101", "class_workshop", ["indoor", "food", "learning", "drinks", "group", "medium_energy"], "Seaside Market", "Brine and Bivalve Oyster Bar", 5, "18:30", 90, (35, 35),
     "Learn to open, grade and slurp three local varieties. Every student leaves with a shucking knife."),
    ("The Tempest at the Tidewater Playhouse", "theater", ["indoor", "art", "date", "low_energy"], "Lighthouse Point", "Tidewater Playhouse", 5, "19:30", 150, (25, 60),
     "Shakespeare's shipwreck comedy staged with real rigging and a water tank at the front of the stage. The first two rows may get wet."),
    ("Deep House at the Deep End", "nightclub", ["indoor", "late_night", "music", "high_energy", "21_plus", "drinks"], "Deepwater Quarter", "The Deep End", 5, "22:00", 300, (20, 30),
     "Resident DJs play underwater-themed deep house under a ceiling of blue light. Doors at 10, peak around 1."),
    ("Hardware Hack Night", "class_workshop", ["indoor", "free", "learning", "group", "late_night", "medium_energy"], "The Shipyard", "The Shipyard Makerspace", 5, "18:00", 240, (0, 0),
     "Open bench night: oscilloscopes, soldering irons and a laser cutter, with mentors on hand. Bring a half-finished project and some ambition."),
    # Friday Oct 2
    ("Friday Fish Fry and Bluegrass", "live_music", ["outdoor", "cheap", "food", "music", "family", "group", "medium_energy"], "Kelp Hollow", "Kelp Hollow Pavilion", 6, "18:00", 180, (15, 15),
     "Fried catfish, hushpuppies and slaw, with the Heron Creek Pickers on the porch stage. Kids eat free."),
    ("Lantern Festival on the Water", "festival", ["outdoor", "cheap", "family", "date", "touristy", "low_energy"], "Lanternfall Beach", "Lanternfall Beach", 6, "18:00", 300, (5, 5),
     "Decorate a paper lantern, write a wish on it and float it out at dusk. Food trucks and taiko drummers line the beach."),
    ("Retro Sea Monster Arcade Tournament", "rec_venue", ["indoor", "cheap", "late_night", "group", "high_energy"], "Deepwater Quarter", "Pixel Pier Arcade", 6, "21:00", 180, (10, 10),
     "Bracket play on a cabinet game about giant squid. Unlimited free play on the rest of the floor with entry."),
    ("Stargazing at Lighthouse Point", "tour", ["outdoor", "free", "nature", "learning", "date", "late_night", "low_energy"], "Lighthouse Point", "Lighthouse Point Lawn", 6, "20:30", 120, (0, 0),
     "The Saltlight Astronomy Club sets up telescopes on the darkest lawn in town. Saturn is up this week."),
    ("Sustainable Seafood Cooking Class", "class_workshop", ["indoor", "food", "learning", "date", "splurge", "medium_energy"], "Seaside Market", "Market Hall Test Kitchen", 6, "17:30", 150, (55, 55),
     "Cook three dishes from overlooked, sustainable species. Includes dinner and a guide to buying fish responsibly at the market."),
]

# ---------------------------------------------------------------- places
# (name, category, tags, hood, hours spec, (min, max) price or None, rating, ratingCount, description)
PLACES = [
    ("Saltlight Maritime Museum", "museum", ["indoor", "cheap", "learning", "family", "touristy", "daytime", "low_energy"], "Marina Row", "tue-sun 10:00-17:00", (12, 12), 4.6, 2140,
     "Three floors of shipwreck salvage, ship models and a walk-through whaling boat. The top floor looks out over the whole harbor."),
    ("Oracle of the Deep Aquarium", "zoo_aquarium", ["indoor", "learning", "family", "touristy", "daytime", "medium_energy"], "Deepwater Quarter", "daily 09:00-18:00", (32, 32), 4.7, 5380,
     "A glowing deep-sea aquarium with anglerfish, giant isopods and a jellyfish room lit by an ML model that reacts to the crowd."),
    ("The Lighthouse Laboratory", "museum", ["indoor", "learning", "family", "daytime", "medium_energy"], "Lighthouse Point", "wed-sun 11:00-19:00", (18, 18), 4.8, 1270,
     "A hands-on science center in a converted keeper's house: AR tide tables, a VR submarine and a room of optical illusions under the lamp."),
    ("Driftglass Gallery", "gallery", ["indoor", "free", "art", "date", "low_energy", "local_favorite"], "Lighthouse Point", "wed-sun 11:00-18:00", (0, 0), 4.5, 310,
     "Sea-glass mosaics and coastal photography by local artists. The back room shows a rotating digital-art installation."),
    ("Barnacle Street Mural Alley", "landmark", ["outdoor", "free", "art", "touristy", "low_energy", "solo_friendly"], "Lighthouse Point", "daily 00:00-23:59", (0, 0), 4.4, 880,
     "A two-block alley of murals: a kraken wrapped around a fire escape, a school of mackerel made of circuit traces, and the famous lighthouse keeper cat."),
    ("Old Saltlight Lighthouse", "landmark", ["outdoor", "cheap", "touristy", "family", "daytime", "medium_energy"], "Lighthouse Point", "daily 09:00-17:00", (5, 5), 4.8, 6120,
     "Climb 217 steps to the lantern room of the 1871 lighthouse for a 360-degree view of the coast. The town's namesake."),
    ("Gull's Perch Overlook", "viewpoint", ["outdoor", "free", "nature", "date", "low_energy", "solo_friendly"], "Tidepool Heights", "daily 06:00-22:00", (0, 0), 4.7, 940,
     "A cliffside platform with the best sunset view in Saltlight. Coin-op binoculars point at the seal rocks."),
    ("Tidewater Botanical Gardens", "garden", ["outdoor", "cheap", "nature", "date", "family", "daytime", "low_energy"], "Kelp Hollow", "daily 08:00-18:00", (10, 10), 4.6, 1760,
     "Salt-tolerant gardens, a seaweed greenhouse and a koi pond bridge. The flower terraces are in full bloom this month."),
    ("Anchor Park", "park", ["outdoor", "free", "family", "group", "daytime", "low_energy"], "Marina Row", "daily 06:00-22:00", (0, 0), 4.5, 1320,
     "The town green, built around a rusted 4-ton anchor. Picnic lawns, a splash pad, and a Saturday frisbee crowd."),
    ("Kelp Hollow Dog Beach", "park", ["outdoor", "free", "nature", "active", "daytime", "medium_energy"], "Kelp Hollow", "daily 07:00-19:00", (0, 0), 4.6, 640,
     "Off-leash sand for pups, with a rinse station and a snack shack that sells dog ice cream."),
    ("Tidepool Discovery Beach", "park", ["outdoor", "free", "nature", "family", "learning", "daytime", "low_energy"], "Tidepool Heights", "daily 06:00-20:00", (0, 0), 4.7, 1110,
     "Rocky shore with signposted tidepools and a volunteer docent station on weekends. Check the tide chart before you go."),
    ("Marina Community Garden", "garden", ["outdoor", "free", "nature", "solo_friendly", "daytime", "low_energy"], "Marina Row", "tue-sun 08:00-17:00", (0, 0), 4.8, 190,
     "A volunteer-run garden that grows produce for the harbor food pantry. Drop in to help weed or just sit among the tomatoes."),
    ("The Rusty Anchor", "bar", ["indoor", "cheap", "drinks", "group", "local_favorite", "late_night", "21_plus", "medium_energy"], "Marina Row", "daily 15:00-02:00", (6, 12), 4.5, 1580,
     "A dockside pub older than the lighthouse lens. Rope-wrapped stools, a shanty on Sundays, and the best fish and chips in town."),
    ("Driftwood Coffee Roasters", "cafe", ["indoor", "cheap", "solo_friendly", "daytime", "low_energy", "local_favorite"], "Seaside Market", "daily 07:00-18:00", (4, 8), 4.7, 2030,
     "Small-batch roaster with long communal tables and fast Wi-Fi. Order the sea-salt honey latte."),
    ("Bytes and Brine Cyber Cafe", "cafe", ["indoor", "cheap", "group", "solo_friendly", "late_night", "low_energy"], "Deepwater Quarter", "daily 09:00-01:00", (5, 12), 4.4, 720,
     "Coffee, bubble tea and 300 board games, with gaming PCs in the back. Popular with students on hack weekends."),
    ("Low Tide Taqueria", "restaurant", ["indoor", "cheap", "food", "group", "late_night", "local_favorite", "low_energy"], "The Shipyard", "daily 11:00-00:00", (4, 14), 4.6, 2410,
     "Baja fish tacos out of a converted shipping container. Open late, cash-friendly, lines move fast."),
    ("The Captain's Table", "restaurant", ["indoor", "splurge", "food", "date", "drinks", "low_energy"], "Marina Row", "tue-sat 17:30-22:00", (70, 140), 4.8, 860,
     "Saltlight's fine-dining room, with white tablecloths, a harbor view and a menu written each morning from the dayboat catch."),
    ("Pho Real Noodle House", "restaurant", ["indoor", "cheap", "food", "solo_friendly", "group", "low_energy"], "Seaside Market", "daily 10:30-21:30", (12, 18), 4.6, 1490,
     "Steaming bowls of seafood pho and crispy imperial rolls. The brisket is also great if you're off fish."),
    ("Sweet Seaweed Vegan Kitchen", "restaurant", ["indoor", "cheap", "food", "solo_friendly", "daytime", "low_energy"], "Kelp Hollow", "tue-sun 11:00-20:00", (10, 16), 4.5, 530,
     "Plant-based comfort food: kelp 'crab' cakes, jackfruit po'boys and a miso caramel sundae."),
    ("Mermaid Scoops", "cafe", ["indoor", "cheap", "food", "family", "date", "low_energy"], "Lanternfall Beach", "daily 12:00-22:00", (4, 9), 4.7, 1880,
     "Beachfront ice cream with flavors like salted caramel tide, ube wave and lighthouse lemon."),
    ("Salt and Smoke BBQ Shack", "restaurant", ["outdoor", "cheap", "food", "group", "local_favorite", "medium_energy"], "Kelp Hollow", "thu-sun 11:00-20:00", (12, 24), 4.6, 970,
     "Smoked brisket and blackened shrimp at picnic tables on the marsh. Sells out by 7 most nights."),
    ("Brine and Bivalve Oyster Bar", "restaurant", ["indoor", "food", "drinks", "date", "21_plus", "medium_energy"], "Seaside Market", "daily 15:00-23:00", (18, 45), 4.6, 1120,
     "Eight kinds of local oysters on crushed ice, with half-price dozens from 3 to 5."),
    ("The Crow's Nest Rooftop", "bar", ["outdoor", "drinks", "date", "late_night", "21_plus", "splurge", "low_energy"], "Marina Row", "daily 16:00-01:00", (14, 22), 4.5, 1340,
     "Cocktails five stories above the marina with heat lamps and string lights. Hosts comedy on Saturdays."),
    ("Siren Song Lounge", "bar", ["indoor", "drinks", "date", "late_night", "21_plus", "music", "low_energy"], "Marina Row", "wed-sun 18:00-02:00", (13, 18), 4.7, 690,
     "A speakeasy behind a fake fishmonger's freezer door. Velvet booths, live piano on weekends."),
    ("The Deep End", "nightclub", ["indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group"], "Deepwater Quarter", "thu-sat 22:00-03:00", (15, 30), 4.3, 870,
     "A basement club with a ceiling of shifting blue light that makes the room feel underwater."),
    ("Fish Box Karaoke", "rec_venue", ["indoor", "cheap", "music", "group", "late_night", "high_energy"], "Seaside Market", "daily 17:00-02:00", (8, 15), 4.4, 760,
     "Private karaoke rooms built from old fish-market cold boxes, plus a main stage on weekends."),
    ("Shipwreck Escape Rooms", "rec_venue", ["indoor", "group", "medium_energy", "learning"], "The Shipyard", "daily 11:00-23:00", (30, 32), 4.8, 1450,
     "Four rooms, from a sinking submarine to a lighthouse keeper's riddle. The 'Oracle' room uses a real voice assistant as the puzzle master."),
    ("Kraken Climbing Gym", "rec_venue", ["indoor", "active", "high_energy", "solo_friendly", "group"], "The Shipyard", "daily 06:00-23:00", (22, 22), 4.7, 1030,
     "Bouldering walls shaped like the hull of a ship, with a 15-meter lead wall up the old crane tower. Shoe rental included."),
    ("Pixel Pier Arcade", "rec_venue", ["indoor", "cheap", "group", "family", "late_night", "high_energy"], "Deepwater Quarter", "daily 12:00-00:00", (10, 25), 4.5, 1610,
     "Retro cabinets, rhythm games and skee-ball on a converted pier warehouse. Free-play Tuesdays."),
    ("Starboard Lanes", "rec_venue", ["indoor", "group", "family", "drinks", "late_night", "medium_energy"], "Marina Row", "daily 12:00-00:00", (6, 30), 4.3, 980,
     "Sixteen lanes of glow-bowling with nautical flags overhead and a surprisingly good crab-fry basket."),
    ("Harbor Kayak and SUP", "rec_venue", ["outdoor", "active", "nature", "daytime", "high_energy", "solo_friendly"], "Kelp Hollow", "daily 08:00-18:00", (25, 45), 4.8, 1190,
     "Rent a kayak or paddleboard by the hour and explore the calm marsh channels. Quick lesson included for first-timers."),
    ("Lanternfall Cinema", "cinema", ["indoor", "date", "low_energy", "group"], "Lanternfall Beach", "daily 12:00-00:00", (12, 16), 4.4, 1270,
     "A restored 1930s movie palace with a porthole-lined lobby. Mixes new releases with ocean-themed cult classics."),
    ("Seaside Market Hall", "market", ["indoor", "food", "local_favorite", "touristy", "daytime", "family", "low_energy"], "Seaside Market", "daily 08:00-19:00", (0, 0), 4.7, 3890,
     "The permanent market under a steel-and-glass roof: fishmongers, flower stalls, spice vendors and forty lunch counters."),
    ("The Shipyard Makerspace", "class_workshop", ["indoor", "cheap", "learning", "solo_friendly", "late_night", "medium_energy"], "The Shipyard", "daily 10:00-23:00", (15, 15), 4.8, 420,
     "A community workshop in an old boat shed with 3D printers, CNC, electronics benches and a day pass for visitors."),
    ("Compass Rose Books and Maps", "shopping", ["indoor", "learning", "solo_friendly", "daytime", "low_energy", "local_favorite"], "Lighthouse Point", "daily 10:00-20:00", (0, 0), 4.8, 610,
     "A creaky bookshop full of sea stories, old charts and a reading nook in a real ship's bow."),
    ("Salvage and Sons Vintage", "shopping", ["indoor", "cheap", "solo_friendly", "daytime", "low_energy"], "The Shipyard", "wed-sun 11:00-18:00", (0, 0), 4.5, 340,
     "Brass portholes, fishermen's sweaters, vintage radios and a bin of mystery keys."),
    ("Circuit Cove Electronics", "shopping", ["indoor", "learning", "solo_friendly", "daytime", "low_energy"], "The Shipyard", "mon-sat 10:00-19:00", (0, 0), 4.6, 280,
     "Parts store for hardware hackers: sensors, microcontrollers, waterproof enclosures and staff who will debug your wiring."),
    ("Harbor Ferry Loop", "tour", ["outdoor", "cheap", "touristy", "nature", "family", "daytime", "low_energy"], "Marina Row", "daily 09:00-19:00", (6, 6), 4.6, 2250,
     "A 50-minute ferry loop past the lighthouse, seal rocks and the shipyard cranes. Hop on at the marina."),
    ("Lighthouse Ghost Walk", "tour", ["outdoor", "late_night", "group", "touristy", "medium_energy"], "Lighthouse Point", "daily 20:00-22:00", (20, 20), 4.5, 740,
     "A lantern-lit walk through Saltlight's old quarter and its shipwreck legends, ending at the lighthouse gate at midnight's bell."),
    ("Sunken Garden Bathhouse", "rec_venue", ["indoor", "splurge", "date", "low_energy", "solo_friendly", "daytime"], "Tidepool Heights", "daily 10:00-22:00", (45, 95), 4.7, 560,
     "Saltwater soaking pools, a cedar sauna and cold plunges carved into the cliff. Ninety-minute sessions."),
]

# ---------------------------------------------------------------- hikes
# (name, park, hood, km, ascent m, loop, tags, description)
HIKES = [
    ("Lighthouse Point Loop", "Lighthouse Point Preserve", "Lighthouse Point", 3.2, 40, True, ["low_energy", "family", "touristy"],
     "An easy loop around the headland with the lighthouse always in view. Benches at every overlook."),
    ("Oracle Bluffs Ridge Trail", "Oracle Bluffs State Park", "Tidepool Heights", 9.8, 320, False, ["high_energy", "solo_friendly"],
     "A steady climb along exposed sea cliffs to the Oracle, a rock arch that whistles in the wind."),
    ("Kelp Hollow Marsh Boardwalk", "Kelp Hollow Nature Preserve", "Kelp Hollow", 1.6, 5, True, ["low_energy", "family", "free"],
     "A flat, stroller-friendly boardwalk over the salt marsh with herons, fiddler crabs and interpretive signs."),
    ("Shipwreck Cove Coastal Trail", "Shipwreck Cove Park", "Lanternfall Beach", 6.4, 110, False, ["medium_energy", "touristy"],
     "Follows the shoreline to the ribs of the schooner Marigold, visible at low tide."),
    ("Tidewater Dunes Trail", "Tidewater Dunes Preserve", "Lanternfall Beach", 4.1, 35, True, ["medium_energy", "date"],
     "Sandy trail over rolling dunes with sea oats and a hidden beach at the far end."),
    ("Mariner's Summit Trail", "Mariner's Summit Wilderness", "Tidepool Heights", 12.5, 540, False, ["high_energy", "solo_friendly"],
     "The hardest climb in the county, up to a fire lookout with a view of three lighthouses on a clear day."),
    ("Heron Creek Greenway", "Heron Creek Greenway", "Kelp Hollow", 7.0, 20, False, ["low_energy", "family", "group"],
     "A paved multi-use path along the creek, popular with runners, cyclists and families on scooters."),
    ("Lantern Falls Trail", "Lantern Falls State Park", "Tidepool Heights", 5.3, 210, True, ["medium_energy", "date"],
     "A shaded forest loop to a 20-meter waterfall that glows orange at sunset."),
    ("Old Rail Causeway", "Saltlight Rail Trail", "The Shipyard", 8.8, 10, False, ["low_energy", "group", "solo_friendly"],
     "A flat gravel trail on a former rail line across the bay, with water on both sides."),
    ("Sea Cliff Staircase Challenge", "Oracle Bluffs State Park", "Tidepool Heights", 2.4, 180, False, ["high_energy", "solo_friendly"],
     "672 wooden steps straight up the cliff. Locals time their laps; the record is on a plaque at the top."),
    ("Driftwood Forest Loop", "Driftwood Forest Preserve", "Lanternfall Beach", 3.8, 60, True, ["low_energy", "date", "local_favorite"],
     "A loop through a bone-white forest of sun-bleached driftwood trees. Very photogenic."),
    ("Pelican Island Tide Walk", "Pelican Island Reserve", "Kelp Hollow", 2.9, 5, False, ["medium_energy", "family"],
     "Only walkable two hours either side of low tide: a sandbar crossing to a pelican rookery."),
    ("Coral Ridge Sunrise Trail", "Coral Ridge Park", "Tidepool Heights", 5.9, 260, False, ["medium_energy", "solo_friendly"],
     "Start in the dark with a headlamp and reach the ridge as the sun comes up out of the ocean."),
    ("Saltmarsh Birding Loop", "Kelp Hollow Nature Preserve", "Kelp Hollow", 4.6, 12, True, ["low_energy", "solo_friendly", "local_favorite"],
     "A quiet loop with two bird blinds. Over 200 species logged, including roseate spoonbills."),
    ("Kraken's Spine Traverse", "Mariner's Summit Wilderness", "Tidepool Heights", 16.2, 780, False, ["high_energy", "group"],
     "An all-day ridgeline scramble over nine rocky knobs. Pack plenty of water; there's no shade."),
]


def build_event(row) -> Activity:
    name, cat, tags, hood, venue, day, hhmm, minutes, (lo, hi), desc = row
    start = DAY0 + timedelta(days=day, hours=int(hhmm[:2]), minutes=int(hhmm[3:]))
    end = start + timedelta(minutes=minutes)
    loc, addr = spot(hood, venue)
    if minutes <= MAX_FIXED_SPAN_MIN and cat != "market":
        duration, attendance = Duration.lognormal(minutes, EVENT_TIMES_SIGMA, "event_times"), "fixed_start"
    else:
        duration, attendance = prior(cat), "drop_in"
    return Activity(
        kind="event", city=CITY, name=name, summary=desc.split(". ")[0].rstrip(".") + ".", description=desc,
        category=cat, sourceCategory=cat, tags=tags, location=loc, address=addr, venueName=venue,
        start=start.astimezone(timezone.utc), end=end.astimezone(timezone.utc), attendance=attendance,
        timezone=str(TZ), duration=duration, price=price_from_range(lo, hi, "USD", TIERS),
        popularity=popularity(name, None), ticketUrl=None if hi == 0 else f"{MERCHANT_BASE}/{slug(name)}/tickets",
        expiresAt=end.astimezone(timezone.utc), **source(name, DEMO_BASE if hi == 0 else MERCHANT_BASE),
    )


def build_place(row) -> Activity:
    name, cat, tags, hood, spec, (lo, hi), rating, count, desc = row
    loc, addr = spot(hood, name)
    return Activity(
        kind="place", city=CITY, name=name, summary=desc.split(". ")[0].rstrip(".") + ".", description=desc,
        category=cat, sourceCategory=cat, tags=tags, location=loc, address=addr, venueName=name,
        attendance="drop_in", timezone=str(TZ), weeklyHours=hours(spec), hoursSource="google",
        duration=prior(cat), price=price_from_range(lo, hi, "USD", TIERS),
        rating=rating, ratingCount=count, popularity=popularity(name, count), **source(name),
    )


def trail_line(lat: float, lng: float, km: float, loop: bool, seed: int) -> dict:
    """A plausible-looking GeoJSON line of roughly `km` length starting near (lat, lng)."""
    n = 40
    kx = 1 / (111.32 * math.cos(math.radians(lat)))  # degrees lng per km
    ky = 1 / 110.57
    heading = (seed % 360) * math.pi / 180
    pts = []
    for i in range(n + 1):
        t = i / n
        if loop:
            r = km / (2 * math.pi)
            a = heading + 2 * math.pi * t
            x, y = r * math.cos(a) - r * math.cos(heading), r * math.sin(a) - r * math.sin(heading)
        else:
            wiggle = 0.08 * km * math.sin(t * math.pi * 3 + seed % 7)
            x = km * t * math.cos(heading) - wiggle * math.sin(heading)
            y = km * t * math.sin(heading) + wiggle * math.cos(heading)
        pts.append((round(lng + x * kx, 6), round(lat + y * ky, 6)))
    return {"type": "LineString", "coordinates": pts}


def build_hike(row) -> Activity:
    name, park, hood, km, ascent, loop, extra, desc = row
    loc, addr = spot(hood, name, spread=0.04)
    lng, lat = loc.coordinates
    minutes = km / 4.5 * 60 + ascent / 10  # Naismith: 4.5 km/h plus 1 min per 10 m of climb
    minutes *= 1 if loop else 2  # out-and-back lengths are one way
    tags = sorted({"outdoor", "free", "nature", "active", "daytime", *extra})
    return Activity(
        kind="place", city=CITY, name=name, summary=desc.split(". ")[0].rstrip(".") + ".", description=desc,
        category="hike", sourceCategory="route=hiking", tags=tags, location=loc, address=addr, venueName=park,
        attendance="drop_in", timezone=str(TZ), weeklyHours=hours("daily 06:00-18:00"), hoursSource="default",
        duration=Duration.lognormal(minutes, 0.3, "trail_model"), price=price_from_range(0, 0, "USD", TIERS),
        popularity=popularity(name, None),
        trail=Trail(lengthKm=km, ascentM=ascent, descentM=ascent, loop=loop, geometry=trail_line(lat, lng, km, loop, _hash(name))),
        **source(name),
    )


def main() -> None:
    acts = [build_event(r) for r in EVENTS] + [build_place(r) for r in PLACES] + [build_hike(r) for r in HIKES]
    keys = [a.sourceKeys[0] for a in acts]
    assert len(set(keys)) == len(keys), "duplicate names"
    docs = []
    for a in acts:
        d = a.model_dump(mode="python")
        d["_id"] = ObjectId(hashlib.sha1(a.sourceKeys[0].encode()).hexdigest()[:24])
        d["createdAt"] = d["updatedAt"] = FETCHED
        docs.append(d)
    OUT.write_text(json_util.dumps(docs, indent=1, json_options=json_util.RELAXED_JSON_OPTIONS))
    counts = {}
    for a in acts:
        k = "hike" if a.category == "hike" else a.kind
        counts[k] = counts.get(k, 0) + 1
    print(f"wrote {len(acts)} activities to {OUT} ({json.dumps(counts)})")


if __name__ == "__main__":
    main()
