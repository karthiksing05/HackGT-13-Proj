"""Embed the activities that have no current vector, through the running ML service.

    cd ml && python -m tools.embed_missing [--uri mongodb://127.0.0.1:27017/?directConnection=true]
        [--db freetime] [--collections activities demo_activities] [--ml-url http://127.0.0.1:8000]
        [--batch 32] [--limit N] [--dry-run] [--no-stale]

Runs every 15 minutes on the VPS (`ml-embed-missing.timer`). A document needs a vector when it has
an `embeddingText` and `embeddingTextHash` and either no `embedding`, or one written for an older
text (`embeddingMeta.textHash` != `embeddingTextHash`; `--no-stale` skips those). This is
`datagen/mongo_backfill.needs_embedding`'s selection. Documents whose stored hash does not match
their text are skipped (the ingestion pipeline always writes both). Texts are deduplicated by hash
across collections and embedded with `POST /v1/embed` (kind "activity": no instruction prefix, the
model of the stored vectors), after `/healthz` confirms the service embeds with that model.

Writes are guarded as the backfill's are: `$set` only where `embeddingTextHash` still names the
embedded text and the vector is missing or one written for another text, so a concurrent text change
or a vector from elsewhere is never overwritten. Exit 1 when the service is unreachable, embeds with
another model or fails midway (what was embedded before the failure is written); the next run picks
up the rest.
"""

from __future__ import annotations

import argparse
import hashlib
import math
import sys
from dataclasses import dataclass, field
from datetime import datetime, timezone

import httpx

DEFAULT_URI = "mongodb://127.0.0.1:27017/?directConnection=true"
DEFAULT_ML_URL = "http://127.0.0.1:8000"
DEFAULT_MODEL = "Qwen/Qwen3-Embedding-0.6B"
DEFAULT_DIM = 1024
MAX_SEQ_LENGTH = 512
WRITE_BATCH = 250  # update ops per bulk_write; a vector is ~9 KB of BSON
SOURCE = "embed_missing"


class ServiceError(RuntimeError):
    """The ML service cannot be used: unreachable, another model, or a bad answer."""


@dataclass
class Todo:
    """The unique texts to embed and, per collection, the documents waiting for each text."""

    texts: dict[str, str] = field(default_factory=dict)  # hash -> text
    docs: dict[str, dict[str, list]] = field(default_factory=dict)  # collection -> hash -> [_id]
    counts: dict[str, dict[str, int]] = field(default_factory=dict)  # collection -> counters


def text_hash(text: str) -> str:
    return hashlib.sha1(text.encode()).hexdigest()


def needs_embedding(col, stale: bool = True) -> list[tuple]:
    """`[(_id, embeddingTextHash, embeddingText)]` for the documents that need a vector."""
    project = {
        "hash": "$embeddingTextHash",
        "embedded": "$embeddingMeta.textHash",
        "text": "$embeddingText",
        "has": {"$ne": [{"$type": "$embedding"}, "missing"]},
    }
    out = []
    for d in col.aggregate([{"$match": {"embeddingTextHash": {"$type": "string"}}}, {"$project": project}]):
        outdated = stale and d.get("embedded") is not None and d["embedded"] != d["hash"]
        if not d["has"] or outdated:
            out.append((d["_id"], d["hash"], d.get("text")))
    return out


def embedding_filter(_id, h: str) -> dict:
    """Only where the text is still `h` and the vector is missing or was written for another text."""
    return {
        "_id": _id,
        "embeddingTextHash": h,
        "$or": [{"embedding": {"$exists": False}}, {"embeddingMeta.textHash": {"$exists": True, "$ne": h}}],
    }


def plan(db, collections: list[str], *, stale: bool = True, limit: int = 0) -> Todo:
    """Select the documents and deduplicate their texts; `limit` caps the unique texts."""
    todo = Todo()
    for name in collections:
        counts = {"candidates": 0, "blank": 0, "hash_mismatch": 0, "over_limit": 0}
        waiting: dict[str, list] = {}
        for _id, h, text in needs_embedding(db[name], stale):
            counts["candidates"] += 1
            if not text or not text.strip():
                counts["blank"] += 1
                continue
            if text_hash(text) != h:
                counts["hash_mismatch"] += 1
                continue
            if h not in todo.texts:
                if limit and len(todo.texts) >= limit:
                    counts["over_limit"] += 1
                    continue
                todo.texts[h] = text
            waiting.setdefault(h, []).append(_id)
        todo.docs[name] = waiting
        todo.counts[name] = counts
    return todo


def check_service(client: httpx.Client, ml_url: str, model: str) -> dict:
    try:
        response = client.get(f"{ml_url}/healthz", timeout=15)
    except httpx.HTTPError as exc:
        raise ServiceError(f"ML service unreachable at {ml_url}: {type(exc).__name__}: {exc}") from exc
    if response.status_code != 200:
        raise ServiceError(f"{ml_url}/healthz answered HTTP {response.status_code}")
    served = ((response.json() or {}).get("embedding") or {}).get("model")
    if served != model:
        raise ServiceError(f"the service embeds with {served!r}; the stored vectors are {model!r}")
    return response.json()


