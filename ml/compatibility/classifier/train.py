"""Training loop, early stopping and checkpointing for the compatibility classifier.

Data comes in as any `torch.utils.data.Dataset` or `DataLoader` whose batches
carry `positive_embedding`, `negative_embedding`, `event_embedding` and
`label` (see `unpack_batch`). Nothing here knows where those tensors came from.
"""

import copy
import logging
import random
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Mapping, Sequence

import numpy as np
import torch
from torch import nn
from torch.utils.data import DataLoader, Dataset

from .config import ClassifierConfig
from .model import CompatibilityClassifier

logger = logging.getLogger(__name__)

POSITIVE_KEY = "positive_embedding"
NEGATIVE_KEY = "negative_embedding"
EVENT_KEY = "event_embedding"
LABEL_KEY = "label"

Batch = tuple[torch.Tensor, torch.Tensor, torch.Tensor, torch.Tensor]


# --- setup -----------------------------------------------------------------


def set_seed(seed: int) -> None:
    """Seed Python, NumPy and torch (all devices) for reproducible runs."""
    random.seed(seed)
    np.random.seed(seed)
    torch.manual_seed(seed)


def resolve_device(device: str | torch.device = "auto") -> torch.device:
    """"auto" picks cuda, then mps, then cpu; anything else is passed to `torch.device`."""
    if isinstance(device, torch.device):
        return device
    if device != "auto":
        return torch.device(device)
    if torch.cuda.is_available():
        return torch.device("cuda")
    if torch.backends.mps.is_available():
        return torch.device("mps")
    return torch.device("cpu")


def build_loss(config: ClassifierConfig) -> nn.Module:
    if config.loss == "huber":
        return nn.HuberLoss(delta=config.huber_delta)
    if config.loss == "mse":
        return nn.MSELoss()
    if config.loss == "bce":
        return nn.BCEWithLogitsLoss()
    raise ValueError(f"unknown loss {config.loss!r}")


# Parameters excluded from weight decay: decay would pull the residual alpha toward 0.
NO_WEIGHT_DECAY = frozenset({"alpha"})


def build_optimizer(model: nn.Module, config: ClassifierConfig) -> torch.optim.Optimizer:
    decay, no_decay = [], []
    for name, param in model.named_parameters():
        (no_decay if name in NO_WEIGHT_DECAY else decay).append(param)
    groups = [{"params": decay, "weight_decay": config.weight_decay}]
    if no_decay:
        groups.append({"params": no_decay, "weight_decay": 0.0})
    return torch.optim.AdamW(groups, lr=config.learning_rate)


def as_dataloader(
    data: Dataset | DataLoader, batch_size: int, shuffle: bool, seed: int | None = None
) -> DataLoader:
    """Pass DataLoaders through untouched; wrap Datasets (seeded shuffle if requested)."""
    if isinstance(data, DataLoader):
        return data
    generator = torch.Generator().manual_seed(seed) if shuffle and seed is not None else None
    return DataLoader(data, batch_size=batch_size, shuffle=shuffle, generator=generator)


def unpack_batch(batch: Mapping[str, Any] | tuple | list, device: torch.device | None = None) -> Batch:
    """Return `(positive, negative, event, label)` as float tensors on `device`.

    Accepts a mapping with the four standard keys (extra keys such as
    `user_id` are ignored) or a sequence whose first four items are in that
    order. Embeddings are `[B, D]`, labels `[B]`.
    """
    if isinstance(batch, Mapping):
        items = (batch[POSITIVE_KEY], batch[NEGATIVE_KEY], batch[EVENT_KEY], batch[LABEL_KEY])
    else:
        items = tuple(batch[:4])
    pos, neg, event, label = (torch.as_tensor(x).float() for x in items)
    label = label.reshape(-1)
    if device is not None:
        pos, neg, event, label = (x.to(device) for x in (pos, neg, event, label))
    return pos, neg, event, label


# --- epochs ----------------------------------------------------------------


def train_one_epoch(
    model: nn.Module,
    loader: DataLoader,
    loss_fn: nn.Module,
    optimizer: torch.optim.Optimizer,
    device: torch.device,
    on_batch_end: Callable[[float], None] | None = None,
) -> float:
    """One pass over `loader`; returns the sample-weighted mean training loss.

    `on_batch_end`, if given, receives each batch's loss (e.g. for live logging).
    """
    model.train()
    total, count = 0.0, 0
    for batch in loader:
        pos, neg, event, label = unpack_batch(batch, device)
        optimizer.zero_grad()
        loss = loss_fn(model(pos, neg, event), label)
        loss.backward()
        optimizer.step()
        batch_loss = loss.item()
        total += batch_loss * label.shape[0]
        count += label.shape[0]
        if on_batch_end is not None:
            on_batch_end(batch_loss)
    if count == 0:
        raise ValueError("training loader produced no samples")
    return total / count


