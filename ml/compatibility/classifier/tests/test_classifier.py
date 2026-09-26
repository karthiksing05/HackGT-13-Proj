import math
import tempfile
import unittest
from pathlib import Path

import numpy as np
import torch
import torch.nn.functional as F
from torch import nn
from torch.utils.data import DataLoader, Dataset

from compatibility import CosineCompatibilityModel, Embedding, IncompatibleEmbeddingsError, UserEmbedding
from compatibility.classifier import (
    MODEL_VARIANTS,
    ClassifierCompatibilityModel,
    ClassifierConfig,
    CompatibilityClassifier,
    EarlyStopping,
    FlatMLP,
    LateFusionMLP,
    build_embedding_interaction_features,
    build_features,
    compute_cosine_baseline,
    compute_similarity_features,
    cosine_baseline_score,
    cosine_similarity,
    evaluate,
    feature_dim,
    format_comparison,
    interaction_feature_dim,
    load_checkpoint,
    save_checkpoint,
    score_dataset,
    summarize_components,
    train,
)
from compatibility.classifier import metrics
from compatibility.classifier.train import build_loss, build_optimizer, train_one_epoch, unpack_batch, validate

DIM = 16


class SyntheticCompatibilityDataset(Dataset):
    """Test-only data: the event is a blend of the user's positive and negative vectors.

    label in [0, 1] is the blend weight on the positive vector, so an event
    close to `positive` has a high label and one close to `negative` a low one.
    Each user has fixed positive/negative vectors and `events_per_user` events.
    """

    def __init__(self, num_users: int = 4, events_per_user: int = 8, dim: int = DIM, seed: int = 0, noise: float = 0.05):
        g = torch.Generator().manual_seed(seed)
        pos = torch.randn(num_users, dim, generator=g)
        neg = torch.randn(num_users, dim, generator=g)
        n = num_users * events_per_user
        self.user_id = torch.arange(num_users).repeat_interleave(events_per_user)
        self.label = torch.rand(n, generator=g)
        self.positive = pos[self.user_id]
        self.negative = neg[self.user_id]
        w = self.label.unsqueeze(-1)
        self.event = w * self.positive + (1 - w) * self.negative + noise * torch.randn(n, dim, generator=g)

    def __len__(self) -> int:
        return self.label.shape[0]

    def __getitem__(self, i: int) -> dict:
        return {
            "positive_embedding": self.positive[i],
            "negative_embedding": self.negative[i],
            "event_embedding": self.event[i],
            "label": self.label[i],
            "user_id": int(self.user_id[i]),
        }


class TupleDataset(Dataset):
    """Same samples as `SyntheticCompatibilityDataset` but as (pos, neg, event, label) tuples."""

    def __init__(self, base: SyntheticCompatibilityDataset):
        self.base = base

    def __len__(self) -> int:
        return len(self.base)

    def __getitem__(self, i: int):
        s = self.base[i]
        return s["positive_embedding"], s["negative_embedding"], s["event_embedding"], s["label"]


def small_config(**overrides) -> ClassifierConfig:
    defaults = dict(embedding_dim=DIM, hidden_dims=(64, 32), dropout=0.0, device="cpu", batch_size=16)
    return ClassifierConfig(**{**defaults, **overrides})


def random_triple(batch: int = 5, dim: int = DIM, seed: int = 0):
    g = torch.Generator().manual_seed(seed)
    return tuple(torch.randn(batch, dim, generator=g) for _ in range(3))


class ConfigTests(unittest.TestCase):
    def test_scalar_dropout_broadcasts(self):
        self.assertEqual(ClassifierConfig(embedding_dim=8, hidden_dims=(4, 4, 4), dropout=0.3).dropout, (0.3, 0.3, 0.3))

    def test_rejects_bad_values(self):
        for bad in (
            dict(embedding_dim=0),
            dict(embedding_dim=8, dropout=(0.1,)),
            dict(embedding_dim=8, loss="hinge"),
            dict(embedding_dim=8, dropout=1.0),
        ):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                ClassifierConfig(**bad)

    def test_round_trips_through_dict(self):
        config = small_config(loss="mse", negative_weight=0.5)
        self.assertEqual(ClassifierConfig.from_dict(config.to_dict()), config)


