"""Tests for the training pipeline around the recipe: triples, split checks, hooks, experiment plans."""

import json
import math
import tempfile
import unittest
from pathlib import Path

import numpy as np
import torch

from compatibility.classifier import ClassifierConfig, TrainingCallback, evaluate, train
from compatibility.classifier.data import TripleSet, build_triples, load_triples
from compatibility.classifier.experiment import phase1_plan, phase2_plan, rank_key, RunSpec

DIM = 8


def unit(rows: int, seed: int) -> np.ndarray:
    x = np.random.default_rng(seed).normal(size=(rows, DIM)).astype(np.float32)
    return x / np.linalg.norm(x, axis=1, keepdims=True)


def toy_triples(users: int = 12, events: int = 30, per_user: int = 6, seed: int = 0) -> TripleSet:
    """Labels follow the cosine between a user's positive vector and the event, so they're learnable."""
    rng = np.random.default_rng(seed)
    pos, neg, ev = unit(users, seed), unit(users, seed + 1), unit(events, seed + 2)
    neg[0] = 0.0  # a user without dislikes
    candidates = {}
    for u in range(users):
        picks = rng.choice(events, per_user, replace=False)
        candidates[f"u{u}"] = [{"event_id": f"e{e}", "label": float((pos[u] @ ev[e] + 1) / 2)} for e in picks]
    return build_triples([f"u{u}" for u in range(users)], pos, neg, [f"e{e}" for e in range(events)], ev, candidates)


class TripleSetTest(unittest.TestCase):
    def test_batches_have_recipe_keys_and_shapes(self):
        t = toy_triples()
        batch = next(iter(t.loader(5)))
        self.assertEqual(set(batch), {"positive_embedding", "negative_embedding", "event_embedding", "label", "user_id"})
        self.assertEqual(batch["positive_embedding"].shape, (5, DIM))
        self.assertEqual(batch["label"].shape, (5,))

    def test_epoch_covers_every_pair_once_and_shuffle_is_seeded(self):
        t = toy_triples()
        labels = torch.cat([b["label"] for b in t.loader(7, shuffle=True, seed=3)])
        self.assertEqual(len(labels), len(t))
        self.assertTrue(torch.allclose(labels.sort().values, t.labels.sort().values))
        again = torch.cat([b["label"] for b in t.loader(7, shuffle=True, seed=3)])
        self.assertTrue(torch.equal(labels, again))

    def test_single_items_and_compact_event_matrix(self):
        t = toy_triples(events=100, users=3, per_user=4)
        self.assertLessEqual(t.events.shape[0], 12)  # only used events are kept
        self.assertEqual(t[0]["event_embedding"].shape, (DIM,))

    def test_state_round_trip(self):
        t = toy_triples()
        again = TripleSet.from_state(t.state())
        self.assertTrue(torch.equal(t.labels, again.labels))
        self.assertEqual(t.user_ids, again.user_ids)


def write_split(root: Path, name: str, split: str, ids: list[str], **arrays: np.ndarray) -> None:
    d = root / name / split
    d.mkdir(parents=True)
    for key, value in arrays.items():
        np.save(d / f"{key}.npy", value)
    (d / "ids.json").write_text(json.dumps(ids))


class LoadTriplesTest(unittest.TestCase):
    def build(self, test_candidate: str) -> dict:
        tmp = Path(tempfile.mkdtemp())
        write_split(tmp, "events", "train", ["a", "b", "c"], embedding_text=unit(3, 1))
        write_split(tmp, "events", "test", ["t1", "t2"], embedding_text=unit(2, 2))
        users = {}
        for split, uid, events in (("train", "u1", ["a", "b"]), ("validation", "u2", ["c", "c"][:1] + ["c"]),
                                   ("test", "u3", ["t1", test_candidate])):
            write_split(tmp, "users", split, [uid], positive_text=unit(1, 3), negative_text=unit(1, 4))
            users[split] = [{"id": uid, "candidates": [{"event_id": e, "label": 0.5} for e in events]}]
        return load_triples(tmp, users)

    def test_valid_splits_load(self):
        triples = self.build("t2")
        self.assertEqual(len(triples["test"]), 2)

    def test_training_event_in_test_split_is_rejected(self):
        with self.assertRaises(ValueError):
            self.build("a")


