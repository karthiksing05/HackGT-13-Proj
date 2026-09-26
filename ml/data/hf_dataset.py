"""Load the synthetic event dataset from the Hugging Face Hub and inspect it.

The dataset is private, so a token is needed. Put it in the repo's .env file
(gitignored) as `HF_TOKEN=hf_...`, or export HF_TOKEN, or run `hf auth login`.

    python -m data.hf_dataset                    # summary of every split
    python -m data.hf_dataset --split test -n 3  # also print 3 example rows
    python -m data.hf_dataset --repo other/repo  # a different dataset
"""

import argparse
import os
from collections import Counter

import numpy as np
from datasets import DatasetDict, load_dataset
from dotenv import find_dotenv, load_dotenv

DEFAULT_REPO = "karthiksing05/sidequestz-event-embedding-text"
SECTIONS = ("Interests", "Activities", "Social", "Environment", "Pace", "Cost", "Timing", "Experience")


def load_events(
    repo_id: str = DEFAULT_REPO, split: str | None = None, revision: str | None = None, name: str | None = None
):
    """Return a DatasetDict (or one Dataset when `split` is given).

    `name` selects a config of the repo: None is the default (events), "users" the synthetic
    users with rated candidate events. The first call downloads into the HF cache
    (~/.cache/huggingface); later calls are served from there.
    """
    load_dotenv(find_dotenv(usecwd=True))
    token = os.environ.get("HF_TOKEN")  # None falls back to a `hf auth login` token
    # Name the default config explicitly: offline, with several configs cached, `datasets` refuses
    # to guess ("There are multiple ... configurations in the cache").
    return load_dataset(repo_id, name or "default", split=split, revision=revision, token=token)


def _length_stats(values: list[int]) -> str:
    a = np.asarray(values)
    p50, p95, p99 = np.percentile(a, [50, 95, 99])
    return f"min {a.min()}  p50 {p50:.0f}  p95 {p95:.0f}  p99 {p99:.0f}  max {a.max()}"


def inspect_split(ds, name: str, n_examples: int = 0) -> None:
    print(f"\n=== {name}: {ds.num_rows:,} rows, {ds.num_columns} columns ===")
    for col, feature in ds.features.items():
        print(f"  {col:<16} {feature}")

    # Empty / missing values per string column.
    for col, feature in ds.features.items():
        if getattr(feature, "dtype", None) != "string":
            continue
        empty = sum(1 for v in ds[col] if v is None or not v.strip())
        if empty:
            print(f"  ! {col}: {empty:,} empty or null values")

    if "raw_event" in ds.column_names:
        print(f"\n  raw_event chars:      {_length_stats([len(t) for t in ds['raw_event']])}")
    if "embedding_text" in ds.column_names:
        print(f"  embedding_text chars: {_length_stats([len(t) for t in ds['embedding_text']])}")

    if "sections" in ds.column_names:
        sections = ds["sections"]
        print("\n  section         rows with it   mean bullets (when present)")
        for s in SECTIONS:
            counts = [len(row.get(s.lower()) or []) for row in sections]  # stored with lowercase keys
            present = [c for c in counts if c]
            mean = np.mean(present) if present else 0.0
            print(f"  {s:<15} {len(present) / len(counts):>12.1%}   {mean:.2f}")

    for col in ("category", "completeness", "input_format"):
        if col in ds.column_names:
            top = Counter(ds[col]).most_common(5)
            print(f"\n  {col}: {len(set(ds[col]))} values; top: " + ", ".join(f"{k} ({v:,})" for k, v in top))

    for i in range(min(n_examples, ds.num_rows)):
        row = ds[i]
        print(f"\n--- example {i} ({row.get('category')}, {row.get('completeness')}) ---")
        print("raw_event:\n" + row.get("raw_event", ""))
        print("\nembedding_text:\n" + row.get("embedding_text", ""))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--repo", default=DEFAULT_REPO)
    parser.add_argument("--split", default=None, help="only this split (default: all)")
    parser.add_argument("--revision", default=None, help="branch, tag or commit to pin")
    parser.add_argument("-n", "--examples", type=int, default=0, help="example rows to print per split")
    args = parser.parse_args()

    data = load_events(args.repo, split=args.split, revision=args.revision)
    splits = data.items() if isinstance(data, DatasetDict) else [(args.split, data)]
    for name, ds in splits:
        inspect_split(ds, name, args.examples)


if __name__ == "__main__":
    main()