class FeatureTests(unittest.TestCase):
    def test_feature_dimension(self):
        for dim in (1, 3, 16, 384):
            with self.subTest(dim=dim):
                pos, neg, event = random_triple(4, dim)
                self.assertEqual(feature_dim(dim), 7 * dim + 2)
                self.assertEqual(build_features(pos, neg, event).shape, (4, 7 * dim + 2))
                self.assertEqual(build_features(pos[0], neg[0], event[0]).shape, (7 * dim + 2,))

    def test_feature_layout(self):
        pos, neg, event = torch.tensor([1.0, 2.0]), torch.tensor([0.0, -1.0]), torch.tensor([3.0, 0.0])
        f = build_features(pos, neg, event)
        expected_blocks = [pos, neg, event, (pos - event).abs(), (neg - event).abs(), pos * event, neg * event]
        torch.testing.assert_close(f[:14], torch.cat(expected_blocks))
        torch.testing.assert_close(f[14], torch.tensor(1 / math.sqrt(5)))
        torch.testing.assert_close(f[15], torch.tensor(0.0))

    def test_shape_mismatch_raises(self):
        with self.assertRaises(ValueError):
            build_features(torch.zeros(2, 4), torch.zeros(2, 4), torch.zeros(2, 5))

    def test_cosine_similarity_values(self):
        a = torch.tensor([[1.0, 0.0], [1.0, 0.0], [1.0, 1.0], [2.0, 0.0], [0.0, 0.0]])
        b = torch.tensor([[3.0, 0.0], [0.0, 2.0], [-1.0, -1.0], [1.0, 1.0], [1.0, 0.0]])
        expected = torch.tensor([1.0, 0.0, -1.0, 1 / math.sqrt(2), 0.0])
        torch.testing.assert_close(cosine_similarity(a, b), expected)

    def test_cosine_baseline(self):
        pos = torch.tensor([[1.0, 0.0]])
        neg = torch.tensor([[0.0, 1.0]])
        event = torch.tensor([[1.0, 1.0]])
        c = 1 / math.sqrt(2)
        torch.testing.assert_close(cosine_baseline_score(pos, neg, event), torch.tensor([c - c]))
        torch.testing.assert_close(cosine_baseline_score(pos, neg, event, negative_weight=0.5), torch.tensor([0.5 * c]))
        # Event aligned with the positive vector beats one aligned with the negative vector.
        close_to_pos = cosine_baseline_score(pos, neg, torch.tensor([[1.0, 0.1]]))
        close_to_neg = cosine_baseline_score(pos, neg, torch.tensor([[0.1, 1.0]]))
        self.assertGreater(close_to_pos.item(), close_to_neg.item())


class LateFusionFeatureTests(unittest.TestCase):
    def test_interaction_feature_dimension_is_7d(self):
        for dim in (1, 16, 384):
            with self.subTest(dim=dim):
                pos, neg, event = random_triple(4, dim)
                self.assertEqual(interaction_feature_dim(dim), 7 * dim)
                self.assertEqual(build_embedding_interaction_features(pos, neg, event).shape, (4, 7 * dim))

    def test_interaction_features_exclude_cosines(self):
        pos, neg, event = random_triple(3)
        interactions = build_embedding_interaction_features(pos, neg, event)
        torch.testing.assert_close(build_features(pos, neg, event)[:, : 7 * DIM], interactions)

    def test_similarity_features_are_pos_cos_neg_cos_and_difference(self):
        pos = torch.tensor([[1.0, 0.0], [1.0, 1.0]])
        neg = torch.tensor([[0.0, 1.0], [-1.0, -1.0]])
        event = torch.tensor([[1.0, 1.0], [1.0, 1.0]])
        c = 1 / math.sqrt(2)
        expected = torch.tensor([[c, c, 0.0], [1.0, -1.0, 2.0]])
        similarity = compute_similarity_features(pos, neg, event)
        self.assertEqual(similarity.shape, (2, 3))
        torch.testing.assert_close(similarity, expected)

    def test_baseline_scoring_is_unchanged(self):
        pos, neg, event = random_triple(10)
        for weight in (0.0, 0.5, 1.0, 2.0):
            with self.subTest(weight=weight):
                expected = F.cosine_similarity(pos, event, dim=-1) - weight * F.cosine_similarity(neg, event, dim=-1)
                torch.testing.assert_close(compute_cosine_baseline(pos, neg, event, weight), expected)
        self.assertIs(cosine_baseline_score, compute_cosine_baseline)


