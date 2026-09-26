# Qwen Embeddings on Vertex AI

How to get embeddings from our Qwen embedding model hosted on a Google Cloud Vertex AI endpoint, and how to use them for users, itineraries and events.

| | |
|---|---|
| Project (number) | `586468035526` |
| Region | `us-central1` |
| Endpoint ID | `mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852` |
| Deployed from | Model Garden (the `mg-endpoint-` prefix) |

---

## 1. When we embed

| Trigger | Text we build | Qwen role | Vectors stored |
|---|---|---|---|
| Account creation / profile edit | Positive signals (likes, interests, vibe) | **query** | `users.positiveEmbedding` |
| Account creation / profile edit | Negative signals (dislikes, dealbreakers) | **query** | `users.negativeEmbedding` |
| New itinerary request | Itinerary request (who, when, budget, vibe, constraints) | **query** | on the itinerary doc, or used once and dropped |
| Event ingestion (maybe) | Event/activity description | **document** | `activities.embedding` |

The query and document roles matter because Qwen embeddings are asymmetric (see §5).

---

## 2. One-time setup

### Local development

```bash
gcloud auth login
gcloud auth application-default login
gcloud config set project 586468035526
```

`application-default login` is what the client libraries pick up. Your Google account needs the **Vertex AI User** role (`roles/aiplatform.user`) on the project.

### Server (VPS)

The VPS is not on GCP, so it has no metadata-server credentials. Use a service account:

1. Create a service account, e.g. `sidequestz-embedder`, and grant it `roles/aiplatform.user`.
2. Create a JSON key and copy it to the VPS (e.g. `/opt/backend/gcp-sa.json`, `chmod 600`, owned by the service user).
3. Add it to `sidequestz.service`:
   ```ini
   Environment=GOOGLE_APPLICATION_CREDENTIALS=/opt/backend/gcp-sa.json
   ```
4. Don't commit the key.

### Python dependency

```bash
pip install google-cloud-aiplatform
```

---

## 3. Find out what the endpoint expects

This step can't be skipped. The `instances={"instance_key_1": "value", ...}` in the console snippet is a placeholder. The real request shape depends on the **serving container** that Model Garden deployed. Check it once:

```bash
# Deployed model ID and whether a dedicated endpoint is enabled
gcloud ai endpoints describe mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852 \
  --region=us-central1 --format=yaml

# Container image and model name (use the model ID from the output above)
gcloud ai models describe <MODEL_ID> --region=us-central1 \
  --format="yaml(displayName,containerSpec)"
```

From that output, record:

- **`dedicatedEndpointDns`**, if it's present. Model Garden endpoints usually have a dedicated endpoint, and calls to the shared `us-central1-aiplatform.googleapis.com` host then fail with *"This endpoint is a dedicated endpoint … Please access the endpoint using its dedicated dns name"*. Send requests to the dedicated DNS instead (see §4).
- **The container image**, which decides the instance format:
  - Hugging Face **TEI** (`text-embeddings-inference`): `{"inputs": "text"}`
  - **vLLM** or another Model Garden container: often `{"prompt": "text"}` or `{"text": "text"}`. Check the model card in Model Garden.
- **The model name** (Qwen3-Embedding-0.6B / 4B / 8B). This sets the vector size: 1024 / 2560 / 4096.

If you're not sure, run the probe in §4.3. It tries the common shapes and prints the one that works.

---

## 4. Calling the endpoint

### 4.1 Python (recommended)

Use the high-level `aiplatform.Endpoint` rather than copying the raw GitHub sample. The sample works, but it makes you convert protobuf `Value`s yourself.

```python
# ml/embeddings/qwen_client.py
import numpy as np
from google.cloud import aiplatform

PROJECT = "586468035526"
LOCATION = "us-central1"
ENDPOINT_ID = "mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852"
INPUT_KEY = "inputs"  # set this from §3 ("inputs" / "prompt" / "text")

aiplatform.init(project=PROJECT, location=LOCATION)
_endpoint = aiplatform.Endpoint(ENDPOINT_ID)


def embed(texts: list[str], batch_size: int = 16) -> list[list[float]]:
    """Embed a list of strings. Returns L2-normalized vectors in the same order."""
    out: list[list[float]] = []
    for i in range(0, len(texts), batch_size):
        chunk = texts[i : i + batch_size]
        resp = _endpoint.predict(
            instances=[{INPUT_KEY: t} for t in chunk],
            use_dedicated_endpoint=True,  # drop this if §3 showed no dedicated endpoint
        )
        for p in resp.predictions:
            v = np.asarray(_unwrap(p), dtype=np.float32)
            out.append((v / np.linalg.norm(v)).tolist())
    return out


def _unwrap(p):
    # Containers differ: some return [floats], some [[floats]], some {"embedding": [...]}
    if isinstance(p, dict):
        p = p.get("embedding") or p.get("embeddings") or next(iter(p.values()))
    if p and isinstance(p[0], list):
        p = p[0]
    return p
```

Notes:
- `use_dedicated_endpoint=True` requires a recent `google-cloud-aiplatform`. If your version doesn't accept it, upgrade.
- If you'd rather use the GitHub sample (`predict_custom_trained_model_sample`), change its `api_endpoint` from `us-central1-aiplatform.googleapis.com` to the `dedicatedEndpointDns` value, and pass `instances` as a **list** of dicts.

### 4.2 Go (from the backend directly)

The backend is Go, so we can skip a Python hop and call the REST API:

