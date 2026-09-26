#!/usr/bin/env python3
"""Generate synthetic events in the embedding-text format of ml/description_generation.md.

Each chunk takes two vLLM passes with the same model:
  1. write a raw event listing (JSON) from randomly sampled attributes, then add
     scraper-style noise in code (IDs, URLs, placeholder values, price conflicts);
  2. convert the rendered listing to embedding text with the conversion prompt copied
     verbatim from ml/description_generation.md (section 2).
Conversions that break the spec's schema rules are dropped. Sampling is deliberately
nondeterministic: fresh random seeds on every run. Run one process per GPU; each appends
to its own JSONL shard after every chunk, so hitting the walltime loses at most a chunk.
"""
from __future__ import annotations

import argparse
import dataclasses
import json
import math
import os
import random
import re
import secrets
import time
import uuid
from collections import Counter
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SPEC = REPO / "ml" / "description_generation.md"
SECTIONS = ["Interests", "Activities", "Social", "Environment", "Pace", "Cost", "Timing", "Experience"]
# Stage 1 is where the randomness lives (diverse listings). Stage 2 should be faithful to its
# input: a low temperature cut invented facts in testing, and presence penalty is left off
# because it would fight the repeated "- " bullet structure.
S1_SAMPLING = {"temperature": 1.0, "top_p": 0.95, "top_k": 40, "presence_penalty": 0.3, "max_tokens": 700}
S2_SAMPLING = {"temperature": 0.3, "top_p": 0.8, "top_k": 20, "max_tokens": 256}


def conversion_prompt() -> str:
    """Section 2 of the spec, so the dataset always follows the document."""
    text = SPEC.read_text()
    tail = text[text.index("## 2. Event description generation prompt"):]
    return re.search(r"```text\n(.*?)\n```", tail, re.S).group(1).strip()


# ----------------------------------------------------------------- random briefs