class ModelTests(unittest.TestCase):
    def test_default_config_is_residual_late_fusion(self):
        model = CompatibilityClassifier.from_config(ClassifierConfig(embedding_dim=DIM))
        self.assertIsInstance(model.backbone, LateFusionMLP)
        self.assertTrue(model.use_residual_baseline)
        self.assertIsInstance(model.alpha, nn.Parameter)
        self.assertEqual(model.alpha.item(), 1.0)
        linears = [m for m in model.modules() if isinstance(m, nn.Linear)]
        self.assertEqual(
            [(l.in_features, l.out_features) for l in linears],
            [(7 * DIM, 256), (256, 64), (64 + 3, 32), (32, 1)],
        )
        self.assertFalse(any(isinstance(m, nn.Sigmoid) for m in model.modules()))

    def test_flat_architecture_is_unchanged(self):
        model = FlatMLP(embedding_dim=DIM)
        linears = [m for m in model.modules() if isinstance(m, nn.Linear)]
        self.assertEqual([(l.in_features, l.out_features) for l in linears], [(7 * DIM + 2, 512), (512, 128), (128, 1)])
        self.assertEqual([m.p for m in model.modules() if isinstance(m, nn.Dropout)], [0.2, 0.1])

    def test_output_shape_for_every_variant(self):
        for variant in MODEL_VARIANTS:
            model = CompatibilityClassifier.from_config(small_config().with_variant(variant))
            for batch in (1, 7):
                with self.subTest(variant=variant, batch=batch):
                    self.assertEqual(model(*random_triple(batch)).shape, (batch,))

    def test_rejects_wrong_embedding_dim(self):
        model = CompatibilityClassifier.from_config(small_config())
        with self.assertRaisesRegex(ValueError, "embedding dim"):
            model(*random_triple(2, DIM + 1))

    def test_batched_matches_single_inference(self):
        pos, neg, event = random_triple(6)
        for variant in MODEL_VARIANTS:
            with self.subTest(variant=variant):
                torch.manual_seed(0)
                model = CompatibilityClassifier.from_config(small_config().with_variant(variant)).eval()
                _randomize_output_layer(model)
                with torch.no_grad():
                    batched = model(pos, neg, event)
                    single = torch.cat([model(pos[i : i + 1], neg[i : i + 1], event[i : i + 1]) for i in range(6)])
                torch.testing.assert_close(batched, single)


def _randomize_output_layer(model: CompatibilityClassifier) -> None:
    """Undo the residual zero-init so the correction term is non-trivial."""
    last = [m for m in model.backbone.modules() if isinstance(m, nn.Linear)][-1]
    nn.init.normal_(last.weight, std=0.5)
    nn.init.normal_(last.bias, std=0.5)


class ResidualTests(unittest.TestCase):
    def setUp(self):
        torch.manual_seed(0)
        self.inputs = random_triple(8)

    def test_prediction_is_alpha_times_baseline_plus_correction(self):
        model = CompatibilityClassifier.from_config(small_config(negative_weight=0.5, initial_alpha=0.7)).eval()
        _randomize_output_layer(model)
        pos, neg, event = self.inputs
        with torch.no_grad():
            expected = 0.7 * compute_cosine_baseline(pos, neg, event, 0.5) + model.backbone(pos, neg, event)
            torch.testing.assert_close(model(pos, neg, event), expected)
            c = model.score_components(pos, neg, event)
        torch.testing.assert_close(c.final_score, c.alpha * c.baseline_score + c.learned_correction)
        self.assertNotEqual(c.learned_correction.abs().sum().item(), 0.0)

    def test_untrained_residual_model_equals_scaled_baseline(self):
        model = CompatibilityClassifier.from_config(small_config(initial_alpha=1.3)).eval()
        pos, neg, event = self.inputs
        with torch.no_grad():
            torch.testing.assert_close(model(pos, neg, event), 1.3 * compute_cosine_baseline(pos, neg, event))

    def test_without_residual_alpha_is_zero_and_correction_is_the_score(self):
        model = CompatibilityClassifier.from_config(small_config().with_variant("late_fusion")).eval()
        self.assertFalse(hasattr(model, "alpha"))
        with torch.no_grad():
            c = model.score_components(*self.inputs)
        self.assertEqual(c.alpha.item(), 0.0)
        torch.testing.assert_close(c.final_score, c.learned_correction)

    def test_learnable_alpha_receives_gradients_and_is_not_decayed(self):
        config = small_config(weight_decay=0.5)
        model = CompatibilityClassifier.from_config(config)
        optimizer = build_optimizer(model, config)
        loss = model(*self.inputs).pow(2).mean()
        loss.backward()
        self.assertIsNotNone(model.alpha.grad)
        self.assertNotEqual(model.alpha.grad.item(), 0.0)

        before = model.alpha.item()
        optimizer.step()
        self.assertNotEqual(model.alpha.item(), before)
        alpha_group = next(g for g in optimizer.param_groups if any(p is model.alpha for p in g["params"]))
        self.assertEqual(alpha_group["weight_decay"], 0.0)

    def test_fixed_alpha_is_a_buffer_that_training_does_not_change(self):
        config = small_config(learnable_alpha=False, initial_alpha=0.8, learning_rate=1e-2)
        model = CompatibilityClassifier.from_config(config)
        self.assertNotIn("alpha", dict(model.named_parameters()))
        self.assertIn("alpha", dict(model.named_buffers()))
        self.assertFalse(model.alpha.requires_grad)

        loader = DataLoader(SyntheticCompatibilityDataset(), batch_size=8)
        train_one_epoch(model, loader, build_loss(config), build_optimizer(model, config), torch.device("cpu"))
        self.assertEqual(model.alpha.item(), torch.tensor(0.8).item())

        model.eval()
        pos, neg, event = self.inputs
        with torch.no_grad():
            expected = 0.8 * compute_cosine_baseline(pos, neg, event) + model.backbone(pos, neg, event)
            torch.testing.assert_close(model(pos, neg, event), expected)

    def test_high_positive_low_negative_cosine_gives_high_baseline(self):
        model = CompatibilityClassifier.from_config(small_config(negative_weight=0.5)).eval()
        event = torch.zeros(1, DIM)
        event[0, 0] = 1.0
        aligned = event.clone()  # cos = 1
        orthogonal = torch.zeros(1, DIM)
        orthogonal[0, 1] = 1.0  # cos = 0

        with torch.no_grad():
            liked = model.score_components(aligned, orthogonal, event)
            disliked = model.score_components(orthogonal, aligned, event)

        self.assertAlmostEqual(liked.positive_cosine.item(), 1.0, places=6)
        self.assertAlmostEqual(liked.negative_cosine.item(), 0.0, places=6)
        self.assertAlmostEqual(liked.baseline_score.item(), 1.0, places=6)
        self.assertAlmostEqual(disliked.positive_cosine.item(), 0.0, places=6)
        self.assertAlmostEqual(disliked.negative_cosine.item(), 1.0, places=6)
        self.assertAlmostEqual(disliked.baseline_score.item(), -0.5, places=6)
        self.assertGreater(liked.final_score.item(), disliked.final_score.item())


