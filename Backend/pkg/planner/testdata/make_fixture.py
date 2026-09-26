#!/usr/bin/env python3
"""Build saltlight_2026-09-26.json from the demo catalog.

The demo file (dataingestion/demo/saltlight_harbor.json) is MongoDB extended
JSON ({"$oid": …}, {"$date": …}); Go's models.Activity decodes plain JSON, so
ids become hex strings and dates RFC 3339. Long text fields the planner never
reads (description, sources, embedding text) are dropped to keep the fixture
small. Vectors are not stored: the tests derive deterministic ones from each
document's category and tags (see helpers_test.go).

Run from the repo root:

    python3 Backend/pkg/planner/testdata/make_fixture.py
"""
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[4]
SRC = ROOT / "dataingestion" / "demo" / "saltlight_harbor.json"
DST = pathlib.Path(__file__).resolve().parent / "saltlight_2026-09-26.json"

DROP = {"description", "sources", "sourceKeys", "embeddingText", "embeddingTextHash",
        "embeddingTextMeta", "embedding", "embeddingMeta", "embeddingModel", "recurrence"}


def plain(value):
    if isinstance(value, dict):
        if set(value) == {"$oid"}:
            return value["$oid"]
        if set(value) == {"$date"}:
            return value["$date"]
        return {k: plain(v) for k, v in value.items() if k not in DROP}
    if isinstance(value, list):
        return [plain(v) for v in value]
    return value


def main():
    docs = json.loads(SRC.read_text())
    out = [plain(d) for d in docs]
    DST.write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
    print(f"wrote {len(out)} activities to {DST}")


if __name__ == "__main__":
    main()
