"""The tables the profile and search texts are built from.

Phrases reuse the vocabulary of the activity texts (`dataingestion/.../event_embedding_text.md` and
the backfilled `embeddingText`s: "outdoor setting", "live performance", "meeting new people", ...)
so a user's bullets land near the events they describe. Editing any table changes the rendered
texts: bump `profiles.TEMPLATE_VERSION` so stored profile hashes go stale and users are re-embedded.
"""

from __future__ import annotations

import re

# The app's rating keys, in the order ties are broken (`frontend/API_CONTRACT.md`).
RATING_KEYS = [
    "outdoors",
    "food",
    "museums",
    "live_music",
    "nightlife",
    "sports",
    "shopping",
    "big_crowds",
    "early_mornings",
    "long_walks",
]

# Spellings callers have used or might use, after camelCase/space/dash normalization.
RATING_ALIASES = {
    "music": "live_music",
    "crowds": "big_crowds",
    "big_crowd": "big_crowds",
    "early_morning": "early_mornings",
    "mornings": "early_mornings",
    "long_walk": "long_walks",
    "walks": "long_walks",
    "walking": "long_walks",
    "museum": "museums",
    "art": "museums",
    "arts": "museums",
    "outdoor": "outdoors",
    "foods": "food",
    "sport": "sports",
    "shop": "shopping",
    "night_life": "nightlife",
}

# Rating 5 takes the first two bullets of each list, 4 the first only; 3 contributes nothing.
RATING_LIKES: dict[str, dict[str, list[str]]] = {
    "outdoors": {"Interests": ["outdoor recreation"], "Activities": ["park visit", "guided hike"], "Environment": ["outdoor setting"]},
    "food": {"Interests": ["local food"], "Activities": ["food tasting", "eating out"]},
    "museums": {"Interests": ["arts and culture"], "Activities": ["museum visit", "gallery viewing"], "Environment": ["cultural venue"]},
    "live_music": {"Interests": ["live music"], "Activities": ["live music performance"]},
    "nightlife": {"Interests": ["nightlife"], "Activities": ["bar hopping"], "Environment": ["bar setting"], "Timing": ["late night"]},
    "sports": {"Interests": ["sports"], "Activities": ["sports watch party", "pickup games"], "Pace": ["physically active"]},
    "shopping": {"Interests": ["shopping"], "Activities": ["market browsing", "vintage shopping"]},
    "big_crowds": {"Social": ["big lively crowds", "large-group setting"]},
    "early_mornings": {"Timing": ["early morning"], "Pace": ["early start"]},
    "long_walks": {"Activities": ["long walks"], "Pace": ["extended walking"]},
}

# Rating 1 takes the first two bullets of each list, 2 the first only. Written as the things
# themselves, never as negations: the vector is used as a penalty, so it must land near what the
# user avoids.
RATING_DISLIKES: dict[str, dict[str, list[str]]] = {
    "outdoors": {"Interests": ["outdoor recreation"], "Activities": ["hiking"], "Environment": ["outdoor setting", "rugged terrain"]},
    "food": {"Interests": ["local food"], "Activities": ["food tasting", "eating out"]},
    "museums": {"Interests": ["arts and culture"], "Activities": ["museum visit", "gallery viewing"], "Environment": ["cultural venue"]},
    "live_music": {"Interests": ["live music"], "Activities": ["live music performance"], "Environment": ["loud concert venue"]},
    "nightlife": {"Interests": ["nightlife"], "Environment": ["loud nightclub setting", "crowded bars"], "Timing": ["late night"]},
    "sports": {"Interests": ["sports"], "Activities": ["sports watch party", "pickup games"]},
    "shopping": {"Interests": ["shopping"], "Activities": ["market browsing", "mall shopping"]},
    "big_crowds": {"Social": ["large crowds"], "Environment": ["crowded venue"]},
    "early_mornings": {"Timing": ["early morning", "sunrise start"]},
    "long_walks": {"Activities": ["long walks"], "Pace": ["extended walking", "strenuous"]},
}

COMPANY: dict[str, dict[str, list[str]]] = {
    "solo": {"Social": ["solo attendance", "meeting new people"]},
    "small_group": {"Social": ["small-group setting", "close friends"]},
    "big_group": {"Social": ["large-group outing", "big lively crowds"]},
}

# The app sends relaxed|balanced|packed; "chill" is normalized to "relaxed" before lookup.
PACE: dict[str, dict[str, list[str]]] = {
    "relaxed": {"Pace": ["relaxed rhythm", "low-key"]},
    "balanced": {"Pace": ["moderate pace"]},
    "packed": {"Pace": ["fast-paced", "back-to-back activities"]},
}

