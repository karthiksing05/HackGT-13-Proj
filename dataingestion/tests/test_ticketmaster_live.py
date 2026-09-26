"""Smoke test: is the Ticketmaster Discovery API reachable with our key?

Run directly:  python3 dataingestion/tests/test_ticketmaster_live.py
Or via pytest: pytest dataingestion/tests/test_ticketmaster_live.py

Stdlib only, so it works before the ingestion deps are installed.
"""

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ENV_PATH = Path(__file__).resolve().parents[2] / ".env"
EVENTS_URL = "https://app.ticketmaster.com/discovery/v2/events.json"
ATLANTA_LATLONG = "33.7756,-84.3963"  # Klaus / Midtown


def load_api_key():
    key = os.environ.get("TICKETMASTER_API_KEY")
    if key:
        return key
    if ENV_PATH.exists():
        for line in ENV_PATH.read_text().splitlines():
            name, _, value = line.partition("=")
            if name.strip() == "TICKETMASTER_API_KEY":
                return value.strip().strip("'\"")
    raise RuntimeError("TICKETMASTER_API_KEY not set in environment or .env")


def fetch_events(size=5):
    params = urllib.parse.urlencode({
        "apikey": load_api_key(),
        "latlong": ATLANTA_LATLONG,
        "radius": 40,
        "unit": "km",
        "size": size,
        "sort": "date,asc",
    })
    with urllib.request.urlopen(f"{EVENTS_URL}?{params}", timeout=15) as resp:
        return resp.status, json.load(resp)


def check_atlanta_events():
    try:
        status, body = fetch_events()
    except urllib.error.HTTPError as e:
        raise AssertionError(f"Ticketmaster returned HTTP {e.code}: {e.read()[:300]!r}")

    assert status == 200
    assert body["page"]["totalElements"] > 0, "no events found near Atlanta"
    events = body["_embedded"]["events"]
    for event in events:
        assert event["id"] and event["name"]
        assert "start" in event["dates"]
    return body


def test_ticketmaster_returns_atlanta_events():
    check_atlanta_events()


if __name__ == "__main__":
    body = check_atlanta_events()
    print(f"OK: {body['page']['totalElements']} events near Atlanta. First few:")
    for e in body["_embedded"]["events"]:
        start = e["dates"]["start"].get("localDate", "?")
        venue = e.get("_embedded", {}).get("venues", [{}])[0].get("name", "?")
        print(f"  {start}  {e['name']}  @ {venue}")