```go
// pkg/embeddings/qwen.go
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2/google"
)

const (
	project  = "586468035526"
	location = "us-central1"
	endpoint = "mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852"
	host     = "<dedicatedEndpointDns from §3>" // or "us-central1-aiplatform.googleapis.com"
	inputKey = "inputs"                         // from §3
)

func Embed(ctx context.Context, texts []string) ([][]float64, error) {
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}

	instances := make([]map[string]string, len(texts))
	for i, t := range texts {
		instances[i] = map[string]string{inputKey: t}
	}
	body, _ := json.Marshal(map[string]any{"instances": instances})

	url := fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/endpoints/%s:predict",
		host, project, location, endpoint)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex predict: %s", resp.Status)
	}

	// Adjust if the container nests differently (see _unwrap in the Python version)
	var out struct {
		Predictions [][]float64 `json:"predictions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Predictions, nil // normalize before storing (§5)
}
```

`google.DefaultClient` reads `GOOGLE_APPLICATION_CREDENTIALS` on the VPS and your `gcloud auth application-default login` credentials locally.

### 4.3 Smoke test / probe with curl

```bash
TOKEN=$(gcloud auth print-access-token)
HOST=<dedicatedEndpointDns>   # or us-central1-aiplatform.googleapis.com
URL="https://$HOST/v1/projects/586468035526/locations/us-central1/endpoints/mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852:predict"

for KEY in inputs prompt text; do
  echo "== $KEY"
  curl -s -X POST "$URL" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"instances\":[{\"$KEY\":\"live jazz in a small bar\"}]}" | head -c 300; echo
done
```

The key that returns a long list of floats is your `INPUT_KEY`. Check that the vector length matches the model size from §3.

---

## 5. Using Qwen embeddings correctly

### Queries get an instruction prefix; documents don't

Qwen3-Embedding is trained so that query-side text starts with a task instruction and document-side text is embedded as is. If you skip the prefix on queries, retrieval quality usually drops by a few percent.

```
Instruct: {task}\nQuery: {text}
```

| Text | What to send |
|---|---|
| User positive profile | `Instruct: Given a person's interests and preferences, retrieve activities and events they would enjoy\nQuery: {positive_text}` |
| User negative profile | `Instruct: Given things a person dislikes or wants to avoid, retrieve activities and events that match them\nQuery: {negative_text}` |
| Itinerary request | `Instruct: Given a plan request for an outing, retrieve activities and events that fit it\nQuery: {itinerary_text}` |
| Event / activity | `{event_text}` (no prefix) |

Keep these strings in constants and **don't change them after launch**. Changing an instruction changes every query vector.

### Write negative signals as topics, not negations

Embeddings don't handle "not" well. The text *"I don't like loud clubs"* lands close to loud clubs, which is actually what we want here, as long as we use the vector as a penalty and never as a match. So:

- Build the negative text as a plain list of the things themselves: `"Loud nightclubs. Crowded bars. Long strenuous hikes. Horror movies."`
- Score candidates like this:
  ```
  score(event) = cos(pos, event) − λ · cos(neg, event)      # start with λ ≈ 0.5, tune
  ```
- If the user gave no negatives, skip the embedding call and set λ = 0. Don't embed an empty string.

### Structured text templates

Keep them short and stable, with a fixed field order, and leave empty fields out instead of writing "none":

```
# positive
Interests: live music, board games, thrifting
Vibe: chill, social, outdoors when it's nice
Food: ramen, tacos, vegetarian-friendly
Budget: low to medium

# itinerary
Group: 3 friends, ages 21+
When: Saturday evening, about 4 hours
Where: Midtown Atlanta, walkable
Wants: something active and then dinner
Budget: under $30 each

# event
Title: Friday Night Jazz at Venkman's
Category: live music, bar
Description: …(trim to ~1–2k chars)
Price: $10 cover
Neighborhood: Old Fourth Ward
```

### Other rules

- **L2-normalize** every vector before storing, so cosine similarity is just a dot product.
- **Store the model name** with each vector (`embeddingModel`, e.g. `qwen3-embedding-0.6b@v1`). Never compare vectors from different models or instruction versions.
- **Hash before embedding.** `sha256(prefix + text)` → `embeddingTextHash`. If the hash hasn't changed, skip the call. Activities already have this field.
- **Batch ingestion.** Send 16–32 instances per request and keep the payload under ~1.5 MB. Retry on 429 / 503 with exponential backoff.
- **Account creation:** embed positive and negative in one request (2 instances), not two calls.
- **Optional shrinking:** Qwen3-Embedding supports Matryoshka truncation. You can keep the first N dims (e.g. 512), then re-normalize, to save storage. If you do, use the same N everywhere.

---

## 6. Schema changes needed

In `pkg/models/models.go`:

- `User` has a single `Embedding []float64`. Replace it with:
  ```go
  PositiveEmbedding []float64 `bson:"positiveEmbedding,omitempty" json:"-"`
  NegativeEmbedding []float64 `bson:"negativeEmbedding,omitempty" json:"-"`
  ```
  Use `json:"-"` so we don't send thousands of floats to the app. Keep `EmbeddingModel` and `EmbeddingTextHash`, or split the hash into positive and negative.
- `Activity` has `EmbeddingText` and `EmbeddingTextHash` but **no vector field**. Add `Embedding []float64` and `EmbeddingModel string` there too.

Self-hosted `mongod` has no `$vectorSearch`, which is Atlas-only. At hackathon scale, filter candidates first with a normal query (geo, time, price, age bracket), load their vectors, and compute the scores in Go or Python.

---

## 7. Cost warning

A Vertex endpoint with a deployed model is **billed per node-hour while it's deployed**, even when idle. Undeploy it when we're done:

```bash
gcloud ai endpoints undeploy-model mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852 \
  --region=us-central1 --deployed-model-id=<DEPLOYED_MODEL_ID>
```
