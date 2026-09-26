"""Training triples for the classifier: training.md's "Triple Dataset + persona split".

Joins the frozen embeddings written by ml/data/embed.py with the rated candidates of the
dataset's `users` config. Users arrive already split by persona: the users config's
train / validation / test splits, whose id lists embed.py saves as users/<split>/ids.json next
to the embeddings. Events of both event splits are loaded once and indexed by id.

A TripleSet stores each user and event embedding once and every (user, event) pair as two
indices, so a 160k-pair split costs a few MB instead of 160k x 3 x D floats. `loader()` gathers
whole batches with tensor indexing and is a real DataLoader, so `train` and `evaluate` use it
as-is. Batches carry `user_id` (an int per pair) for per-user ranking metrics.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping, Sequence

import numpy as np
import torch
from torch.utils.data import BatchSampler, DataLoader, Dataset, RandomSampler, SequentialSampler

from .train import EVENT_KEY, LABEL_KEY, NEGATIVE_KEY, POSITIVE_KEY

USER_KEY = "user_id"
SPLITS = ("train", "validation", "test")
# Which event split each user split may draw from (training.md: test events never in training).
EVENT_SPLIT_OF = {"train": "train", "validation": "train", "test": "test"}


@dataclass(eq=False)
class TripleSet(Dataset):
    """One user split's (positive, negative, event, label) pairs, as indices into shared matrices."""

    positive: torch.Tensor  # [U, D] one row per user of this split
    negative: torch.Tensor  # [U, D] zero rows for users without dislikes
    events: torch.Tensor  # [E, D] only the events this split uses
    user_index: torch.Tensor  # [N] row of `positive` / `negative`
    event_index: torch.Tensor  # [N] row of `events`
    labels: torch.Tensor  # [N] float32 in [0, 1]
    user_ids: list[str]
    event_ids: list[str]

    def __len__(self) -> int:
        return int(self.labels.shape[0])

    def batch(self, indices: Any) -> dict[str, torch.Tensor]:
        idx = torch.as_tensor(indices, dtype=torch.long, device=self.labels.device)
        users, events = self.user_index[idx], self.event_index[idx]
        return {
            POSITIVE_KEY: self.positive[users],
            NEGATIVE_KEY: self.negative[users],
            EVENT_KEY: self.events[events],
            LABEL_KEY: self.labels[idx],
            USER_KEY: users,
        }

    def __getitem__(self, i: int) -> dict[str, torch.Tensor]:
        return {key: value[0] for key, value in self.batch([i]).items()}

    def loader(self, batch_size: int, shuffle: bool = False, seed: int | None = None) -> DataLoader:
        """Batches of `batch_size` pairs; shuffled with a seeded generator when asked."""
        order = range(len(self))
        if shuffle:
            generator = torch.Generator().manual_seed(seed) if seed is not None else None
            sampler = RandomSampler(order, generator=generator)
        else:
            sampler = SequentialSampler(order)
        return DataLoader(_Batches(self), sampler=BatchSampler(sampler, batch_size, drop_last=False), batch_size=None)

    def to(self, device: str | torch.device) -> "TripleSet":
        move = {k: getattr(self, k).to(device) for k in
                ("positive", "negative", "events", "user_index", "event_index", "labels")}
        return TripleSet(**move, user_ids=self.user_ids, event_ids=self.event_ids)

    def summary(self) -> dict[str, float]:
        labels = self.labels.float()
        pairs_per_user = torch.bincount(self.user_index, minlength=len(self.user_ids)).float()
        return {
            "users": len(self.user_ids),
            "events": len(self.event_ids),
            "pairs": len(self),
            "pairs_per_user": float(pairs_per_user.mean()),
            "label_mean": float(labels.mean()),
            "relevant_share": float((labels >= 0.5).float().mean()),
            "users_without_dislikes": float((self.negative.abs().sum(dim=1) == 0).float().mean()),
        }

    def state(self) -> dict[str, Any]:
        """Plain-tensor form for torch.save (see `from_state`)."""
        return {k: getattr(self, k) for k in ("positive", "negative", "events", "user_index", "event_index",
                                                "labels", "user_ids", "event_ids")}

    @classmethod
    def from_state(cls, state: Mapping[str, Any]) -> "TripleSet":
        return cls(**state)


