# Classifier Training Procedure

## Overview and inputs

The classifier trains only on precomputed embeddings and labels; the encoder is frozen and never part of training. Code lives in `ml/compatibility/classifier/` (`config.py`, `features.py`, `model.py`, `train.py`, `evaluate.py`, `metrics.py`).

Each training example is a mapping with these keys (`train.unpack_batch`). Extra keys are ignored by training; `user_id` is read by evaluation to group candidates for ranking metrics.

```
positive_embedding: Tensor[D]
negative_embedding: Tensor[D]
event_embedding:    Tensor[D]
label:              float
user_id:            hashable   # optional, needed for NDCG / Precision / Recall @ K
```

Embeddings come from `ml/data/embed.py`: `Qwen/Qwen3-Embedding-0.6B` (D = 1024), float32, L2-normalized, stored as `.npy` matrices per split. A user with no dislikes gets a zero `negative_embedding`; its negative cosine is then 0, so the baseline reduces to plain positive cosine.

`train()` accepts any `torch.utils.data.Dataset` or `DataLoader` that yields this shape. A Dataset is wrapped with `batch_size` and a seeded shuffle; a DataLoader is used as-is.

## Data splits

Split by user/persona, 80 / 10 / 10 train / validation / test, so no user appears in more than one split. Splitting randomly by (user, event) pair would leak each user's preferences into validation and inflate every metric.

- Seed the split and save the user-id lists for each split next to the embeddings so every variant sees identical data.
- The event dataset (`karthiksing05/sidequestz-event-embedding-text`) ships its own 98k / 2k train / test split. Keep test events out of training triples too, so test measures generalization to unseen users and unseen events.
- Each validation and test user needs several candidate events, or ranking metrics are skipped (groups smaller than 2 are dropped; `num_groups` reports how many were scored).
- Never use the test set for model, architecture or hyperparameter selection.