@torch.no_grad()
def validate(model: nn.Module, loader: DataLoader, loss_fn: nn.Module, device: torch.device) -> float:
    """Sample-weighted mean loss over `loader` in eval mode."""
    model.eval()
    total, count = 0.0, 0
    for batch in loader:
        pos, neg, event, label = unpack_batch(batch, device)
        total += loss_fn(model(pos, neg, event), label).item() * label.shape[0]
        count += label.shape[0]
    if count == 0:
        raise ValueError("validation loader produced no samples")
    return total / count


# --- early stopping and checkpoints -----------------------------------------


class EarlyStopping:
    """Tracks the best validation loss; `should_stop` after `patience` epochs without improvement.

    An epoch improves when its loss is below `best - min_delta`. With
    `patience=0`, training stops at the first epoch that does not improve.
    """

    def __init__(self, patience: int, min_delta: float = 0.0) -> None:
        self.patience = patience
        self.min_delta = min_delta
        self.best = float("inf")
        self.epochs_without_improvement = 0

    def step(self, val_loss: float) -> bool:
        """Record an epoch's loss; returns True if it is a new best."""
        if val_loss < self.best - self.min_delta:
            self.best = val_loss
            self.epochs_without_improvement = 0
            return True
        self.epochs_without_improvement += 1
        return False

    @property
    def should_stop(self) -> bool:
        return self.epochs_without_improvement >= self.patience