CATEGORIES = {
    "tech meetup": ["machine learning", "web development", "cybersecurity", "cloud infrastructure",
                    "data engineering", "mobile app development", "open source", "game development",
                    "robotics", "quantum computing", "developer tools", "AI agents"],
    "hackathon": ["civic tech", "climate tech", "health tech", "fintech", "generative AI",
                  "education technology", "accessibility tech", "space data"],
    "hands-on workshop": ["pottery", "watercolor painting", "woodworking", "bike repair", "home composting",
                          "screen printing", "calligraphy", "sewing and mending", "3D printing",
                          "candle making", "sourdough baking", "knife skills"],
    "professional development": ["resume writing", "public speaking", "salary negotiation", "personal finance",
                                 "grant writing", "UX research methods", "product management", "leadership"],
    "live music": ["indie rock", "jazz", "classical chamber music", "hip-hop", "electronic DJ set",
                   "folk and acoustic", "metal", "latin music", "k-pop", "bluegrass", "blues", "orchestra"],
    "comedy": ["stand-up comedy", "improv comedy", "sketch comedy", "comedy open mic"],
    "performing arts": ["musical theater", "outdoor Shakespeare", "experimental theater", "ballet",
                        "contemporary dance", "spoken word", "magic show", "opera", "puppetry"],
    "film": ["outdoor movie night", "documentary screening", "short film festival", "anime screening",
             "classic film series", "filmmaker Q&A"],
    "food and drink": ["food truck festival", "wine tasting", "craft beer festival", "cooking class",
                       "farmers market", "coffee cupping", "dumpling making class", "vegan potluck",
                       "barbecue cook-off", "cocktail workshop", "cheese tasting", "street food tour"],
    "fitness": ["vinyasa yoga", "HIIT bootcamp", "running club", "group cycling ride", "bouldering",
                "pilates", "open water swim", "martial arts class", "dance fitness", "strength training clinic"],
    "outdoors": ["guided hike", "birdwatching walk", "kayaking trip", "stargazing night", "trail cleanup",
                 "overnight camping trip", "foraging walk", "fly fishing lesson", "sunrise paddleboarding"],
    "sports": ["pickup basketball", "recreational soccer league", "sports watch party", "tennis clinic",
               "ultimate frisbee", "bowling league night", "charity 5K", "volleyball tournament"],
    "games": ["board game night", "pub trivia", "chess club", "tabletop RPG one-shot", "escape room challenge",
              "video game tournament", "speedcubing meetup", "puzzle hunt"],
    "arts and culture": ["gallery opening", "museum late night", "art walk", "sculpture exhibition",
                         "street art tour", "cultural heritage festival", "photography exhibition",
                         "architecture tour"],
    "books and ideas": ["book club", "author talk", "poetry reading", "science café", "history lecture",
                        "philosophy discussion", "writing workshop", "zine fair"],
    "language exchange": ["Spanish conversation hour", "Japanese language exchange", "French conversation café",
                          "ASL practice meetup", "international student mixer"],
    "networking": ["startup pitch night", "industry networking mixer", "investor panel", "founder office hours",
                   "design critique night", "career fair", "alumni mixer", "freelancer coworking day"],
    "community and volunteering": ["park cleanup", "food bank volunteer shift", "community garden workday",
                                   "blood drive", "tutoring volunteer orientation", "animal shelter volunteering",
                                   "neighborhood association meeting", "repair café"],
    "wellness": ["guided meditation", "sound bath", "stress management workshop", "breathwork session",
                 "nutrition talk", "forest bathing walk"],
    "family and kids": ["children's storytime", "kids science fair", "kids coding camp", "pumpkin patch day",
                        "family movie night", "puppet show", "family bike ride", "LEGO building club"],
    "markets and fairs": ["craft fair", "flea market", "vintage clothing market", "holiday market", "plant swap",
                          "record fair", "night market", "antique show"],
    "nightlife": ["salsa social", "karaoke night", "silent disco", "rooftop party", "swing dance social",
                  "jazz lounge night", "80s dance party", "themed costume party"],
    "science talks": ["astronomy talk", "AI ethics panel", "climate science lecture", "biotech seminar",
                      "robotics demo day", "neuroscience public lecture"],
    "hobbies": ["knitting circle", "model railroad show", "photography walk", "origami workshop",
                "amateur radio meetup", "aquarium hobbyist swap", "home brewing club", "birding club meeting"],
    "pets and animals": ["dog park meetup", "cat café social", "pet adoption event", "horseback riding lesson",
                         "reptile expo"],
    "fashion and design": ["runway show", "clothing swap", "design sprint", "interior design talk",
                           "sneaker convention", "jewelry making class"],
    "campus life": ["study group", "guest lecture", "student club fair", "research poster session",
                    "campus concert", "late-night study break"],
    "civic life": ["town hall meeting", "city council public comment", "participatory budgeting workshop",
                   "library board meeting", "neighborhood safety walk"],
    "conventions": ["developer conference", "academic symposium", "gaming convention", "comic convention",
                    "maker faire", "anime convention", "home and garden show"],
}
SETTINGS = {"indoor": 0.58, "outdoor": 0.30, "online": 0.08, "hybrid": 0.04}
VENUES = {
    "indoor": ["community center", "public library", "brewery taproom", "coffee shop", "coworking space",
               "university lecture hall", "art gallery", "museum", "concert hall", "small bar",
               "converted warehouse", "hotel ballroom", "yoga studio", "restaurant back room", "makerspace",
               "bookstore", "convention center", "recreation center gym", "black box theater"],
    "outdoor": ["city park", "beach", "rooftop terrace", "botanical garden", "trailhead", "public plaza",
                "stadium", "lakefront", "farm", "campground", "riverside amphitheater", "community garden",
                "parking lot pop-up"],
    "online": ["Zoom webinar", "Discord server", "YouTube livestream", "Twitch stream"],
    "hybrid": ["in-person venue with a livestream"],
}
RECURRENCE = ["one-time", "one-time", "one-time", "weekly", "every other week", "monthly",
              "first Friday of each month", "multi-day (2-3 days)", "daily for a week", "seasonal series"]
DAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday", "weekday", "weekend"]
TIMES = ["early morning", "morning", "lunchtime", "afternoon", "early evening", "evening", "late night"]
DURATIONS = ["about 1 hour", "about 2 hours", "3-4 hours", "all day", "a whole weekend"]
COSTS = ["free", "free with RSVP", "pay what you can", "suggested donation of $5-10", "$5-15", "$20-40",
         "$45-90", "$100-250 premium ticket", "members free, non-members $10-20",
         "early-bird discount, then full price", "sliding scale", "free for students, paid general admission"]
