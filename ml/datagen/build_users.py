#!/usr/bin/env python3
"""Assemble the `users` config from a users.sbatch run, check its splits, and optionally push it.

    python ml/datagen/build_users.py RUN_DIR --repo-id USER/NAME [--push]

RUN_DIR holds users.jsonl, tasks-*.jsonl, ratings-*.jsonl and the stats files (fetched with
`mpcdf.py fetch`). One row per user, with the rated candidate events as a list. The split
rules from ml/training.md are asserted, not assumed: users are split by persona, training
candidates come only from the train event pool, and no event is shared between the train,
validation and test pools. The card (ml/dataset.md) gets a users section between markers and a
`users` entry in its YAML configs; --push uploads the config (users/*.parquet) and the card.
"""
from __future__ import annotations

import argparse
import glob
import json
import re
import statistics
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import generate as gen  # noqa: E402
import judge  # noqa: E402

CARD = gen.REPO / "ml" / "dataset.md"
START, END = "<!-- users:start -->", "<!-- users:end -->"
SPLITS = ("train", "validation", "test")
YAML_USERS = """- config_name: users
  data_files:
  - split: train
    path: users/train-*
  - split: validation
    path: users/validation-*
  - split: test
    path: users/test-*
"""


def jsonl(pattern: str) -> list[dict]:
    return [json.loads(line) for path in sorted(glob.glob(pattern)) for line in open(path)]


def load(run_dir: Path) -> tuple[dict[str, list[dict]], dict]:
    users = {u["id"]: u for u in jsonl(str(run_dir / "users.jsonl"))}
    tasks = {(t["user_id"], t["event_id"]): t for t in jsonl(str(run_dir / "tasks-*.jsonl"))}
    ratings = jsonl(str(run_dir / "ratings-*.jsonl"))
    by_user: dict[str, list[dict]] = defaultdict(list)
    for r in ratings:
        t = tasks[(r["user_id"], r["event_id"])]
        by_user[r["user_id"]].append({
            "event_id": r["event_id"], "event_split": t["event_split"], "pool": t["pool"], "source": t["source"],
            "rating": r["rating"], "label": r["label"], "reason": r["reason"],
            "cos_positive": t["cos_positive"], "cos_negative": t["cos_negative"]})
    splits: dict[str, list[dict]] = {s: [] for s in SPLITS}
    for uid, user in users.items():
        cands = sorted(by_user.get(uid, []), key=lambda c: c["event_id"])
        if len(cands) < 2:
            continue  # ranking metrics need at least two candidates per user
        splits[user["split"]].append({
            "id": uid, "positive_text": user["positive_text"], "negative_text": user["negative_text"],
            "positive_sections": user["positive_sections"], "negative_sections": user["negative_sections"],
            "persona": json.dumps(user["persona"], ensure_ascii=False), "candidates": cands})
    stats = {"tasks": len(tasks), "ratings": len(ratings),
             "selection": json.loads((run_dir / "selection.json").read_text()),
             "users_gen": [json.loads(Path(p).read_text()) for p in sorted(glob.glob(str(run_dir / "users-stats-*.json")))],
             "judge": [json.loads(Path(p).read_text()) for p in sorted(glob.glob(str(run_dir / "judge-stats-*.json")))]}
    return splits, stats


def check_splits(splits: dict[str, list[dict]]) -> dict[str, set]:
    """training.md: split by user; no test events in training; pools disjoint."""
    ids = {s: {u["id"] for u in rows} for s, rows in splits.items()}
    assert not (ids["train"] & ids["validation"] or ids["train"] & ids["test"] or ids["validation"] & ids["test"])
    events = {s: {c["event_id"] for u in rows for c in u["candidates"]} for s, rows in splits.items()}
    expected = {"train": ("train", "train"), "validation": ("train", "validation"), "test": ("test", "test")}
    for split, rows in splits.items():
        for u in rows:
            for c in u["candidates"]:
                assert (c["event_split"], c["pool"]) == expected[split], (split, c)
    assert not (events["train"] & events["validation"]), "validation events leak into training"
    assert not (events["train"] & events["test"]), "test events leak into training"
    return events


def pct(a: int, b: int) -> str:
    return f"{100 * a / b:.1f}%" if b else "-"


