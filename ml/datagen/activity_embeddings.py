#!/usr/bin/env python3
"""Embed the activity texts of the backfill (step 2 of mongo_backfill.py), as the classifier was trained.

Qwen3-Embedding-0.6B through sentence-transformers, loaded by ml/data/embed.py (bf16 on the GPU,
max_seq_length 512), no instruction prefix, L2-normalized: the setup the compatibility
classifier's event embeddings were trained with. Each unique text (by `embeddingTextHash`) is
embedded once; documents sharing a text share its vector.

Input: embed_todo.jsonl (existing texts) and texts-*.jsonl (texts written by activity_texts.py).
Output in --out: vectors.npy (float32 [N, 1024]), hashes.json (row order), meta.json.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # ml/, for data.embed
from data.embed import DEFAULT_MODEL, embed_column, embedding_dim, load_model  # noqa: E402


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--todo", required=True, help="embed_todo.jsonl from mongo_backfill.py export")
    ap.add_argument("--texts", nargs="*", default=[], help="texts-*.jsonl from activity_texts.py")
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--model", default=DEFAULT_MODEL)
    ap.add_argument("--batch-size", type=int, default=128)
    ap.add_argument("--max-seq-length", type=int, default=512)
    ap.add_argument("--limit", type=int, default=0, help="only the first N existing texts (testing)")
    a = ap.parse_args()

    texts: dict[str, str] = {}
    sources = {"existing": 0, "generated": 0}
    todo = [json.loads(line) for line in open(a.todo)]
    for row in todo[:a.limit] if a.limit else todo:
        texts[row["text_hash"]] = row["text"]
        sources["existing"] += 1
    for path in a.texts:
        for row in map(json.loads, open(path)):
            if row["text_hash"] not in texts:
                texts[row["text_hash"]] = row["text"]
                sources["generated"] += 1
    bad = [h for h, t in texts.items() if hashlib.sha1(t.encode()).hexdigest() != h]
    if bad:
        raise SystemExit(f"{len(bad)} texts don't match their hash, e.g. {bad[0]}")
    hashes = sorted(texts)
    print(f"{len(hashes)} unique texts ({sources})", flush=True)

    started = time.time()
    model = load_model(a.model, max_seq_length=a.max_seq_length)
    lengths = [len(ids) for ids in model.tokenizer([texts[h] for h in hashes])["input_ids"]]
    vectors, present = embed_column(model, [texts[h] for h in hashes], a.batch_size)
    if not present.all():
        raise SystemExit(f"{(~present).sum()} empty texts")
    norms = np.linalg.norm(vectors.astype(np.float64), axis=1)

    a.out.mkdir(parents=True, exist_ok=True)
    np.save(a.out / "vectors.npy", vectors)
    (a.out / "hashes.json").write_text(json.dumps(hashes))
    meta = {
        "model": a.model, "dimension": embedding_dim(model), "normalized": True, "prompt": None,
        "max_seq_length": a.max_seq_length, "dtype": "float32", "count": len(hashes), "sources": sources,
        "tokens": {"max": max(lengths), "mean": round(float(np.mean(lengths)), 1),
                   "truncated": sum(n > a.max_seq_length for n in lengths)},
        "norm": {"min": float(norms.min()), "max": float(norms.max())},
        "device": str(model.device), "seconds": round(time.time() - started, 1),
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
    }
    (a.out / "meta.json").write_text(json.dumps(meta, indent=2))
    print(json.dumps(meta, indent=2))


if __name__ == "__main__":
    main()
