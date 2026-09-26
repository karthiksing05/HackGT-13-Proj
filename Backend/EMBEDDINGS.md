# Embeddings

Moved to [`docs/EMBEDDINGS.md`](../docs/EMBEDDINGS.md). Two things this file said earlier no longer
apply: user and search texts get **no** `Instruct: … Query:` prefix, and vectors are never truncated
(Matryoshka), because the compatibility classifier and the stored activity vectors use plain
`Qwen/Qwen3-Embedding-0.6B` output. What still applies moved with it: the Vertex endpoint is one of three
providers behind the ML service (`ml/embedding`), needs a service account with `roles/aiplatform.user`
(missing on 2026-09-26, so the local model serves) and a probe to learn its instance key, should be
enabled only after a parity check, and bills per node-hour while deployed (undeploy it when idle); negative preferences are written as topics, not negations, and
used only as a penalty; every vector is L2-normalized, stored with its model name, and recomputed only
when the text hash changes. The Go API never calls a provider itself: it asks the ML service
(`Backend/pkg/ml`).