class _Batches(Dataset):
    """Maps a list of pair indices to one ready-made batch (used with BatchSampler, batch_size=None)."""

    def __init__(self, triples: TripleSet) -> None:
        self.triples = triples

    def __len__(self) -> int:
        return len(self.triples)

    def __getitem__(self, indices: Sequence[int]) -> dict[str, torch.Tensor]:
        return self.triples.batch(indices)


def build_triples(
    user_ids: Sequence[str],
    positive: np.ndarray,
    negative: np.ndarray,
    event_ids: Sequence[str],
    events: np.ndarray,
    candidates: Mapping[str, Sequence[Mapping[str, Any]]],
) -> TripleSet:
    """One split's TripleSet from embedding matrices (rows in id order) and rated candidates.

    `candidates[user_id]` lists `{"event_id", "label"}` dicts. Only events that appear are
    kept, so each split carries its own compact event matrix.
    """
    user_row = {uid: i for i, uid in enumerate(user_ids)}
    event_row = {eid: i for i, eid in enumerate(event_ids)}
    u_idx, e_idx, labels = [], [], []
    for uid, cands in candidates.items():
        if uid not in user_row:
            raise KeyError(f"user {uid!r} has candidates but no embedding")
        for c in cands:
            if c["event_id"] not in event_row:
                raise KeyError(f"event {c['event_id']!r} (candidate of {uid}) has no embedding")
            u_idx.append(user_row[uid])
            e_idx.append(event_row[c["event_id"]])
            labels.append(float(c["label"]))
    used, compact = np.unique(np.asarray(e_idx, dtype=np.int64), return_inverse=True)
    return TripleSet(
        positive=torch.from_numpy(np.array(positive, dtype=np.float32)),  # copies: embed.py arrays are read-only memmaps
        negative=torch.from_numpy(np.array(negative, dtype=np.float32)),
        events=torch.from_numpy(np.array(events[used], dtype=np.float32)),
        user_index=torch.as_tensor(u_idx, dtype=torch.long),
        event_index=torch.from_numpy(compact.astype(np.int64)),
        labels=torch.as_tensor(labels, dtype=torch.float32),
        user_ids=list(user_ids),
        event_ids=[event_ids[i] for i in used],
    )


def load_triples(emb_dir: str | Path, users: Mapping[str, Any]) -> dict[str, TripleSet]:
    """All three user splits from an embed.py output dir and the users config (a DatasetDict).

    Checks the split rules of training.md: validation and test users only meet events of the
    allowed event split, and no event is shared between the train, validation and test pools.
    """
    from data.embed import load_embeddings  # ml/data; imported here to keep this module light

    event_ids, event_vectors, event_split = [], [], {}
    for split in ("train", "test"):
        ids, arrays = load_embeddings(emb_dir, "events", split)
        event_ids += ids
        event_vectors.append(np.asarray(arrays["embedding_text"]))
        event_split.update({eid: split for eid in ids})
    events = np.concatenate(event_vectors)

    triples, used = {}, {}
    for split in SPLITS:
        ids, arrays = load_embeddings(emb_dir, "users", split)
        rows = users[split]
        candidates = {row["id"]: row["candidates"] for row in rows}
        bad = {c["event_id"] for cands in candidates.values() for c in cands
               if event_split.get(c["event_id"]) != EVENT_SPLIT_OF[split]}
        if bad:
            raise ValueError(f"{split} users have {len(bad)} candidates outside the {EVENT_SPLIT_OF[split]} event split")
        triples[split] = build_triples(ids, arrays["positive_text"], arrays["negative_text"], event_ids, events,
                                       candidates)
        used[split] = set(triples[split].event_ids)
    for a, b in (("train", "validation"), ("train", "test"), ("validation", "test")):
        if used[a] & used[b]:
            raise ValueError(f"{len(used[a] & used[b])} events are shared between {a} and {b} pairs")
    return triples
