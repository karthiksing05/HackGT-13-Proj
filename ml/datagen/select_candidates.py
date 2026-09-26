#!/usr/bin/env python3
"""Split users by persona and pick candidate events for each, ready for the judge.

- Users: the generated shards are merged, duplicates dropped, and `--users` of them split
  80 / 10 / 10 into train / validation / test with a fixed seed (training.md: split by user).
- Event pools keep splits apart (training.md: test events never appear in training):
  train users draw from the event train split minus a held-out validation pool; validation
  users draw from that pool; test users draw from the event test split.
- Candidates per user, from the user's pool, using the frozen encoder the classifier uses
  (Qwen3-Embedding-0.6B via ml/data/embed.py): `--retrieved` sampled from the top 40 by
  cos(positive, event); `--hard` hard negatives (the most dislike-like events among the top
  300, or near misses ranked 40-300 for users without dislikes); `--random` uniform picks.

Writes <out>/users.jsonl, <out>/tasks-<k>.jsonl (one shard per judge worker; a user's
candidates stay together so the judge can reuse its prompt prefix) and <out>/selection.json.
"""
from __future__ import annotations

import argparse
import glob
import json
import logging
import random
import sys
import time
from collections import Counter
from pathlib import Path

import numpy as np
import torch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # ml/, for the data package
from data.embed import embedding_dim, load_model  # noqa: E402
from data.hf_dataset import DEFAULT_REPO, load_events  # noqa: E402

logger = logging.getLogger("select")
SPLITS = (("train", 0.8), ("validation", 0.1), ("test", 0.1))
RETRIEVE_FROM, TOP = 40, 300  # retrieved candidates come from the top 40; hard negatives from the top 300


def choose(top: np.ndarray, neg_top: np.ndarray, has_negative: bool, pool_size: int, pick: random.Random,
           n_retrieved: int, n_hard: int, n_random: int) -> dict[int, str]:
    """Candidate pool positions for one user, in random order, mapped to how each was picked.

    `top` holds pool positions ranked by cos(positive, event), best first; `neg_top` the
    cos(negative, event) of those same positions.
    """
    chosen: dict[int, str] = {}
    for j in pick.sample(list(top[:RETRIEVE_FROM]), n_retrieved):
        chosen[int(j)] = "retrieved"
    rest = [(int(j), float(s)) for j, s in zip(top, neg_top) if int(j) not in chosen]
    if has_negative:  # looks relevant, but most like what the user avoids
        hard = [j for j, _ in sorted(rest, key=lambda js: -js[1])[:n_hard]]
    else:  # no dislikes: near misses just below the retrieval window
        hard = pick.sample([j for j, _ in rest[RETRIEVE_FROM - n_retrieved:]], n_hard)
    for j in hard:
        chosen[j] = "hard_negative"
    target = len(chosen) + n_random
    while len(chosen) < target:  # rejection sampling: pools are far larger than the candidates
        chosen.setdefault(pick.randrange(pool_size), "random")
    items = list(chosen.items())
    pick.shuffle(items)
    return dict(items)


def load_users(pattern: str, limit: int, seed: int) -> list[dict]:
    rows, seen = [], set()
    for path in sorted(glob.glob(pattern)):
        for line in open(path):
            row = json.loads(line)
            key = (row["positive_text"], row["negative_text"])
            if key not in seen:
                seen.add(key)
                rows.append(row)
    logger.info("%d unique users from %s", len(rows), pattern)
    rng = random.Random(seed)
    rng.shuffle(rows)
    rows = rows[:limit]
    cut1, cut2 = int(len(rows) * SPLITS[0][1]), int(len(rows) * (SPLITS[0][1] + SPLITS[1][1]))
    for i, row in enumerate(rows):
        row["split"] = "train" if i < cut1 else "validation" if i < cut2 else "test"
        row["id"] = f"u{i:05d}"
    return rows


