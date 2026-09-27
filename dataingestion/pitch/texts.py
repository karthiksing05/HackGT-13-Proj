"""Embedding texts for the pitch catalog: the pipeline's embed-text prompt, written by Claude.

    cd dataingestion
    .venv/bin/python -m pitch.texts inputs [--batches 5]     # out/text_batches/batch_NN.json
    .venv/bin/python -m pitch.texts check <batch_NN.out.json>
    .venv/bin/python -m pitch.texts import                   # out/embedding_texts.json

Each document's input is exactly what `ingest.agent.embed_text` would send: its
`event_data(doc, research=None)` in place of {{EVENT_DATA}} in prompts/event_embedding_text.md.
There is no web research (the listings are synthetic, so nothing can be grounded; like
demo_activities, `grounded` is false). The pipeline's Gemini write step is replaced by Claude
following the same prompt; `embeddingTextMeta.model` records the model that wrote each text.
Occurrences of a weekly series have the same input (weekday and time, no date) and share one
text, as the pipeline's series do. A text is accepted only if it passes the pipeline's own
`check()` and the format rules of docs/EMBEDDINGS.md (fixed section order, lowercase bullets of at
most eight words, no duplicates).

A batch output file is JSON: {"model": "<exact model id>", "texts": {"<inputHash>": "<text>"}}.
"""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

from ingest.agent.embed_text import SECTIONS, check, clean

from .generate import OUT, TEXT_PROMPT, TEXTS_CACHE, build, text_input

BATCH_DIR = OUT / "text_batches"
MAX_WORDS = 8


def strict(text: str) -> str | None:
    """check() plus the documented format: sections in order, lowercase short bullets, no repeats."""
    reason = check(text)
    if reason:
        return reason
    order, seen = [], set()
    for line in text.splitlines():
        s = line.strip()
        if not s:
            continue
        if not s.startswith("- "):
            order.append(s[:-1])
            continue
        item = s[2:].strip()
        if item != item.lower():
            return f"bullet '{item}' is not lowercase"
        if len(item.split()) > MAX_WORDS:
            return f"bullet '{item}' has more than {MAX_WORDS} words"
        if item in seen:
            return f"bullet '{item}' appears twice"
        if any(ch.isdigit() for ch in item) and any(m in item for m in ("jan", "feb", "sep", "oct", "nov", "2026")):
            return f"bullet '{item}' looks like a calendar date"
        seen.add(item)
    if order != [s for s in SECTIONS if s in order]:
        return f"sections out of order: {order} (expected the order {SECTIONS})"
    if "\n\n" not in text and len(order) > 1:
        return "sections must be separated by a blank line"
    return None


def unique_inputs() -> list[dict]:
    docs, _ = build(with_texts=False)
    by_hash: dict[str, dict] = {}
    for d in docs:
        data, h = text_input(d)
        row = by_hash.setdefault(h, {"inputHash": h, "sourceKeys": [], "eventData": data})
        row["sourceKeys"].append(d["sourceKeys"][0])
    return list(by_hash.values())


def cmd_inputs(args) -> None:
    rows = unique_inputs()
    BATCH_DIR.mkdir(parents=True, exist_ok=True)
    n = args.batches
    for i in range(n):
        part = rows[i::n]
        path = BATCH_DIR / f"batch_{i + 1:02d}.json"
        path.write_text(json.dumps({"prompt": TEXT_PROMPT, "items": part}, indent=1, ensure_ascii=False) + "\n")
        print(f"{path}: {len(part)} inputs")
    print(f"{len(rows)} unique inputs for {sum(len(r['sourceKeys']) for r in rows)} documents")


def validate_file(path: Path) -> tuple[dict, list[str]]:
    body = json.loads(path.read_text())
    batch = json.loads((path.parent / path.name.replace(".out.json", ".json")).read_text())
    wanted = {r["inputHash"] for r in batch["items"]}
    errors = []
    if not isinstance(body.get("model"), str) or not body["model"].strip():
        errors.append("missing 'model' (the exact id of the model that wrote the texts)")
    texts = body.get("texts") or {}
    for h in sorted(wanted - set(texts)):
        errors.append(f"{h}: no text")
    for h in sorted(set(texts) - wanted):
        errors.append(f"{h}: not an input of this batch")
    for h, t in texts.items():
        reason = strict(clean(t)) if isinstance(t, str) else "not a string"
        if reason:
            errors.append(f"{h}: {reason}")
    return body, errors