def embed_batch(client: httpx.Client, ml_url: str, texts: list[str], dim: int) -> tuple[list[list[float]], str, str]:
    """Vectors (unit norm) for non-blank texts, the provider that served them and the model."""
    try:
        response = client.post(f"{ml_url}/v1/embed", json={"texts": texts, "kind": "activity"}, timeout=300)
    except httpx.HTTPError as exc:
        raise ServiceError(f"/v1/embed failed: {type(exc).__name__}: {exc}") from exc
    if response.status_code != 200:
        raise ServiceError(f"/v1/embed answered HTTP {response.status_code}: {response.text[:200]}")
    body = response.json()
    vectors = body.get("embeddings") or []
    if len(vectors) != len(texts) or body.get("dim") != dim:
        raise ServiceError(f"/v1/embed returned {len(vectors)} vectors of dim {body.get('dim')} for {len(texts)} texts")
    for vector in vectors:
        norm = math.sqrt(sum(x * x for x in vector))
        if len(vector) != dim or not abs(norm - 1.0) < 1e-3:
            raise ServiceError(f"/v1/embed returned a vector of length {len(vector)} and norm {norm:.4f}")
    return vectors, body.get("provider") or "unknown", body.get("model") or ""


def write(db, todo: Todo, vectors: dict[str, tuple[list[float], str]], model: str, now: datetime) -> dict[str, tuple[int, int]]:
    """Guarded `$set`s; returns `{collection: (planned, modified)}`."""
    from pymongo import UpdateOne

    meta = {"model": model, "dimension": None, "normalized": True, "prompt": None, "maxSeqLength": MAX_SEQ_LENGTH,
            "generatedAt": now, "source": SOURCE}
    out = {}
    for name, waiting in todo.docs.items():
        ops = []
        for h, ids in waiting.items():
            if h not in vectors:
                continue
            vector, provider = vectors[h]
            update = {"$set": {
                "embedding": vector,
                "embeddingModel": model,
                "embeddingMeta": {**meta, "dimension": len(vector), "textHash": h, "provider": provider},
            }}
            ops.extend(UpdateOne(embedding_filter(_id, h), update) for _id in ids)
        modified = 0
        for start in range(0, len(ops), WRITE_BATCH):
            modified += db[name].bulk_write(ops[start : start + WRITE_BATCH], ordered=False).modified_count
        out[name] = (len(ops), modified)
    return out


def run(args: argparse.Namespace, *, client: httpx.Client | None = None, db=None, out=sys.stdout) -> int:
    """The tool's work; `client` and `db` are injectable for tests. Returns the exit code."""
    if db is None:
        from pymongo import MongoClient

        db = MongoClient(args.uri, tz_aware=True, serverSelectionTimeoutMS=8000)[args.db]
    ml_url = args.ml_url.rstrip("/")
    todo = plan(db, args.collections, stale=not args.no_stale, limit=args.limit)
    for name in args.collections:
        c = todo.counts[name]
        docs = sum(len(ids) for ids in todo.docs[name].values())
        print(f"{name}: {c['candidates']} need a vector; {docs} to embed; skipped: {c['blank']} blank, "
              f"{c['hash_mismatch']} hash mismatch, {c['over_limit']} over --limit", file=out)
    print(f"{len(todo.texts)} unique texts", file=out)
    if args.dry_run or not todo.texts:
        return 0

    own_client = client is None
    client = client or httpx.Client()
    vectors: dict[str, tuple[list[float], str]] = {}
    model = args.model
    status = 0
    try:
        check_service(client, ml_url, args.model)
        hashes = list(todo.texts)
        for start in range(0, len(hashes), args.batch):
            batch = hashes[start : start + args.batch]
            rows, provider, served = embed_batch(client, ml_url, [todo.texts[h] for h in batch], args.dim)
            if served != args.model:
                raise ServiceError(f"/v1/embed answered with model {served!r}, expected {args.model!r}")
            for h, row in zip(batch, rows):
                vectors[h] = (row, provider)
            print(f"embedded {len(vectors)}/{len(hashes)} ({provider})", file=out)
    except ServiceError as exc:
        print(f"error: {exc}", file=out)
        status = 1
    finally:
        if own_client:
            client.close()

    if vectors:
        for name, (planned, modified) in write(db, todo, vectors, model, datetime.now(timezone.utc)).items():
            print(f"{name}: wrote {modified} of {planned} planned ({planned - modified} changed meanwhile)", file=out)
    return status


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--uri", default=DEFAULT_URI)
    ap.add_argument("--db", default="freetime")
    ap.add_argument("--collections", nargs="+", default=["activities", "demo_activities"])
    ap.add_argument("--ml-url", default=DEFAULT_ML_URL)
    ap.add_argument("--model", default=DEFAULT_MODEL, help="the model the stored vectors come from")
    ap.add_argument("--dim", type=int, default=DEFAULT_DIM)
    ap.add_argument("--batch", type=int, default=32, help="texts per /v1/embed request (the service takes up to 64)")
    ap.add_argument("--limit", type=int, default=0, help="at most N unique texts per run (0: all)")
    ap.add_argument("--dry-run", action="store_true", help="report what would be embedded; no service calls, no writes")
    ap.add_argument("--no-stale", action="store_true", help="only documents without a vector")
    args = ap.parse_args(argv)
    if not 1 <= args.batch <= 64:
        ap.error("--batch must be between 1 and 64")
    return args


def main(argv: list[str] | None = None) -> None:
    sys.exit(run(parse_args(argv)))


if __name__ == "__main__":
    main()
