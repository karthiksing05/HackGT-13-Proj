"""Rank real catalog activities for a profile through a running service: the end-to-end sanity check.

    cd ml && python -m tools.rank_smoke [--ml-url http://127.0.0.1:8000]
        [--uri mongodb://127.0.0.1:27017/?directConnection=true] [--db freetime] [--catalog demo_activities]
        [--profile tests/fixtures/profile_seed_user.json] [--mood "something outdoorsy"] [--sample 300] [--top 10]

Builds the profile's texts and vectors with /v1/user-profile (the fixture's `request`), and a search from
--mood with /v1/search-profile; reads up to --sample activities that have a stored vector (read-only,
in _id order); ranks them with /v1/events/rank (classifier only: no hard filters apply without
constraints, no Jev); prints the best --top and the worst three with their categories, so a person can
judge whether the order makes sense. Exit 1 when the service or the catalog cannot be used.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import httpx

DEFAULT_PROFILE = Path(__file__).resolve().parents[1] / "tests" / "fixtures" / "profile_seed_user.json"


def load_activities(uri: str, db: str, catalog: str, sample: int) -> list[dict]:
    from pymongo import MongoClient

    projection = {"name": 1, "category": 1, "embedding": 1, "embeddingText": 1}
    with MongoClient(uri, serverSelectionTimeoutMS=8000) as client:
        cursor = client[db][catalog].find({"embedding": {"$exists": True}}, projection).sort("_id", 1).limit(sample)
        return [{**d, "_id": str(d["_id"])} for d in cursor]


def post(client: httpx.Client, url: str, body: dict) -> dict:
    response = client.post(url, json=body)
    if response.status_code != 200:
        raise RuntimeError(f"{url}: HTTP {response.status_code}: {response.text[:200]}")
    return response.json()


def rank(client: httpx.Client, ml_url: str, profile_request: dict, activities: list[dict], mood: str | None) -> tuple[dict, list[dict], str | None]:
    profile = post(client, f"{ml_url}/v1/user-profile", profile_request)
    if not profile["positive_text"]:
        raise RuntimeError("the profile is empty: nothing to rank by")
    body = {
        "user": {
            "positive_embedding": profile["positive_embedding"], "negative_embedding": profile["negative_embedding"],
            "positive_text": profile["positive_text"], "negative_text": profile["negative_text"],
        },
        "events": [{"id": a["_id"], "embedding": a["embedding"], "category": a.get("category")} for a in activities],
        "options": {"rerank": False},
    }
    search_text = None
    if mood:
        search = post(client, f"{ml_url}/v1/search-profile", {"mood_text": mood})
        if search["search_embedding"]:
            body["search_embedding"], body["search_text"] = search["search_embedding"], search["search_text"]
            search_text = search["search_text"]
    return profile, post(client, f"{ml_url}/v1/events/rank", body)["events"], search_text


def run(argv: list[str] | None = None, *, activities: list[dict] | None = None,
        transport: httpx.BaseTransport | None = None, out=sys.stdout) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--ml-url", default="http://127.0.0.1:8000")
    ap.add_argument("--uri", default="mongodb://127.0.0.1:27017/?directConnection=true")
    ap.add_argument("--db", default="freetime")
    ap.add_argument("--catalog", default="demo_activities")
    ap.add_argument("--profile", type=Path, default=DEFAULT_PROFILE, help="a JSON file with the /v1/user-profile body under `request`")
    ap.add_argument("--mood", default=None, help="optional mood text, sent as this request's search")
    ap.add_argument("--sample", type=int, default=300)
    ap.add_argument("--top", type=int, default=10)
    args = ap.parse_args(argv)

    try:
        profile_request = json.loads(args.profile.read_text())["request"]
        catalog = activities if activities is not None else load_activities(args.uri, args.db, args.catalog, args.sample)
        if not catalog:
            raise RuntimeError(f"{args.db}.{args.catalog} has no activities with a vector")
        with httpx.Client(timeout=300, transport=transport) as client:
            profile, ranked, search_text = rank(client, args.ml_url.rstrip("/"), profile_request, catalog, args.mood)
    except Exception as exc:  # an operator tool: report and exit non-zero
        print(f"error: {type(exc).__name__}: {exc}", file=out)
        return 1

    names = {a["_id"]: (a.get("name") or "?", a.get("category") or "?") for a in catalog}
    print(f"profile via {profile['provider']} ({profile['profile_text_hash'][:22]}…); {len(catalog)} activities from {args.catalog}", file=out)
    if search_text:
        print("search: " + search_text.replace("\n", " | "), file=out)

    def line(i: int, e: dict) -> str:
        name, category = names.get(e["event_id"], ("?", "?"))
        return f"{i:>4}. {e['score']:.3f}  {category:<16} {name}"

    for i, e in enumerate(ranked[: args.top], 1):
        print(line(i, e), file=out)
    if len(ranked) > args.top + 3:
        print("   …", file=out)
        for i, e in enumerate(ranked[-3:], len(ranked) - 2):
            print(line(i, e), file=out)
    return 0


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
