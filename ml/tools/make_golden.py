"""Regenerate the byte-exact fixtures under tests/fixtures.

    python -m tools.make_golden --profiles      # profile_jordan / profile_seed_user / search_jordan (.json)
    python -m tools.make_golden --embeddings    # golden_texts.json + qwen3_golden.json with the local model
    python -m tools.make_golden                 # both

The text fixtures hold the request and the rendered texts; the tests re-render the request and
compare bytes, so after an intentional lexicon change rerun `--profiles`, review the diff by eye
and bump `profiles.TEMPLATE_VERSION`. The embedding golden is what every remote provider is
measured against (`tools.parity_check`): five texts embedded by `LocalEmbedder` (fp32, CPU,
`max_seq_length` 512, no prompt). It also keeps one activity's production vector (backfilled on
Raven in bf16) next to the local one, so parity with the stored catalog is checked offline too.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import platform
import sys
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # ml/

from api.schemas.profile import SearchProfileRequest, UserProfileRequest  # noqa: E402
from embedding import CANARY_TEXT, DIM, MODEL, LocalEmbedder  # noqa: E402
from embedding.local import DEFAULT_MAX_SEQ_LENGTH  # noqa: E402
from profiles import TEMPLATE_VERSION, build_profile_texts, build_search_text, profile_hash  # noqa: E402

FIXTURES = Path(__file__).resolve().parents[1] / "tests" / "fixtures"

# A: the contract's `Preferences` example + `FacebookImport.interests` + two rated events.
PROFILE_JORDAN = {
    "ratings": {"food": 4, "museums": 3, "nightlife": 2, "outdoors": 5},
    "company": "small_group",
    "pace": "balanced",
    "spend": "under_15",
    "flexibility": "bit_over_ok",
    "prefer_free": True,
    "answers": {},
    "facebook_interests": ["Hiking", "Indie rock", "Coffee", "Street food", "Board games"],
    "rated_events": [
        {"stars": 5, "tags": ["Great people"], "category": "park", "activity_tags": ["outdoor"]},
        {"stars": 2, "tags": ["Too crowded"], "category": "bar", "activity_tags": ["late_night"]},
    ],
}

# B: the demo seed user (Sandy Byte, docs/design/backend-contract.md).
PROFILE_SEED_USER = {
    "ratings": {
        "outdoors": 5, "long_walks": 5, "live_music": 4, "food": 4, "early_mornings": 4, "museums": 3,
        "sports": 3, "shopping": 2, "nightlife": 2, "big_crowds": 1,
    },
    "company": "small_group",
    "pace": "balanced",
    "spend": "under_15",
    "flexibility": "bit_over_ok",
    "prefer_free": True,
    "answers": {
        "perfect_afternoon": "A long walk along the water, a snack from the market, then live music somewhere small while the sun goes down.",
        "never_do": "Packed clubs, huge crowds, or anything that only gets going after midnight.",
        "plan_around": "Sunrise swims, the Saturday market, and whoever's free to wander.",
    },
}

# C: the contract's `PlanRequest` (Friday 14:10 in New York, back by 18:30).
SEARCH_JORDAN = {
    "mood_text": "Something chill and outside, then cheap food after.",
    "tags": ["Outdoors", "Food", "Meet people"],
    "who": "friends",
    "pace": "balanced",
    "budget": 1,
    "start_time": "2026-09-25T18:10:00Z",
    "back_by": "2026-09-25T22:30:00Z",
    "timezone": "America/New_York",
}

# One activity from the production catalog (freetime.activities), text + hash as stored.
REFERENCE_ACTIVITY_TEXT = (
    "Interests:\n- disney\n- theater\n- ice skating\n\nActivities:\n- live performance\n- spectator sports\n\n"
    "Social:\n- family friendly\n- large group\n\nEnvironment:\n- indoor\n- arena\n- loud atmosphere\n- high energy\n\n"
    "Pace:\n- high energy\n\nTiming:\n- morning\n\nExperience:\n- ice show\n- character performance"
)
REFERENCE_ACTIVITY_HASH = "b0060f0fe111bfe2ed5e03b2bccf3091fe48381f"


def write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n")
    print(f"wrote {path.relative_to(FIXTURES.parent.parent)}")


def make_profiles(out: Path) -> dict[str, str]:
    """Render the three text fixtures; returns the texts the embedding golden reuses."""
    texts: dict[str, str] = {}
    for name, raw in (("profile_jordan", PROFILE_JORDAN), ("profile_seed_user", PROFILE_SEED_USER)):
        request = UserProfileRequest.model_validate(raw)
        rendered = build_profile_texts(request.to_input())
        write_json(
            out / f"{name}.json",
            {
                "request": raw,
                "template_version": TEMPLATE_VERSION,
                "positive_text": rendered.positive,
                "negative_text": rendered.negative,
                "profile_text_hash": profile_hash(rendered.positive, rendered.negative),
            },
        )
        texts[f"{name}:positive"] = rendered.positive
        texts[f"{name}:negative"] = rendered.negative
    request = SearchProfileRequest.model_validate(SEARCH_JORDAN)
    search_text = build_search_text(request.to_input())
    write_json(out / "search_jordan.json", {"request": SEARCH_JORDAN, "template_version": TEMPLATE_VERSION, "search_text": search_text})
    texts["search_jordan"] = search_text
    return texts


def golden_texts(profile_texts: dict[str, str]) -> list[dict[str, str]]:
    assert hashlib.sha1(REFERENCE_ACTIVITY_TEXT.encode()).hexdigest() == REFERENCE_ACTIVITY_HASH
    return [
        {"name": "canary", "text": CANARY_TEXT},
        {"name": "profile_jordan:positive", "text": profile_texts["profile_jordan:positive"]},
        {"name": "profile_jordan:negative", "text": profile_texts["profile_jordan:negative"]},
        {"name": "search_jordan", "text": profile_texts["search_jordan"]},
        {"name": "activity:" + REFERENCE_ACTIVITY_HASH[:12], "text": REFERENCE_ACTIVITY_TEXT},
    ]


def stored_reference_vector(uri: str | None, db: str) -> list[float] | None:
    """The reference activity's production vector, when a Mongo with the catalog is reachable."""
    if not uri:
        return None
    from pymongo import MongoClient

    doc = MongoClient(uri, serverSelectionTimeoutMS=4000)[db]["activities"].find_one(
        {"embeddingTextHash": REFERENCE_ACTIVITY_HASH, "embedding": {"$exists": True}}, {"embedding": 1}
    )
    return None if doc is None else [float(x) for x in doc["embedding"]]


