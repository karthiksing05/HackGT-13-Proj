"""The 250 activities of the Atlanta pitch catalog, as data.

Synthetic demonstration data. Every venue is a real record of `freetime.activities` (its
`_id` is the `ref`): the generator copies that record's coordinates, address, opening hours,
Google rating and link verbatim. Everything written here (event names, dates, times, prices,
descriptions, tags) was made up for the pitch and is not a verified public listing.

Places are flexible visits inside the venue's real opening hours. Events happen on a local
date and time in America/New_York; two or more dates make a weekly series (one document per
date, sharing a `recurrence`). Prices are (min, max) in whole US dollars per person; a place
with `price=None` keeps the range Google gave its real record.
"""

from dataclasses import dataclass

# Planning groups (the brief's table), not categories.
NIGHT = "nightlife"          # Nightclubs, DJ nights, and dancing
MUSIC = "music"              # Concerts and live music
DINNER = "dinner"            # Restaurants and dinner experiences
BRUNCH = "brunch"            # Breakfast, brunch, and cafés
OUTDOOR = "outdoor"          # Hikes, parks, and outdoor recreation
CULTURE = "culture"          # Museums, galleries, and cultural visits
CLASS = "classes"            # Workshops and creative classes
ACTIVE = "active"            # Sports, fitness, and active recreation
STAGE = "stage"              # Comedy, theater, and performances
COMMUNITY = "community"      # Markets, festivals, and community activities

GROUP_TARGETS = {  # group -> (paid, free)
    NIGHT: (18, 4), MUSIC: (22, 8), DINNER: (28, 0), BRUNCH: (16, 0), OUTDOOR: (6, 34),
    CULTURE: (13, 15), CLASS: (18, 6), ACTIVE: (13, 13), STAGE: (15, 3), COMMUNITY: (1, 17),
}

FREE = (0, 0)


@dataclass(frozen=True)
class Place:
    group: str
    ref: str               # freetime.activities _id of the real venue
    category: str
    tags: tuple
    description: str
    price: tuple | None = None  # None: the real record's Google price range
    name: str | None = None     # display name when the real one needs context


@dataclass(frozen=True)
class Event:
    group: str
    name: str
    category: str
    tags: tuple
    ref: str               # the venue: a real place record, or a real event record for its location
    dates: tuple           # local dates; two or more make a weekly series
    time: str              # local start, "HH:MM"
    minutes: int
    price: tuple
    description: str
    attendance: str = "fixed_start"
    venue: str | None = None  # display name of the venue when the record's is awkward


# ------------------------------------------------------------------------ real venues
# Places (freetime.activities, Google Places unless noted).
MJQ = "6ab7466f3d255134cf271bc9"
ROYAL_PEACOCK = "6ab7466f3d255134cf271c89"
HERETIC = "6ab7466f3d255134cf271c08"
LORE = "6ab7466f3d255134cf271c8c"
PBR = "6ab7466f3d255134cf271c3c"
HAVANA = "6ab7466f3d255134cf271c12"
HIGH_NOTE = "6ab7466f3d255134cf271c51"
ROOF_PCM = "6ab7466f3d255134cf271b51"
BLIND_WILLIES = "6ab7466f3d255134cf271c82"
NORTHSIDE_TAVERN = "6ab7466f3d255134cf271be7"
THE_EARL = "6ab7466f3d255134cf271bb8"
KOPLEFF = "6ab7466f3d255134cf271c8b"
SYMPHONY_HALL = "6ab7466f3d255134cf271c86"
CENTER_STAGE = "6ab7466f3d255134cf2719ae"
BUCKHEAD_THEATRE = "6ab7466f3d255134cf271a0b"
FOX = "6ab7466f3d255134cf271962"
FERST = "6ab7466f3d255134cf2719a0"
POUR_TAPROOM = "6ab7466f3d255134cf271c79"
CYPRESS = "6ab7466f3d255134cf271beb"
PORTER = "6ab746703d255134cf271ca3"
BRICK_STORE = "6ab7466f3d255134cf271bd5"
MANUELS = "6ab7466f3d255134cf271bce"
NEW_REALM = "6ab7466f3d255134cf271bcc"
FADO = "6ab7466f3d255134cf271c60"
MELTONS = "6ab7466f3d255134cf271c0f"
THINKING_MAN = "6ab7466f3d255134cf271bd6"
STEINBECKS = "6ab7466f3d255134cf271bda"
IMPERIAL = "6ab7466f3d255134cf271bdb"
MILLTOWN = "6ab7466f3d255134cf271c96"
BAR42 = "6ab7466f3d255134cf271bb2"
MANNYS = "6ab7466f3d255134cf271bbd"
MOES_JOES = "6ab7466f3d255134cf271bf1"
ELEVENTH_ST = "6ab7466f3d255134cf271c5c"
NORTH_HIGHLAND_PUB = "6ab746703d255134cf271ca4"
MR_CS = "6ab7466f3d255134cf271c01"
MARLOWS = "6ab7466f3d255134cf271c1d"
SUGAR_FACTORY = "6ab7466f3d255134cf271bed"
BANTAM = "6ab7466f3d255134cf271c70"
CAFE_BELLI = "6ab7466f3d255134cf271b98"
TWO_BEST_FRIENDS = "6ab7466f3d255134cf271c94"
THREE_ARCHES = "6ab7466f3d255134cf271c29"
PARK_BAR = "6ab7466f3d255134cf271bcb"
MIDWAY = "6ab7466f3d255134cf271bba"
MNB_GARAGE = "6ab7466f3d255134cf271ba9"
MNB_GROVE = "6ab7466f3d255134cf271be5"
SIDE_SADDLE = "6ab7466f3d255134cf271bb3"
WILD_HEAVEN = "6ab7466f3d255134cf271baa"
SWEETWATER_BREW = "6ab7466f3d255134cf271c02"
TASTE_WINE = "6ab7466f3d255134cf271bfd"
THREE_TAVERNS = "6ab7466f3d255134cf271c77"
HALFWAY_CROOKS = "6ab7466f3d255134cf271bb1"
RANGER_STATION = "6ab7466f3d255134cf271c8e"
LIPS = "6ab7466f3d255134cf271a17"
PAINTED_PICKLE = "6ab7466f3d255134cf271c03"
PUTTSHACK = "6ab7466f3d255134cf271c5e"
PAINTED_DUCK = "6ab7466f3d255134cf271b53"
MIDTOWN_BOWL = "6ab7466f3d255134cf271b5d"
SKYLINE_PARK = "6ab7466f3d255134cf271b5a"
SANDBOX_VR = "6ab7466f3d255134cf27199f"
BEAT_THE_BOMB = "6ab7466f3d255134cf271b5c"
ACTIVATE = "6ab7466f3d255134cf271b73"
HIGH_MUSEUM = "6ab7466f3d255134cf2719ac"
FERNBANK = "6ab7466f3d255134cf27198e"
HISTORY_CENTER = "6ab7466f3d255134cf271a0a"
CARLOS = "6ab7466f3d255134cf2719d1"
CARTER_CENTER = "6ab7466f3d255134cf27197f"
MODA = "6ab7466f3d255134cf2719b5"
AQUARIUM = "6ab7466f3d255134cf271952"
ZOO = "6ab7466f3d255134cf271a8e"
PUPPETRY = "6ab7466f3d255134cf2719af"
APEX = "6ab7466f3d255134cf27196c"
MLK_NHP = "6ab7466f3d255134cf271963"
KING_CENTER = "6ab7466f3d255134cf271969"
KROG_TUNNEL = "6ab7466f3d255134cf271980"
ABV = "6ab7466f3d255134cf271941"
ZUCOT = "6ab7466f3d255134cf271959"
MET_ATL = "6ab7466f3d255134cf27192b"
CDC_MUSEUM = "6ab7466f3d255134cf2719d2"
CAPITOL_MUSEUM = "6ab7466f3d255134cf271939"
FERNBANK_SCIENCE = "6ab7466f3d255134cf2719d0"
MASON_FINE_ART = "6ab7466f3d255134cf271a1b"
OUTKAST_MURAL = "6ab7466f3d255134cf271986"
DECATUR_GLASS = "6ab7466f3d255134cf271998"
CALLANWOLDE = "6ab7466f3d255134cf2719c7"
CHARIS = "6ab7466f3d255134cf271c9b"
BN_GEORGIA_TECH = "6ab7466f3d255134cf271c95"
OXFORD_COMICS = "6ab7466f3d255134cf271c9c"
WHOLE_WORLD = "6ab7466f3d255134cf2719bc"
LAUGHING_SKULL = "6ab7466f3d255134cf2719b3"
SHAKESPEARE_TAVERN = "6ab7466f3d255134cf271967"
PUNCHLINE = "6ab7466f3d255134cf271a29"
DINNER_DETECTIVE = "6ab7466f3d255134cf271a53"
MAGIC_THEATER = "6ab7466f3d255134cf27195c"
HORIZON = "6ab7466f3d255134cf271984"
ALLIANCE = "6ab7466f3d255134cf2719b0"
RIALTO = "6ab7466f3d255134cf27196b"
PLAZA = "6ab7466f3d255134cf271b50"
STARLIGHT = "6ab7466f3d255134cf271b45"
SWAN_HOUSE = "6ab7466f3d255134cf271a0c"
PCM = "6ab7466f3d255134cf27197e"
BELTLINE_SHED = "6ab746703d255134cf271ca1"
DEKALB_MARKET = "6ab746703d255134cf271ca7"
DECATUR_MARKET = "6ab746703d255134cf271caa"
WESTSIDE_PROVISIONS = "6ab746703d255134cf271ca6"
BUCKHEAD_VILLAGE = "6ab746703d255134cf271cac"
UNDERGROUND = "6ab7466f3d255134cf271964"
FOOD_FOREST = "6ab7466f3d255134cf271a74"
DECATUR_SQUARE = "6ab7466f3d255134cf271997"
CONSTITUTION_LAKES = "6ab7466f3d255134cf271919"
PACES_MILL = "6ab7466f3d255134cf271b0e"
BOTANICAL_GARDEN = "6ab7466f3d255134cf2719aa"
PIEDMONT = "6ab7466f3d255134cf2719ab"
CENTENNIAL = "6ab7466f3d255134cf271aa5"
FOURTH_WARD = "6ab7466f3d255134cf271981"
FREEDOM_PARK = "6ab7466f3d255134cf271982"
GRANT_PARK = "6ab7466f3d255134cf271938"
CANDLER_PARK = "6ab7466f3d255134cf27198d"
WESTSIDE_PARK = "6ab7466f3d255134cf271ac7"
CHASTAIN = "6ab7466f3d255134cf271a43"
TANYARD = "6ab7466f3d255134cf2719ec"
WOODRUFF = "6ab7466f3d255134cf271968"
LULLWATER = "6ab7466f3d255134cf271a00"
WOODLANDS = "6ab7466f3d255134cf2719da"
RODNEY_COOK = "6ab7466f3d255134cf271955"
MASON_MILL = "6ab7466f3d255134cf2719ff"
HERBERT_TAYLOR = "6ab7466f3d255134cf2719fd"
EAST_LAKE = "6ab7466f3d255134cf271948"
GLENLAKE = "6ab7466f3d255134cf2719d7"
DOLLS_HEAD = "6ab7466f3d255134cf271a75"
# Trails (OpenStreetMap, freetime.activities category hike).
EASTSIDE_TRAIL = "6ab745a8926eaaa573c25968"
WESTSIDE_TRAIL = "6ab74592926eaaa573c25959"
NORTHEAST_TRAIL = "6ab74590926eaaa573c25958"
SOUTHSIDE_TRAIL = "6ab7459f926eaaa573c25962"
PROCTOR_CREEK = "6ab7454e926eaaa573c2592a"
PEACHTREE_CREEK = "6ab7455d926eaaa573c25934"
TROLLEY_LINE = "6ab744b7926eaaa573c258be"
BOB_CALLAN = "6ab744a6926eaaa573c258b1"
BROOK_RUN = "6ab74527926eaaa573c2590e"
FRAZER_FOREST = "6ab744a7926eaaa573c258b2"
HERITAGE_PARK_TRAIL = "6ab744c1926eaaa573c258c4"
MOUNTAIN_TO_RIVER = "6ab74543926eaaa573c25922"
SWEETWATER_WHITE = "6ab74508926eaaa573c258f5"
STONE_MTN_CHEROKEE = "6ab744b6926eaaa573c258bd"
COCHRAN_SHOALS = "6ab744be926eaaa573c258c1"
EAST_PALISADES = "6ab744e3926eaaa573c258d9"
# Event venues known only from real event listings (Ticketmaster, Resident Advisor).
TERMINAL_WEST = "6ab7449b25e9f6cc68d5829d"
AISLE5 = "6ab7449e25e9f6cc68d582ce"
VINYL = "6ab7449b25e9f6cc68d5829b"
THE_LOFT = "6ab7449d25e9f6cc68d582c9"
MASQ_ALTAR = "6ab7449b25e9f6cc68d5828b"
MASQ_HEAVEN = "6ab7449b25e9f6cc68d5828a"
MASQ_HELL = "6ab7449b25e9f6cc68d58287"
MASQ_PURGATORY = "6ab7449c25e9f6cc68d582bf"
VARIETY = "6ab7449b25e9f6cc68d5829c"
EDDIES_ATTIC = "6ab7449b25e9f6cc68d5828e"
THE_EASTERN = "6ab7449b25e9f6cc68d582b5"
TABERNACLE = "6ab7449b25e9f6cc68d5828c"
PISCES = "6ab7449fc9fab8dd61a0441c"
LUNCHBOX = "6ab7449fc9fab8dd61a0441b"
WESTSIDE_MOTOR_LOUNGE = "6ab7449fc9fab8dd61a0441e"