class LossAndOptimizationTests(unittest.TestCase):
    def test_build_loss(self):
        self.assertIsInstance(build_loss(small_config(loss="huber")), nn.HuberLoss)
        self.assertIsInstance(build_loss(small_config(loss="mse")), nn.MSELoss)
        self.assertIsInstance(build_loss(small_config(loss="bce")), nn.BCEWithLogitsLoss)

    def test_huber_loss_value(self):
        loss = build_loss(small_config(loss="huber", huber_delta=1.0))
        pred, target = torch.tensor([0.0, 0.0]), torch.tensor([0.5, 3.0])
        # 0.5 * 0.5^2 (quadratic region) and 1 * (3 - 0.5) (linear region)
        torch.testing.assert_close(loss(pred, target), torch.tensor((0.125 + 2.5) / 2))

    def test_optimization_steps_update_parameters_and_reduce_loss(self):
        torch.manual_seed(0)
        config = small_config(learning_rate=1e-2)
        model = CompatibilityClassifier.from_config(config)
        loss_fn, optimizer = build_loss(config), build_optimizer(model, config)
        loader = DataLoader(SyntheticCompatibilityDataset(), batch_size=8)
        before = [p.detach().clone() for p in model.parameters()]

        first = validate(model, loader, loss_fn, torch.device("cpu"))
        for _ in range(5):
            train_loss = train_one_epoch(model, loader, loss_fn, optimizer, torch.device("cpu"))
        after = validate(model, loader, loss_fn, torch.device("cpu"))

        self.assertTrue(math.isfinite(train_loss))
        self.assertTrue(any(not torch.equal(b, p) for b, p in zip(before, model.parameters())))
        self.assertLess(after, first)

    def test_unpack_batch_accepts_dicts_and_tuples(self):
        base = SyntheticCompatibilityDataset()
        from_dict = unpack_batch(next(iter(DataLoader(base, batch_size=4))))
        from_tuple = unpack_batch(next(iter(DataLoader(TupleDataset(base), batch_size=4))))
        for a, b in zip(from_dict, from_tuple):
            torch.testing.assert_close(a, b)
        self.assertEqual(from_dict[0].shape, (4, DIM))
        self.assertEqual(from_dict[3].shape, (4,))


class CheckpointTests(unittest.TestCase):
    def test_save_and_load_round_trip(self):
        torch.manual_seed(0)
        config = small_config(hidden_dims=(8, 4), dropout=(0.1, 0.0))
        model = CompatibilityClassifier.from_config(config).eval()
        inputs = random_triple(3)
        with tempfile.TemporaryDirectory() as tmp:
            path = save_checkpoint(Path(tmp) / "nested" / "ckpt.pt", model, config, epoch=3, val_loss=0.25)
            loaded, checkpoint = load_checkpoint(path)

        self.assertFalse(loaded.training)
        self.assertEqual(checkpoint["epoch"], 3)
        self.assertEqual(checkpoint["val_loss"], 0.25)
        self.assertEqual(ClassifierConfig.from_dict(checkpoint["config"]), config)
        with torch.no_grad():
            torch.testing.assert_close(loaded(*inputs), model(*inputs))