AUDIENCES = ["beginner-friendly", "all skill levels", "intermediate", "advanced / experienced participants",
             "family-friendly, kids welcome", "21+ only", "18+", "university students", "working professionals",
             "seniors", "wheelchair accessible", "no experience needed"]
GROUPS = ["solo-friendly", "small group (under 15 people)", "medium group (20-60 people)",
          "large crowd (hundreds)", "team-based", "networking-focused", "audience / spectator format",
          "partner activity", "one-on-one mentoring"]
PACES = ["relaxed drop-in", "structured schedule", "fast-paced and competitive", "intensive / immersive",
         "self-paced", "casual and social", "physically demanding"]
STYLES = ["ticketing-site listing with structured fields", "Meetup group event post", "community newsletter blurb",
          "university events calendar entry", "venue website listing", "casual social media post",
          "Facebook event page", "public library program guide entry", "terse calendar entry",
          "press release excerpt", "email invitation", "transcribed flyer text"]
TONES = ["neutral and informative", "enthusiastic and promotional, with exclamation marks",
         "casual, with a few emoji", "formal and institutional", "terse", "chatty, with a few typos and abbreviations"]
CITIES = ["Atlanta, GA", "Austin, TX", "Seattle, WA", "Chicago, IL", "Boston, MA", "Denver, CO", "Portland, OR",
          "Miami, FL", "Minneapolis, MN", "New Orleans, LA", "Brooklyn, NY", "Oakland, CA", "San Diego, CA",
          "Nashville, TN", "Philadelphia, PA", "Detroit, MI", "Pittsburgh, PA", "Raleigh, NC",
          "Salt Lake City, UT", "Madison, WI", "Toronto", "Vancouver", "London", "Berlin", "Amsterdam", "Dublin",
          "Sydney", "Melbourne", "Singapore", "Bangalore", "Mexico City", "Barcelona", "Lisbon", "Stockholm",
          "Tokyo", "Seoul", "Cape Town", "Buenos Aires", "Auckland", "Edinburgh"]
COMPLETENESS = {"complete": 0.35, "partial": 0.35, "sparse": 0.20, "minimal": 0.10}
DETAIL = {
    "complete": "Rich listing: a description of 70-150 words that fills in most fields; plausible extra "
                "specifics are fine.",
    "partial": "Typical listing: a description of 25-70 words that uses only the brief's details; leave "
               "several fields null.",
    "sparse": "Sparse listing: one or two short sentences that mention only a couple of the brief's details; "
              "most fields null.",
    "minimal": "Bare-bones listing: a title and a single short line (under 12 words); every other field null.",
}
KEEP = {"complete": 0.9, "partial": 0.65, "sparse": 0.4, "minimal": 0.2}  # chance each brief attribute is given


def weighted(rng: random.Random, table: dict) -> str:
    return rng.choices(list(table), weights=list(table.values()))[0]


def when_phrase(rng: random.Random) -> str:
    parts = [rng.choice(RECURRENCE), rng.choice(DAYS), rng.choice(TIMES)]
    if rng.random() < 0.6:
        parts.append(rng.choice(DURATIONS))
    return ", ".join(parts)


def sample_brief(rng: random.Random) -> dict:
    category = rng.choice(list(CATEGORIES))
    completeness = weighted(rng, COMPLETENESS)
    setting = weighted(rng, SETTINGS)
    brief = {"category": category, "topic": rng.choice(CATEGORIES[category]), "completeness": completeness,
             "listing_style": rng.choice(STYLES), "tone": rng.choice(TONES)}
    optional = {"setting": f"{setting}, {rng.choice(VENUES[setting])}", "when": when_phrase(rng),
                "cost": rng.choice(COSTS), "audience": rng.choice(AUDIENCES), "group": rng.choice(GROUPS),
                "pace": rng.choice(PACES), "city": rng.choice(CITIES)}
    for key, value in optional.items():
        if rng.random() < KEEP[completeness]:
            brief[key] = value
    return brief


# ------------------------------------------------------------- stage 1: raw listing

FIELDS = ["title", "description", "when", "venue", "address", "price", "category", "tags", "organizer",
          "age_policy", "capacity"]