def encode(model, texts: list[str], batch_size: int) -> torch.Tensor:
    present = [i for i, t in enumerate(texts) if t and t.strip()]
    out = np.zeros((len(texts), embedding_dim(model)), dtype=np.float32)
    if present:
        out[present] = model.encode([texts[i] for i in present], batch_size=batch_size,
                                    normalize_embeddings=True, convert_to_numpy=True, show_progress_bar=False)
    return torch.from_numpy(out)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--users-glob", required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--users", type=int, default=10_000)
    ap.add_argument("--events-repo", default=DEFAULT_REPO)
    ap.add_argument("--model", default="Qwen/Qwen3-Embedding-0.6B")
    ap.add_argument("--val-events", type=int, default=2_000, help="train-split events held out for validation")
    ap.add_argument("--retrieved", type=int, default=8)
    ap.add_argument("--hard", type=int, default=4)
    ap.add_argument("--random", type=int, default=8)
    ap.add_argument("--split-seed", type=int, default=13, help="seeds the persona split and the event pools")
    ap.add_argument("--shards", type=int, default=4)
    ap.add_argument("--limit-events", type=int, default=None, help="use only N events per split (testing)")
    ap.add_argument("--batch-size", type=int, default=256)
    a = ap.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")
    started = time.time()

    users = load_users(a.users_glob, a.users, a.split_seed)
    events = load_events(a.events_repo)
    ev_ids, ev_texts, ev_split = [], [], []
    for split in ("train", "test"):
        ds = events[split]
        if a.limit_events:
            ds = ds.select(range(min(a.limit_events, ds.num_rows)))
        ev_ids += ds["id"]
        ev_texts += ds["embedding_text"]
        ev_split += [split] * ds.num_rows
    train_idx = [i for i, s in enumerate(ev_split) if s == "train"]
    rng = np.random.default_rng(a.split_seed)
    val_pool = set(rng.choice(train_idx, size=min(a.val_events, len(train_idx) // 10), replace=False).tolist())
    pools = {"train": np.array([i for i in train_idx if i not in val_pool]),
             "validation": np.array(sorted(val_pool)),
             "test": np.array([i for i, s in enumerate(ev_split) if s == "test"])}
    logger.info("event pools: %s", {k: len(v) for k, v in pools.items()})

    model = load_model(a.model)
    device = "cuda" if torch.cuda.is_available() else "cpu"
    E = encode(model, ev_texts, a.batch_size).to(device)
    P = encode(model, [u["positive_text"] for u in users], a.batch_size).to(device)
    N = encode(model, [u["negative_text"] for u in users], a.batch_size).to(device)
    logger.info("embedded %d events and %d users in %.0fs", len(ev_texts), len(users), time.time() - started)

    pick = random.Random(a.split_seed + 1)
    shards = [open(a.out / f"tasks-{k}.jsonl", "w") for k in range(a.shards)]
    sources, labels_by_split = Counter(), Counter()
    P_np, N_np = P.float().cpu().numpy(), N.float().cpu().numpy()
    with open(a.out / "users.jsonl", "w") as user_sink:
        for split, pool in pools.items():
            members = [i for i, u in enumerate(users) if u["split"] == split]
            pool_E = E[torch.as_tensor(pool, device=device)]      # [|pool|, D]
            pool_np = pool_E.float().cpu().numpy()
            k = min(TOP, len(pool))
            for start in range(0, len(members), 256):
                batch = members[start:start + 256]
                top = torch.topk(P[batch] @ pool_E.T, k, dim=1).indices    # [b, k], best first
                neg_top = torch.gather(N[batch] @ pool_E.T, 1, top).cpu().numpy()
                top = top.cpu().numpy()
                for row, ui in enumerate(batch):
                    user = users[ui]
                    chosen = choose(top[row], neg_top[row], bool(user["negative_text"]), len(pool), pick,
                                    a.retrieved, a.hard, a.random)
                    sink = shards[int(user["id"][1:]) % a.shards]
                    js = np.fromiter(chosen, dtype=np.int64)
                    cos_pos, cos_neg = pool_np[js] @ P_np[ui], pool_np[js] @ N_np[ui]
                    for j, source, cp, cn in zip(js, chosen.values(), cos_pos, cos_neg):
                        ev = int(pool[j])
                        sink.write(json.dumps({
                            "user_id": user["id"], "event_id": ev_ids[ev], "event_split": ev_split[ev],
                            "pool": split, "source": source, "cos_positive": round(float(cp), 4),
                            "cos_negative": round(float(cn), 4),
                            "positive_text": user["positive_text"], "negative_text": user["negative_text"],
                            "event_text": ev_texts[ev]}, ensure_ascii=False) + "\n")
                        sources[source] += 1
                    labels_by_split[split] += 1
                    user_sink.write(json.dumps({k: user[k] for k in ("id", "uid", "split", "persona", "persona_text",
                                                                    "positive_text", "negative_text",
                                                                    "positive_sections", "negative_sections")},
                                               ensure_ascii=False) + "\n")
    for f in shards:
        f.close()
    summary = {"users": dict(labels_by_split), "event_pools": {k: len(v) for k, v in pools.items()},
               "candidates": dict(sources), "split_seed": a.split_seed, "model": a.model,
               "seconds": round(time.time() - started, 1)}
    (a.out / "selection.json").write_text(json.dumps(summary, indent=2))
    logger.info("done: %s", summary)


if __name__ == "__main__":
    main()