class ResidualCheckpointTests(unittest.TestCase):
    def test_checkpoint_preserves_alpha(self):
        for learnable in (True, False):
            with self.subTest(learnable=learnable):
                config = small_config(learnable_alpha=learnable, initial_alpha=0.5)
                model = CompatibilityClassifier.from_config(config).eval()
                with torch.no_grad():
                    model.alpha.fill_(0.37)
                _randomize_output_layer(model)
                inputs = random_triple(4)
                with tempfile.TemporaryDirectory() as tmp:
                    loaded, _ = load_checkpoint(save_checkpoint(Path(tmp) / "c.pt", model, config, epoch=0, val_loss=0.0))
                self.assertAlmostEqual(loaded.alpha.item(), 0.37, places=6)
                self.assertEqual(isinstance(loaded.alpha, nn.Parameter), learnable)
                with torch.no_grad():
                    torch.testing.assert_close(loaded(*inputs), model(*inputs))

    def test_loads_checkpoints_saved_before_late_fusion(self):
        torch.manual_seed(0)
        legacy_model = FlatMLP(DIM, hidden_dims=(8, 4), dropout=0.0).eval()
        legacy_config = {
            k: v
            for k, v in small_config(hidden_dims=(8, 4)).to_dict().items()
            if k not in ("architecture", "use_residual_baseline", "learnable_alpha", "initial_alpha")
            and not k.startswith(("embedding_branch", "final_head"))
        }
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "legacy.pt"
            torch.save({"model_state": legacy_model.state_dict(), "config": legacy_config, "epoch": 0, "val_loss": 0.0}, path)
            loaded, _ = load_checkpoint(path)
        self.assertIsInstance(loaded.backbone, FlatMLP)
        self.assertFalse(loaded.use_residual_baseline)
        inputs = random_triple(4)
        with torch.no_grad():
            torch.testing.assert_close(loaded(*inputs), legacy_model(*inputs))


class EarlyStoppingTests(unittest.TestCase):
    def test_stops_after_patience_epochs_without_improvement(self):
        stopper = EarlyStopping(patience=2)
        outcomes = [(stopper.step(loss), stopper.should_stop) for loss in (1.0, 0.8, 0.9, 0.7, 0.75, 0.71)]
        self.assertEqual(
            outcomes,
            [(True, False), (True, False), (False, False), (True, False), (False, False), (False, True)],
        )
        self.assertEqual(stopper.best, 0.7)

    def test_min_delta(self):
        stopper = EarlyStopping(patience=1, min_delta=0.1)
        self.assertTrue(stopper.step(1.0))
        self.assertFalse(stopper.step(0.95))
        self.assertTrue(stopper.should_stop)

    def test_training_stops_early_and_keeps_best_checkpoint(self):
        # With lr=0 the validation loss never improves after epoch 0.
        config = small_config(learning_rate=0.0, weight_decay=0.0, max_epochs=20, early_stopping_patience=3)
        data = SyntheticCompatibilityDataset()
        with tempfile.TemporaryDirectory() as tmp:
            result = train(config, data, data, checkpoint_path=Path(tmp) / "best.pt")
            _, checkpoint = load_checkpoint(result.checkpoint_path)

        self.assertTrue(result.history.stopped_early)
        self.assertEqual(result.history.epochs_run, 4)
        self.assertEqual(result.history.best_epoch, 0)
        self.assertEqual(checkpoint["epoch"], 0)

    def test_returns_best_weights_not_last(self):
        # A high learning rate makes validation loss noisy; the returned model must match the best epoch.
        config = small_config(learning_rate=0.5, max_epochs=8, early_stopping_patience=8)
        train_data, val_data = SyntheticCompatibilityDataset(seed=0), SyntheticCompatibilityDataset(seed=1)
        with tempfile.TemporaryDirectory() as tmp:
            result = train(config, train_data, val_data, checkpoint_path=Path(tmp) / "best.pt")
            loaded, checkpoint = load_checkpoint(result.checkpoint_path)

        history = result.history
        self.assertEqual(history.best_val_loss, min(history.val_loss))
        loss_fn, loader = build_loss(config), DataLoader(val_data, batch_size=16)
        cpu = torch.device("cpu")
        self.assertAlmostEqual(validate(result.model, loader, loss_fn, cpu), history.best_val_loss, places=5)
        self.assertAlmostEqual(validate(loaded, loader, loss_fn, cpu), history.best_val_loss, places=5)
        self.assertEqual(checkpoint["epoch"], history.best_epoch)


