#!/usr/bin/env python3
"""Backfill embedding texts and embeddings for the activities in the live MongoDB.

  1. export (here, through an SSH tunnel to the VPS):
       activities without `embeddingText` -> texts_todo.jsonl: the pipeline's prompt, once per unique input
       texts without an `embedding`       -> embed_todo.jsonl: once per unique `embeddingTextHash`
  2. on Raven (ml/datagen/backfill.sbatch): Qwen3.5-9B writes the missing texts with the ingestion
     pipeline's prompt and checks (activity_texts.py), then Qwen3-Embedding-0.6B embeds every text
     the way the compatibility classifier was trained: 1024-d, L2-normalized, no instruction prefix
     (activity_embeddings.py)
  3. apply (here): add-only, guarded writes; then verify

    ssh -f -N -L 27018:127.0.0.1:27017 root@<vps>       # tunnel, see Backend/INFRASTRUCTURE.md
    URI=mongodb://127.0.0.1:27018/?directConnection=true
    python ml/datagen/mongo_backfill.py export --uri $URI --out <dir> [--embed-collections activities demo_activities]
    (copy <dir> to Raven; mpcdf.py submit raven ml/datagen/backfill.sbatch -- --in <dir on Raven>; fetch the run dir)
    python ml/datagen/mongo_backfill.py apply --uri $URI --export <dir> --results <run dir> --dry-run
    python ml/datagen/mongo_backfill.py apply --uri $URI --export <dir> --results <run dir>
    python ml/datagen/mongo_backfill.py verify --uri $URI
    python ml/datagen/mongo_backfill.py rollback --uri $URI --run <run id> [--yes]

Writes are guarded. A text is written only where `embeddingText` is still missing. An embedding is
written only where `embeddingTextHash` still names the embedded text, and only where there is no
vector yet or the vector is one this tool wrote for a text that has since changed (spec 6.5:
re-embed when `embeddingTextHash` changes); vectors from anywhere else are never touched. Every
write carries the export's run id (`embeddingTextMeta.backfill`, `embeddingMeta.backfill`), which
`rollback` uses to remove exactly what that run added.

Without the pipeline's web research, sparse listings (most trails and parks) get sparse and often
identical texts, as the prompt asks. Running the pipeline's grounded `embed-text --kind place` later
rewrites them; rerunning this backfill afterwards re-embeds exactly the texts that changed.
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parent))
import activity_text as at  # noqa: E402

REPO = Path(__file__).resolve().parents[2]
TEMPLATE_PATH = "dataingestion/ingest/agent/prompts/event_embedding_text.md"
TEXT_FIELDS = ("embeddingText", "embeddingTextHash", "embeddingTextMeta")
EMBED_FIELDS = ("embedding", "embeddingModel", "embeddingMeta")
BATCH = 250  # update ops per bulk_write; an embedding is ~9 KB of BSON


def connect(uri: str, db: str):
    from pymongo import MongoClient
    return MongoClient(uri, tz_aware=True, serverSelectionTimeoutMS=8000)[db]  # tz_aware, as the pipeline


def write_jsonl(path: Path, rows) -> int:
    from bson import json_util
    n = 0
    with path.open("w") as f:
        for row in rows:
            f.write(json_util.dumps(row, ensure_ascii=False) + "\n")
            n += 1
    return n


def read_jsonl(path: Path):
    from bson import json_util
    with path.open() as f:
        for line in f:
            yield json_util.loads(line)


def read_template(a) -> str:
    if a.template:
        return Path(a.template).read_text()
    return subprocess.run(["git", "show", f"{a.template_ref}:{TEMPLATE_PATH}"], cwd=REPO, check=True,
                          capture_output=True, text=True).stdout


def bulk(coll, ops: list, dry_run: bool) -> tuple[int, int]:
    """Run update ops in batches; returns (matched, modified)."""
    if dry_run or not ops:
        return 0, 0
    matched = modified = 0
    for start in range(0, len(ops), BATCH):
        res = coll.bulk_write(ops[start:start + BATCH], ordered=False)
        matched += res.matched_count
        modified += res.modified_count
        print(f"  {coll.name}: {min(start + BATCH, len(ops))}/{len(ops)} written", end="\r", flush=True)
    print()
    return matched, modified


def needs_embedding(col, with_text: bool = False) -> dict:
    """{_id: (embeddingTextHash, embeddingText or None)} for documents with a text that need a
    vector: none yet, or one this tool wrote (it has `embeddingMeta.textHash`) for an older text."""
    project = {"hash": "$embeddingTextHash", "embedded": "$embeddingMeta.textHash",
               "has": {"$ne": [{"$type": "$embedding"}, "missing"]}}
    if with_text:
        project["text"] = "$embeddingText"
    out = {}
    for d in col.aggregate([{"$match": {"embeddingTextHash": {"$type": "string"}}}, {"$project": project}]):
        if not d["has"] or (d.get("embedded") is not None and d["embedded"] != d["hash"]):
            out[d["_id"]] = (d["hash"], d.get("text"))
    return out


def embedding_filter(_id, h: str) -> dict:
    return {"_id": _id, "embeddingTextHash": h,
            "$or": [{"embedding": {"$exists": False}}, {"embeddingMeta.textHash": {"$exists": True, "$ne": h}}]}


# ---- 1. export --------------------------------------------------------------------------------

def cmd_export(a) -> None:
    db = connect(a.uri, a.db)
    template = read_template(a)
    prompt_id = f"{at.PROMPT_NAME}@{at.template_hash(template)}"
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    run = a.run or "backfill-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")

    used = Counter(d["embeddingTextMeta"].get("prompt") for d in db[a.text_collection].find(
        {"embeddingTextMeta": {"$exists": True}}, {"embeddingTextMeta.prompt": 1}))
    if used and prompt_id not in used:
        print(f"warning: existing texts were written with {dict(used)}, not {prompt_id}", file=sys.stderr)

    groups: dict[str, dict] = {}  # input hash -> prompt and the documents sharing it
    for doc in db[a.text_collection].find({"embeddingText": {"$exists": False}}, {"embedding": 0, "trail.geometry": 0}):
        data = at.backfill_input(doc)
        h = at.input_hash(template, data)
        row = groups.setdefault(h, {"input_hash": h, "prompt": template.replace("{{EVENT_DATA}}", data),
                                    "kind": doc.get("kind"), "name": doc.get("name"),
                                    "opening_hours": "\nOpening hours: " in data, "ids": []})
        row["ids"].append(doc["_id"])
    rows = sorted(groups.values(), key=lambda r: r["input_hash"])
    n_docs = sum(len(r["ids"]) for r in rows)
    write_jsonl(out / "texts_todo.jsonl", rows)

    texts: dict[str, str] = {}
    embed_docs, mismatched = Counter(), []
    for name in a.embed_collections:
        for _id, (stored, text) in needs_embedding(db[name], with_text=True).items():
            h = at.text_hash(text or "")
            if not text or stored != h:  # the pipeline always writes both; skip if they disagree
                mismatched.append(f"{name}/{_id}")
                continue
            texts.setdefault(h, text)
            embed_docs[name] += 1
    write_jsonl(out / "embed_todo.jsonl", ({"text_hash": h, "text": t} for h, t in sorted(texts.items())))

    summary = {
        "run": run, "exported_at": datetime.now(timezone.utc).isoformat(timespec="seconds"), "db": a.db,
        "prompt": prompt_id, "template_hash": at.template_hash(template),
        "text_collection": a.text_collection, "embed_collections": a.embed_collections,
        "texts_todo": {"inputs": len(rows), "documents": n_docs, "by_kind": dict(Counter(r["kind"] for r in rows)),
                       "with_opening_hours": sum(r["opening_hours"] for r in rows)},
        "embed_todo": {"texts": len(texts), "documents": dict(embed_docs), "hash_mismatch_skipped": mismatched},
    }
    (out / "export.json").write_text(json.dumps(summary, indent=2))
    print(json.dumps(summary, indent=2))


# ---- 3. apply ---------------------------------------------------------------------------------

def load_results(results: Path):
    texts = {}
    for f in sorted(results.glob("texts-*.jsonl")):
        for row in map(json.loads, f.open()):
            texts[row["input_hash"]] = row
    emb = results / "embeddings"
    hashes = json.loads((emb / "hashes.json").read_text())
    vectors = np.load(emb / "vectors.npy")
    meta = json.loads((emb / "meta.json").read_text())
    if len(hashes) != len(vectors) or vectors.shape[1] != meta["dimension"]:
        raise SystemExit(f"embeddings don't line up: {len(hashes)} hashes, vectors {vectors.shape}")
    return texts, {h: i for i, h in enumerate(hashes)}, vectors, meta


def unit(vector: np.ndarray) -> list[float]:
    """float64 and exactly unit norm (bf16 normalization on the GPU leaves norms ~1e-3 off)."""
    v = vector.astype(np.float64)
    return (v / np.linalg.norm(v)).tolist()


def cmd_apply(a) -> None:
    from pymongo import UpdateOne

    db = connect(a.uri, a.db)
    export_dir, results = Path(a.export), Path(a.results)
    exp = json.loads((export_dir / "export.json").read_text())
    run = exp["run"]
    texts, index, vectors, emeta = load_results(results)
    print(f"run {run}: {len(texts)} generated texts, {len(index)} vectors ({emeta['model']}, {emeta['dimension']}-d)"
          + (" [dry run]" if a.dry_run else ""))

    # 1. texts, only where none exists yet
    col = db[exp["text_collection"]]
    ops, planned, stats = [], {}, Counter()
    for row in read_jsonl(export_dir / "texts_todo.jsonl"):
        gen = texts.get(row["input_hash"])
        if gen is None:
            stats["inputs without a generated text"] += 1
            continue
        reason = at.check(gen["text"])
        if reason is not None or at.text_hash(gen["text"]) != gen["text_hash"]:
            stats["texts failing the checks here"] += 1
            continue
        meta = {"model": gen["model"], "prompt": exp["prompt"], "grounded": False, "sources": [],
                "inputHash": row["input_hash"], "generatedAt": datetime.fromisoformat(gen["generated_at"]),
                "backfill": run}
        if row["opening_hours"]:
            meta["inputExtras"] = ["openingHours"]
        for _id in row["ids"][:a.limit - len(ops) if a.limit else None]:
            planned[_id] = gen["text_hash"]
            ops.append(UpdateOne({"_id": _id, "embeddingText": {"$exists": False}},
                                 {"$set": {"embeddingText": gen["text"], "embeddingTextHash": gen["text_hash"],
                                           "embeddingTextMeta": meta}}))
    if a.dry_run:
        ids = list(planned)
        still_missing = sum(col.count_documents({"_id": {"$in": ids[i:i + 1000]}, "embeddingText": {"$exists": False}})
                            for i in range(0, len(ids), 1000))
        print(f"texts: would write {still_missing} of {len(ops)} planned ({len(ops) - still_missing} gained a text meanwhile)")
    else:
        matched, modified = bulk(col, ops, False)
        print(f"texts: {modified} written of {len(ops)} planned ({len(ops) - matched} gained a text meanwhile)")
    for k, v in stats.items():
        print(f"  {k}: {v}")

    # 2. embeddings, only where none exists and the text is still the one embedded
    emb_meta = {"model": emeta["model"], "dimension": emeta["dimension"], "normalized": True, "prompt": None,
                "maxSeqLength": emeta["max_seq_length"], "generatedAt": datetime.fromisoformat(emeta["generated_at"]),
                "backfill": run}
    names = list(dict.fromkeys([exp["text_collection"], *exp["embed_collections"]]))
    for name in names:
        col = db[name]
        docs = {_id: h for _id, (h, _) in needs_embedding(col).items()}
        if a.dry_run and name == exp["text_collection"]:
            docs.update({i: h for i, h in planned.items() if i not in docs})  # texts step 1 would add
        ops, no_vector = [], 0
        if a.limit:  # canary: the documents whose text was just written first, then others
            docs = dict(sorted(docs.items(), key=lambda kv: kv[0] not in planned)[:a.limit])
        for _id, h in docs.items():
            if h not in index:
                no_vector += 1
                continue
            ops.append(UpdateOne(embedding_filter(_id, h),
                                 {"$set": {"embedding": unit(vectors[index[h]]), "embeddingModel": emeta["model"],
                                           "embeddingMeta": {**emb_meta, "textHash": h}}}))
        if a.dry_run:
            print(f"embeddings/{name}: would write {len(ops)}; {no_vector} documents without a vector")
        else:
            matched, modified = bulk(col, ops, False)
            print(f"embeddings/{name}: {modified} written of {len(ops)} planned; {no_vector} documents without a vector")

    if not a.dry_run:
        log = results / f"applied-{run}.json"
        log.write_text(json.dumps({"run": run, "applied_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
                                   "rollback": f"python ml/datagen/mongo_backfill.py rollback --run {run} --yes"}, indent=2))


# ---- verify / rollback ------------------------------------------------------------------------

def cmd_verify(a) -> None:
    db = connect(a.uri, a.db)
    for name in a.collections:
        col = db[name]
        total = col.count_documents({})
        print(f"\n{name}: {total} documents")
        for kind in sorted(col.distinct("kind")):
            q = {"kind": kind}
            print(f"  {kind:6} {col.count_documents(q):>6}  text {col.count_documents({**q, 'embeddingText': {'$exists': True}}):>6}"
                  f"  embedding {col.count_documents({**q, 'embedding': {'$exists': True}}):>6}")
        stale = col.count_documents({"embedding": {"$exists": True}, "embeddingMeta.textHash": {"$exists": True},
                                     "$expr": {"$ne": ["$embeddingMeta.textHash", "$embeddingTextHash"]}})
        print(f"  embeddings whose text changed since: {stale}")
        print(f"  embedding models: {dict(Counter(d.get('embeddingModel') for d in col.find({'embedding': {'$exists': True}}, {'embeddingModel': 1})))}")
        print(f"  backfill runs: texts {dict(Counter(d['embeddingTextMeta'].get('backfill') for d in col.find({'embeddingTextMeta': {'$exists': True}}, {'embeddingTextMeta.backfill': 1})))}")
        sample = list(col.aggregate([{"$match": {"embedding": {"$exists": True}}}, {"$sample": {"size": a.sample}},
                                     {"$project": {"embedding": 1}}]))
        if sample:
            m = np.array([d["embedding"] for d in sample], dtype=np.float64)
            norms = np.linalg.norm(m, axis=1)
            print(f"  sample of {len(sample)}: dims {sorted({len(d['embedding']) for d in sample})}, "
                  f"norm {norms.min():.8f}-{norms.max():.8f}, finite {bool(np.isfinite(m).all())}, "
                  f"mean pairwise cosine {float((m @ m.T)[np.triu_indices(len(m), 1)].mean()):.3f}")


def cmd_rollback(a) -> None:
    db = connect(a.uri, a.db)
    for name in a.collections:
        col = db[name]
        for fields, marker in ((EMBED_FIELDS, "embeddingMeta.backfill"), (TEXT_FIELDS, "embeddingTextMeta.backfill")):
            q = {marker: a.run}
            n = col.count_documents(q)
            if a.yes and n:
                res = col.update_many(q, {"$unset": {f: "" for f in fields}})
                print(f"{name}: removed {', '.join(fields)} from {res.modified_count} documents")
            else:
                print(f"{name}: {n} documents carry {marker} = {a.run}" + ("" if a.yes else " (pass --yes to remove)"))


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--uri", default="mongodb://127.0.0.1:27017/?directConnection=true")
    ap.add_argument("--db", default="freetime")
    sub = ap.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("export", help="write texts_todo.jsonl, embed_todo.jsonl and export.json")
    p.add_argument("--out", required=True)
    p.add_argument("--run", help="run id recorded on every write (default: backfill-<UTC time>)")
    p.add_argument("--text-collection", default="activities")
    p.add_argument("--embed-collections", nargs="+", default=["activities"])
    p.add_argument("--template", help="prompt template file (default: read from --template-ref)")
    p.add_argument("--template-ref", default="origin/dataingenstion")

    p = sub.add_parser("apply", help="write the generated texts and embeddings")
    p.add_argument("--export", required=True)
    p.add_argument("--results", required=True, help="the Raven run directory, fetched")
    p.add_argument("--dry-run", action="store_true")
    p.add_argument("--limit", type=int, default=0, help="at most N text and N embedding writes per collection (a canary)")

    p = sub.add_parser("verify", help="coverage, dimensions and norms")
    p.add_argument("--collections", nargs="+", default=["activities"])
    p.add_argument("--sample", type=int, default=200)

    p = sub.add_parser("rollback", help="remove what one run added")
    p.add_argument("--run", required=True)
    p.add_argument("--collections", nargs="+", default=["activities"])
    p.add_argument("--yes", action="store_true")

    a = ap.parse_args()
    {"export": cmd_export, "apply": cmd_apply, "verify": cmd_verify, "rollback": cmd_rollback}[a.cmd](a)


if __name__ == "__main__":
    main()