SPEND: dict[str, list[str]] = {
    "free_only": ["free admission"],
    "under_15": ["under $15 admission"],
    "15_to_40": ["moderate price point", "$15-40 admission"],
    "over_40": ["premium tickets"],
}
PREFER_FREE = ["free admission"]
FLEXIBILITY_NEG: dict[str, dict[str, list[str]]] = {"stick_to_budget": {"Cost": ["expensive tickets"]}}

# `Rating.tagOptions` in the app: applied whatever the star count, since they are explicit signals.
RATING_TAGS_POS: dict[str, dict[str, list[str]]] = {
    "great people": {"Social": ["meeting new people"]},
    "would go again": {"Experience": ["worth repeating"]},
    "good value": {"Cost": ["good value"]},
}
RATING_TAGS_NEG: dict[str, dict[str, list[str]]] = {
    "too crowded": {"Social": ["large crowds"]},
    "too pricey": {"Cost": ["expensive tickets"]},
    "hard to get to": {"Environment": ["remote location"]},
}

# Activity categories in the catalogs (`activities`, `demo_activities`) -> an Interests phrase.
CATEGORY_PHRASES: dict[str, str] = {
    "museum": "arts and culture",
    "art_gallery": "arts and culture",
    "gallery": "arts and culture",
    "park": "outdoor recreation",
    "garden": "outdoor recreation",
    "trail": "hiking",
    "hike": "hiking",
    "restaurant": "local food",
    "cafe": "coffee shops",
    "bar": "nightlife",
    "nightclub": "nightlife",
    "music_venue": "live music",
    "live_music": "live music",
    "theater": "performing arts",
    "comedy": "comedy shows",
    "cinema": "film",
    "market": "markets and fairs",
    "shopping": "shopping",
    "landmark": "sightseeing",
    "viewpoint": "sightseeing",
    "tour": "sightseeing",
    "festival": "festivals",
    "sports_event": "sports",
    "rec_venue": "active recreation",
    "class_workshop": "hands-on workshops",
    "community_event": "community events",
    "zoo_aquarium": "animals and nature",
}

# Tags on the activities themselves (`21_plus`, `high_energy`, ...), after `_` -> space.
ACTIVITY_TAG_PHRASES: dict[str, dict[str, list[str]]] = {
    "outdoor": {"Environment": ["outdoor setting"]},
    "outdoors": {"Environment": ["outdoor setting"]},
    "nature": {"Environment": ["natural scenery"]},
    "indoor": {"Environment": ["indoor venue"]},
    "music": {"Interests": ["live music"]},
    "art": {"Interests": ["arts and culture"]},
    "food": {"Interests": ["local food"]},
    "drinks": {"Interests": ["drinks"]},
    "hiking": {"Activities": ["hiking"]},
    "active": {"Pace": ["physically active"]},
    "high energy": {"Pace": ["high energy"]},
    "low energy": {"Pace": ["relaxed rhythm"]},
    "late night": {"Timing": ["late night"]},
    "daytime": {"Timing": ["daytime"]},
    "free": {"Cost": ["free admission"]},
    "cheap": {"Cost": ["under $15 admission"]},
    "splurge": {"Cost": ["premium tickets"]},
    "group": {"Social": ["large-group setting"]},
    "solo friendly": {"Social": ["solo attendance"]},
    "family": {"Social": ["family friendly"]},
    "date": {"Social": ["date night"]},
    "learning": {"Experience": ["learning something new"]},
    "touristy": {"Experience": ["tourist attraction"]},
    "local favorite": {"Experience": ["local favorite"]},
}

# Create > quick picks (`CreateModel.quickPicks`) and the forum's interest tags, lowercased.
TAGS: dict[str, dict[str, list[str]]] = {
    "outdoors": {"Interests": ["outdoor recreation"], "Environment": ["outdoor setting"]},
    "food": {"Interests": ["local food"], "Activities": ["eating out"]},
    "art": {"Interests": ["arts and culture"], "Activities": ["gallery viewing"]},
    "music": {"Interests": ["live music"]},
    "chill": {"Pace": ["relaxed rhythm"], "Environment": ["laid-back atmosphere"]},
    "active": {"Pace": ["physically active"], "Activities": ["active recreation"]},
    "meet people": {"Social": ["meeting new people"], "Activities": ["social mixer"]},
    "nerdy": {"Interests": ["science and technology", "games and puzzles"]},
    "nightlife": {"Interests": ["nightlife"], "Timing": ["late night"]},
    "games": {"Interests": ["games and puzzles"], "Activities": ["board games"]},
    "shopping": {"Interests": ["shopping"], "Activities": ["market browsing"]},
}