No split code exists yet; see [Implementation status](#implementation-status).

## Model architecture

`CompatibilityClassifier` wraps one of two backbones and optionally adds a residual cosine prior; the default is the late-fusion backbone with the residual enabled. It returns raw scores `[B]` with no sigmoid.

**Interaction features** (`build_embedding_interaction_features`, width 7D = 7,168):

```
[pos, neg, event, |pos - event|, |neg - event|, pos * event, neg * event]
```

**Similarity features** (`compute_similarity_features`, width 3). The difference is unweighted; lambda only enters the baseline.

```
[cos(pos, event), cos(neg, event), cos(pos, event) - cos(neg, event)]
```

**Backbones**

| Architecture | Input | Layers (defaults) |
| --- | --- | --- |
| `late_fusion` (default) | 7D interactions, then concat 3 cosines | Branch: 7168 → 256 (ReLU, dropout 0.2) → 64 (ReLU). Head: 67 → 32 (ReLU) → 1 |
| `flat` | 7D interactions + 2 cosines in one vector | 7170 → 512 (dropout 0.2) → 128 (dropout 0.1) → 1 |

**Residual prior** (`use_residual_baseline=True`):

```
score = alpha * (cos(pos, event) - lambda * cos(neg, event)) + backbone(pos, neg, event)
```

- The backbone's last Linear layer is zero-initialized, so an untrained model scores exactly `initial_alpha * baseline`. Training starts from the cosine baseline and learns a correction.
- `alpha` is an `nn.Parameter` when `learnable_alpha` (default), else a fixed buffer. It is excluded from weight decay so AdamW does not pull it toward 0.
- `lambda` is `negative_weight` (default 1.0), shared by the residual path and standalone baseline evaluation.

**Variants compared** (`MODEL_VARIANTS`, applied with `config.with_variant(name)`):

| Variant | architecture | use_residual_baseline |
| --- | --- | --- |
| `flat` | flat | False |
| `late_fusion` | late_fusion | False |
| `residual` | late_fusion | True |

The fourth comparison point, the cosine baseline, needs no model.

## Loss and optimizer

Huber loss with AdamW is the default; every value below is a `ClassifierConfig` field.

| `loss` | Module | Use for |
| --- | --- | --- |
| `huber` (default) | `nn.HuberLoss(delta=huber_delta)`, delta 1.0 | Continuous compatibility labels |
| `mse` | `nn.MSELoss()` | Comparison against Huber |
| `bce` | `nn.BCEWithLogitsLoss()` | Binary 0/1 labels; evaluation applies a sigmoid to scores |

| Field | Default |
| --- | --- |
| `learning_rate` | 1e-3 |
| `weight_decay` | 1e-4 (alpha exempt) |
| `batch_size` | 128 |
| `max_epochs` | 50 |
| `early_stopping_patience` | 5 |
| `early_stopping_min_delta` | 0.0 |
| `seed` | 0 (seeds Python, NumPy, torch and the shuffle) |
| `device` | `auto`: cuda, then mps, then cpu |

With Huber delta 1.0, labels should sit roughly in [0, 1]; rescale if they use a wider range, or Huber behaves like L1 almost everywhere.

## Training loop and checkpointing

`train(config, train_data, val_data, checkpoint_path)` runs the loop, selects the epoch with the lowest validation loss, and returns the model with those weights restored, in eval mode.

Each epoch (0-indexed):

1. `model.train()`; for each batch: move tensors to the device, `zero_grad`, forward, loss, `backward`, `optimizer.step`.
2. `model.eval()` and compute the sample-weighted mean validation loss under `torch.no_grad()`.
3. If validation loss < best − `min_delta`: record it as the best epoch, copy the weights in memory, write the checkpoint (if a path was given), reset the counter.
4. Otherwise increment the counter.
5. Stop once the counter reaches `early_stopping_patience` (5 epochs in a row without improvement).

After the loop the best in-memory weights are loaded back. `TrainingResult` carries `model`, `history` (per-epoch train and validation loss, `best_epoch`, `best_val_loss`, `stopped_early`), `checkpoint_path` and `device`.

Selection uses validation loss only. Ranking metrics are not computed per epoch; run `evaluate()` on the validation set afterwards to compare variants and hyperparameters.

**Checkpoint contents** (`save_checkpoint`):

| Key | Contents |
| --- | --- |
| `model_state` | State dict on CPU; includes alpha |
| `optimizer_state` | AdamW state dict |
| `config` | `ClassifierConfig.to_dict()` |
| `epoch` | Best epoch |
| `val_loss` | Best validation loss |
| `history` | `TrainingHistory.to_dict()` up to that epoch |

`load_checkpoint(path, device)` rebuilds the model from the saved config and returns `(model, checkpoint)` in eval mode. It also loads older flat-MLP checkpoints saved before backbones existed.

## Metrics and baseline comparison

`evaluate(models, data, config)` scores the cosine baseline and every model on the same data in one pass; ranking metrics decide between models because the product task is event ranking.

| Metric | Learned models | Cosine baseline | Notes |
| --- | --- | --- | --- |
| Training / validation loss | Yes (`history`) | No | Per epoch, from `train()` |
| MAE, MSE, RMSE | Yes | No | Baseline scores span [−1 − λ, 1 + λ], not the label scale |
| Spearman | Yes | Yes | Over all samples; ties get average ranks |
| NDCG@5, NDCG@10 | Yes | Yes | Per user, labels as graded relevance, averaged over users |
| Precision@5/10, Recall@5/10 | Yes | Yes | Relevant = label ≥ `relevance_threshold` (0.5) |

Ranking metrics appear only when every batch carries `user_id`; users with fewer than 2 candidates are skipped, and users with no relevant events are left out of NDCG and Recall.

Models with a residual also report score diagnostics: `alpha`, mean baseline, mean and mean-absolute correction, and `correction_to_baseline_std_ratio`. A ratio well below 1 means the network refines the cosine prior; well above 1 means it overrides it.

**Comparison procedure**

1. Evaluate the cosine baseline alone first: `evaluate(None, val_data, config)`.
2. Train `flat`, `late_fusion` and `residual` with `config.with_variant(name)`, each to its own checkpoint.
3. `evaluate({"flat": m1, "late_fusion": m2, "residual": m3}, val_data, config)` and print with `format_comparison`.
4. Pick the variant and hyperparameters on validation NDCG@10 first, then Recall@10, then Spearman.

The baseline's lambda must match between the classifier config (default 1.0) and the production `CosineCompatibilityModel` (default 0.5) when comparing to what ships. Tune lambda on validation for the baseline too, so the comparison is fair.

## Final evaluation and recommended run

The test set is evaluated once, after the architecture and hyperparameters are frozen on validation.

1. `model, ckpt = load_checkpoint(best_path)`.
2. `results = evaluate(model, test_data, ClassifierConfig.from_dict(ckpt["config"]))`; this scores the cosine baseline on the same data.
3. Record `format_comparison(results)`, the config, the checkpoint's `epoch` and `val_loss`, and the embedding model name.
4. Do not tune anything further based on test results.

**Recommended first run** (all are current `ClassifierConfig` defaults, so only `embedding_dim` is required):

```python
config = ClassifierConfig(embedding_dim=1024)   # late_fusion, residual, learnable alpha = 1.0,
                                                # huber, AdamW lr 1e-3 wd 1e-4, batch 128,
                                                # 50 epochs, patience 5, lambda 1.0, seed 0
print(format_comparison(evaluate(None, val_data, config)))        # baseline first
result = train(config, train_data, val_data, "runs/residual/best.pt")
print(format_comparison(evaluate(result.model, val_data, config)))
```

Then repeat with `config.with_variant("flat")` and `with_variant("late_fusion")`, and try `loss="mse"` for comparison.

## Implementation status

The model, loop, checkpointing, metrics and baseline comparison are built and tested; the data side between embeddings and `train()` is not.

| Piece | Status | Where |
| --- | --- | --- |
| Features, backbones, residual alpha | Done | `classifier/features.py`, `model.py` |
| Training loop, early stopping, checkpoints | Done | `classifier/train.py` |
| Metrics, baseline comparison, diagnostics | Done | `classifier/metrics.py`, `evaluate.py` |
| Unit tests | Done | `classifier/tests/test_classifier.py` |
| Event embeddings | Done, not yet run on full data | `ml/data/embed.py`, `embed.sbatch` |
| User dataset (positive / negative text) | Missing | `embed.py` takes `--users-repo`, none exists yet |
| Labels for (user, event) pairs | Missing | No labeling source defined |
| Triple Dataset + persona split | Missing | Needs to join user and event embeddings by id |
| Training / eval CLI or sbatch | Missing | Only the Python API exists |

**Open questions**

- [ ] Where do labels come from, and are they continuous (Huber) or binary (BCE)?
- [ ] How many candidate events per user in validation and test? At least 10 for NDCG@10 to be meaningful.
- [ ] Should model selection move from validation loss to validation NDCG@10 inside `train()`?