S1_SYSTEM = ("You write realistic raw event listings: the messy source data an event-recommendation app collects "
             "from ticketing sites, calendars, newsletters and social media. Follow the brief's listing style, "
             "tone and detail level. Output one JSON object and nothing else.")
_NULLABLE = {"anyOf": [{"type": "string"}, {"type": "null"}]}
S1_SCHEMA = {
    "type": "object",
    "properties": {"title": {"type": "string"}, "description": {"type": "string"}, "when": _NULLABLE,
                   "venue": _NULLABLE, "address": _NULLABLE, "price": _NULLABLE, "category": _NULLABLE,
                   "tags": {"anyOf": [{"type": "array", "items": {"type": "string"}}, {"type": "null"}]},
                   "organizer": _NULLABLE, "age_policy": _NULLABLE,
                   "capacity": {"anyOf": [{"type": "integer"}, {"type": "null"}]}},
    "required": FIELDS,
    "additionalProperties": False,
}
BRIEF_LABELS = {"setting": "Setting", "when": "When", "cost": "Cost", "audience": "Audience",
                "group": "Group format", "pace": "Pace", "city": "City"}


def stage1_user(brief: dict) -> str:
    lines = [f"Event: {brief['topic']} ({brief['category']})", f"Listing style: {brief['listing_style']}",
             f"Tone: {brief['tone']}", f"Detail level: {DETAIL[brief['completeness']]}"]
    lines += [f"{label}: {brief[key]}" for key, label in BRIEF_LABELS.items() if key in brief]
    lines.append("JSON keys: " + ", ".join(FIELDS) + ". Use null for anything this listing would not state.")
    return "\n".join(lines)


def parse_stage1(text: str) -> dict | None:
    text = re.sub(r"<think>.*?</think>", "", text, flags=re.S).strip()
    try:
        rec = json.loads(text)
    except json.JSONDecodeError:
        start, end = text.find("{"), text.rfind("}")
        try:
            rec = json.loads(text[start:end + 1]) if start >= 0 < end else None
        except json.JSONDecodeError:
            return None
    if not isinstance(rec, dict):
        return None
    title, desc = rec.get("title"), rec.get("description")
    if not (isinstance(title, str) and isinstance(desc, str) and 2 < len(title) <= 150 and 0 < len(desc) <= 2500):
        return None
    return {k: rec.get(k) for k in FIELDS}


# ------------------------------------------------------ noise and rendering (code)

DOMAINS = ["eventbrite.com", "meetup.com", "allevents.in", "facebook.com/events", "citycalendar.org", "tixr.com",
           "lu.ma", "dice.fm", "events.example.edu"]
SOURCES = ["eventbrite_scraper_v3", "meetup_api_v2", "city_calendar_ical", "fb_events_crawl", "venue_site_scrape",
           "newsletter_parser"]
PLACEHOLDERS = ["N/A", "TBD", "TBA", "unknown", "Unknown", "Not provided", "none", "null", "-", ""]
PRICE_MENTION = re.compile(r"\bfree\b|\$\s?\d|€\s?\d|£\s?\d|\bdonation\b|\bpay what\b", re.I)
KEY_NAMES = {"title": ["Title", "Event", "Event Name", "Name"], "description": ["Description", "Details", "About"],
             "when": ["When", "Date/Time", "Date", "Schedule"], "venue": ["Venue", "Location", "Where"],
             "address": ["Address", "Street Address"], "price": ["Price", "Cost", "Tickets", "Admission"],
             "category": ["Category", "Type"], "tags": ["Tags", "Keywords"],
             "organizer": ["Organizer", "Hosted by", "Host"], "age_policy": ["Ages", "Age Restriction"],
             "capacity": ["Capacity", "Spots"], "event_id": ["Event ID", "id"], "url": ["URL", "Link"],
             "source": ["Source"], "scraped_at": ["Scraped At", "Last Updated"], "organizer_id": ["Organizer ID"]}
RENDER_ORDER = ["title", "when", "venue", "address", "price", "description", "category", "tags", "organizer",
                "age_policy", "capacity", "event_id", "url", "source", "scraped_at", "organizer_id"]