# ------------------------------------------------------------------------ dates
D0 = "2026-09-27"   # Sunday: the pitch day (DEMO_DATE)
MON1, TUE1, WED1, THU1, FRI1, SAT1, SUN1 = (
    "2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04")
MON2, TUE2, WED2, THU2, FRI2, SAT2 = (
    "2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10")


# ======================================================================== places
PLACES = [
    # ---------------------------------------------------------------- nightlife
    Place(NIGHT, MJQ, "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group", "local_favorite"),
          "A long-running Atlanta dance club, now in Underground Atlanta, where DJs play hip-hop, house and indie dance until 4 a.m. "
          "Expect a crowded floor after midnight and to spend about $20–$30 per person, cover included."),
    Place(NIGHT, ROYAL_PEACOCK, "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group", "local_favorite"),
          "A nightclub in a storied Auburn Avenue music venue, with DJs and dancing late on weekend nights. "
          "Expect to spend about $20–$30 per person, cover included."),
    Place(NIGHT, HERETIC, "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group"),
          "A big Cheshire Bridge Road dance club popular with Atlanta's LGBTQ+ community, with themed nights and a large floor. "
          "Expect to spend about $20–$30 per person, cover included."),
    Place(NIGHT, LORE, "nightclub", ("indoor", "late_night", "music", "medium_energy", "21_plus", "drinks", "group", "local_favorite"),
          "An Edgewood Avenue bar and dance space with DJs most nights and a friendly, queer-inclusive crowd. "
          "Expect to spend about $10–$50 per person, cover included; drinks are the main spend."),
    Place(NIGHT, PBR, "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group", "touristy"),
          "A country bar at The Battery with a big dance floor, live bands or DJs, and line dancing on weekend nights. "
          "Expect to spend about $10–$50 per person, cover included."),

    # ---------------------------------------------------------------- live music (bars with nightly bands)
    Place(MUSIC, BLIND_WILLIES, "bar", ("indoor", "late_night", "music", "drinks", "21_plus", "local_favorite", "medium_energy", "date"),
          "A Virginia-Highland blues bar with live bands most nights on a small stage close to the tables. "
          "Expect to spend about $10–$20 per person, cover included."),
    Place(MUSIC, NORTHSIDE_TAVERN, "bar", ("indoor", "late_night", "music", "drinks", "21_plus", "local_favorite", "medium_energy", "group"),
          "A no-frills West Midtown tavern known for live blues and a loyal neighborhood crowd. "
          "Bands play most evenings; expect to spend about $20–$30 per person, cover included."),

    # ---------------------------------------------------------------- restaurants (real pubs, taverns and brewpubs with full kitchens)
    Place(DINNER, CYPRESS, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy", "local_favorite"),
          "A Midtown gastropub near Tech Square with a big patio, a long craft-beer list, burgers and shareable plates. "
          "Expect to spend about $20–$30 per person."),
    Place(DINNER, PORTER, "restaurant", ("indoor", "food", "drinks", "date", "low_energy", "local_favorite"),
          "A Little Five Points beer bar with an enormous bottle and draft list and a kitchen doing elevated pub food. "
          "Expect about $20–$30 per person."),
    Place(DINNER, BRICK_STORE, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy", "local_favorite"),
          "A beloved Decatur Square pub with a deep draft list, an upstairs Belgian beer bar and hearty pub food. "
          "Expect about $20–$30 per person."),
    Place(DINNER, MANUELS, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy", "local_favorite", "touristy"),
          "A Poncey-Highland tavern open since 1956, known for burgers, wings and walls covered in Atlanta political history. "
          "Expect about $10–$20 per person."),
    Place(DINNER, NEW_REALM, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy"),
          "A brewery and restaurant right on the BeltLine's Eastside Trail, with house beers, a full kitchen and a large patio. "
          "Plan on about $15–$35 per person.",
          price=(15, 35)),
    Place(DINNER, FADO, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy"),
          "A Midtown Irish pub serving fish and chips, shepherd's pie and pints, with soccer on the screens. "
          "Expect about $20–$30 per person."),
    Place(DINNER, MELTONS, "restaurant", ("indoor", "food", "drinks", "group", "low_energy"),
          "A Decatur neighborhood spot built around shareable appetizers and a rotating tap list. "
          "Expect about $10–$20 per person."),
    Place(DINNER, THINKING_MAN, "restaurant", ("indoor", "food", "drinks", "group", "low_energy", "local_favorite"),
          "A laid-back Decatur tavern by the railroad tracks with pub classics and craft beer. "
          "Expect about $20–$30 per person."),
    Place(DINNER, STEINBECKS, "restaurant", ("indoor", "food", "drinks", "group", "low_energy"),
          "A cozy Oakhurst pub with pub food, craft beer and a neighborhood patio crowd. "
          "Expect about $10–$20 per person."),
    Place(DINNER, IMPERIAL, "restaurant", ("indoor", "food", "drinks", "date", "low_energy"),
          "A Decatur pub near the square with a long beer list, pub fare and a relaxed patio. "
          "Expect about $20–$30 per person."),
    Place(DINNER, MILLTOWN, "restaurant", ("indoor", "food", "drinks", "group", "low_energy", "local_favorite"),
          "A tavern in the historic Cabbagetown mill village serving comfort food and craft beer. "
          "Expect about $10–$20 per person."),
    Place(DINNER, BAR42, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy"),
          "A bar and grill on Memorial Drive with burgers, wings and games on the screens. "
          "Expect about $20–$60 per person."),
    Place(DINNER, MANNYS, "restaurant", ("indoor", "food", "drinks", "group", "low_energy"),
          "A Grant Park neighborhood bar and grill with pub food and a casual patio. "
          "Expect about $10–$20 per person."),
    Place(DINNER, MOES_JOES, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy", "local_favorite"),
          "A classic Virginia-Highland tavern, open since the 1940s, with cheap pitchers and simple pub food. "
          "Plan on about $10–$20 per person.",
          price=(10, 20)),
    Place(DINNER, ELEVENTH_ST, "restaurant", ("indoor", "food", "drinks", "group", "medium_energy", "late_night"),
          "A Midtown pub a short walk from Tech Square, with bar food, a patio and late hours. "
          "Expect about $20–$30 per person."),
    Place(DINNER, NORTH_HIGHLAND_PUB, "restaurant", ("indoor", "food", "drinks", "group", "low_energy"),
          "A cozy pub on North Highland Avenue with burgers, wings and a neighborhood crowd. "
          "Expect about $10–$30 per person."),
    Place(DINNER, MR_CS, "restaurant", ("indoor", "food", "drinks", "group", "low_energy"),
          "A casual bar and grill on Howell Mill Road with burgers, wings and televised games. "
          "Expect about $10–$20 per person."),
    Place(DINNER, MARLOWS, "restaurant", ("indoor", "food", "drinks", "date", "low_energy"),
          "A polished neighborhood tavern in the Cumberland area serving American comfort food and cocktails. "
          "Expect about $20–$30 per person."),

    # ---------------------------------------------------------------- breakfast, brunch, cafés
    Place(BRUNCH, SUGAR_FACTORY, "restaurant", ("indoor", "food", "drinks", "group", "touristy", "medium_energy"),
          "A candy-bright Midtown brasserie known for oversized milkshakes and towering desserts, with brunch from 10 on weekends. "
          "Plan on about $20–$40 per person.",
          price=(20, 40), name="Sugar Factory American Brasserie"),
    Place(BRUNCH, BANTAM, "restaurant", ("indoor", "food", "drinks", "group", "low_energy", "local_favorite"),
          "An Old Fourth Ward pub with British-style comfort food and a popular weekend brunch from 10 a.m. "
          "Expect about $20–$30 per person."),
    Place(BRUNCH, CAFE_BELLI, "cafe", ("indoor", "food", "cheap", "solo_friendly", "low_energy", "local_favorite"),
          "A Hapeville café serving espresso drinks and breakfast by day and cocktails in the evening. "
          "Expect about $10–$20 per person."),
    Place(BRUNCH, TWO_BEST_FRIENDS, "cafe", ("indoor", "food", "cheap", "solo_friendly", "low_energy", "learning"),
          "A small café and bookshop with coffee, snacks and shelves to browse. "
          "Plan on about $5–$15 for coffee and a snack.",
          price=(5, 15)),
    Place(BRUNCH, THREE_ARCHES, "restaurant", ("indoor", "food", "daytime", "solo_friendly", "low_energy"),
          "A Buckhead breakfast room serving eggs, pancakes and coffee from 7 a.m., with brunch until 2 on weekends. "
          "Plan on about $15–$30 per person.",
          price=(15, 30)),

    # ---------------------------------------------------------------- parks and gardens (free)
    Place(OUTDOOR, PIEDMONT, "park", ("outdoor", "free", "nature", "family", "group", "low_energy", "touristy", "local_favorite"),
          "Atlanta's central park: nearly 200 acres of lawns, trails, a lake and skyline views on the edge of Midtown. "
          "Free and open daily from 6 a.m."),
    Place(OUTDOOR, CENTENNIAL, "park", ("outdoor", "free", "family", "touristy", "low_energy", "group"),
          "The downtown park built for the 1996 Olympics, with the Fountain of Rings, open lawns and the city's big attractions around it. "
          "Free."),
    Place(OUTDOOR, FOURTH_WARD, "park", ("outdoor", "free", "nature", "family", "low_energy", "local_favorite"),
          "A park beside the BeltLine's Eastside Trail built around a stormwater lake, with a splash pad, playground and amphitheater. "
          "Free."),
    Place(OUTDOOR, FREEDOM_PARK, "park", ("outdoor", "free", "nature", "active", "solo_friendly", "low_energy"),
          "A long linear park along Freedom Parkway with paved PATH trails, open lawns and public art between Poncey-Highland and Candler Park. "
          "Free."),
    Place(OUTDOOR, GRANT_PARK, "park", ("outdoor", "free", "nature", "family", "low_energy", "local_favorite"),
          "One of Atlanta's oldest parks, with shaded paths, Victorian homes around its edges and Zoo Atlanta inside. "
          "Free."),
    Place(OUTDOOR, CANDLER_PARK, "park", ("outdoor", "free", "family", "group", "low_energy", "local_favorite"),
          "An east-side neighborhood park with a public golf course, a pool and wide lawns. "
          "Free to visit."),
    Place(OUTDOOR, WESTSIDE_PARK, "park", ("outdoor", "free", "nature", "active", "solo_friendly", "medium_energy"),
          "Atlanta's largest park, built around the old Bellwood Quarry, with a reservoir overlook and miles of trails. "
          "Free."),
    Place(OUTDOOR, CHASTAIN, "park", ("outdoor", "free", "active", "family", "group", "medium_energy"),
          "A big north-side park with a walking-trail loop, sports fields and tennis courts. "
          "Free."),
    Place(OUTDOOR, TANYARD, "park", ("outdoor", "free", "nature", "solo_friendly", "low_energy"),
          "A creekside park on the BeltLine's Northside Trail with shaded lawns and a paved path. "
          "Free."),
    Place(OUTDOOR, WOODRUFF, "park", ("outdoor", "free", "solo_friendly", "low_energy"),
          "Downtown's central green, with a fountain, movable seating and a reading-room kiosk between Five Points and Georgia State. "
          "Free."),
    Place(OUTDOOR, LULLWATER, "park", ("outdoor", "free", "nature", "solo_friendly", "low_energy", "date"),
          "Emory University's wooded preserve with trails, a lake, a suspension bridge and a small waterfall. "
          "Free to walk."),
    Place(OUTDOOR, WOODLANDS, "garden", ("outdoor", "free", "nature", "solo_friendly", "low_energy", "daytime"),
          "A small woodland garden in Decatur with native plants along shaded paths. "
          "Free to visit; donations welcome."),
    Place(OUTDOOR, RODNEY_COOK, "park", ("outdoor", "free", "family", "low_energy", "learning"),
          "A newer Vine City park with a stormwater pond, a plaza of civil-rights monuments and views of downtown. "
          "Free."),
    Place(OUTDOOR, MASON_MILL, "park", ("outdoor", "free", "nature", "solo_friendly", "low_energy"),
          "A DeKalb County park with tennis courts, a dog park and trails down to old mill ruins on South Peachtree Creek. "
          "Free."),
    Place(OUTDOOR, HERBERT_TAYLOR, "park", ("outdoor", "free", "nature", "solo_friendly", "low_energy"),
          "A quiet Morningside neighborhood park with wooded paths along a creek. "
          "Free."),
    Place(OUTDOOR, EAST_LAKE, "park", ("outdoor", "free", "family", "low_energy"),
          "A neighborhood park in East Lake with lawns, playgrounds and walking paths. "
          "Free."),
    Place(OUTDOOR, GLENLAKE, "park", ("outdoor", "free", "family", "active", "low_energy"),
          "A Decatur park with a pool, tennis courts and a wooded walking loop. "
          "Free to visit."),
    Place(OUTDOOR, DOLLS_HEAD, "park", ("outdoor", "free", "nature", "art", "solo_friendly", "low_energy", "local_favorite"),
          "A folk-art trail at Constitution Lakes where visitors build sculptures from doll parts and objects found along the path. "
          "Free."),

    # ---------------------------------------------------------------- trails (free)
    Place(OUTDOOR, EASTSIDE_TRAIL, "hike", ("outdoor", "free", "active", "art", "group", "low_energy", "touristy"),
          "The busiest stretch of the BeltLine: a paved multi-use trail past murals, Ponce City Market and Krog Street. "
          "Free."),
    Place(OUTDOOR, WESTSIDE_TRAIL, "hike", ("outdoor", "free", "active", "art", "solo_friendly", "low_energy"),
          "A paved BeltLine trail through the West End and Adair Park, with public art and breweries a short walk away. "
          "Free."),
    Place(OUTDOOR, NORTHEAST_TRAIL, "hike", ("outdoor", "free", "active", "nature", "solo_friendly", "low_energy"),
          "A newer BeltLine segment running north from Piedmont Park through Morningside toward Lindbergh. "
          "Free."),
    Place(OUTDOOR, SOUTHSIDE_TRAIL, "hike", ("outdoor", "free", "active", "solo_friendly", "low_energy"),
          "A quieter BeltLine segment through southeast Atlanta neighborhoods. "
          "Free."),
    Place(OUTDOOR, PROCTOR_CREEK, "hike", ("outdoor", "free", "active", "nature", "solo_friendly", "low_energy"),
          "A greenway along Proctor Creek on the city's west side, through wetlands and woods near Shirley Clarke Franklin Park. "
          "Free."),
    Place(OUTDOOR, PEACHTREE_CREEK, "hike", ("outdoor", "free", "active", "nature", "family", "low_energy"),
          "A paved, tree-shaded trail along North Fork Peachtree Creek in Brookhaven. "
          "Free."),
    Place(OUTDOOR, TROLLEY_LINE, "hike", ("outdoor", "free", "active", "nature", "solo_friendly", "low_energy"),
          "A short trail on the route of an old streetcar line, passing through Coan Park. "
          "Free."),
    Place(OUTDOOR, BOB_CALLAN, "hike", ("outdoor", "free", "active", "nature", "solo_friendly", "medium_energy"),
          "A paved creekside trail through wooded DeKalb County land, popular with runners and cyclists. "
          "Free."),
    Place(OUTDOOR, BROOK_RUN, "hike", ("outdoor", "free", "active", "nature", "family", "low_energy"),
          "A paved loop through the woods of Dunwoody's Brook Run Park, with a dog park and skate park nearby. "
          "Free."),
    Place(OUTDOOR, FRAZER_FOREST, "hike", ("outdoor", "free", "nature", "solo_friendly", "low_energy"),
          "Short footpaths through Frazer Forest, a quiet stand of old woods in Druid Hills. "
          "Free."),
    Place(OUTDOOR, HERITAGE_PARK_TRAIL, "hike", ("outdoor", "free", "active", "nature", "learning", "low_energy"),
          "A trail through Mableton's Heritage Park along Nickajack Creek, near the ruins of a 19th-century mill. "
          "Free."),
    Place(OUTDOOR, MOUNTAIN_TO_RIVER, "hike", ("outdoor", "free", "active", "solo_friendly", "low_energy"),
          "A paved segment of Cobb County's Mountain to River Trail along Spring Road in Smyrna. "
          "Free."),

    # ---------------------------------------------------------------- paid outdoors
    Place(OUTDOOR, BOTANICAL_GARDEN, "garden", ("outdoor", "nature", "date", "family", "touristy", "low_energy", "daytime"),
          "A 30-acre garden beside Piedmont Park with a treetop canopy walk, an orchid center and seasonal exhibits. "
          "Adult admission is about $22–$28.",
          price=(22, 28)),
    Place(OUTDOOR, SWEETWATER_WHITE, "hike", ("outdoor", "nature", "active", "cheap", "solo_friendly", "medium_energy"),
          "A 7.4 km wooded loop in Sweetwater Creek State Park, the park that holds the ruins of a Civil War-era textile mill. "
          "Parking requires a $5 Georgia State Parks ParkPass per vehicle.",
          price=(5, 5), name="White Trail at Sweetwater Creek State Park"),
    Place(OUTDOOR, STONE_MTN_CHEROKEE, "hike", ("outdoor", "nature", "active", "cheap", "touristy", "medium_energy"),
          "A 7.5 km loop around the base of Stone Mountain, through woods and along the lakeshore. "
          "Cars need a Stone Mountain Park parking permit, about $20 a day.",
          price=(20, 20), name="Cherokee Trail at Stone Mountain Park"),
    Place(OUTDOOR, COCHRAN_SHOALS, "hike", ("outdoor", "nature", "active", "cheap", "solo_friendly", "low_energy"),
          "A flat loop beside the Chattahoochee River in the national recreation area, popular with runners, with boardwalks over wetlands. "
          "Parking is $5 per vehicle per day.",
          price=(5, 5)),
    Place(OUTDOOR, EAST_PALISADES, "hike", ("outdoor", "nature", "active", "cheap", "solo_friendly", "high_energy"),
          "A rugged Chattahoochee River trail with bluffs, a bamboo grove and river overlooks. "
          "Parking is $5 per vehicle per day.",
          price=(5, 5)),

    # ---------------------------------------------------------------- museums and attractions (paid)
    Place(CULTURE, HIGH_MUSEUM, "museum", ("indoor", "art", "learning", "date", "touristy", "low_energy", "daytime"),
          "The Southeast's leading art museum, in a white Richard Meier building at the Woodruff Arts Center, with American, European and contemporary collections. "
          "Adult admission is about $19.",
          price=(19, 19)),
    Place(CULTURE, FERNBANK, "museum", ("indoor", "learning", "nature", "family", "touristy", "medium_energy", "daytime"),
          "A natural history museum with giant dinosaur skeletons, a giant-screen theater and 65 acres of forest trails out back. "
          "Adult admission is about $25–$30.",
          price=(25, 30), name="Fernbank Museum"),
    Place(CULTURE, HISTORY_CENTER, "museum", ("indoor", "learning", "family", "touristy", "low_energy", "daytime"),
          "A Buckhead history campus with museum galleries, the Cyclorama painting, historic houses and 33 acres of gardens. "
          "Adult admission is about $24–$28.",
          price=(24, 28)),
    Place(CULTURE, CARLOS, "museum", ("indoor", "art", "learning", "solo_friendly", "low_energy", "daytime"),
          "Emory University's museum of art and archaeology, with Egyptian, Greek, Roman and ancient American collections. "
          "Adult admission is about $8.",
          price=(8, 8)),
    Place(CULTURE, CARTER_CENTER, "museum", ("indoor", "learning", "solo_friendly", "low_energy", "daytime"),
          "Exhibits on the Carter presidency, a replica Oval Office and a Japanese garden, in parkland off Freedom Parkway. "
          "Adult admission is about $12.",
          price=(12, 12)),
    Place(CULTURE, MODA, "museum", ("indoor", "art", "learning", "solo_friendly", "low_energy"),
          "A small Midtown museum devoted to design, with rotating exhibitions on architecture, products and technology. "
          "Adult admission is about $12–$15.",
          price=(12, 15)),
    Place(CULTURE, AQUARIUM, "zoo_aquarium", ("indoor", "nature", "family", "touristy", "medium_energy", "daytime", "splurge"),
          "One of the world's largest aquariums, with whale sharks, beluga whales and a walk-through ocean tunnel. "
          "General admission typically runs $45–$55.",
          price=(45, 55)),
    Place(CULTURE, ZOO, "zoo_aquarium", ("outdoor", "nature", "family", "touristy", "medium_energy", "daytime"),
          "A zoo in historic Grant Park with gorillas, elephants and a big reptile and amphibian house. "
          "Adult admission is about $30–$35.",
          price=(30, 35)),
    Place(CULTURE, PUPPETRY, "museum", ("indoor", "art", "family", "learning", "low_energy", "daytime"),
          "A museum of puppetry with a Jim Henson collection and puppets from around the world, from shadow figures to marionettes. "
          "Museum admission is about $15–$20.",
          price=(15, 20)),
    Place(CULTURE, APEX, "museum", ("indoor", "learning", "solo_friendly", "low_energy", "daytime", "cheap"),
          "A Sweet Auburn museum telling the story of African American history, from Africa to Atlanta's Auburn Avenue. "
          "Admission is about $7–$10.",
          price=(7, 10)),

    # ---------------------------------------------------------------- culture (free)
    Place(CULTURE, MLK_NHP, "museum", ("indoor", "free", "learning", "touristy", "low_energy", "daytime", "solo_friendly"),
          "The national historical park on Auburn Avenue covering Dr. King's birth home, Ebenezer Baptist Church and a visitor center with exhibits. "
          "Free.",
          price=FREE),
    Place(CULTURE, KING_CENTER, "landmark", ("outdoor", "free", "learning", "touristy", "low_energy", "daytime"),
          "The memorial where Dr. and Mrs. King are entombed, with a reflecting pool, an eternal flame and exhibits. "
          "Free.",
          price=FREE),
    Place(CULTURE, KROG_TUNNEL, "landmark", ("outdoor", "free", "art", "solo_friendly", "low_energy", "local_favorite", "touristy"),
          "A graffiti-covered tunnel between Cabbagetown and Inman Park, repainted constantly by local artists. "
          "Free and open around the clock.",
          price=FREE),
    Place(CULTURE, ABV, "gallery", ("indoor", "free", "art", "date", "low_energy", "local_favorite"),
          "A southeast Atlanta gallery showing street art, pop surrealism and emerging artists. "
          "Free to browse.",
          price=FREE),
    Place(CULTURE, ZUCOT, "gallery", ("indoor", "free", "art", "learning", "low_energy", "daytime"),
          "A large African American-owned fine art gallery near Centennial Olympic Park, showing painting, sculpture and prints. "
          "Free to browse.",
          price=FREE),
    Place(CULTURE, MET_ATL, "gallery", ("indoor", "free", "art", "solo_friendly", "low_energy", "daytime"),
          "A converted industrial complex in Adair Park with artist studios, galleries and creative businesses. "
          "Free to walk around.",
          price=FREE),
    Place(CULTURE, CDC_MUSEUM, "museum", ("indoor", "free", "learning", "solo_friendly", "low_energy", "daytime"),
          "The CDC's public museum on disease detection and public-health history, at the agency's headquarters. "
          "Free; bring a photo ID.",
          price=FREE),
    Place(CULTURE, CAPITOL_MUSEUM, "museum", ("indoor", "free", "learning", "touristy", "low_energy", "daytime"),
          "Exhibits on Georgia history and government inside the gold-domed State Capitol. "
          "Free on weekdays.",
          price=FREE),
    Place(CULTURE, FERNBANK_SCIENCE, "museum", ("indoor", "free", "learning", "family", "low_energy"),
          "A public science center with exhibits, a planetarium and an observatory that opens on some evenings. "
          "Free admission; planetarium shows cost extra.",
          price=FREE),
    Place(CULTURE, MASON_FINE_ART, "gallery", ("indoor", "free", "art", "date", "low_energy", "daytime"),
          "A Miami Circle gallery showing contemporary painting and sculpture by regional and national artists. "
          "Free to browse.",
          price=FREE),
    Place(CULTURE, OUTKAST_MURAL, "landmark", ("outdoor", "free", "art", "music", "solo_friendly", "low_energy", "touristy"),
          "A large mural honoring Atlanta hip-hop duo OutKast on a wall near Little Five Points. "
          "Free to visit any time.",
          price=FREE),

    # ---------------------------------------------------------------- active recreation (paid)
    Place(ACTIVE, PUTTSHACK, "rec_venue", ("indoor", "group", "date", "drinks", "medium_energy"),
          "Tech-enabled mini golf in West Midtown with automatic scoring, cocktails and shareable food. "
          "A round is about $15–$25 per person.",
          price=(15, 25), name="Puttshack Midtown"),
    Place(ACTIVE, PAINTED_DUCK, "rec_venue", ("indoor", "group", "date", "drinks", "medium_energy", "local_favorite"),
          "Duckpin bowling, lawn games and a full menu in a converted West Midtown warehouse. "
          "Games run about $10–$25 per person.",
          price=(10, 25)),
    Place(ACTIVE, PAINTED_PICKLE, "rec_venue", ("indoor", "active", "group", "drinks", "high_energy"),
          "An indoor-outdoor pickleball club near Armour Yards with courts to rent, a bar and food. "
          "Court time is about $15–$25 per person.",
          price=(15, 25)),
    Place(ACTIVE, MIDTOWN_BOWL, "rec_venue", ("indoor", "group", "family", "late_night", "cheap", "medium_energy", "local_favorite"),
          "A classic Atlanta bowling alley with a bar and late weekend hours. "
          "Plan on about $10–$25 per person with shoes.",
          price=(10, 25)),
    Place(ACTIVE, SKYLINE_PARK, "rec_venue", ("outdoor", "group", "family", "touristy", "medium_energy"),
          "Carnival games, mini golf and a small thrill ride on the roof of Ponce City Market, with skyline views. "
          "Admission is about $15–$20.",
          price=(15, 20)),
    Place(ACTIVE, SANDBOX_VR, "rec_venue", ("indoor", "active", "group", "high_energy", "splurge"),
          "Full-body virtual-reality missions for small groups, with motion capture and haptic vests. "
          "Sessions are about $40–$55 per person.",
          price=(40, 55)),
    Place(ACTIVE, BEAT_THE_BOMB, "rec_venue", ("indoor", "active", "group", "high_energy"),
          "Team game rooms that end with a paint or foam 'bomb' if you fail the final puzzle. "
          "About $35–$45 per person.",
          price=(35, 45)),
    Place(ACTIVE, ACTIVATE, "rec_venue", ("indoor", "active", "group", "high_energy"),
          "Game rooms of lights, lasers and pressure-sensitive floors that test speed and teamwork. "
          "About $25–$35 per person.",
          price=(25, 35)),

    # ---------------------------------------------------------------- markets and districts (free to browse)
    Place(COMMUNITY, PCM, "market", ("indoor", "free", "food", "group", "touristy", "low_energy", "local_favorite"),
          "A food hall and shopping center in the old Sears building on the BeltLine, with dozens of food stalls and shops. "
          "Free to wander; food and shopping are extra.",
          price=FREE),
    Place(COMMUNITY, DEKALB_MARKET, "market", ("indoor", "free", "food", "family", "low_energy", "local_favorite"),
          "A huge international market with produce, seafood and a cafeteria-style hot bar. "
          "Free to wander; groceries and food are extra.",
          price=FREE),
    Place(COMMUNITY, DECATUR_MARKET, "market", ("outdoor", "free", "food", "family", "low_energy", "local_favorite"),
          "A small farmers market with local produce, bread and prepared food on weekday afternoons. "
          "Free to browse; vendors sell by the item.",
          price=FREE),
    Place(COMMUNITY, WESTSIDE_PROVISIONS, "shopping", ("outdoor", "free", "food", "date", "low_energy"),
          "A West Midtown district of design shops, boutiques and restaurants in converted industrial buildings. "
          "Free to browse.",
          price=FREE),
    Place(COMMUNITY, BUCKHEAD_VILLAGE, "shopping", ("outdoor", "free", "date", "touristy", "low_energy"),
          "An upscale walkable shopping district of boutiques, galleries and cafés in the heart of Buckhead. "
          "Free to browse.",
          price=FREE),
]