def users_section(repo_id: str, splits: dict[str, list[dict]], events: dict[str, set], stats: dict) -> str:
    rows = [u for s in SPLITS for u in splits[s]]
    n = len(rows)
    cands = [c for u in rows for c in u["candidates"]]
    ratings = Counter(c["rating"] for c in cands)
    by_source = defaultdict(Counter)
    for c in cands:
        by_source[c["source"]][c["rating"]] += 1
    no_dislikes = sum(1 for u in rows if not u["negative_text"])
    per_user = statistics.mean(len(u["candidates"]) for u in rows)
    relevant = statistics.mean(sum(c["label"] >= 0.5 for c in u["candidates"]) for u in rows)
    gen_attempted = sum(s["attempted"] for s in stats["users_gen"])
    gen_valid = sum(s["valid"] for s in stats["users_gen"])
    gen_rejects = Counter()
    for s in stats["users_gen"]:
        gen_rejects.update(s["rejects"])
    failures = sum(sum(s["failures"].values()) for s in stats["judge"])
    pools = stats["selection"]["event_pools"]
    ex = next((u for u in splits["test"] if u["negative_text"]), splits["test"][0])
    ex_c = sorted(ex["candidates"], key=lambda c: -c["rating"])
    ex_c = [ex_c[0], ex_c[len(ex_c) // 2], ex_c[-1]]

    def rating_row(name: str, counter: Counter) -> str:
        total = sum(counter.values())
        return f"| {name} | {total:,} | " + " | ".join(pct(counter[r], total) for r in range(4)) + " |"

    source_rows = "\n".join(rating_row(f"`{s}`", by_source[s]) for s in ("retrieved", "hard_negative", "random"))
    split_rows = "\n".join(f"| `{s}` | {len(splits[s]):,} | {sum(len(u['candidates']) for u in splits[s]):,} | "
                           f"{len(events[s]):,} |" for s in SPLITS)
    reject_list = ", ".join(f"`{k}` {v:,}" for k, v in gen_rejects.most_common(6)) or "none"
    cand_lines = "\n".join(f"- rating {c['rating']} ({c['source']}): {c['reason']}" for c in ex_c)
    return f"""{START}
## `users` config

```python
users = load_dataset("{repo_id}", "users")
```

{n:,} synthetic users for training and evaluating compatibility models (see `ml/training.md`).
Each user has preference texts in the same eight-section format as the events and
{per_user:.0f} candidate events rated by an LLM judge. The split is by user, so no user appears
in two splits; the event pools behind the splits are disjoint as well.

| Split | Users | Rated pairs | Distinct events |
|---|---|---|---|
{split_rows}

### Fields

| Field | Type | Meaning |
|---|---|---|
| `id` | string | user id (`u00000`...) |
| `positive_text` | string | what the user wants, in the eight-section format |
| `negative_text` | string | what the user avoids; empty for the {pct(no_dislikes, n)} of users with no dislikes |
| `positive_sections`, `negative_sections` | struct of 8 lists | the same bullets, parsed |
| `persona` | string (JSON) | the random brief the texts were written from |
| `candidates` | list of structs | rated events: `event_id` (joins the default config's `id`), `event_split`, `pool`, `source`, `rating` (0-3), `label` (rating / 3), `reason` (the judge's one-line rationale), `cos_positive`, `cos_negative` |

### How it was made

1. **Persona briefs (code).** Interests are drawn from the event taxonomy (1-3 categories).
   Each brief also has 1-3 dislikes (none for about 15%), plus optional social style,
   budget, availability, setting, energy, experience, life context and a detail level.
2. **Preference texts (model).** `Qwen/Qwen3.5-9B` writes `positive_text` and
   `negative_text` from the brief in JSON-constrained decoding. The prompt turns life
   context into preferences and forbids demographics. Both texts pass the same validator as
   the event texts, plus a demographic-terms check. {gen_valid:,} of {gen_attempted:,} attempts
   passed; the top reject reasons were {reject_list}.
3. **Split and candidates (code and frozen encoder).** Users are split 80/10/10 with seed
   {stats['selection']['split_seed']}. Each split has its own event pool, so there's no leakage:
   - train users use the events config's train split, minus {pools['validation']:,} held-out events ({pools['train']:,} events);
   - validation users use those held-out events;
   - test users use the events config's test split ({pools['test']:,} events).

   Candidates come from the pool, ranked with the classifier's frozen encoder
   (`Qwen/Qwen3-Embedding-0.6B`, cosine to `positive_text`):
   - 8 retrieved, sampled from the top 40;
   - 4 hard negatives: the most dislike-like of the top 300, or ranks 40-300 for users with
     no dislikes;
   - 8 random.
4. **Ratings (model).** `Qwen/Qwen3.5-9B` sees exactly what the classifier sees (both
   preference texts and the event's embedding text). It gives a one-sentence reason, then a
   0-3 rating: 3 = strong match, 2 = good, 1 = weak or conflicting, 0 = poor or hits a
   dislike. Missing event details count as neutral. Sampling:
   {', '.join(f'{k}={v}' for k, v in judge.SAMPLING.items())}. {failures:,} pairs were
   unparseable twice and dropped.

### Statistics

{relevant:.1f} of each user's {per_user:.0f} candidates are relevant on average (`label` ≥ 0.5, i.e. rating ≥ 2).

| Candidate source | Pairs | Rating 0 | Rating 1 | Rating 2 | Rating 3 |
|---|---|---|---|---|---|
{source_rows}
{rating_row("**all**", ratings)}

### Example (from `test`)

`positive_text`:
```text
{ex['positive_text']}
```

`negative_text`:
```text
{ex['negative_text']}
```

Three of its candidates:
{cand_lines}

### Limitations

- **Labels are LLM judgments, not behavior.** They encode one 9B model's reading of
  compatibility, from texts written by the same model family. They're useful for bootstrapping
  the learned models, but no substitute for real interaction labels.
- **Retrieval bias.** Retrieved and hard-negative candidates were chosen with the same frozen
  encoder the classifier uses, so candidates skew toward what cosine similarity already finds
  plausible. Random candidates are there to temper this.
- **Synthetic users.** Personas combine attributes at random, so some are unusual.
{END}"""


def update_card(section: str) -> str:
    card = CARD.read_text()
    if START in card:
        card = re.sub(re.escape(START) + ".*?" + re.escape(END), lambda _: section, card, flags=re.S)
    else:
        card = card.replace("\n## Limitations", f"\n{section}\n\n## Limitations", 1)
    if "config_name: users" not in card:
        card = card.replace("\n---\n", "\n" + YAML_USERS + "---\n", 1)
    card = card.replace(
        "- **No preference data.** There are no user preferences or user-event interaction labels, so\n"
        "  this doesn't replace behavioral data for the learned compatibility models (approaches B\n"
        "  and C).",
        "- **No behavioral data.** The `users` config adds synthetic preferences and LLM-judged\n"
        "  labels, not real interactions, so it doesn't replace behavioral data for the learned\n"
        "  compatibility models (approaches B and C).")
    CARD.write_text(card)
    return card


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("run_dir", type=Path)
    ap.add_argument("--repo-id", required=True)
    ap.add_argument("--push", action="store_true")
    a = ap.parse_args()

    splits, stats = load(a.run_dir)
    events = check_splits(splits)
    print({s: len(rows) for s, rows in splits.items()}, "users;",
          sum(len(u["candidates"]) for rows in splits.values() for u in rows), "rated pairs; split checks passed")

    from datasets import Dataset, DatasetDict, Features, Sequence, Value
    sections = {s.lower(): Sequence(Value("string")) for s in gen.SECTIONS}
    features = Features({
        "id": Value("string"), "positive_text": Value("string"), "negative_text": Value("string"),
        "positive_sections": sections, "negative_sections": sections, "persona": Value("string"),
        "candidates": [{"event_id": Value("string"), "event_split": Value("string"), "pool": Value("string"),
                        "source": Value("string"), "rating": Value("int8"), "label": Value("float32"),
                        "reason": Value("string"), "cos_positive": Value("float32"),
                        "cos_negative": Value("float32")}],
    })
    dsd = DatasetDict({s: Dataset.from_list(rows, features=features) for s, rows in splits.items()})
    out = a.run_dir / "dataset"
    out.mkdir(exist_ok=True)
    for split, ds in dsd.items():
        ds.to_parquet(out / f"users-{split}.parquet")

    card = update_card(users_section(a.repo_id, splits, events, stats))
    print(f"card updated: {CARD}")
    if a.push:
        from huggingface_hub import HfApi
        dsd.push_to_hub(a.repo_id, config_name="users", private=True,
                        commit_message="Add users config: synthetic users with LLM-rated candidate events")
        HfApi().upload_file(path_or_fileobj=card.encode(), path_in_repo="README.md", repo_id=a.repo_id,
                            repo_type="dataset", commit_message="Document the users config")
        print(f"pushed config 'users' to https://huggingface.co/datasets/{a.repo_id}")


if __name__ == "__main__":
    main()