class TrainingIntegrationTests(unittest.TestCase):
    def test_overfits_tiny_synthetic_dataset(self):
        config = small_config(loss="mse", learning_rate=3e-3, max_epochs=300, early_stopping_patience=300, batch_size=8)
        data = SyntheticCompatibilityDataset(num_users=4, events_per_user=8)
        result = train(config, data, data)

        self.assertLess(result.history.best_val_loss, 1e-3)
        scores = evaluate(result.model, data, config)["classifier"]
        self.assertGreater(scores["spearman"], 0.98)
        self.assertLess(scores["rmse"], 0.05)

    def test_training_is_deterministic(self):
        config = small_config(max_epochs=3)
        data = SyntheticCompatibilityDataset()
        first, second = train(config, data, data), train(config, data, data)
        self.assertEqual(first.history.train_loss, second.history.train_loss)

    def test_accepts_prebuilt_dataloaders(self):
        config = small_config(max_epochs=2)
        data = SyntheticCompatibilityDataset()
        result = train(config, DataLoader(TupleDataset(data), batch_size=5, shuffle=True), DataLoader(data, batch_size=7))
        self.assertEqual(result.history.epochs_run, 2)

    def test_binary_mode(self):
        config = small_config(loss="bce", max_epochs=3)
        data = SyntheticCompatibilityDataset()
        data.label = (data.label > 0.5).float()
        result = train(config, data, data)
        scores = evaluate(result.model, data, config, group_key=None)["classifier"]
        self.assertTrue(math.isfinite(result.history.best_val_loss))
        self.assertGreaterEqual(scores["mae"], 0.0)
        self.assertLessEqual(scores["mae"], 1.0)  # sigmoid applied, so errors are bounded


class VariantTrainingTests(unittest.TestCase):
    def test_every_variant_fits_tiny_synthetic_dataset(self):
        data = SyntheticCompatibilityDataset(num_users=4, events_per_user=8)
        for variant in MODEL_VARIANTS:
            with self.subTest(variant=variant):
                config = small_config(loss="mse", learning_rate=3e-3, max_epochs=150, early_stopping_patience=150, batch_size=8)
                result = train(config.with_variant(variant), data, data)
                self.assertLess(result.history.best_val_loss, 5e-3)
                self.assertGreater(evaluate(result.model, data, config)["classifier"]["spearman"], 0.95)

    def test_learnable_alpha_moves_during_training(self):
        config = small_config(max_epochs=5, learning_rate=1e-2)
        data = SyntheticCompatibilityDataset()
        result = train(config, data, data)
        self.assertNotEqual(result.model.alpha.item(), config.initial_alpha)


class EvaluationTests(unittest.TestCase):
    def setUp(self):
        torch.manual_seed(0)
        self.config = small_config(ranking_ks=(3, 5))
        self.model = CompatibilityClassifier.from_config(self.config)
        self.data = SyntheticCompatibilityDataset()

    def test_compares_classifier_with_baseline(self):
        results = evaluate(self.model, self.data, self.config)
        self.assertEqual(set(results), {"classifier", "cosine_baseline"})
        self.assertTrue({"mae", "mse", "rmse", "spearman", "ndcg@3", "recall@5", "precision@5"} <= set(results["classifier"]))
        self.assertNotIn("mae", results["cosine_baseline"])
        self.assertIn("ndcg@3", results["cosine_baseline"])
        self.assertEqual(results["classifier"]["num_groups"], 4)
        self.assertIn("cosine_baseline", format_comparison(results))

    def test_baseline_ranks_synthetic_data_well(self):
        results = evaluate(None, self.data, self.config)
        self.assertEqual(set(results), {"cosine_baseline"})
        self.assertGreater(results["cosine_baseline"]["spearman"], 0.9)

    def test_ranking_metrics_skipped_without_groups(self):
        results = evaluate(self.model, TupleDataset(self.data), self.config)
        self.assertNotIn("ndcg@3", results["classifier"])
        self.assertIn("spearman", results["classifier"])