def make_embeddings(out: Path, profile_texts: dict[str, str], mongo_uri: str | None, db: str, threads: int | None) -> None:
    texts = golden_texts(profile_texts)
    write_json(out / "golden_texts.json", {"model": MODEL, "texts": texts})

    embedder = LocalEmbedder(threads=threads)
    vectors = embedder.embed([t["text"] for t in texts]).astype(np.float64)
    norms = np.linalg.norm(vectors, axis=1)
    payload = {
        "model": MODEL,
        "dim": DIM,
        "max_seq_length": DEFAULT_MAX_SEQ_LENGTH,
        "normalized": True,
        "prompt": None,
        "dtype": "float32",
        "device": embedder.device,
        "platform": platform.platform(),
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "norm": {"min": float(norms.min()), "max": float(norms.max())},
        "texts": [t["name"] for t in texts],
        "embeddings": [[round(float(x), 6) for x in row] for row in vectors],
    }
    reference = stored_reference_vector(mongo_uri, db)
    if reference is not None:
        cosine = float(np.dot(vectors[-1], np.asarray(reference)) / np.linalg.norm(reference))
        print(f"reference activity: cosine(local fp32, stored Raven bf16) = {cosine:.6f}")
        payload["reference_activity"] = {
            "text_hash": REFERENCE_ACTIVITY_HASH,
            "source": f"{db}.activities embedding (backfill, bf16 on GPU)",
            "cosine_vs_local": round(cosine, 6),
            "embedding": [round(x, 6) for x in reference],
        }
    else:
        print("reference activity: no Mongo given or vector not found; fixture has no stored vector")
    write_json(out / "qwen3_golden.json", payload)
    print(f"embedded {len(texts)} texts, dim {vectors.shape[1]}, norms {norms.min():.6f}-{norms.max():.6f}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--out", type=Path, default=FIXTURES)
    ap.add_argument("--profiles", action="store_true", help="only the text fixtures")
    ap.add_argument("--embeddings", action="store_true", help="only the embedding goldens (loads the local model)")
    ap.add_argument("--mongo-uri", default=None, help="optional: read the reference activity's stored vector")
    ap.add_argument("--db", default="freetime")
    ap.add_argument("--threads", type=int, default=None)
    a = ap.parse_args()
    both = not a.profiles and not a.embeddings
    texts = make_profiles(a.out) if (a.profiles or both) else _texts_from_fixtures(a.out)
    if a.embeddings or both:
        make_embeddings(a.out, texts, a.mongo_uri, a.db, a.threads)


def _texts_from_fixtures(out: Path) -> dict[str, str]:
    jordan = json.loads((out / "profile_jordan.json").read_text())
    search = json.loads((out / "search_jordan.json").read_text())
    return {
        "profile_jordan:positive": jordan["positive_text"],
        "profile_jordan:negative": jordan["negative_text"],
        "search_jordan": search["search_text"],
    }


if __name__ == "__main__":
    main()