# ======================================================================== events
EVENTS = [
    # ---------------------------------------------------------------- nightlife (13 paid, 4 free)
    Event(NIGHT, "Amapiano Sunset Session", "nightclub", ("outdoor", "music", "drinks", "21_plus", "group", "medium_energy"),
          HIGH_NOTE, (D0,), "16:00", 300, (15, 20),
          "DJs play amapiano and Afro-house on a Midtown rooftop as the sun goes down. "
          "Tickets are $15–$20 at the door; drinks are sold separately.",
          attendance="drop_in"),
    Event(NIGHT, "Thursday Deep House Session", "nightclub", ("indoor", "late_night", "music", "drinks", "21_plus", "medium_energy"),
          LORE, (THU2,), "21:00", 240, (10, 10),
          "Local selectors play deep and soulful house into the night on Edgewood Avenue. $10 cover.",
          attendance="drop_in"),
    Event(NIGHT, "Sunday Soul & Disco Night", "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group"),
          ROYAL_PEACOCK, (D0,), "22:00", 300, (15, 15),
          "Classic soul, funk and disco records keep the floor moving past midnight on Auburn Avenue. $15 cover.",
          attendance="drop_in"),
    Event(NIGHT, "Midweek Dance Night", "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group"),
          HERETIC, (WED1,), "22:00", 240, (10, 10),
          "Pop remixes and dance anthems on a Wednesday, with drink specials before 11. $10 cover.",
          attendance="drop_in"),
    Event(NIGHT, "House & Disco Friday", "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group"),
          MJQ, (FRI1,), "23:00", 300, (15, 20),
          "Resident DJs mix classic house and disco edits until close in the club's Underground Atlanta rooms. "
          "$15–$20 cover.",
          attendance="drop_in"),
    Event(NIGHT, "Salsa & Bachata Fridays", "nightclub", ("indoor", "late_night", "music", "high_energy", "group", "21_plus", "drinks", "learning"),
          HAVANA, (FRI1, FRI2), "21:00", 240, (15, 15),
          "A beginner salsa and bachata lesson at 9, then social dancing to a Latin DJ until 1. "
          "$15 at the door, lesson included."),
    Event(NIGHT, "Silent Disco on the Roof", "nightclub", ("outdoor", "late_night", "music", "high_energy", "group", "drinks"),
          ROOF_PCM, (FRI1,), "20:00", 240, (20, 20),
          "Three DJ channels on wireless headphones above the BeltLine, with skyline views from the Ponce City Market roof. "
          "$20 includes headphones.",
          attendance="drop_in"),
    Event(NIGHT, "Warehouse Techno Night", "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks"),
          PISCES, (SAT1,), "22:00", 300, (15, 25),
          "Hypnotic techno from local and touring DJs in a dark Edgewood Avenue room until 3 a.m. $15–$25; 21+.",
          attendance="drop_in"),
    Event(NIGHT, "Country Night & Line Dancing Lesson", "nightclub", ("indoor", "late_night", "music", "high_energy", "group", "21_plus", "drinks", "learning"),
          PBR, (SAT1,), "21:00", 240, (10, 10),
          "A line-dancing lesson at 9 comes with the $10 cover, then a country band plays until 1."),
    Event(NIGHT, "Sunday Day Party on the Patio", "nightclub", ("outdoor", "music", "drinks", "21_plus", "group", "high_energy"),
          WESTSIDE_MOTOR_LOUNGE, (SUN1,), "15:00", 360, (20, 30),
          "An afternoon-into-evening dance party with house and disco DJs on a big West Midtown patio. $20–$30; 21+.",
          attendance="drop_in"),
    Event(NIGHT, "Afrobeats Friday", "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "drinks", "group"),
          ROYAL_PEACOCK, (FRI2,), "22:00", 300, (20, 20),
          "Afrobeats, amapiano and dancehall until late on Auburn Avenue. $20 cover.",
          attendance="drop_in"),
    Event(NIGHT, "Bass Music Night", "nightclub", ("indoor", "late_night", "music", "high_energy", "21_plus", "group"),
          LUNCHBOX, (SAT2,), "23:00", 300, (10, 15),
          "Dubstep, drum and bass and UK garage on a heavy sound system in Underground Atlanta. $10–$15 at the door; 21+.",
          attendance="drop_in"),
    Event(NIGHT, "Sunset Salsa Social", "community_event", ("outdoor", "free", "music", "group", "solo_friendly", "medium_energy"),
          PIEDMONT, (D0,), "17:00", 180, FREE,
          "A free open-air salsa social on the park lawn, with a short beginner lesson at 5. "
          "Partners rotate, so no experience or partner is needed.",
          attendance="drop_in"),
    Event(NIGHT, "First Friday DJ Session", "community_event", ("outdoor", "free", "music", "group", "medium_energy"),
          BELTLINE_SHED, (FRI1,), "18:00", 180, FREE,
          "A free all-ages DJ set under the Beltline Shed, steps from the Eastside Trail, with dancing as the sun goes down.",
          attendance="drop_in"),
    Event(NIGHT, "Swing Dance Social", "community_event", ("outdoor", "free", "music", "group", "solo_friendly", "medium_energy"),
          FOURTH_WARD, (SUN1,), "16:00", 180, FREE,
          "A free swing dance social by the park's amphitheater, with a 20-minute beginner lesson at 4. No partner needed.",
          attendance="drop_in"),
    Event(NIGHT, "Downtown Dance Party", "community_event", ("outdoor", "free", "music", "group", "high_energy"),
          WOODRUFF, (SAT2,), "19:00", 180, FREE,
          "A free DJ dance party in Woodruff Park with lights and a playlist running from Motown to Afrobeats.",
          attendance="drop_in"),

    # ---------------------------------------------------------------- concerts and live music (20 paid, 8 free)
    Event(MUSIC, "Soul & Funk Revue", "live_music", ("indoor", "music", "group", "drinks", "high_energy"),
          TERMINAL_WEST, (D0,), "19:00", 180, (25, 35),
          "A ten-piece band with a horn section runs through classic soul and funk for a packed dance floor. Tickets $25–$35."),
    Event(MUSIC, "Indie Rock Triple Bill", "live_music", ("indoor", "music", "group", "drinks", "medium_energy"),
          AISLE5, (D0,), "20:00", 150, (15, 18),
          "Three up-and-coming local indie bands share the bill in a Little Five Points club. Tickets $15–$18."),
    Event(MUSIC, "Punk & Garage Night", "live_music", ("indoor", "music", "drinks", "21_plus", "late_night", "high_energy", "local_favorite"),
          THE_EARL, (D0,), "20:00", 180, (12, 15),
          "Loud, fast local punk and garage bands in the back room of an East Atlanta Village institution. $12–$15; 21+."),
    Event(MUSIC, "R&B Showcase", "live_music", ("indoor", "music", "date", "drinks", "medium_energy"),
          BUCKHEAD_THEATRE, (D0,), "19:30", 150, (20, 25),
          "Rising Atlanta R&B singers perform short sets with a live band in a restored Buckhead theater. Tickets $20–$25."),
    Event(MUSIC, "Alt-Rock Double Bill", "live_music", ("indoor", "music", "group", "high_energy"),
          MASQ_ALTAR, (MON1,), "20:00", 150, (18, 22),
          "Two touring alternative rock bands split a Monday night in The Masquerade's smallest room. Tickets $18–$22."),
    Event(MUSIC, "Americana Night", "live_music", ("indoor", "music", "date", "low_energy"),
          VARIETY, (TUE1,), "20:00", 150, (25, 35),
          "Roots, folk and alt-country songwriters with a full band in a restored Little Five Points theater. Tickets $25–$35."),
    Event(MUSIC, "Metal Triple Bill", "live_music", ("indoor", "music", "group", "late_night", "high_energy"),
          MASQ_HELL, (MON2,), "20:00", 180, (20, 25),
          "Three heavy metal bands play back to back in The Masquerade's Hell room. Tickets $20–$25."),
    Event(MUSIC, "Hip-Hop Showcase", "live_music", ("indoor", "music", "group", "late_night", "high_energy"),
          TABERNACLE, (FRI1,), "20:00", 180, (30, 45),
          "Atlanta rappers and producers take turns on the stage of a converted downtown church. Tickets $30–$45."),
    Event(MUSIC, "Neo-Soul Night", "live_music", ("indoor", "music", "date", "medium_energy"),
          CENTER_STAGE, (THU2,), "20:00", 150, (25, 40),
          "A neo-soul singer and band play a seated show in a Midtown theater. Tickets $25–$40."),
    Event(MUSIC, "Latin Jazz Orchestra", "live_music", ("indoor", "music", "date", "medium_energy"),
          BUCKHEAD_THEATRE, (SAT1,), "20:00", 150, (30, 55),
          "A 16-piece Latin jazz orchestra plays mambo, son and bossa nova in a restored Buckhead theater. Tickets $30–$55."),
    Event(MUSIC, "Folk Singer-Songwriter Night", "live_music", ("indoor", "music", "date", "low_energy"),
          VARIETY, (SUN1,), "19:00", 150, (25, 30),
          "Three folk songwriters trade songs and stories on a Sunday night in Little Five Points. Tickets $25–$30."),
    Event(MUSIC, "Singer-Songwriter Showcase", "live_music", ("indoor", "music", "solo_friendly", "low_energy", "cheap"),
          VINYL, (MON2,), "20:00", 120, (15, 15),
          "Four local songwriters play short acoustic sets in a small Midtown club. $15."),
    Event(MUSIC, "Jam Band Night", "live_music", ("indoor", "music", "group", "drinks", "late_night", "medium_energy"),
          TERMINAL_WEST, (TUE2,), "20:00", 180, (20, 25),
          "Long improvised sets from two jam bands in a West Midtown club. Tickets $20–$25."),
    Event(MUSIC, "Acoustic Duo in the Listening Room", "live_music", ("indoor", "music", "date", "low_energy"),
          EDDIES_ATTIC, (WED2,), "19:00", 120, (15, 20),
          "A guitar-and-violin duo plays original songs in a Decatur listening room known for quiet, attentive crowds. Tickets $15–$20."),
    Event(MUSIC, "Jazz-Funk Fusion Night", "live_music", ("indoor", "music", "group", "drinks", "medium_energy"),
          AISLE5, (WED2,), "20:00", 150, (15, 20),
          "A jazz-funk quintet and an opening trio play two sets in Little Five Points. Tickets $15–$20."),
    Event(MUSIC, "Romantic Masterworks", "live_music", ("indoor", "music", "date", "low_energy", "splurge"),
          SYMPHONY_HALL, (THU2,), "20:00", 120, (35, 110),
          "An orchestral program of 19th-century Romantic symphonies in Symphony Hall. Tickets $35–$110."),
    Event(MUSIC, "Synth-Pop Night", "live_music", ("indoor", "music", "group", "late_night", "medium_energy"),
          MASQ_PURGATORY, (THU2,), "20:00", 180, (22, 30),
          "Two synth-pop acts bring drum machines and neon to The Masquerade's Purgatory room. Tickets $22–$30."),
    Event(MUSIC, "Pop-Punk Revival", "live_music", ("indoor", "music", "group", "high_energy"),
          MASQ_HEAVEN, (FRI2,), "19:00", 210, (25, 35),
          "Three bands revive 2000s pop-punk for a singalong crowd in The Masquerade's biggest room. Tickets $25–$35."),
    Event(MUSIC, "Symphonic Film Music Concert", "live_music", ("indoor", "music", "date", "touristy", "low_energy", "splurge"),
          FOX, (FRI2,), "19:30", 150, (45, 95),
          "A full orchestra plays famous film scores under the Fox Theatre's starry ceiling. Tickets $45–$95."),
    Event(MUSIC, "Indie Dance Night", "live_music", ("indoor", "music", "group", "late_night", "high_energy"),
          THE_EASTERN, (SAT2,), "20:00", 210, (25, 25),
          "An indie dance band and a DJ keep the floor moving at a Reynoldstown venue. Tickets $25."),
    Event(MUSIC, "Jazz on the Lawn", "live_music", ("outdoor", "free", "music", "family", "daytime", "low_energy", "group"),
          PIEDMONT, (D0,), "12:00", 180, FREE,
          "A local jazz trio plays standards on the park lawn; bring a blanket and stay for a set or the whole afternoon. Free.",
          attendance="drop_in"),
    Event(MUSIC, "Faculty & Student Chamber Recital", "live_music", ("indoor", "free", "music", "learning", "daytime", "low_energy", "solo_friendly"),
          KOPLEFF, (D0,), "14:00", 90, FREE,
          "Georgia State music students and faculty perform chamber works for strings and piano. Free; seating is first come, first served."),
    Event(MUSIC, "Acoustic Open Mic", "live_music", ("indoor", "free", "music", "solo_friendly", "low_energy"),
          POUR_TAPROOM, (TUE1, TUE2), "19:00", 180, FREE,
          "Singers and songwriters take turns on a small stage by the BeltLine; performers sign up at 6:30. No cover.",
          attendance="drop_in"),
    Event(MUSIC, "Brass Band on the BeltLine", "live_music", ("outdoor", "free", "music", "family", "daytime", "medium_energy"),
          FOURTH_WARD, (SAT1,), "11:00", 180, FREE,
          "A New Orleans-style brass band plays by the park's lake for walkers and runners on the Eastside Trail. Free.",
          attendance="drop_in"),
    Event(MUSIC, "Bluegrass Jam in the Park", "live_music", ("outdoor", "free", "music", "solo_friendly", "daytime", "low_energy", "local_favorite"),
          GRANT_PARK, (SUN1,), "14:00", 180, FREE,
          "Pickers circle up under the trees for an open bluegrass jam; bring an instrument or just listen. Free.",
          attendance="drop_in"),
    Event(MUSIC, "Student Jazz Ensemble Concert", "live_music", ("indoor", "free", "music", "learning", "solo_friendly", "low_energy"),
          FERST, (FRI2,), "19:00", 120, FREE,
          "Georgia Tech's student jazz ensemble plays big-band standards and new arrangements on campus. Free admission."),
    Event(MUSIC, "Sunset Acoustic Set", "live_music", ("outdoor", "free", "music", "date", "low_energy"),
          BELTLINE_SHED, (SAT2,), "16:30", 180, FREE,
          "A singer-songwriter plays covers and originals under the Beltline Shed as the light fades. Free.",
          attendance="drop_in"),

    # ---------------------------------------------------------------- dinner experiences (10, all paid)
    Event(DINNER, "Sunday Supper: Beer Pairing Dinner", "restaurant", ("indoor", "food", "drinks", "21_plus", "date", "group", "low_energy"),
          MNB_GARAGE, (D0,), "18:00", 150, (65, 75),
          "A guest chef cooks four courses paired with house beers in the brewery's West End taproom. "
          "Tickets $65–$75 include food and pairings; 21+."),
    Event(DINNER, "Garden Harvest Dinner", "restaurant", ("outdoor", "food", "drinks", "date", "splurge", "low_energy"),
          BOTANICAL_GARDEN, (TUE1,), "19:00", 150, (95, 125),
          "A three-course seasonal dinner at long tables in the garden after closing, cooked from local farm produce. "
          "Tickets $95–$125, wine included."),
    Event(DINNER, "Wine & Cheese Pairing Dinner", "restaurant", ("indoor", "food", "drinks", "21_plus", "date", "low_energy", "splurge"),
          SIDE_SADDLE, (WED1,), "19:00", 120, (55, 65),
          "Five small plates matched with five wines, led by the bar's team in a cozy southeast Atlanta wine saloon. $55–$65; 21+."),
    Event(DINNER, "Rooftop Supper Club", "restaurant", ("outdoor", "food", "drinks", "date", "splurge", "low_energy"),
          ROOF_PCM, (THU1,), "19:00", 150, (85, 95),
          "A family-style Southern supper at shared tables on the Ponce City Market roof, with city views at dusk. "
          "Tickets $85–$95 include a welcome cocktail."),
    Event(DINNER, "Low-Country Boil on the Patio", "restaurant", ("outdoor", "food", "drinks", "group", "medium_energy"),
          WILD_HEAVEN, (SAT1,), "18:30", 150, (45, 55),
          "Shrimp, sausage, corn and potatoes poured onto paper-covered tables in the brewery's garden. $45–$55 includes one beer."),
    Event(DINNER, "Pig Roast & Bluegrass Supper", "restaurant", ("outdoor", "food", "drinks", "music", "group", "medium_energy"),
          SWEETWATER_BREW, (SUN1,), "17:00", 150, (40, 50),
          "Whole-hog barbecue with sides, a pint and a bluegrass band on the brewery lawn. Tickets $40–$50."),
    Event(DINNER, "Italian Wine Dinner", "restaurant", ("indoor", "food", "drinks", "21_plus", "date", "low_energy", "splurge"),
          TASTE_WINE, (WED2,), "19:00", 150, (60, 75),
          "Four courses of northern Italian cooking paired with wines from Piedmont and the Veneto. Tickets $60–$75; 21+."),
    Event(DINNER, "Chef Collaboration Dinner", "restaurant", ("indoor", "food", "drinks", "21_plus", "date", "splurge", "low_energy"),
          THREE_TAVERNS, (THU2,), "19:00", 150, (70, 85),
          "Two local chefs cook a five-course menu around the brewery's Belgian-style beers. Tickets $70–$85; 21+."),
    Event(DINNER, "Rooftop Oyster Roast", "restaurant", ("outdoor", "food", "drinks", "date", "21_plus", "medium_energy", "splurge"),
          HIGH_NOTE, (FRI2,), "19:00", 150, (50, 70),
          "Roasted and raw Gulf oysters with sides and a sunset view over Midtown. Tickets $50–$70; 21+."),
    Event(DINNER, "Candlelit Dinner at the Swan House", "restaurant", ("indoor", "food", "drinks", "date", "splurge", "low_energy", "learning"),
          SWAN_HOUSE, (SAT2,), "19:00", 150, (110, 150),
          "A four-course dinner by candlelight in the 1928 Swan House mansion, with a short history talk between courses. "
          "Tickets $110–$150."),

    # ---------------------------------------------------------------- brunch and breakfast events (11, all paid)
    Event(BRUNCH, "Beer & Biscuits Brunch", "restaurant", ("indoor", "food", "drinks", "group", "daytime", "medium_energy"),
          MNB_GROVE, (D0,), "11:00", 120, (25, 30),
          "Scratch biscuits, fried chicken and a beer flight at the brewery's West Midtown Sunday brunch seating. $25–$30 per person."),
    Event(BRUNCH, "Bluegrass Brunch", "restaurant", ("indoor", "food", "music", "drinks", "group", "daytime", "low_energy", "local_favorite"),
          THE_EARL, (D0, SUN1), "11:30", 120, (20, 30),
          "A Sunday brunch plate with a local bluegrass band playing the East Atlanta Village room. $20–$30 per person."),
    Event(BRUNCH, "Sunday Jazz Brunch", "restaurant", ("indoor", "food", "drinks", "music", "daytime", "date", "low_energy"),
          PARK_BAR, (D0,), "11:30", 120, (25, 35),
          "A jazz trio plays through brunch at a downtown bar a short walk from Centennial Olympic Park. $25–$35 per person."),
    Event(BRUNCH, "Drag Brunch", "restaurant", ("indoor", "food", "drinks", "art", "group", "daytime", "high_energy"),
          LIPS, (D0, SUN1), "12:30", 120, (35, 45),
          "Queens perform between courses at a Sunday brunch show; tickets include a brunch entrée. $35–$45."),
    Event(BRUNCH, "Community Pancake Breakfast", "community_event", ("outdoor", "food", "family", "group", "daytime", "cheap", "low_energy"),
          DECATUR_SQUARE, (SAT1,), "08:00", 180, (10, 10),
          "Volunteers flip pancakes and sausage on Decatur Square to raise money for local schools. $10 a plate, coffee included.",
          attendance="drop_in"),
    Event(BRUNCH, "Donut & Coffee Walking Tour", "tour", ("outdoor", "food", "group", "daytime", "touristy", "medium_energy"),
          KROG_TUNNEL, (SAT1, SAT2), "10:00", 120, (35, 40),
          "A guide leads a two-hour walk through Inman Park and Cabbagetown with tastings at four coffee and doughnut stops. "
          "Meets at the Krog Street Tunnel; $35–$40 includes tastings."),
    Event(BRUNCH, "Drag Bingo Brunch", "restaurant", ("indoor", "food", "drinks", "group", "daytime", "high_energy"),
          MIDWAY, (SUN1,), "12:00", 120, (20, 25),
          "Brunch plates and bingo cards with a drag host calling numbers in East Atlanta Village. "
          "$20–$25 includes an entrée and three cards."),
    Event(BRUNCH, "Rooftop Brunch", "restaurant", ("outdoor", "food", "drinks", "date", "daytime", "low_energy"),
          ROOF_PCM, (SUN1,), "11:00", 150, (35, 45),
          "Chicken and waffles, shrimp and grits and skyline views at a seated brunch on the Ponce City Market roof. $35–$45 per person."),

    # ---------------------------------------------------------------- outdoors (4 free walks, 1 paid paddle)
    Event(OUTDOOR, "Naturalist-Led Walk at Constitution Lakes", "tour", ("outdoor", "free", "nature", "learning", "daytime", "low_energy", "solo_friendly"),
          CONSTITUTION_LAKES, (D0,), "09:00", 90, FREE,
          "A volunteer naturalist leads a slow loop around the lakes and boardwalks, pointing out herons, turtles and wildflowers. "
          "Free; meet at the parking lot."),
    Event(OUTDOOR, "Early Birding Walk", "tour", ("outdoor", "free", "nature", "learning", "daytime", "low_energy", "solo_friendly"),
          LULLWATER, (SUN1,), "08:30", 90, FREE,
          "Binoculars on: a volunteer birder leads a morning walk around the lake looking for fall migrants. Free; loaner binoculars available."),
    Event(OUTDOOR, "Piedmont Park Tree Walk", "tour", ("outdoor", "free", "nature", "learning", "daytime", "low_energy", "family"),
          PIEDMONT, (SAT2,), "11:30", 90, FREE,
          "A volunteer arborist leads a walk past the park's oldest trees and shows how to identify them. Free."),
    Event(OUTDOOR, "Butterfly Walk", "tour", ("outdoor", "free", "nature", "learning", "daytime", "low_energy", "family"),
          WOODLANDS, (SAT2,), "13:00", 60, FREE,
          "A garden volunteer shows which native plants draw monarchs and other butterflies in early fall. Free."),
    Event(OUTDOOR, "Guided Sunset Kayak on the Chattahoochee", "tour", ("outdoor", "nature", "active", "group", "date", "medium_energy", "splurge"),
          PACES_MILL, (SAT1,), "17:00", 120, (55, 65),
          "A guided two-hour paddle on a calm stretch of the Chattahoochee that ends as the sun sets. "
          "$55–$65 includes kayak, paddle and life jacket; no experience needed."),

    # ---------------------------------------------------------------- culture events (3 paid, 4 free)
    Event(CULTURE, "Curator-Led Gallery Tour After Hours", "tour", ("indoor", "art", "learning", "date", "low_energy"),
          HIGH_MUSEUM, (THU1,), "18:00", 90, (25, 30),
          "A curator walks a small group through the current special exhibition after the galleries close. $25–$30, admission included."),
    Event(CULTURE, "Lunchtime Architecture Walk", "tour", ("outdoor", "learning", "touristy", "daytime", "low_energy", "solo_friendly"),
          WOODRUFF, (FRI1,), "12:00", 90, (20, 20),
          "A guide explains downtown's skyline, from Fairlie-Poplar's early skyscrapers to Peachtree Center's 1970s towers. $20."),
    Event(CULTURE, "Behind-the-Scenes Aquarium Tour", "tour", ("indoor", "learning", "family", "touristy", "daytime", "low_energy", "splurge"),
          AQUARIUM, (FRI1,), "17:00", 60, (60, 70),
          "An aquarium guide takes a small group above the exhibits to see filtration systems and animal kitchens. "
          "$60–$70 includes general admission."),
    Event(CULTURE, "Ranger-Led Walk on Auburn Avenue", "tour", ("outdoor", "free", "learning", "touristy", "daytime", "low_energy", "solo_friendly"),
          MLK_NHP, (D0,), "11:00", 60, FREE,
          "A park ranger leads a short walk past Dr. King's birth home and Ebenezer Baptist Church. Free; meet at the visitor center."),
    Event(CULTURE, "Opening Reception: New Works", "gallery", ("indoor", "free", "art", "group", "date", "medium_energy"),
          ABV, (THU1,), "18:00", 180, FREE,
          "The gallery opens a new group show with the artists in the room and a DJ. Free admission.",
          attendance="drop_in"),
    Event(CULTURE, "Free Observatory Night", "museum", ("indoor", "free", "learning", "nature", "family", "low_energy"),
          FERNBANK_SCIENCE, (FRI1,), "20:00", 120, FREE,
          "If skies are clear, astronomers point the science center's research telescope at the Moon, planets and star clusters. Free.",
          attendance="drop_in"),
    Event(CULTURE, "Open Studios Night", "gallery", ("indoor", "free", "art", "group", "medium_energy"),
          MET_ATL, (FRI2,), "18:00", 180, FREE,
          "Artists at the MET open their studio doors for an evening of new work and conversation. Free.",
          attendance="drop_in"),

    # ---------------------------------------------------------------- workshops and classes (18 paid, 6 free)
    Event(CLASS, "Make a Glass Paperweight", "class_workshop", ("indoor", "art", "learning", "date", "medium_energy", "splurge"),
          DECATUR_GLASS, (D0,), "14:00", 60, (85, 95),
          "An instructor guides you through gathering and shaping molten glass into a paperweight to pick up a few days later. $85–$95."),
    Event(CLASS, "Wheel-Throwing Taster", "class_workshop", ("indoor", "art", "learning", "solo_friendly", "daytime", "low_energy", "splurge"),
          CALLANWOLDE, (SAT1, SAT2), "10:00", 120, (55, 55),
          "Try the pottery wheel in the arts center's clay studio: centering, pulling walls and trimming a first bowl. "
          "$55 includes clay and firing."),
    Event(CLASS, "Create-A-Puppet Workshop", "class_workshop", ("indoor", "art", "family", "learning", "daytime", "low_energy", "cheap"),
          PUPPETRY, (D0,), "13:30", 60, (15, 15),
          "Build a simple puppet to take home after the family show; materials are included. $15 per person."),
    Event(CLASS, "Design Lab: Intro to 3D Printing", "class_workshop", ("indoor", "learning", "art", "solo_friendly", "low_energy"),
          MODA, (THU2,), "17:00", 120, (35, 45),
          "Model a small object, slice it and watch it print in the museum's design lab. $35–$45 includes admission."),
    Event(CLASS, "Cocktail Class: Classic Sours", "class_workshop", ("indoor", "drinks", "learning", "21_plus", "date", "group", "low_energy"),
          RANGER_STATION, (TUE1,), "18:30", 90, (50, 50),
          "Shake three classic sours with the bar team and take home the recipes. $50 includes every drink; 21+."),
    Event(CLASS, "Wine Tasting 101", "class_workshop", ("indoor", "drinks", "learning", "21_plus", "date", "low_energy"),
          TASTE_WINE, (THU1,), "18:30", 90, (40, 40),
          "Taste six wines side by side and learn how grape, place and winemaking shape each glass. $40; 21+."),
    Event(CLASS, "Latte Art Basics", "class_workshop", ("indoor", "learning", "daytime", "solo_friendly", "low_energy"),
          CAFE_BELLI, (SAT1,), "09:00", 90, (35, 35),
          "Learn to steam milk and pour hearts and rosettas on the café's espresso machine. $35 includes every drink you pour."),
    Event(CLASS, "Improv for Absolute Beginners", "class_workshop", ("indoor", "learning", "group", "solo_friendly", "daytime", "medium_energy"),
          WHOLE_WORLD, (SAT2,), "11:00", 120, (30, 30),
          "Warm-ups, scene games and a lot of laughing with a teacher from the theater's ensemble. $30; no experience needed."),
    Event(CLASS, "Watercolor in the Garden", "class_workshop", ("outdoor", "art", "learning", "nature", "daytime", "low_energy", "solo_friendly"),
          BOTANICAL_GARDEN, (SUN1,), "10:00", 120, (55, 55),
          "Paint the fall borders with a teaching artist; supplies and garden admission are included. $55."),
    Event(CLASS, "Sketching in the Galleries", "class_workshop", ("indoor", "art", "learning", "daytime", "low_energy", "solo_friendly"),
          HIGH_MUSEUM, (SAT2,), "10:30", 90, (30, 35),
          "An artist-led drawing session among the museum's sculptures; pencils and paper provided. $30–$35 with admission."),
    Event(CLASS, "Personal Essay Writing Workshop", "class_workshop", ("indoor", "learning", "solo_friendly", "low_energy"),
          CHARIS, (WED1,), "19:00", 120, (25, 25),
          "A local writer leads prompts and gentle feedback on short personal essays in Decatur's long-running feminist bookstore. $25."),
    Event(CLASS, "Street-Art Photography Walk", "class_workshop", ("outdoor", "art", "learning", "group", "medium_energy"),
          KROG_TUNNEL, (WED1,), "17:30", 90, (30, 30),
          "A photographer teaches composition and low-light tips while walking the murals from Krog Street Tunnel along the BeltLine. "
          "$30; bring any camera or phone."),
    Event(CLASS, "Homebrewing 101", "class_workshop", ("indoor", "drinks", "learning", "21_plus", "group", "low_energy"),
          SWEETWATER_BREW, (SAT2,), "14:00", 120, (35, 35),
          "The brewers walk through a five-gallon batch from grain to fermenter, with tastings along the way. $35; 21+."),
    Event(CLASS, "Screen-Printing Workshop", "class_workshop", ("indoor", "art", "learning", "group", "medium_energy", "splurge"),
          MET_ATL, (WED2,), "18:00", 150, (60, 60),
          "Burn a screen and pull a two-color print on a tote bag in a working studio. $60 includes materials."),
    Event(CLASS, "Terrarium Workshop", "class_workshop", ("outdoor", "nature", "art", "learning", "daytime", "low_energy"),
          WOODLANDS, (SUN1,), "13:00", 90, (40, 40),
          "Build a small woodland terrarium from native mosses and ferns with a garden volunteer. $40 includes the glass."),
    Event(CLASS, "Glass Mosaic Workshop", "class_workshop", ("indoor", "art", "learning", "solo_friendly", "low_energy"),
          CALLANWOLDE, (THU2,), "18:30", 120, (45, 45),
          "Cut and set stained glass into a mosaic coaster set in the arts center's studio. $45 includes materials."),
    Event(CLASS, "Learn to Play: Tabletop RPG Night", "class_workshop", ("indoor", "learning", "group", "solo_friendly", "low_energy", "cheap"),
          OXFORD_COMICS, (MON1,), "18:00", 180, (10, 10),
          "A game master teaches a one-shot tabletop role-playing adventure for first-timers; dice and characters provided. $10."),
    Event(CLASS, "Plein-Air Painting Meetup", "class_workshop", ("outdoor", "free", "art", "nature", "solo_friendly", "daytime", "low_energy"),
          PIEDMONT, (D0,), "10:00", 120, FREE,
          "Local painters set up easels by the lake and share tips with newcomers; bring your own supplies. Free.",
          attendance="drop_in"),
    Event(CLASS, "Intro-to-Coding Night", "class_workshop", ("indoor", "free", "learning", "solo_friendly", "group", "low_energy"),
          BN_GEORGIA_TECH, (TUE1,), "17:00", 90, FREE,
          "Student volunteers walk beginners through a first Python program at the campus bookstore; bring a laptop. Free."),
    Event(CLASS, "Family Science Saturday", "class_workshop", ("indoor", "free", "learning", "family", "daytime", "low_energy"),
          FERNBANK_SCIENCE, (SAT1,), "10:00", 120, FREE,
          "Drop in for hands-on experiments with the science center's educators, from slime to simple circuits. Free.",
          attendance="drop_in"),
    Event(CLASS, "Composting 101", "class_workshop", ("outdoor", "free", "learning", "nature", "low_energy", "solo_friendly"),
          FOOD_FOREST, (WED2,), "18:00", 90, FREE,
          "Learn to build and turn a compost pile at the city's urban food forest, then take home a bucket of finished compost. Free."),
    Event(CLASS, "Bike Maintenance Basics", "class_workshop", ("outdoor", "free", "learning", "solo_friendly", "low_energy"),
          BELTLINE_SHED, (MON2,), "18:00", 90, FREE,
          "Volunteer mechanics show how to fix a flat, adjust brakes and lube a chain; bring your bike. Free."),
    Event(CLASS, "Zine-Making Workshop", "class_workshop", ("indoor", "free", "art", "learning", "solo_friendly", "low_energy"),
          CHARIS, (SAT2,), "14:00", 120, FREE,
          "Cut, collage and fold a mini zine with supplies provided by the bookstore. Free."),

    # ---------------------------------------------------------------- sports and fitness (5 paid, 13 free)
    Event(ACTIVE, "Fall 5K Fun Run", "sports_event", ("outdoor", "active", "high_energy", "group", "daytime"),
          PIEDMONT, (SAT1,), "08:00", 90, (35, 40),
          "A timed 5K loop through the park with water stops and a finish-line party. Registration is $35–$40 and includes a shirt."),
    Event(ACTIVE, "Pickleball Round-Robin Night", "rec_venue", ("indoor", "active", "group", "solo_friendly", "medium_energy", "drinks"),
          PAINTED_PICKLE, (THU1,), "19:00", 120, (20, 20),
          "Sign up solo or with a partner for fast rotating games organized by skill level. $20 includes court time and paddles."),
    Event(ACTIVE, "Beginner Tennis Clinic", "class_workshop", ("outdoor", "active", "learning", "solo_friendly", "medium_energy"),
          PIEDMONT, (TUE1,), "18:00", 90, (25, 25),
          "A pro teaches grips, footwork and rallying at the park's public tennis center; loaner rackets available. $25."),
    Event(ACTIVE, "Guided BeltLine Bike Tour", "tour", ("outdoor", "active", "touristy", "group", "daytime", "medium_energy", "art"),
          BELTLINE_SHED, (SUN1,), "09:30", 120, (45, 45),
          "A guide leads an easy two-hour ride along the Eastside Trail with stops for murals and history. $45 includes a bike and helmet."),
    Event(ACTIVE, "Rooftop Pilates", "class_workshop", ("outdoor", "active", "solo_friendly", "medium_energy"),
          SKYLINE_PARK, (TUE2,), "18:30", 60, (25, 25),
          "A mat Pilates class on the Ponce City Market roof as the city lights come on; mats provided. $25."),
    Event(ACTIVE, "Tuesday Evening Run Club", "community_event", ("outdoor", "free", "active", "group", "solo_friendly", "medium_energy"),
          BELTLINE_SHED, (TUE1, TUE2), "18:30", 75, FREE,
          "A free, no-drop 5K group run on the Eastside Trail with pace groups from walk-jog to fast. Meet at the Beltline Shed."),
    Event(ACTIVE, "Saturday Yoga in the Park", "class_workshop", ("outdoor", "free", "nature", "solo_friendly", "daytime", "low_energy"),
          PIEDMONT, (SAT1, SAT2), "10:00", 60, FREE,
          "An all-levels yoga flow on the park lawn; bring a mat or towel. Free, donations welcome."),
    Event(ACTIVE, "Wednesday Night Group Ride", "community_event", ("outdoor", "free", "active", "group", "medium_energy"),
          FREEDOM_PARK, (WED1, WED2), "18:30", 90, FREE,
          "A social 15-mile ride on city streets and PATH trails at a conversational pace; lights and helmets required. Free."),
    Event(ACTIVE, "Bootcamp in the Park", "class_workshop", ("outdoor", "free", "active", "high_energy", "group", "daytime"),
          CENTENNIAL, (D0,), "09:00", 60, FREE,
          "A trainer runs an hour of circuits, sprints and body-weight work on the lawn; all levels welcome. Free."),
    Event(ACTIVE, "Pickup Ultimate Frisbee", "community_event", ("outdoor", "free", "active", "high_energy", "group", "solo_friendly", "daytime"),
          CANDLER_PARK, (D0,), "14:00", 120, FREE,
          "Show up and get put on a team for friendly pickup games on the big field; bring a light and a dark shirt. Free.",
          attendance="drop_in"),
    Event(ACTIVE, "Sunset Yoga by the Lake", "class_workshop", ("outdoor", "free", "nature", "solo_friendly", "low_energy"),
          FOURTH_WARD, (D0,), "18:00", 60, FREE,
          "A gentle flow on the lawn above the park's lake as the sun sets behind the city. Free; bring a mat."),
    Event(ACTIVE, "Tai Chi in the Park", "class_workshop", ("outdoor", "free", "nature", "solo_friendly", "daytime", "low_energy"),
          GRANT_PARK, (SUN1,), "08:00", 60, FREE,
          "A slow, beginner-friendly tai chi class under the park's big oaks. Free."),
    Event(ACTIVE, "Community Kickball Game", "community_event", ("outdoor", "free", "active", "group", "solo_friendly", "daytime", "medium_energy"),
          WESTSIDE_PARK, (SUN1,), "15:00", 120, FREE,
          "Neighbors mix into teams for a relaxed kickball game on the park's lawn. Free; all skill levels.",
          attendance="drop_in"),
    Event(ACTIVE, "Lunchtime Zumba", "class_workshop", ("outdoor", "free", "active", "high_energy", "group", "daytime", "solo_friendly"),
          WOODRUFF, (WED2,), "12:00", 60, FREE,
          "A high-energy dance workout in the park for the downtown lunch crowd. Free."),
    Event(ACTIVE, "Morning Walking Club", "community_event", ("outdoor", "free", "active", "solo_friendly", "group", "daytime", "low_energy"),
          CHASTAIN, (TUE2,), "08:00", 60, FREE,
          "A brisk three-mile walk around the park's trail loop with a friendly group. Free."),

    # ---------------------------------------------------------------- comedy, theater and performances (15 paid, 3 free)
    Event(STAGE, "Family Puppet Show", "theater", ("indoor", "art", "family", "daytime", "low_energy"),
          PUPPETRY, (D0,), "12:00", 60, (20, 25),
          "A fairy-tale adaptation performed with rod puppets and live music in the center's main theater. Tickets $20–$25."),
    Event(STAGE, "Shakespeare Comedy Matinee", "theater", ("indoor", "art", "date", "food", "low_energy", "daytime"),
          SHAKESPEARE_TAVERN, (D0,), "14:00", 150, (25, 45),
          "An Elizabethan comedy staged in a pub-style playhouse, with optional British pub food before the curtain. Tickets $25–$45."),
    Event(STAGE, "Sunday Stand-Up Showcase", "comedy", ("indoor", "drinks", "group", "date", "medium_energy"),
          LAUGHING_SKULL, (D0,), "19:00", 90, (20, 25),
          "Five local and touring comics in a tight 90-minute show at a small Midtown club. Tickets $20–$25."),
    Event(STAGE, "Monday Night Showcase", "comedy", ("indoor", "drinks", "group", "medium_energy", "cheap"),
          LAUGHING_SKULL, (MON1,), "20:00", 90, (15, 15),
          "New and established comics try fresh material on a Monday crowd. $15."),
    Event(STAGE, "Long-Form Improv Night", "comedy", ("indoor", "group", "date", "medium_energy"),
          WHOLE_WORLD, (THU1,), "20:00", 90, (15, 20),
          "Two house teams build 40-minute improvised stories from a single audience suggestion. Tickets $15–$20."),
    Event(STAGE, "Headliner Stand-Up", "comedy", ("indoor", "drinks", "group", "date", "medium_energy"),
          PUNCHLINE, (FRI1,), "20:00", 105, (25, 35),
          "A national headliner and two openers at Atlanta's long-running comedy club. Tickets $25–$35."),
    Event(STAGE, "Broadway Hits in Concert", "theater", ("indoor", "music", "art", "date", "touristy", "low_energy", "splurge"),
          FOX, (SAT1,), "20:00", 150, (45, 125),
          "Broadway singers and a live band perform showstoppers from classic and new musicals at the Fox. Tickets $45–$125."),
    Event(STAGE, "Murder Mystery Dinner Show", "theater", ("indoor", "food", "drinks", "group", "date", "medium_energy", "splurge"),
          DINNER_DETECTIVE, (SAT1,), "18:00", 180, (70, 90),
          "Solve an interactive mystery while a four-course dinner is served; one guest will be named a suspect. "
          "Tickets $70–$90 include dinner."),
    Event(STAGE, "Family Magic Show", "theater", ("indoor", "family", "daytime", "low_energy"),
          MAGIC_THEATER, (SAT1,), "17:00", 75, (25, 30),
          "Sleight of hand, comedy and audience volunteers in an intimate downtown magic theater. Tickets $25–$30."),
    Event(STAGE, "Contemporary Play Matinee", "theater", ("indoor", "art", "date", "daytime", "low_energy"),
          HORIZON, (SUN1,), "15:00", 150, (35, 50),
          "A new American comedy-drama in an intimate Inman Park theater. Tickets $35–$50."),
    Event(STAGE, "Sketch Comedy Night", "comedy", ("indoor", "group", "drinks", "medium_energy", "cheap"),
          VINYL, (TUE2,), "20:00", 90, (15, 15),
          "A local sketch troupe performs new sketches, videos and songs in a Midtown club. $15."),
    Event(STAGE, "New Play Premiere", "theater", ("indoor", "art", "date", "low_energy", "splurge"),
          ALLIANCE, (THU2,), "19:30", 150, (35, 75),
          "A world-premiere drama on the Alliance Theatre's main stage at the Woodruff Arts Center. Tickets $35–$75."),
    Event(STAGE, "Midnight Cult Classic Screening", "cinema", ("indoor", "late_night", "group", "cheap", "medium_energy", "local_favorite"),
          PLAZA, (FRI2,), "23:30", 120, (12, 15),
          "A cult-classic film on the big screen of Atlanta's oldest operating cinema, with costumes and callbacks welcome. $12–$15."),
    Event(STAGE, "Close-Up Magic Late Show", "theater", ("indoor", "date", "drinks", "low_energy"),
          MAGIC_THEATER, (FRI2,), "20:00", 90, (35, 40),
          "A grown-up evening of close-up card magic and mentalism for a small audience. Tickets $35–$40."),
    Event(STAGE, "Fall Dance Program", "theater", ("indoor", "art", "music", "date", "low_energy"),
          RIALTO, (SAT2,), "19:30", 120, (25, 45),
          "A contemporary dance company performs three new works with live music downtown. Tickets $25–$45."),
    Event(STAGE, "Movie Night on the Lawn", "cinema", ("outdoor", "free", "family", "date", "low_energy"),
          FOURTH_WARD, (FRI1,), "19:30", 120, FREE,
          "A family-friendly classic on an inflatable screen by the lake; bring a blanket. Free."),
    Event(STAGE, "Shakespeare Scenes in the Park", "theater", ("outdoor", "free", "art", "family", "daytime", "low_energy"),
          PIEDMONT, (SUN1,), "16:00", 90, FREE,
          "Actors perform favorite scenes from the comedies on the park lawn; bring a chair. Free."),
    Event(STAGE, "Free Improv Jam", "comedy", ("indoor", "free", "group", "solo_friendly", "medium_energy"),
          WHOLE_WORLD, (THU2,), "21:30", 60, FREE,
          "Anyone can put their name in the hat to play scenes with the house team in a late, loose improv set. Free."),

    # ---------------------------------------------------------------- markets, festivals and community (1 paid, 12 free)
    Event(COMMUNITY, "Grant Park Sunday Farmers Market", "market", ("outdoor", "free", "food", "family", "daytime", "low_energy", "local_favorite"),
          GRANT_PARK, (D0, SUN1), "09:30", 240, FREE,
          "Local farmers, bakers and prepared-food vendors set up in the park on Sunday mornings. Free entry; vendors sell by the item.",
          attendance="drop_in"),
    Event(COMMUNITY, "Saturday Farmers Market at the Carter Center", "market", ("outdoor", "free", "food", "family", "daytime", "low_energy", "local_favorite"),
          CARTER_CENTER, (SAT1, SAT2), "08:00", 240, FREE,
          "Growers, bakers and food trucks fill the grounds of the Carter Center on Saturday mornings. Free entry; vendors sell by the item.",
          attendance="drop_in"),
    Event(COMMUNITY, "Sunday Makers Market", "market", ("outdoor", "free", "art", "daytime", "low_energy", "local_favorite"),
          BELTLINE_SHED, (D0,), "12:00", 360, FREE,
          "Local makers sell ceramics, prints, jewelry and plants under the Beltline Shed. Free to browse.",
          attendance="drop_in"),
    Event(COMMUNITY, "Flea Market at the Drive-In", "market", ("outdoor", "free", "daytime", "low_energy", "local_favorite"),
          STARLIGHT, (D0,), "08:00", 360, FREE,
          "Vendors fill the drive-in's lot with vintage clothes, records, tools and oddities on Sunday mornings. Free entry; bring cash.",
          attendance="drop_in"),
    Event(COMMUNITY, "Fall Arts & Crafts Festival", "festival", ("outdoor", "free", "art", "family", "group", "daytime", "touristy", "low_energy"),
          PIEDMONT, (SAT1,), "11:00", 420, FREE,
          "More than a hundred artist booths, food trucks and a kids' art tent across the park's meadow. Free entry.",
          attendance="drop_in"),
    Event(COMMUNITY, "Fall Neighborhood Festival", "festival", ("outdoor", "free", "music", "food", "family", "group", "daytime", "local_favorite", "medium_energy"),
          CANDLER_PARK, (SUN1,), "12:00", 360, FREE,
          "A neighborhood festival with local bands, a chili cook-off, artist booths and a kids' zone. Free entry; food is extra.",
          attendance="drop_in"),
    Event(COMMUNITY, "Night Market at Underground", "market", ("indoor", "free", "food", "art", "group", "medium_energy"),
          UNDERGROUND, (FRI2,), "17:00", 300, FREE,
          "Street-food stalls, local designers and a DJ in the passages of Underground Atlanta. Free entry.",
          attendance="drop_in"),
    Event(COMMUNITY, "Proctor Creek Volunteer Cleanup", "community_event", ("outdoor", "free", "nature", "active", "group", "daytime", "medium_energy"),
          PROCTOR_CREEK, (SAT2,), "09:00", 180, FREE,
          "Volunteers pick up litter along the creek and greenway; gloves, bags and water are provided. Free; sign up at the trailhead."),
    Event(COMMUNITY, "Community Potluck & Seed Swap", "community_event", ("outdoor", "free", "food", "nature", "group", "low_energy"),
          FOOD_FOREST, (WED1,), "18:00", 120, FREE,
          "Bring a dish and any spare seeds to share at the urban food forest's evening potluck. Free.",
          attendance="drop_in"),
    Event(COMMUNITY, "Board Game Night", "community_event", ("indoor", "free", "group", "solo_friendly", "low_energy"),
          OXFORD_COMICS, (THU1,), "18:00", 180, FREE,
          "The store opens its game tables and demo library for a free night of board games; staff teach new players.",
          attendance="drop_in"),
    Event(COMMUNITY, "Oktoberfest Party", "festival", ("outdoor", "food", "drinks", "music", "group", "21_plus", "medium_energy"),
          HALFWAY_CROOKS, (SAT1,), "12:00", 600, (15, 20),
          "German-style lagers, pretzels and sausages, an oompah band and stein-holding contests at a Summerhill brewery. "
          "$15–$20 entry includes a souvenir glass; 21+.",
          attendance="drop_in"),
]