def cmd_check(args) -> None:
    _, errors = validate_file(Path(args.file))
    if errors:
        print("\n".join(errors))
        sys.exit(1)
    print("ok")


def cmd_import(args) -> None:
    rows = {r["inputHash"]: r for r in unique_inputs()}
    cache = json.loads(TEXTS_CACHE.read_text()) if TEXTS_CACHE.exists() else {}
    imported, missing = 0, []
    for path in sorted(BATCH_DIR.glob("*.out.json")):
        body, errors = validate_file(path)
        if errors:
            sys.exit(f"{path.name}: {len(errors)} problem(s), first: {errors[0]}")
        written = datetime.fromtimestamp(path.stat().st_mtime, timezone.utc).isoformat()
        for h, text in body["texts"].items():
            if h not in rows:
                continue  # an input from an older build
            for key in rows[h]["sourceKeys"]:
                cache[key] = {"text": clean(text), "model": body["model"].strip(), "prompt": TEXT_PROMPT,
                              "inputHash": h, "generatedAt": written}
                imported += 1
    for r in rows.values():
        missing += [k for k in r["sourceKeys"] if k not in cache or cache[k]["inputHash"] != r["inputHash"]]
    TEXTS_CACHE.write_text(json.dumps(cache, indent=1, ensure_ascii=False) + "\n")
    print(f"imported {imported} document texts into {TEXTS_CACHE}; {len(missing)} documents still without a current text")


def cmd_pull(args) -> None:
    """Copy the texts already stored in the collection into the local cache (e.g. on another machine)."""
    from pymongo import MongoClient

    coll = MongoClient(args.uri, tz_aware=True, serverSelectionTimeoutMS=10000)["freetime"]["pitch_activities"]
    cache = json.loads(TEXTS_CACHE.read_text()) if TEXTS_CACHE.exists() else {}
    n = 0
    for d in coll.find({"embeddingText": {"$type": "string"}}, {"sourceKeys": 1, "embeddingText": 1, "embeddingTextMeta": 1}):
        meta = d["embeddingTextMeta"]
        cache[d["sourceKeys"][0]] = {"text": d["embeddingText"], "model": meta["model"], "prompt": meta["prompt"],
                                     "inputHash": meta["inputHash"], "generatedAt": meta["generatedAt"].isoformat()}
        n += 1
    TEXTS_CACHE.parent.mkdir(parents=True, exist_ok=True)
    TEXTS_CACHE.write_text(json.dumps(cache, indent=1, ensure_ascii=False) + "\n")
    print(f"pulled {n} stored texts into {TEXTS_CACHE}")


def cmd_pending(args) -> None:
    """Write the inputs that still have no current text into out/text_batches/pending_NN.json."""
    cache = json.loads(TEXTS_CACHE.read_text()) if TEXTS_CACHE.exists() else {}
    rows = [r for r in unique_inputs()
            if not all(k in cache and cache[k]["inputHash"] == r["inputHash"] for k in r["sourceKeys"])]
    BATCH_DIR.mkdir(parents=True, exist_ok=True)
    for old in BATCH_DIR.glob("pending_*.json"):
        if not old.name.endswith(".out.json"):
            old.unlink()
    n = max(1, args.batches)
    for i in range(n):
        part = rows[i::n]
        if part:
            path = BATCH_DIR / f"pending_{i + 1:02d}.json"
            path.write_text(json.dumps({"prompt": TEXT_PROMPT, "items": part}, indent=1, ensure_ascii=False) + "\n")
            print(f"{path}: {len(part)} inputs")
    print(f"{len(rows)} unique inputs still need a text")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("inputs")
    p.add_argument("--batches", type=int, default=5)
    p.set_defaults(fn=cmd_inputs)
    p = sub.add_parser("check")
    p.add_argument("file")
    p.set_defaults(fn=cmd_check)
    sub.add_parser("import").set_defaults(fn=cmd_import)
    p = sub.add_parser("pull")
    p.add_argument("--uri", default="mongodb://127.0.0.1:27017/?directConnection=true")
    p.set_defaults(fn=cmd_pull)
    p = sub.add_parser("pending")
    p.add_argument("--batches", type=int, default=1)
    p.set_defaults(fn=cmd_pending)
    args = ap.parse_args()
    args.fn(args)


if __name__ == "__main__":
    main()