class VariantEvaluationTests(unittest.TestCase):
    def setUp(self):
        torch.manual_seed(0)
        self.config = small_config(ranking_ks=(3, 5))
        self.models = {v: CompatibilityClassifier.from_config(self.config.with_variant(v)) for v in MODEL_VARIANTS}
        self.data = SyntheticCompatibilityDataset()

    def test_compares_all_variants_with_the_same_metrics(self):
        results = evaluate(self.models, self.data, self.config)
        self.assertEqual(set(results), {"flat", "late_fusion", "residual", "cosine_baseline"})
        shared = {"spearman", "ndcg@3", "ndcg@5", "precision@3", "recall@5", "num_groups"}
        for name, values in results.items():
            with self.subTest(name=name):
                self.assertTrue(shared <= set(values))
        for name in ("flat", "late_fusion", "residual"):
            self.assertTrue({"mae", "rmse", "alpha", "mean_abs_correction"} <= set(results[name]))
        self.assertEqual(results["residual"]["alpha"], 1.0)
        self.assertEqual(results["flat"]["alpha"], 0.0)
        table = format_comparison(results)
        self.assertIn("residual", table)
        self.assertIn("mean_learned_correction", table)

    def test_untrained_residual_model_ranks_like_the_baseline(self):
        results = evaluate(self.models, self.data, self.config)
        for metric in ("spearman", "ndcg@3", "precision@5"):
            self.assertAlmostEqual(results["residual"][metric], results["cosine_baseline"][metric], places=5)
        self.assertEqual(results["residual"]["mean_abs_correction"], 0.0)

    def test_components_are_exposed_per_sample(self):
        model = self.models["residual"]
        _randomize_output_layer(model)
        scored = score_dataset(self.data, {"residual": model}, device="cpu")
        comps = scored.components["residual"]
        n = len(self.data)
        for key in ("positive_cosine", "negative_cosine", "cosine_difference", "baseline_score", "learned_correction", "final_score"):
            self.assertEqual(comps[key].shape, (n,), key)
        np.testing.assert_allclose(comps["cosine_difference"], comps["positive_cosine"] - comps["negative_cosine"], atol=1e-6)
        np.testing.assert_allclose(comps["baseline_score"], scored.baseline_scores, atol=1e-6)
        np.testing.assert_allclose(
            comps["final_score"], comps["alpha"] * comps["baseline_score"] + comps["learned_correction"], atol=1e-6
        )
        np.testing.assert_allclose(scored.model_scores["residual"], comps["final_score"])

    def test_summarize_components(self):
        summary = summarize_components(
            {
                "alpha": np.array(2.0),
                "baseline_score": np.array([0.5, -0.5]),
                "learned_correction": np.array([0.2, -0.4]),
            }
        )
        self.assertEqual(summary["alpha"], 2.0)
        self.assertAlmostEqual(summary["mean_baseline_score"], 0.0)
        self.assertAlmostEqual(summary["mean_learned_correction"], -0.1)
        self.assertAlmostEqual(summary["mean_abs_correction"], 0.3)
        self.assertAlmostEqual(summary["std_weighted_baseline"], 1.0)
        self.assertAlmostEqual(summary["std_learned_correction"], 0.3)
        self.assertAlmostEqual(summary["correction_to_baseline_std_ratio"], 0.3)

    def test_constant_correction_offset_does_not_count_as_overriding(self):
        summary = summarize_components(
            {"alpha": np.array(1.0), "baseline_score": np.array([0.5, -0.5]), "learned_correction": np.array([3.0, 3.0])}
        )
        self.assertEqual(summary["correction_to_baseline_std_ratio"], 0.0)