def save_checkpoint(
    path: str | Path,
    model: nn.Module,
    config: ClassifierConfig,
    *,
    epoch: int,
    val_loss: float,
    optimizer: torch.optim.Optimizer | None = None,
    history: "TrainingHistory | None" = None,
) -> Path:
    """Write weights plus everything needed to rebuild the model (`load_checkpoint`)."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    torch.save(
        {
            "model_state": {k: v.detach().cpu() for k, v in model.state_dict().items()},
            "config": config.to_dict(),
            "epoch": epoch,
            "val_loss": val_loss,
            "optimizer_state": optimizer.state_dict() if optimizer is not None else None,
            "history": history.to_dict() if history is not None else None,
        },
        path,
    )
    return path


def load_checkpoint(
    path: str | Path, device: str | torch.device = "cpu"
) -> tuple[CompatibilityClassifier, dict[str, Any]]:
    """Rebuild a `CompatibilityClassifier` from `save_checkpoint` output, in eval mode.

    Returns the model and the raw checkpoint dict (config, epoch, val_loss,
    optimizer_state, history).
    """
    device = resolve_device(device)
    checkpoint = torch.load(path, map_location=device, weights_only=True)
    config_dict, state = checkpoint["config"], checkpoint["model_state"]
    if "architecture" not in config_dict:
        # Saved before backbones existed: the model was a bare flat MLP.
        config_dict = {**config_dict, "architecture": "flat", "use_residual_baseline": False}
        state = {f"backbone.{k}": v for k, v in state.items()}
    model = CompatibilityClassifier.from_config(ClassifierConfig.from_dict(config_dict))
    model.load_state_dict(state)
    return model.to(device).eval(), checkpoint


# --- entry point -----------------------------------------------------------


@dataclass
class TrainingHistory:
    train_loss: list[float] = field(default_factory=list)
    val_loss: list[float] = field(default_factory=list)
    best_epoch: int = -1
    best_val_loss: float = float("inf")
    stopped_early: bool = False
    selection_metric: str = "val_loss"
    val_selection: list[float] = field(default_factory=list)
    """Per-epoch value of `selection_metric` (equal to `val_loss` in the default mode)."""

    @property
    def epochs_run(self) -> int:
        return len(self.train_loss)

    def to_dict(self) -> dict[str, Any]:
        return {
            "train_loss": list(self.train_loss),
            "val_loss": list(self.val_loss),
            "best_epoch": self.best_epoch,
            "best_val_loss": self.best_val_loss,
            "stopped_early": self.stopped_early,
            "selection_metric": self.selection_metric,
            "val_selection": list(self.val_selection),
        }


class TrainingCallback:
    """Optional hooks into `train` for logging and monitoring; override what you need."""

    def on_batch_end(self, step: int, loss: float) -> None:
        """After every optimizer step; `step` counts from 0 across epochs."""

    def on_epoch_end(self, epoch: int, history: TrainingHistory, model: nn.Module, improved: bool) -> None:
        """After validation; `history` already includes this epoch."""


@dataclass
class TrainingResult:
    model: nn.Module
    """The model with its best-validation weights loaded, in eval mode."""
    history: TrainingHistory
    checkpoint_path: Path | None
    device: torch.device


def train(
    config: ClassifierConfig,
    train_data: Dataset | DataLoader,
    val_data: Dataset | DataLoader,
    checkpoint_path: str | Path | None = None,
    model: nn.Module | None = None,
    callbacks: Sequence[TrainingCallback] | None = None,
    val_score_fn: Callable[[nn.Module], float] | None = None,
) -> TrainingResult:
    """Train with early stopping on validation loss and keep the best weights.

    With `config.selection_metric` other than "val_loss", `val_score_fn(model)` supplies that
    validation metric each epoch (higher is better) and it replaces validation loss for
    picking the best epoch and for early stopping. `callbacks` receive per-batch and
    per-epoch hooks (see `TrainingCallback`).

    `train_data` / `val_data` may be Datasets (wrapped using
    `config.batch_size`; training is shuffled with a seeded generator) or
    ready-made DataLoaders (used as-is).

    `model` defaults to `CompatibilityClassifier.from_config(config)`. Any
    module with the same `forward(positive, negative, event) -> [B]` contract
    can be passed instead; note `load_checkpoint` only rebuilds the default
    architecture.

    The best epoch's weights are kept in memory and, if `checkpoint_path` is
    given, written there each time validation improves. Epochs are 0-indexed.
    """
    set_seed(config.seed)
    device = resolve_device(config.device)
    model = (model if model is not None else CompatibilityClassifier.from_config(config)).to(device)

    train_loader = as_dataloader(train_data, config.batch_size, shuffle=True, seed=config.seed)
    val_loader = as_dataloader(val_data, config.batch_size, shuffle=False)
    loss_fn = build_loss(config)
    optimizer = build_optimizer(model, config)
    stopper = EarlyStopping(config.early_stopping_patience, config.early_stopping_min_delta)
    history = TrainingHistory(selection_metric=config.selection_metric)
    best_state: dict[str, torch.Tensor] | None = None
    select_on_loss = config.selection_metric == "val_loss"
    if not select_on_loss and val_score_fn is None:
        raise ValueError(f"selection_metric={config.selection_metric!r} needs a val_score_fn")
    callbacks = list(callbacks or [])
    step = 0

    def after_batch(loss: float) -> None:
        nonlocal step
        for cb in callbacks:
            cb.on_batch_end(step, loss)
        step += 1

    for epoch in range(config.max_epochs):
        train_loss = train_one_epoch(model, train_loader, loss_fn, optimizer, device,
                                     on_batch_end=after_batch if callbacks else None)
        val_loss = validate(model, val_loader, loss_fn, device)
        history.train_loss.append(train_loss)
        history.val_loss.append(val_loss)
        selection = val_loss if select_on_loss else float(val_score_fn(model))
        history.val_selection.append(selection)

        # EarlyStopping minimizes, so higher-is-better metrics are negated.
        improved = stopper.step(selection if select_on_loss else -selection)
        if improved:
            history.best_epoch, history.best_val_loss = epoch, val_loss
            best_state = copy.deepcopy({k: v.detach().cpu() for k, v in model.state_dict().items()})
            if checkpoint_path is not None:
                save_checkpoint(
                    checkpoint_path, model, config, epoch=epoch, val_loss=val_loss, optimizer=optimizer, history=history
                )
        logger.info(
            "epoch %d: train_loss=%.5f val_loss=%.5f%s%s", epoch, train_loss, val_loss,
            "" if select_on_loss else f" val_{config.selection_metric}={selection:.5f}",
            " (best)" if improved else "",
        )
        for cb in callbacks:
            cb.on_epoch_end(epoch, history, model, improved)

        if stopper.should_stop:
            history.stopped_early = True
            logger.info("early stopping after epoch %d; best epoch %d", epoch, history.best_epoch)
            break

    if best_state is not None:
        model.load_state_dict(best_state)
    model.eval()
    return TrainingResult(
        model=model,
        history=history,
        checkpoint_path=Path(checkpoint_path) if checkpoint_path is not None else None,
        device=device,
    )