class HooksTest(unittest.TestCase):
    def test_callbacks_see_every_batch_and_epoch(self):
        class Recorder(TrainingCallback):
            def __init__(self):
                self.steps, self.epochs = [], []

            def on_batch_end(self, step, loss):
                self.steps.append(step)

            def on_epoch_end(self, epoch, history, model, improved):
                self.epochs.append((epoch, len(history.val_loss)))

        t = toy_triples()
        rec = Recorder()
        config = ClassifierConfig(embedding_dim=DIM, max_epochs=3, early_stopping_patience=10, device="cpu")
        train(config, t.loader(10, shuffle=True, seed=0), t.loader(50), callbacks=[rec])
        batches = math.ceil(len(t) / 10)
        self.assertEqual(rec.steps, list(range(3 * batches)))
        self.assertEqual(rec.epochs, [(0, 1), (1, 2), (2, 3)])

    def test_selection_on_a_metric_keeps_the_best_scoring_epoch(self):
        t = toy_triples()
        scores = iter([0.2, 0.9, 0.5, 0.4])  # epoch 1 is best; patience 2 stops after epoch 3
        config = ClassifierConfig(embedding_dim=DIM, max_epochs=10, early_stopping_patience=2,
                                  selection_metric="ndcg@10", device="cpu")
        result = train(config, t.loader(10, shuffle=True, seed=0), t.loader(50), val_score_fn=lambda m: next(scores))
        self.assertEqual(result.history.best_epoch, 1)
        self.assertEqual(result.history.epochs_run, 4)
        self.assertEqual(result.history.val_selection, [0.2, 0.9, 0.5, 0.4])

    def test_metric_selection_needs_a_score_function(self):
        t = toy_triples()
        config = ClassifierConfig(embedding_dim=DIM, selection_metric="ndcg@10", device="cpu")
        with self.assertRaises(ValueError):
            train(config, t.loader(10), t.loader(10))

    def test_evaluate_groups_by_user(self):
        t = toy_triples(users=9)
        results = evaluate(None, t.loader(8), ClassifierConfig(embedding_dim=DIM, device="cpu"))
        self.assertEqual(results["cosine_baseline"]["num_groups"], 9)


class PlanTest(unittest.TestCase):
    def test_phase1_is_the_recipe_comparison_over_three_seeds(self):
        names = [r.name for r in phase1_plan()]
        self.assertEqual(len(names), 12)
        self.assertIn("p1-residual-s0", names)
        self.assertIn("p1-residual-mse-s2", names)

    def test_phase2_changes_one_thing_at_a_time(self):
        winner = RunSpec("p1-residual-s0", "residual", {"loss": "huber", "seed": 0})
        runs = {r.name: r for r in phase2_plan(winner, 0.5)}
        self.assertIn("p2-residual-lambda0.5", runs)
        config = runs["p2-residual-lr3e-4"].config(DIM)
        self.assertEqual(config.learning_rate, 3e-4)
        self.assertTrue(config.use_residual_baseline)
        self.assertNotIn("p2-residual-lambda1", {r.name for r in phase2_plan(winner, 1.0)})

    def test_rank_key_orders_by_ndcg_then_recall_then_spearman(self):
        a = {"ndcg@10": 0.8, "recall@10": 0.5, "spearman": 0.1}
        b = {"ndcg@10": 0.8, "recall@10": 0.6, "spearman": 0.0}
        c = {"ndcg@10": float("nan"), "recall@10": 1.0, "spearman": 1.0}
        self.assertEqual(max([a, b, c], key=rank_key), b)


if __name__ == "__main__":
    unittest.main()
