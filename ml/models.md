# SideQuests compatibility model

This is the model that ranks events for a user in SideQuests. It takes a user's likes, their dislikes and
an event, each written in the app's eight-section format and embedded with a frozen
`Qwen/Qwen3-Embedding-0.6B`, and predicts how much that user would enjoy that event. It was trained by
following [`training.md`](training.md) on the synthetic `users` data described in
[`dataset.md`](dataset.md).

| | |
|---|---|
| Model | [`karthiksing05/sidequestz-compatibility-classifier`](https://huggingface.co/karthiksing05/sidequestz-compatibility-classifier); the final model is `final/best.pt`. A CPU copy with only the weights and config is bundled at [`checkpoints/compatibility_classifier.pt`](checkpoints/compatibility_classifier.pt), and the API loads it by default |
| W&B report | [SideQuestz Compatibility Classifier: Training and Results](https://wandb.ai/karthiksing05-Independent/sidequestz-compatibility/reports/SideQuestz-Compatibility-Classifier-Training-and-Results--VmlldzoxODAxMDEwMQ?accessToken=wg1fgpvz80y948xex5tw1i9c40o40usgbwglyvrmj3mj7to264gr6avvkt3a4l2z) |
| W&B project | [`sidequestz-compatibility`](https://wandb.ai/karthiksing05-Independent/sidequestz-compatibility), group `recipe-20260926-031750-compat-train`: all 24 runs (λ sweep, 21 training runs, selection, final test) |
| Training data | [`karthiksing05/sidequestz-event-embedding-text`](https://huggingface.co/datasets/karthiksing05/sidequestz-event-embedding-text), config `users` |
| Code | [`compatibility/classifier/`](compatibility/classifier/) (model, training, evaluation) |

## At a glance

| | |
|---|---|
| Run | `p2-late_fusion-select-ndcg` |
| Model | `CompatibilityClassifier` with the `late_fusion` backbone and no residual cosine prior |
| Parameters | 1,853,921 (7.4 MB of weights) |
| Encoder (frozen, not included) | `Qwen/Qwen3-Embedding-0.6B`: 1024-d, L2-normalized, no prompt, `max_seq_length` 512 |
| Output | one score per (user, event); higher = better fit. Trained to predict rating ÷ 3, so roughly 0–1; not a probability |
| Selected by | the best of 21 runs on validation NDCG@10 (then Recall@10, Spearman). The epoch (7) was also picked by validation NDCG@10 |
| Test | NDCG@10 **0.897**, Recall@10 **0.883**, Spearman **0.697**. The tuned cosine baseline gets 0.849 / 0.817 / 0.501 |
| Trained | 2026-09-26 on MPCDF Raven, one A100. 13 epochs, under 1 min; the whole recipe (21 runs) took 12 min on 4 GPUs |

## Inputs and output

`model(positive, negative, event)` takes three `[B, 1024]` float tensors and returns `[B]` scores:

- `positive`: the embedding of the user's likes (`positive_text`).
- `negative`: the embedding of the user's dislikes (`negative_text`); a **zero vector** when the user has
  none.
- `event`: the embedding of the event's eight-section text (`embedding_text`).

All three texts must be in the eight-section format (Interests, Activities, Social, Environment, Pace, Cost,
Timing, Experience) from [`description_generation.md`](description_generation.md), because that is all the
model has seen:

- **Events:** convert raw listings with the spec's section-2 prompt; `datagen/generate.py` does this with
  Qwen3.5-9B.
- **Users:** write likes and dislikes in the same format. The `users` config and `datagen/generate_users.py`
  show how.

Texts in other formats still rank sensibly but score less decisively. For the same jazz-loving,
crowd-averse user and a jazz event, eight-section texts gave **0.93**. The app's current `TextUserEncoder` /
`TextEventEncoder` texts (`Interests / Preferences / Dislikes` and `Name / Description / Category / Price`)
gave **0.49**.

Reading scores: they approximate the judge's rating ÷ 3, so ≥ 0.5 roughly means "rating 2 or 3", a good
match. They're fitted to that scale by regression, but the model was selected for ranking, so compare
scores within one user's candidates rather than across users.

## Architecture

```
pos, neg, event                           each [B, 1024], unit norm (neg may be all zeros)
├─ interaction features                   [pos, neg, event, |pos−event|, |neg−event|, pos⊙event, neg⊙event] → 7168
│    Linear 7168→256 · ReLU · Dropout 0.2 · Linear 256→64 · ReLU                (embedding branch)
├─ similarity features                    [cos(pos,event), cos(neg,event), cos(pos,event)−cos(neg,event)] → 3
└─ concat 64 + 3 = 67 → Linear 67→32 · ReLU → Linear 32→1 → score               (final head)
```

| Weight | Shape |
|---|---|
| `backbone.embedding_branch.0` | 256 × 7168 (+ 256 bias) |
| `backbone.embedding_branch.3` | 64 × 256 (+ 64) |
| `backbone.head.0` | 32 × 67 (+ 32) |
| `backbone.head.2` | 1 × 32 (+ 1) |

## Training

**Data** (`users` config). Users and their event pools are disjoint across splits, so test users and test
events never appear in training.

| Split | Users | Pairs | Distinct events | Mean label | Relevant (label ≥ 0.5) | Users without dislikes |
|---|---|---|---|---|---|---|
| train | 8,000 | 160,000 | 64,988 | 0.503 | 45.6% | 15.9% |
| validation | 1,000 | 20,000 | 1,991 | 0.402 | 30.7% | 13.6% |
| test | 1,000 | 20,000 | 1,995 | 0.400 | 30.5% | 15.7% |

Each user has 20 candidate events (8 retrieved by cosine, 4 hard negatives, 8 random). The label is the LLM
judge's 0–3 rating ÷ 3.

**Config** (stored in the checkpoint as `ckpt["config"]`):

| Setting | Value |
|---|---|
| Loss | Huber, δ = 1.0 |
| Optimizer | AdamW, lr 1e-3, weight decay 1e-4 |
| Batch size | 128, shuffled, seed 0 |
| Epochs | up to 50, early stopping with patience 5 |
| Selection | `selection_metric = "ndcg@10"` (the best epoch by validation NDCG@10, instead of the recipe's default validation loss) |
| Branch / head | branch (256,) → 64, dropout 0.2; head (32,) |
| Evaluation | `ranking_ks` (5, 10), `relevance_threshold` 0.5 |

**Curve.** Validation NDCG@10 rose each epoch, with small dips, up to epoch 7. Training then ran through epoch 12
without improving and stopped (patience 5).

| Epoch | 0 | 1 | 2 | 3 | 4 | 5 | 6 | **7** |
|---|---|---|---|---|---|---|---|---|
| Train loss | 0.0265 | 0.0227 | 0.0218 | 0.0210 | 0.0204 | 0.0198 | 0.0193 | 0.0188 |
| Val loss | 0.0227 | 0.0227 | 0.0215 | 0.0210 | 0.0230 | 0.0210 | 0.0213 | 0.0209 |
| Val NDCG@10 | 0.8809 | 0.8857 | 0.8874 | 0.8886 | 0.8884 | 0.8916 | 0.8899 | **0.8942** |

## How it was chosen

The steps follow [`training.md`](training.md). All choices were made on validation; test was touched once, at the end.

1. **Cosine baseline** (`cos(pos,event) − λ·cos(neg,event)`) over λ ∈ {0, 0.25, …, 2}. The best was
   λ = 0.5, with NDCG@10 0.8431 (λ = 1 gave 0.8337, λ = 0 gave 0.8263).
2. **Phase 1: the recipe's variants**, each with seeds 0, 1 and 2:

   | Variant | Val NDCG@10 | Val Recall@10 | Val Spearman |
   |---|---|---|---|
   | `late_fusion` | **0.8938 ± 0.0004** | **0.8800 ± 0.0027** | **0.6946** |
   | `flat` | 0.8923 ± 0.0010 | 0.8775 ± 0.0033 | 0.6941 |
   | `residual` + MSE | 0.8890 ± 0.0014 | 0.8744 ± 0.0021 | 0.6843 |
   | `residual` (recipe default) | 0.8888 ± 0.0019 | 0.8782 ± 0.0034 | 0.6807 |

   The residual cosine prior didn't help here: its α stayed near 1, and the plain late-fusion head matched
   or beat it.
3. **Phase 2: one change at a time around `late_fusion`** (seed 0). Validation NDCG@10:

   | Change | Val NDCG@10 |
   |---|---|
   | select epochs by NDCG@10 | **0.8942** |
   | none (phase-1 seed 0) | 0.8939 |
   | patience 10 | 0.8939 |
   | weight decay 1e-3 | 0.8934 |
   | dropout 0.35 | 0.8931 |
   | wider (512 → 128, head 64) | 0.8924 |
   | lr 3e-3 | 0.8919 |
   | dropout 0.1 | 0.8914 |
   | lr 3e-4 | 0.8914 |
   | batch 512 | 0.8908 |

   These differences are about the size of the seed noise (±0.0004), so the recipe's defaults are
   essentially optimal on this data. The top run was kept as the final model.

## Results

| Validation (1,000 users) | NDCG@5 | NDCG@10 | P@5 | P@10 | R@5 | R@10 | Spearman | MAE | RMSE |
|---|---|---|---|---|---|---|---|---|---|
| **this model** | 0.8480 | 0.8942 | 0.6810 | 0.5243 | 0.5982 | 0.8819 | 0.6937 | 0.1593 | 0.2044 |
| cosine, λ = 0.5 (tuned) | 0.7762 | 0.8431 | 0.5930 | 0.4928 | 0.5129 | 0.8279 | 0.5070 | – | – |
| cosine, λ = 1 | 0.7692 | 0.8337 | 0.5826 | 0.4823 | 0.5027 | 0.8080 | 0.4658 | – | – |

| Test (1,000 users, evaluated once) | NDCG@5 | NDCG@10 | P@5 | P@10 | R@5 | R@10 | Spearman | MAE | RMSE |
|---|---|---|---|---|---|---|---|---|---|
| **this model** | **0.8523** | **0.8969** | **0.6786** | **0.5254** | **0.6042** | **0.8829** | **0.6968** | 0.1565 | 0.1998 |
| cosine, λ = 0.5 (tuned) | 0.7887 | 0.8494 | 0.5988 | 0.4872 | 0.5232 | 0.8168 | 0.5012 | – | – |
| cosine, λ = 1 | 0.7797 | 0.8411 | 0.5882 | 0.4792 | 0.5101 | 0.8023 | 0.4583 | – | – |

Rankings are per user over their 20 candidates. "Relevant" (for P@K and R@K) means label ≥ 0.5, i.e. a judge
rating of 2 or 3. The baselines' MAE/RMSE are blank because cosine scores aren't on the label scale.

## Usage

Everything below was run end to end against the Hub repos, which are public, so no token is
needed.

### Setup

```bash
pip install -r ml/requirements.txt   # torch, sentence-transformers, datasets, typesafe-sdk, ...
export HF_TOKEN=hf_...               # optional: the repos are public
cd ml                                # so the `compatibility` package is importable
```

### Load the model and score events

```python
import os
import torch
from huggingface_hub import hf_hub_download
from sentence_transformers import SentenceTransformer
from compatibility.classifier import load_checkpoint

MODEL_REPO = "karthiksing05/sidequestz-compatibility-classifier"
TOKEN = os.environ.get("HF_TOKEN")  # None falls back to the `hf auth login` token

model, ckpt = load_checkpoint(hf_hub_download(MODEL_REPO, "final/best.pt", token=TOKEN))  # eval mode, CPU

encoder = SentenceTransformer("Qwen/Qwen3-Embedding-0.6B")
encoder.max_seq_length = 512  # as in training (data/embed.py)


def embed(texts: list[str]) -> torch.Tensor:
    """[N, 1024] L2-normalized embeddings, exactly as in training; empty texts become zero vectors."""
    out = torch.zeros(len(texts), 1024)
    keep = [i for i, t in enumerate(texts) if t and t.strip()]
    if keep:
        vectors = encoder.encode([texts[i] for i in keep], normalize_embeddings=True, convert_to_tensor=True)
        out[keep] = vectors.float().cpu()
    return out


@torch.no_grad()
def score_events(positive_text: str, negative_text: str, event_texts: list[str]) -> torch.Tensor:
    """One compatibility score per event (higher = better fit; roughly on the 0-1 label scale)."""
    pos, neg = embed([positive_text, negative_text])
    events = embed(event_texts)
    n = len(event_texts)
    return model(pos.expand(n, -1), neg.expand(n, -1), events)


likes = "Interests:\n- live jazz\n- pottery\n\nActivities:\n- hands-on workshop\n- live music listening\n\n" \
        "Social:\n- small-group setting\n\nCost:\n- under $20 admission\n\nTiming:\n- weekday evening"
dislikes = "Social:\n- large crowds\n\nEnvironment:\n- loud nightclub setting"
events = {
    "jazz trio": "Interests:\n- jazz\n\nActivities:\n- live performance\n\nSocial:\n- small audience\n\n"
                 "Cost:\n- $10 cover\n\nTiming:\n- weekday evening",
    "EDM festival": "Interests:\n- electronic dance music\n\nActivities:\n- dancing\n\nSocial:\n- large crowd\n\n"
                    "Environment:\n- outdoor festival grounds\n\nCost:\n- $120 ticket",
    "pottery class": "Interests:\n- ceramics\n\nActivities:\n- hands-on workshop\n\nExperience:\n- beginner-friendly\n\n"
                     "Cost:\n- $15 materials fee",
}
scores = score_events(likes, dislikes, list(events.values()))
for name, s in sorted(zip(events, scores.tolist()), key=lambda x: -x[1]):
    print(f"{s:.3f}  {name}")
# 0.806  pottery class
# 0.702  jazz trio
# 0.176  EDM festival
```

For a user with no dislikes, pass `negative_text=""`; it becomes the zero vector the model was trained with.

### Rank a real test user from the dataset

```python
import numpy as np
from datasets import load_dataset
from compatibility.classifier.metrics import ndcg_at_k

DATA_REPO = "karthiksing05/sidequestz-event-embedding-text"
users = load_dataset(DATA_REPO, "users", split="test", token=TOKEN)
events_ds = load_dataset(DATA_REPO, "default", split="test", token=TOKEN)  # test users only meet test events
event_text = dict(zip(events_ds["id"], events_ds["embedding_text"]))

user = users[0]
cands = user["candidates"]
scores = score_events(user["positive_text"], user["negative_text"], [event_text[c["event_id"]] for c in cands])
for s, c in sorted(zip(scores.tolist(), cands), key=lambda x: -x[0])[:5]:
    print(f"{s:.3f}  judge rating {c['rating']}  {event_text[c['event_id']].splitlines()[1]}")
print("NDCG@10:", ndcg_at_k(scores.numpy(), np.array([c["label"] for c in cands]), 10))
# 0.620  judge rating 3  - neighborhood safety
# 0.585  judge rating 2  - civic engagement
# ...
# NDCG@10: 0.9532
```

### Use it inside the app's `CompatibilityService`

`CompatibilityService` takes any `CompatibilityModel`, so a thin adapter plugs the classifier in next to
`CosineCompatibilityModel`:

```python
import numpy as np
import torch
from compatibility import CompatibilityService, TextEventEncoder, TextUserEncoder
from compatibility.models import CompatibilityModel
from compatibility.schemas import Embedding, ScoringResult, UserEmbedding


class ClassifierCompatibilityModel(CompatibilityModel):
    """The trained classifier behind the CompatibilityModel interface.

    Expects unit-norm, 1024-d Qwen/Qwen3-Embedding-0.6B vectors of eight-section texts.
    """

    name = "classifier"

    def __init__(self, model: torch.nn.Module, require_same_version: bool = True) -> None:
        super().__init__(require_same_version=require_same_version)
        self.model = model.eval()
        self.device = next(model.parameters()).device

    @torch.no_grad()
    def _score_batch(self, user_embedding: UserEmbedding, event_embeddings: list[Embedding]) -> list[ScoringResult]:
        events = torch.tensor(np.stack([e.vector for e in event_embeddings]), dtype=torch.float32, device=self.device)
        pos = torch.tensor(user_embedding.positive.vector, dtype=torch.float32, device=self.device).expand_as(events)
        neg = (torch.tensor(user_embedding.negative.vector, dtype=torch.float32, device=self.device).expand_as(events)
               if user_embedding.negative is not None else torch.zeros_like(events))  # no dislikes -> zero vector
        scores = self.model(pos, neg, events).cpu()
        return [ScoringResult(score=float(s), metadata={"model": self.name, "model_version": user_embedding.model_version})
                for s in scores]


VERSION = "Qwen/Qwen3-Embedding-0.6B"
embed_fn = lambda texts: encoder.encode(texts, normalize_embeddings=True)
service = CompatibilityService(TextUserEncoder(embed_fn, VERSION), TextEventEncoder(embed_fn, VERSION),
                               ClassifierCompatibilityModel(model))
results = service.score_events(app_user, app_events)  # a reranking.models.User and a list of Events
```

This works as is, but the current text encoders don't emit eight-section texts (see
[Inputs and output](#inputs-and-output)). For scores like the ones in [Results](#results), have the encoders
embed eight-section likes, dislikes and event texts instead: store each event's converted `embedding_text`,
and each user's preference texts.

### Event vectors in MongoDB

Each activity in the backend's MongoDB (`freetime.activities`, and `demo_activities`) stores its vector in
`embedding`. The vector encodes the activity's eight-section `embeddingText` with this model's encoder:
1024-d, unit norm, no prompt. `embeddingMeta.textHash` records which text the vector was computed from.
Embed users' preference texts the same way, with no prompt, or the scores are meaningless.

The vectors were backfilled on Raven on 2026-09-26 by `ml/datagen/mongo_backfill.py` (see its docstring).
The same run wrote the 6,911 missing texts, mostly places, with Qwen3.5-9B and the ingestion pipeline's
prompt and checks. Five places that aren't activities (a parking lot, a gate, a tree...) have neither a
text nor a vector. When new activities arrive or the pipeline rewrites texts, rerun the backfill: it embeds
exactly the texts that have no current vector.

### Other checkpoints

Every run of the experiment is in the repo as `runs/<run>/best.pt`, with its validation metrics in
`runs/<run>/val_metrics.json`. For example, `runs/p1-residual-s0/best.pt` is the recipe's recommended first
run, and `runs/p1-flat-s1/best.pt` the flat variant with seed 1. Load any of them with `load_checkpoint`,
as above.

To evaluate a checkpoint on your own labeled pairs, use the recipe's `evaluate`, which also scores the cosine
baseline on the same data:

```python
from compatibility.classifier import ClassifierConfig, evaluate, format_comparison
print(format_comparison(evaluate(model, your_loader, ClassifierConfig.from_dict(ckpt["config"]))))
```

`your_loader` yields batches with `positive_embedding`, `negative_embedding`, `event_embedding`, `label`
and `user_id`. `build_triples(...)` in `compatibility.classifier.data` turns embedding matrices plus rated
pairs into a `TripleSet`, and its `.loader(batch_size)` yields exactly those batches.

### Retraining

To rerun the whole recipe on Raven, follow "Running it" in [`training.md`](training.md):

```bash
mpcdf.py submit raven ml/compatibility/classifier/train.sbatch
```

The job logs to W&B, and `python -m compatibility.classifier.publish <run dir> --repo-id ... --push` uploads
the result.

## Performance

- **Scoring is cheap:** 200 (user, event) pairs took about 6 ms on a laptop CPU.
- **Embedding dominates:** 200 short event texts took about 7 s on an Apple-silicon GPU, and it's faster on
  an NVIDIA GPU.
- **Precompute event embeddings** when events are created. Use the `Embedding` schema's `model_version` to
  keep vectors from different encoders apart.

## Limitations

- **Synthetic labels.** The labels are one LLM judge's (Qwen3.5-9B) reading of synthetic users and events,
  not real behaviour. Treat the metrics as a strong starting point, and retrain on logged
  impressions and outcomes once the app has them.
- **Bound to one encoder.** The model only works with `Qwen/Qwen3-Embedding-0.6B` embeddings (L2-normalized,
  no prompt) of eight-section texts. Changing the encoder or the text format means retraining.
- **A re-ranker, not a search engine.** It was trained on candidates that were already plausible (retrieved
  and hard negatives) plus random ones. Use cosine retrieval to shortlist, then this model to rank the
  shortlist.
- **Blended user vectors are untested.** `/v1/events/rank` blends a search's embedding into the user's
  positive embedding (`SEARCH_WEIGHT`, default 0.6). The model only saw single-text embeddings, so tune the
  weight against real searches.
- **`Social` preferences are under-represented.** Only 7% of training events have a `Social` section, so
  group-size preferences carry less weight than interests or timing.