def add_noise(rec: dict, rng: random.Random) -> list[str]:
    noise = []
    if rng.random() < 0.65:
        slug = re.sub(r"[^a-z0-9]+", "-", rec["title"].lower()).strip("-")[:40]
        rec["event_id"] = rng.choice([f"evt_{secrets.token_hex(5)}", str(rng.randint(10**7, 10**10)),
                                      str(uuid.uuid4())])
        rec["url"] = f"https://{rng.choice(DOMAINS)}/e/{slug}-{rng.randint(1000, 999999)}"
        rec["source"] = rng.choice(SOURCES)
        rec["scraped_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ",
                                          time.gmtime(time.time() - rng.randint(0, 90 * 86400)))
        if rng.random() < 0.5:
            rec["organizer_id"] = f"org_{secrets.token_hex(4)}"
        noise.append("scraper_metadata")
    empty = [k for k in FIELDS[2:] if rec.get(k) in (None, "", [])]
    if empty and rng.random() < 0.3:
        for key in rng.sample(empty, min(len(empty), rng.randint(1, 3))):
            rec[key] = rng.choice(PLACEHOLDERS)
        noise.append("placeholders")
    if rng.random() < 0.1 and PRICE_MENTION.search(rec["description"]):
        says_free = re.search(r"\bfree\b", rec["description"], re.I) is not None
        rec["price"] = (rng.choice(["$25", "$40.00", "$15 advance / $20 door", "$60"]) if says_free
                        else rng.choice(["Free", "FREE", "Free admission"]))
        noise.append("price_conflict")
    return noise


def render(rec: dict, rng: random.Random) -> tuple[str, str]:
    if rng.random() < 0.35:
        return "json", json.dumps(rec, ensure_ascii=False, indent=rng.choice([None, 2]))
    order = RENDER_ORDER[:]
    if rng.random() < 0.5:
        rest = order[1:]
        rng.shuffle(rest)
        order = order[:1] + rest
    lines = []
    for key in order:
        if key not in rec:
            continue
        value = rec[key]
        if value is None or value == []:
            if rng.random() < 0.75:
                continue
            value = rng.choice(["null", "", "None"])
        if isinstance(value, list):
            value = ", ".join(map(str, value))
        lines.append(f"{rng.choice(KEY_NAMES[key])}: {value}")
    return "key_value", "\n".join(lines)


# --------------------------------------------------- stage 2: validate conversion

PLACEHOLDER_OUT = re.compile(r"\b(unknown|not provided|not specified|unspecified|n/a|null|none|tbd|tba|"
                             r"not available)\b", re.I)
METADATA_OUT = re.compile(r"https?://|www\.|\.(com|org|net|io|fm|ma)\b|@|#\w|\b[0-9a-f]{8,}\b|\b\d{5,}\b|"
                          r"\bevt_|\borg_", re.I)
FILLER_OUT = re.compile(r"^(this event|attendees|join us|come |great for|perfect for|don't miss|you will|we will)",
                        re.I)
# Cheap support checks for the two inventions seen most in testing.
PRICE_IN = re.compile(r"free|\$|€|£|price|cost|ticket|admission|donat|pay what|sliding|member|fee|"
                      r"complimentary|no charge", re.I)
ONLINE_OUT = re.compile(r"virtual|online|remote|zoom|livestream|webinar", re.I)
ONLINE_IN = re.compile(r"virtual|online|remote|zoom|stream|webinar|discord|twitch", re.I)


