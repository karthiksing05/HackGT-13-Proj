"""Embed the text columns of the event and user datasets, for classifier training.

Every row of every split is embedded with one frozen text-embedding model and
written as float32 .npy matrices (L2-normalized), one file per text column:

    <out>/meta.json                       model, dimension, row counts
    <out>/events/<split>/ids.json         row ids, in matrix order
    <out>/events/<split>/embedding_text.npy
    <out>/users/<split>/ids.json
    <out>/users/<split>/positive_text.npy
    <out>/users/<split>/negative_text.npy
    <out>/users/<split>/negative_text_present.npy   (only if some texts are empty)

Empty or missing texts (e.g. a user with no dislikes) get a zero vector and are
marked False in `<column>_present.npy`. Finished splits are skipped on re-runs,
so a job that hits its walltime can simply be resubmitted.

    python -m data.embed --out runs/emb --users-repo owner/users-repo
    python -m data.embed --out runs/emb --users-repo owner/repo --users-config users   # users as a config
    python -m data.embed --out runs/emb-test --limit 200   # quick check
"""

import argparse
import json
import logging
import time
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

from .hf_dataset import DEFAULT_REPO, load_events

logger = logging.getLogger(__name__)

DEFAULT_MODEL = "Qwen/Qwen3-Embedding-0.6B"


def load_model(model_name: str, device: str | None = None, max_seq_length: int = 512):
    import torch
    from sentence_transformers import SentenceTransformer

    kwargs = {"torch_dtype": torch.bfloat16} if torch.cuda.is_available() else {}
    model = SentenceTransformer(model_name, device=device, model_kwargs=kwargs)
    model.max_seq_length = max_seq_length  # our texts are ~100 tokens; avoids huge padded batches
    return model


def embedding_dim(model) -> int:
    # Renamed in sentence-transformers 6; the old name still works but warns.
    return getattr(model, "get_embedding_dimension", model.get_sentence_embedding_dimension)()


def embed_column(model, texts: list[str | None], batch_size: int) -> tuple[np.ndarray, np.ndarray]:
    """Return (N, D) float32 unit vectors and an (N,) mask of non-empty texts."""
    present = np.array([bool(t and t.strip()) for t in texts])
    out = np.zeros((len(texts), embedding_dim(model)), dtype=np.float32)
    idx = np.flatnonzero(present)
    if idx.size:
        out[idx] = model.encode(
            [texts[i] for i in idx],
            batch_size=batch_size,
            normalize_embeddings=True,
            convert_to_numpy=True,
            show_progress_bar=True,
        ).astype(np.float32)
    return out, present


def embed_split(model, ds, columns: list[str], split_dir: Path, batch_size: int, id_column: str = "id") -> int:
    missing = [c for c in columns if c not in ds.column_names]
    if missing:
        raise KeyError(f"columns {missing} not in dataset; available: {ds.column_names}")

    split_dir.mkdir(parents=True, exist_ok=True)
    ids = ds[id_column] if id_column in ds.column_names else [str(i) for i in range(ds.num_rows)]
    for col in columns:
        start = time.time()
        vectors, present = embed_column(model, ds[col], batch_size)
        np.save(split_dir / f"{col}.npy", vectors)
        if not present.all():
            np.save(split_dir / f"{col}_present.npy", present)
            logger.warning("%s/%s: %d of %d texts empty (zero vectors)", split_dir, col, (~present).sum(), len(present))
        logger.info("%s/%s: %s in %.0fs", split_dir, col, vectors.shape, time.time() - start)
    # Written last: its presence marks the split as complete.
    (split_dir / "ids.json").write_text(json.dumps(list(ids)))
    return ds.num_rows


def load_embeddings(out_dir: str | Path, name: str, split: str, mmap: bool = True) -> tuple[list[str], dict[str, np.ndarray]]:
    """Read back one split: (ids, {column: (N, D) array, column_present: (N,) bool})."""
    split_dir = Path(out_dir) / name / split
    ids = json.loads((split_dir / "ids.json").read_text())
    arrays = {p.stem: np.load(p, mmap_mode="r" if mmap else None) for p in sorted(split_dir.glob("*.npy"))}
    return ids, arrays


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--model", default=DEFAULT_MODEL)
    parser.add_argument("--batch-size", type=int, default=256)
    parser.add_argument("--max-seq-length", type=int, default=512)
    parser.add_argument("--events-repo", default=DEFAULT_REPO)
    parser.add_argument("--event-columns", nargs="+", default=["embedding_text"])
    parser.add_argument("--users-repo", default=None, help="skipped when not given")
    parser.add_argument("--users-config", default=None, help='config of --users-repo, e.g. "users"')
    parser.add_argument("--user-columns", nargs="+", default=["positive_text", "negative_text"])
    parser.add_argument("--limit", type=int, default=None, help="only the first N rows per split (testing)")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

    jobs = [("events", args.events_repo, None, args.event_columns)]
    if args.users_repo:
        jobs.append(("users", args.users_repo, args.users_config, args.user_columns))

    model = load_model(args.model, max_seq_length=args.max_seq_length)
    dim = embedding_dim(model)
    logger.info("model %s, dim %d, device %s", args.model, dim, model.device)

    meta_path = args.out / "meta.json"
    meta = json.loads(meta_path.read_text()) if meta_path.exists() else {}
    if meta and meta.get("model") != args.model:
        raise SystemExit(f"{args.out} holds embeddings from {meta['model']}; use a new --out for {args.model}")
    meta.update(model=args.model, dimension=dim, normalized=True, dtype="float32", max_seq_length=args.max_seq_length)
    meta.setdefault("datasets", {})

    for name, repo, config, columns in jobs:
        for split, ds in load_events(repo, name=config).items():
            split_dir = args.out / name / split
            if (split_dir / "ids.json").exists():
                logger.info("%s already done, skipping", split_dir)
                continue
            if args.limit:
                ds = ds.select(range(min(args.limit, ds.num_rows)))
            rows = embed_split(model, ds, columns, split_dir, args.batch_size)
            entry = meta["datasets"].setdefault(name, {"repo": repo, "config": config, "columns": columns, "rows": {}})
            entry["rows"][split] = rows
            meta["updated"] = datetime.now(timezone.utc).isoformat(timespec="seconds")
            meta_path.write_text(json.dumps(meta, indent=2))

    logger.info("done: %s", json.dumps(meta["datasets"]))


if __name__ == "__main__":
    main()