class MetricTests(unittest.TestCase):
    def test_error_metrics(self):
        pred, target = [1.0, 2.0, 4.0], [1.0, 3.0, 2.0]
        self.assertAlmostEqual(metrics.mae(pred, target), 1.0)
        self.assertAlmostEqual(metrics.mse(pred, target), 5 / 3)
        self.assertAlmostEqual(metrics.rmse(pred, target), math.sqrt(5 / 3))

    def test_spearman(self):
        self.assertAlmostEqual(metrics.spearman([1, 2, 3, 4], [10, 20, 30, 40]), 1.0)
        self.assertAlmostEqual(metrics.spearman([1, 2, 3, 4], [4, 3, 2, 1]), -1.0)
        # Ties: ranks of [1, 2, 2, 3] are [0, 1.5, 1.5, 3]; value from scipy.stats.spearmanr.
        self.assertAlmostEqual(metrics.spearman([1, 2, 2, 3], [1, 3, 2, 4]), 0.9486832980505138)
        self.assertTrue(math.isnan(metrics.spearman([1, 1, 1], [1, 2, 3])))

    def test_ndcg(self):
        rel = [3.0, 2.0, 0.0, 1.0]
        self.assertAlmostEqual(metrics.ndcg_at_k([4, 3, 2, 1], [3, 2, 1, 0], 4), 1.0)
        # scores [0,1,2,3] rank items 3, 2, 1: relevances 1, 0, 2
        dcg = 1 / np.log2(2) * 1 + 1 / np.log2(3) * 0 + 1 / np.log2(4) * 2
        ideal = 3 + 2 / np.log2(3) + 1 / np.log2(4)
        self.assertAlmostEqual(metrics.ndcg_at_k([0, 1, 2, 3], rel, 3), dcg / ideal)
        with self.assertRaises(ValueError):
            metrics.ndcg_at_k([1, 2], [-1, 1], 2)

    def test_precision_and_recall(self):
        scores, relevant = [0.9, 0.8, 0.7, 0.1], [1, 0, 1, 1]
        self.assertAlmostEqual(metrics.precision_at_k(scores, relevant, 2), 0.5)
        self.assertAlmostEqual(metrics.recall_at_k(scores, relevant, 2), 1 / 3)
        self.assertAlmostEqual(metrics.precision_at_k(scores, relevant, 10), 0.75)  # K > n uses n
        self.assertAlmostEqual(metrics.recall_at_k(scores, relevant, 10), 1.0)
        self.assertTrue(math.isnan(metrics.recall_at_k(scores, [0, 0, 0, 0], 2)))

    def test_ranking_metrics_group_by_id(self):
        scores = [0.9, 0.1, 0.2, 0.8, 0.5]
        labels = [1.0, 0.0, 1.0, 0.0, 1.0]
        groups = ["a", "a", "b", "b", "c"]  # "c" has one item and is skipped
        result = metrics.ranking_metrics(scores, labels, groups, ks=(1,))
        self.assertEqual(result["num_groups"], 2)
        self.assertAlmostEqual(result["precision@1"], 0.5)  # a ranks correctly, b does not
        self.assertAlmostEqual(result["recall@1"], 0.5)
        self.assertAlmostEqual(result["ndcg@1"], 0.5)


class CompatibilityModelAdapterTests(unittest.TestCase):
    """`ClassifierCompatibilityModel`: the classifier behind the `CompatibilityModel` interface."""

    def setUp(self):
        g = torch.Generator().manual_seed(0)
        pos, neg, *events = torch.randn(6, DIM, generator=g).numpy()
        self.user = UserEmbedding(positive=Embedding(pos, "v"), negative=Embedding(neg, "v"))
        self.events = [Embedding(e, "v", source_id=f"e{i}") for i, e in enumerate(events)]

    def test_untrained_residual_model_matches_cosine_model(self):
        # Zero-initialized correction + alpha=1 -> exactly the cosine baseline.
        config = small_config(architecture="late_fusion", use_residual_baseline=True, negative_weight=0.5)
        model = ClassifierCompatibilityModel(CompatibilityClassifier.from_config(config))
        expected = CosineCompatibilityModel(negative_weight=0.5).score_many(self.user, self.events)
        scores = [r.score for r in model.score_many(self.user, self.events)]
        np.testing.assert_allclose(scores, [r.score for r in expected], atol=1e-5)
        self.assertEqual(model.version, "classifier-v1")
        self.assertIsNone(model.score_range)

    def test_missing_negative_is_zero_vector(self):
        config = small_config(architecture="late_fusion", use_residual_baseline=True)
        model = ClassifierCompatibilityModel(CompatibilityClassifier.from_config(config))
        results = model.score_many(UserEmbedding(positive=self.user.positive), self.events)
        for result in results:
            self.assertEqual(result.metadata["component_scores"]["negative"], 0.0)
            self.assertAlmostEqual(result.score, result.metadata["component_scores"]["positive"], places=5)

    def test_from_bce_checkpoint_returns_probabilities(self):
        config = small_config(loss="bce")
        with tempfile.TemporaryDirectory() as tmp:
            path = save_checkpoint(
                Path(tmp) / "model.pt", CompatibilityClassifier.from_config(config), config, epoch=0, val_loss=0.0
            )
            model = ClassifierCompatibilityModel.from_checkpoint(path, version="classifier-v2")
        self.assertEqual((model.version, model.embedding_dim, model.score_range), ("classifier-v2", DIM, (0.0, 1.0)))
        for result in model.score_many(self.user, self.events):
            self.assertTrue(0.0 <= result.score <= 1.0)
            self.assertAlmostEqual(result.score, 1 / (1 + math.exp(-result.metadata["component_scores"]["raw"])), places=5)

    def test_rejects_wrong_dimension(self):
        model = ClassifierCompatibilityModel(CompatibilityClassifier.from_config(small_config()))
        user = UserEmbedding(positive=Embedding(np.ones(DIM + 1), "v"))
        with self.assertRaisesRegex(IncompatibleEmbeddingsError, f"expects dimension {DIM}"):
            model.score_many(user, [Embedding(np.ones(DIM + 1), "v")])


if __name__ == "__main__":
    unittest.main()