def validate(text: str, noise: list[str], raw: str) -> tuple[str | None, dict | str]:
    """Return (normalized embedding text, sections) or (None, reject reason).

    Normalization: sections are put in schema order, bullets lowercased and deduplicated,
    and for listings with an injected price conflict the Cost section is removed, which is
    the spec's rule for conflicting values (the model rarely applies it itself).
    """
    text = re.sub(r"<think>.*?</think>", "", text, flags=re.S).strip()
    text = re.sub(r"^```\w*\n|\n?```$", "", text).strip()
    sections: dict[str, list[str]] = {}
    current = None
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        header = re.fullmatch(r"([A-Za-z]+):", line)
        if header:
            name = header.group(1).capitalize()
            if name not in SECTIONS:
                return None, "unknown_section"
            if name in sections:
                return None, "duplicate_section"
            sections[name], current = [], name
            continue
        if not (line.startswith("- ") and current):
            return None, "stray_text"
        phrase = line[2:].strip().rstrip(".;,").strip().lower()
        if not phrase or len(phrase) > 64 or len(phrase.split()) > 8:
            return None, "long_phrase"
        if re.search(r"[.!?]\s|[!?]$", phrase) or FILLER_OUT.match(phrase):
            return None, "sentence"
        if PLACEHOLDER_OUT.search(phrase):
            return None, "placeholder"
        if METADATA_OUT.search(phrase):
            return None, "metadata"
        if phrase not in sections[current]:
            sections[current].append(phrase)
    if any(not bullets for bullets in sections.values()):
        return None, "empty_section"
    if "Cost" in sections and not PRICE_IN.search(raw):
        return None, "unsupported_cost"
    if any(ONLINE_OUT.search(p) for p in sections.get("Environment", [])) and not ONLINE_IN.search(raw):
        return None, "unsupported_online"
    if "price_conflict" in noise:
        sections.pop("Cost", None)
    if not sections:
        return None, "empty"
    order = sorted(sections, key=SECTIONS.index)
    body = "\n\n".join(f"{name}:\n" + "\n".join(f"- {p}" for p in sections[name]) for name in order)
    return body, {name: sections[name] for name in order}


# ------------------------------------------------------------------------- main

def text_only_kwargs() -> dict:
    """Skip Qwen3.5's vision encoder: text-only use frees memory for the KV cache."""
    from vllm.engine.arg_utils import EngineArgs
    names = {f.name for f in dataclasses.fields(EngineArgs)}
    if "language_model_only" in names:
        return {"language_model_only": True}
    if "limit_mm_per_prompt" in names:
        return {"limit_mm_per_prompt": {"image": 0, "video": 0}}
    return {}


def json_sampling(schema: dict, **kw):
    """SamplingParams constrained to a JSON schema, across vLLM API generations."""
    from vllm import SamplingParams
    try:
        from vllm.sampling_params import StructuredOutputsParams
        return SamplingParams(structured_outputs=StructuredOutputsParams(json=schema), **kw), "structured_outputs"
    except (ImportError, TypeError):
        pass
    try:
        from vllm.sampling_params import GuidedDecodingParams
        return SamplingParams(guided_decoding=GuidedDecodingParams(json=schema), **kw), "guided_decoding"
    except (ImportError, TypeError):
        return SamplingParams(**kw), "unconstrained"


