"""Ticket-site listings for the pitch catalog's 150 paid items (the sandbox merchant, events.sidequestz.tech).

    cd dataingestion
    .venv/bin/python -m pitch.tickets        # pitch/out/ticket_listings.json

One listing per paid document, in the ticket site's own `models.Event` shape (Events/pkg/models), keyed by
the slug of the document's `ticketUrl` (https://events.sidequestz.tech/{slug}/tickets). Prices follow the
app: a stop costs `price.min`, so `unit_cents` is the minimum and `max_unit_cents` the maximum. Events keep
their own start and end. Places sell admission, a cover or a prepaid reservation valid on any day of the
pitch range, so their window is the whole range (see TICKETS_HANDOFF.md). Synthetic listings: the venues are
real, the events and prices are invented for the demo.
"""

from __future__ import annotations

import json
import re
from collections import Counter
from datetime import datetime, time, timezone

from .generate import OUT, RANGE, TZ
from .validate import TICKET_RE, load_export

LISTINGS = OUT / "ticket_listings.json"

CAPACITY = {  # rough room sizes by category, for events; places get 1000
    "comedy": 120, "theater": 400, "cinema": 150, "live_music": 250, "nightclub": 300, "bar": 120,
    "restaurant": 60, "class_workshop": 20, "tour": 25, "festival": 1000, "sports_event": 500,
    "community_event": 150, "market": 500,
}


# What a paid place sells: a prepaid table (counts toward the bill), a cover, or admission.
PLACE_LISTING = {"restaurant": "reservation", "cafe": "reservation", "bar": "cover", "nightclub": "cover"}


def cents(x) -> int:
    return int(round(float(x) * 100))


def listing(d: dict) -> dict:
    slug = TICKET_RE.fullmatch(d["ticketUrl"])[1]
    p, event = d["price"], d["kind"] == "event"
    if event:
        start, end = d["start"], d["end"]
    else:  # a pass for the whole range
        start = datetime.combine(datetime.fromisoformat(RANGE[0]).date(), time(0), TZ).astimezone(timezone.utc)
        end = datetime.combine(datetime.fromisoformat(RANGE[1]).date(), time(23, 59), TZ).astimezone(timezone.utc)
    capacity = CAPACITY.get(d["category"], 200) if event else 1000
    return {
        "slug": slug,
        "title": d["name"],
        "description": d["description"],
        "summary": d["summary"],
        "category": d["category"],
        "tags": d["tags"],
        "venue": d["venueName"] or d["name"],
        "address": (d.get("address") or {}).get("formatted") or "",
        "starts_at": start.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "ends_at": end.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "unit_cents": cents(p["min"]),
        "max_unit_cents": cents(p["max"] if p.get("max") is not None else p["min"]),
        "capacity": capacity,
        "remaining": capacity,
        "image_url": "",
        # not in models.Event: what the site needs to render the listing right
        "listing_type": "event" if event else PLACE_LISTING.get(d["category"], "admission_pass"),
        "attendance": d.get("attendance"),
        "age_21_plus": "21_plus" in d["tags"],
        "series": (d.get("recurrence") or {}).get("text"),
        "catalog_id": str(d["_id"]),
        "ticket_url": d["ticketUrl"],
    }


def main() -> None:
    docs = [d for d in load_export() if d.get("ticketUrl")]
    rows = sorted((listing(d) for d in docs), key=lambda r: (r["starts_at"], r["title"]))
    slugs = [r["slug"] for r in rows]
    assert len(slugs) == len(set(slugs)) == 150, (len(slugs), len(set(slugs)))
    assert all(re.fullmatch(r"[a-z0-9]+(-[a-z0-9]+)*", s) for s in slugs)
    LISTINGS.write_text(json.dumps({"count": len(rows), "currency": "USD", "listings": rows}, indent=1, ensure_ascii=False) + "\n")
    kinds = Counter(r["listing_type"] for r in rows)
    print(f"wrote {len(rows)} listings ({dict(kinds)}) to {LISTINGS}")


if __name__ == "__main__":
    main()
