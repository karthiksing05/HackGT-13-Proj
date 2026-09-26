"""Rank one dataset user's candidate events through the ranking API, end to end.

    python dataset_example.py                        # train user #4, the README walkthrough
    python dataset_example.py --split test --index 0

Loads a user and their 20 judged candidate events from the private
`users` config (needs HF_TOKEN), embeds the eight-section texts with
Qwen3-Embedding-0.6B, and posts them to `/v1/events/rank` on the app built by
`api.main` (same env config as the server, called in-process). The Jev rerank
runs when TYPESAFE_API_KEY is set. Prints the top events with the model's
score, Jev's score and the dataset's judge rating, and NDCG@10 against those
ratings.
"""

import argparse
import os

import numpy as np
from datasets import load_dataset
from dotenv import find_dotenv, load_dotenv
from fastapi.testclient import TestClient
from sentence_transformers import SentenceTransformer

from compatibility.classifier.metrics import ndcg_at_k

DATA_REPO = "karthiksing05/sidequestz-event-embedding-text"
# Users in the validation split draw events from the event train split.
EVENT_SPLIT = {"train": "train", "validation": "train", "test": "test"}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--split", default="train", choices=list(EVENT_SPLIT))
    parser.add_argument("--index", type=int, default=4)
    parser.add_argument("--top", type=int, default=5)
    args = parser.parse_args()

    load_dotenv(find_dotenv(usecwd=True))
    token = os.environ.get("HF_TOKEN")
    user = load_dataset(DATA_REPO, "users", split=args.split, token=token)[args.index]
    events = load_dataset(DATA_REPO, "default", split=EVENT_SPLIT[args.split], token=token)
    row = {event_id: i for i, event_id in enumerate(events["id"])}
    candidates = user["candidates"]
    texts = {c["event_id"]: events[row[c["event_id"]]]["embedding_text"] for c in candidates}
    topics = {c["event_id"]: events[row[c["event_id"]]]["topic"] for c in candidates}

    encoder = SentenceTransformer("Qwen/Qwen3-Embedding-0.6B")
    encoder.max_seq_length = 512

    def embed(text: str) -> list[float]:
        if not text.strip():
            return [0.0] * 1024
        return encoder.encode(text, normalize_embeddings=True).tolist()

    body = {
        "user": {
            "positive_embedding": embed(user["positive_text"]),
            "negative_embedding": embed(user["negative_text"]),
            "positive_text": user["positive_text"],
            "negative_text": user["negative_text"],
        },
        "events": [{"id": i, "embedding": embed(t), "description": t} for i, t in texts.items()],
    }

    from api.main import app  # built from the environment, like the server

    with TestClient(app) as client:
        model_only = client.post("/v1/events/rank", json={**body, "options": {"rerank": False}}).json()
        final = client.post("/v1/events/rank", json=body).json()

    ratings = {c["event_id"]: c["rating"] for c in candidates}
    sources = {c["event_id"]: c["source"] for c in candidates}

    def ndcg(ranked: list[dict]) -> float:
        # Descending positions as scores, so ndcg_at_k sees the API's order.
        order = np.arange(len(ranked), 0, -1, dtype=float)
        return ndcg_at_k(order, np.array([ratings[e["event_id"]] / 3 for e in ranked]), 10)

    print(f"user {user['id']} ({args.split} split), {len(candidates)} candidates, model {final['model_version']}")
    print(f"reranked by Jev: {final['reranked']}\n")
    print(f"{'#':>2}  {'model':>5}  {'Jev':>4}  {'judge':>5}  {'source':<13}  topic")
    for rank, e in enumerate(final["events"], 1):
        jev = "-" if e["rerank_score"] is None else f"{e['rerank_score']:.2f}"
        print(f"{rank:>2}  {e['score']:.3f}  {jev:>4}  {ratings[e['event_id']]:>5}  "
              f"{sources[e['event_id']]:<13}  {topics[e['event_id']]}")
    print(f"\nNDCG@10 vs judge ratings: model only {ndcg(model_only['events']):.3f}, "
          f"model + Jev {ndcg(final['events']):.3f}")
    print(f"\ntop {args.top} event texts:")
    for rank, e in enumerate(final["events"][: args.top], 1):
        print(f"\n--- #{rank} {topics[e['event_id']]} ({e['event_id']})\n{texts[e['event_id']]}")


if __name__ == "__main__":
    main()