def log(msg: str) -> None:
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model", default="Qwen/Qwen3.5-9B")
    ap.add_argument("--total", type=int, required=True, help="valid rows wanted across all workers")
    ap.add_argument("--num-workers", type=int, default=1)
    ap.add_argument("--worker-index", type=int, default=0)
    ap.add_argument("--chunk", type=int, default=2048, help="events per generation round")
    ap.add_argument("--out", required=True, help="JSONL shard to append to")
    ap.add_argument("--stats", required=True, help="JSON file for counters")
    ap.add_argument("--max-model-len", type=int, default=4096)
    ap.add_argument("--gpu-memory-utilization", type=float, default=0.90)
    ap.add_argument("--max-num-seqs", type=int, default=512)
    ap.add_argument("--margin-minutes", type=float, default=6.0,
                    help="don't start a round this close to the job's end time")
    ap.add_argument("--reject-samples", type=int, default=300,
                    help="rejected conversions to keep for inspection (rejects-<worker>.jsonl)")
    a = ap.parse_args()

    from vllm import LLM, SamplingParams, __version__ as vllm_version

    target = math.ceil(a.total / a.num_workers)
    out = Path(a.out)
    done = sum(1 for _ in out.open()) if out.exists() else 0
    rng = random.Random(secrets.randbits(64))
    engine_seed = secrets.randbits(31)
    prompt = conversion_prompt()
    llm = LLM(model=a.model, max_model_len=a.max_model_len, gpu_memory_utilization=a.gpu_memory_utilization,
              max_num_seqs=a.max_num_seqs, enable_prefix_caching=True, seed=engine_seed, **text_only_kwargs())
    s1_params, s1_mode = json_sampling(S1_SCHEMA, **S1_SAMPLING)
    s2_params = SamplingParams(**S2_SAMPLING)
    template = {"enable_thinking": False}
    log(f"worker {a.worker_index}/{a.num_workers}: target {target} rows (have {done}), vllm {vllm_version}, "
        f"stage-1 decoding {s1_mode}")

    stats = Counter()
    rejects, noise_seen = Counter(), Counter()
    end_time = float(os.environ.get("SLURM_JOB_END_TIME") or 0)
    started, last_round, rejected_samples = time.time(), 0.0, 0
    reject_path = out.with_name(out.name.replace("shard-", "rejects-"))
    with out.open("a") as sink, reject_path.open("a") as reject_sink:
        while done < target:
            if end_time and time.time() + 1.3 * last_round > end_time - 60 * a.margin_minutes:
                log("stopping early: the next round would run into the walltime")
                break
            t0 = time.time()
            n = min(a.chunk, math.ceil((target - done) * 1.2) + 16)  # oversample to cover rejects
            briefs = [sample_brief(rng) for _ in range(n)]
            outs = llm.chat([[{"role": "system", "content": S1_SYSTEM},
                              {"role": "user", "content": stage1_user(b)}] for b in briefs],
                            s1_params, use_tqdm=False, chat_template_kwargs=template)
            items = []
            for brief, o in zip(briefs, outs):
                stats["stage1_prompt_tokens"] += len(o.prompt_token_ids)
                stats["stage1_output_tokens"] += len(o.outputs[0].token_ids)
                rec = parse_stage1(o.outputs[0].text) if o.outputs[0].finish_reason == "stop" else None
                if rec is None:
                    rejects["stage1_invalid"] += 1
                    continue
                noise = add_noise(rec, rng)
                fmt, raw = render(rec, rng)
                items.append((brief, rec, noise, fmt, raw))
            outs = llm.chat([[{"role": "system", "content": prompt}, {"role": "user", "content": it[4]}]
                             for it in items], s2_params, use_tqdm=False, chat_template_kwargs=template)
            kept = 0
            for (brief, rec, noise, fmt, raw), o in zip(items, outs):
                stats["stage2_prompt_tokens"] += len(o.prompt_token_ids)
                stats["stage2_output_tokens"] += len(o.outputs[0].token_ids)
                if o.outputs[0].finish_reason != "stop":
                    rejects["truncated"] += 1
                    continue
                body, sections = validate(o.outputs[0].text, noise, raw)
                if body is None:
                    rejects[sections] += 1
                    if rejected_samples < a.reject_samples:
                        reject_sink.write(json.dumps({"reason": sections, "raw_event": raw,
                                                      "output": o.outputs[0].text}, ensure_ascii=False) + "\n")
                        rejected_samples += 1
                    continue
                if "price_conflict" in noise:
                    stats["conflicts_resolved"] += 1
                if done + kept >= target:
                    break
                row = {"id": uuid.uuid4().hex, "category": brief["category"], "topic": brief["topic"],
                       "listing_style": brief["listing_style"], "completeness": brief["completeness"],
                       "input_format": fmt, "noise": noise, "raw_event": raw,
                       "raw_record": json.dumps(rec, ensure_ascii=False), "embedding_text": body,
                       "sections": {s.lower(): sections.get(s, []) for s in SECTIONS},
                       "brief": json.dumps(brief, ensure_ascii=False)}
                sink.write(json.dumps(row, ensure_ascii=False) + "\n")
                noise_seen.update(noise)
                kept += 1
            sink.flush()
            reject_sink.flush()
            done += kept
            stats["attempted"] += n
            last_round = time.time() - t0
            gen = stats["stage1_output_tokens"] + stats["stage2_output_tokens"]
            log(f"round: kept {kept}/{n} in {last_round:.0f}s | total {done}/{target} | "
                f"{gen / (time.time() - started):.0f} generated tok/s overall | rejects {dict(rejects)}")

    summary = {"worker": a.worker_index, "target": target, "valid": done, "model": a.model,
               "vllm": vllm_version, "engine_seed": engine_seed, "stage1_decoding": s1_mode,
               "seconds": round(time.time() - started, 1), "counters": dict(stats), "rejects": dict(rejects),
               "noise": dict(noise_seen)}
    Path(a.stats).write_text(json.dumps(summary, indent=2))
    log(f"done: {done}/{target} rows in {summary['seconds']}s")


if __name__ == "__main__":
    main()
