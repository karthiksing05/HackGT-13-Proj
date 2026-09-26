"""Runnable demo of the Jev reranking stage.

    python example.py          # uses a local stub instead of the real Jev API
    python example.py --live   # calls Jev via the TypeSafe SDK (needs TYPESAFE_API_KEY)

The API key is read from the environment or from the repo's .env file.
"""

import asyncio
import logging
import sys

from dotenv import find_dotenv, load_dotenv

from reranking import Event, User, rerank_events
from reranking.jev import MAX_SCORE, call_jev


async def stub_jev(request: dict) -> dict:
    """Offline stand-in for Jev: scores by word overlap with the user's interests.

    Deliberately omits one answer to show that unscored events are ranked last.
    """
    state = request["state"]
    interest_words = {
        word
        for line in state["user"]["description"].lower().splitlines()
        if line.startswith("- ")
        for word in line[2:].split()
    }
    answers = {}
    for event in state["events"]:
        if event["id"] == "evt_yoga":
            continue  # simulate Jev missing an answer
        overlap = len(interest_words & set(event["description"].lower().split()))
        answers[f"event_{event['id']}"] = {
            "type": "score",
            "score": min(float(MAX_SCORE), overlap * 1.2),
            "confidence": 0.5,
        }
    return {"model": "stub", "answers": answers}


USER = User(
    id="user_123",
    name="Alex",
    interests=["live music", "coffee", "technology"],
    preferred_environment="social",
    budget=25,
)

# Pretend these are the top-k results from vector similarity search.
CANDIDATES = [
    Event("evt_hackathon", "Midtown Hack Night", "A casual technology meetup with coffee and demos.", "Technology", "Midtown Atlanta", 0),
    Event("evt_opera", "Grand Opera Gala", "Formal black-tie opera evening.", "Performing arts", "Downtown Atlanta", 180),
    Event("evt_jazz", "Atlanta Jazz Night", "A live jazz performance featuring local musicians.", "Live music", "Midtown Atlanta", 15),
    Event("evt_yoga", "Sunrise Yoga", "Quiet outdoor yoga session in the park.", "Wellness", "Piedmont Park", 10),
]


async def main() -> None:
    load_dotenv(find_dotenv(usecwd=True))
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s: %(message)s")
    jev = call_jev if "--live" in sys.argv else stub_jev
    ranked = await rerank_events(USER, CANDIDATES, jev=jev)
    for rank, event in enumerate(ranked, 1):
        print(f"{rank}. {event.name} ({event.id})")


if __name__ == "__main__":
    asyncio.run(main())
