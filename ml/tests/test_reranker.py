import asyncio
import unittest

from reranking import Event, User, rerank_events
from reranking.jev import EVENT_MATCH_CRITERIA, build_jev_request, parse_jev_scores
from reranking.context import event_to_context, user_to_context

USER = User(id="u1", name="Sam", interests=["jazz"], preferred_environment="social", budget=20)


def make_event(event_id: str) -> Event:
    return Event(id=event_id, name=f"Event {event_id}", description="desc", category="Music")


def fake_jev(scores: dict[str, object]):
    """Returns a Jev client that answers with the given raw score per event id."""

    async def client(request: dict) -> dict:
        return {"answers": {f"event_{eid}": {"type": "score", "score": s} for eid, s in scores.items()}}

    return client


def rerank(events, jev):
    return asyncio.run(rerank_events(USER, events, jev=jev))


class RequestTests(unittest.TestCase):
    def test_request_shape(self):
        events = [event_to_context(make_event("a")), event_to_context(make_event("b"))]
        request = build_jev_request(user_to_context(USER), events)

        state = request["state"]
        self.assertEqual(state["user"]["id"], "u1")
        self.assertEqual([e["id"] for e in state["events"]], ["a", "b"])
        self.assertEqual(set(request["questions"]), {"event_a", "event_b"})
        for question in request["questions"].values():
            self.assertEqual(question["type"], "score")
            self.assertEqual(question["criteria"], EVENT_MATCH_CRITERIA)


class ParseTests(unittest.TestCase):
    def test_skips_missing_and_malformed(self):
        response = {
            "answers": {
                "event_ok": {"score": 1.43, "confidence": 0.35},
                "event_str": {"score": "high"},
                "event_range": {"score": 7},
                "event_bool": {"score": True},
            }
        }
        scores = parse_jev_scores(response, ["ok", "str", "range", "bool", "missing"])
        self.assertEqual(list(scores), ["ok"])
        self.assertEqual(scores["ok"].score, 1.43)
        self.assertEqual(scores["ok"].confidence, 0.35)

    def test_no_answers_object(self):
        self.assertEqual(parse_jev_scores({"error": "boom"}, ["a"]), {})


class RerankTests(unittest.TestCase):
    def test_sorts_descending_and_returns_originals(self):
        events = [make_event("a"), make_event("b"), make_event("c")]
        ranked = rerank(events, fake_jev({"a": 0.2, "b": 1.9, "c": 1.1}))
        self.assertEqual([e.id for e in ranked], ["b", "c", "a"])
        self.assertIs(ranked[0], events[1])

    def test_unscored_events_go_last_in_retrieval_order(self):
        events = [make_event(x) for x in "abcd"]
        ranked = rerank(events, fake_jev({"b": 0.5, "d": "bad"}))
        self.assertEqual([e.id for e in ranked], ["b", "a", "c", "d"])

    def test_empty_list_skips_jev(self):
        async def exploding(_):
            raise AssertionError("Jev should not be called")

        self.assertEqual(rerank([], exploding), [])

    def test_duplicates_keep_first(self):
        first, dup = make_event("a"), make_event("a")
        ranked = rerank([first, dup, make_event("b")], fake_jev({"a": 0.1, "b": 2.0}))
        self.assertEqual([e.id for e in ranked], ["b", "a"])
        self.assertIs(ranked[1], first)

    def test_jev_failure_falls_back_to_retrieval_order(self):
        async def failing(_):
            raise ConnectionError("down")

        events = [make_event("a"), make_event("b")]
        with self.assertLogs("reranking.reranker", level="ERROR"):
            self.assertEqual(rerank(events, failing), events)


if __name__ == "__main__":
    unittest.main()