WHO: dict[str, dict[str, list[str]]] = {
    "just_me": {"Social": ["solo outing"]},
    "friends": {"Social": ["small group of friends"]},
    "open": {"Social": ["open group", "meeting new people"]},
}

# PlanRequest.budget: 0 = Free, 1 = $, 2 = $$, 3 = $$$ (no constraint worth a bullet).
BUDGET: dict[int, list[str]] = {0: ["free admission"], 1: ["under $15 admission"], 2: ["$15-40 admission"], 3: []}

# Free-text cleaning: words stripped from the start / end of a phrase, repeatedly.
LEADING_FILLERS: tuple[str, ...] = (
    "i'd love", "i'd like", "i would love", "i would like", "i'd", "i would", "i want to", "i want",
    "i like to", "i like", "i love to", "i love", "i enjoy", "i prefer", "i'm into", "i am into", "i'm",
    "i am", "we'd", "we would", "we want", "we like", "let's", "lets", "something", "somewhere",
    "anything", "anywhere", "that", "which", "what", "whatever", "maybe", "perhaps", "probably",
    "definitely", "ideally", "preferably", "just", "really", "also", "to", "a", "an", "the", "some",
    "any", "like", "love", "want", "prefer", "enjoy", "into", "do", "go to", "go", "get", "have",
    "being", "doing", "going", "getting", "having", "kind of", "sort of", "a bit of", "a lot of",
    "lots of", "more", "less", "with", "for", "at", "in", "on", "of",
)
TRAILING_FILLERS: tuple[str, ...] = (
    "after", "afterwards", "later", "too", "as well", "though", "please", "first", "next", "then",
    "again", "etc", "and so on", "for me", "for us", "with me", "with us", "or so", "maybe",
    "probably", "ideally", "if possible", "somewhere", "something", "of course",
)

# Where a free-text phrase goes, first match wins; no match -> Interests.
ROUTES: list[tuple[str, re.Pattern[str]]] = [
    (
        "Timing",
        re.compile(
            r"\b(mornings?|afternoons?|evenings?|nights?|midnight|noon|sunrise|sunset|weekends?|weekdays?|"
            r"mondays?|tuesdays?|wednesdays?|thursdays?|fridays?|saturdays?|sundays?|late|early|after work|"
            r"lunch|brunch|dinner|daytime|nighttime|hours?|o'clock|\d{1,2}(:\d{2})?\s?(am|pm))\b"
        ),
    ),
    (
        "Social",
        re.compile(
            r"\b(friends?|solo|alone|groups?|crowds?|crowded|people|strangers|partner|dates?|family|kids|"
            r"meet|meeting|social|networking|team|company|whoever|whoever's|everyone|anyone|someone|together)\b"
        ),
    ),
    (
        "Environment",
        re.compile(
            r"\b(outside|outdoors?|indoors?|inside|parks?|water|beach|trails?|rooftop|gardens?|nature|city|"
            r"downtown|cozy|quiet|loud|noisy|venues?|bars?|clubs?|nightclubs?|markets?|atmosphere|setting|"
            r"scenery|views?|fresh air|neighborhood)\b"
        ),
    ),
    (
        "Pace",
        re.compile(
            r"\b(chill|relaxed|relaxing|slow|lazy|calm|laid[- ]back|easygoing|low[- ]key|packed|busy|fast|"
            r"active|intense|energetic|high[- ]energy|hectic|rushed|leisurely|mellow|unhurried)\b"
        ),
    ),
    (
        "Cost",
        re.compile(
            r"\b(cheap|free|budget|expensive|pricey|affordable|splurge|dollars?|costs?|costly|prices?|priced|"
            r"spend|spending|tickets?)\b|\$\d"
        ),
    ),
    (
        "Activities",
        re.compile(
            r"\b(walks?|walking|hikes?|hiking|swims?|swimming|runs?|running|bikes?|biking|cycling|climb|climbing|"
            r"dance|dancing|eat|eating|drinks?|drinking|shop|shopping|browse|browsing|tours?|class|classes|"
            r"workshops?|games?|gaming|watch|watching|listen|listening|play|playing|explore|exploring|wander|"
            r"wandering|picnic|kayak|kayaking|yoga|cook|cooking|paint|painting|read|reading|photograph|"
            r"photography|karaoke|trivia|bowling|skate|skating|volunteer|volunteering|tasting)\b"
        ),
    ),
]
