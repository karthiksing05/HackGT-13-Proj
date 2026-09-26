from datetime import datetime, timezone

import pytest
from bson import ObjectId

from ingest.agent import blurb as blurb_mod
from ingest.agent.blurb import BlurbAgent, check, facts_packet, input_hash
from ingest.agent.gemini import Result, _is_search_page
from ingest.config import load_global

from .conftest import gemini_cfg

GOOD = (
    "Collect-A-Con fills the Georgia World Congress Center with over 900 dealer tables of trading cards, "
    "comics, video games and vintage toys. You can browse franchises like Pokemon and Yu-Gi-Oh, watch box "
    "breaks, and catch celebrity guests and live concerts between aisles. It's an all-ages show, so bring "
    "cash for smaller vendors and plan to walk a lot of floor."
)
NOTES = "- Over 900 dealer tables\n- Pokemon, Yu-Gi-Oh, sports cards\n- celebrity guests, live concerts"


def event_doc(**kw):
    doc = {
        "_id": ObjectId(),
        "kind": "event",
        "name": "Collect-A-Con",
        "category": "festival",
        "venueName": "Georgia World Congress Center",
        "address": {"formatted": "285 Andrew Young International Blvd NW, Atlanta, GA 30313"},
        "timezone": "America/New_York",
        "start": datetime(2026, 9, 26, 14, 0, tzinfo=timezone.utc),
        "duration": {"medianMin": 150, "sigma": 0.45, "p75Min": 204, "source": "category_prior"},
        "url": "https://www.ticketmaster.com/x",
    }
    doc.update(kw)
    return doc


def test_facts_packet_local_time_and_no_prior_duration():
    f = facts_packet(event_doc())
    assert f["startLocal"] == "Sat 2026-09-26 10:00"
    assert "typicalStayMinutes" not in f  # a category prior is a guess, not a fact
    f = facts_packet(event_doc(duration={"medianMin": 120, "sigma": 0.1, "p75Min": 128, "source": "event_times"}))
    assert f["typicalStayMinutes"] == 120


def test_input_hash_ignores_dates_so_series_share_it():
    a = facts_packet(event_doc())
    b = facts_packet(event_doc(start=datetime(2026, 10, 3, 14, 0, tzinfo=timezone.utc)))
    assert input_hash(a) == input_hash(b)
    assert input_hash(a) != input_hash(facts_packet(event_doc(name="Other")))


def test_check_accepts_grounded_blurb():
    assert check(GOOD, facts_packet(event_doc()), NOTES, 50, 90) is None


@pytest.mark.parametrize(
    "text, fragment",
    [
        ("Too short.", "words"),
        (GOOD.replace("It's an all-ages", "It's an amazing all-ages"), "banned"),
        (GOOD.replace("over 900", "over 1,200"), "1200"),
        (GOOD + "!", "exclamation"),
    ],
)
def test_check_rejects(text, fragment):
    assert fragment in check(text, facts_packet(event_doc()), NOTES, 50, 90)


def test_search_pages_are_not_sources():
    assert _is_search_page("https://www.google.com/search?q=time+in+Dodge+County,+US")
    assert not _is_search_page("https://collectaconusa.com/atlanta-2/")


class FakeGemini:
    def __init__(self, writes):
        self.writes = list(writes)
        self.calls = []

    def generate(self, model, prompt, **kw):
        self.calls.append((model, kw.get("search", False)))
        if kw.get("search"):
            return Result(text=NOTES, model=model, sources=[{"title": "x", "url": "https://collectaconusa.com/atlanta-2/"}])
        return Result(text=self.writes.pop(0), model=model)


@pytest.fixture
def agent():
    a = BlurbAgent(None, gemini_cfg(), "Atlanta", dry_run=True)
    return a


def test_agent_researches_then_writes(agent, monkeypatch):
    agent.researcher.gemini = FakeGemini([GOOD])
    out = agent.run(event_doc())
    assert out.status == "written"
    assert out.blurb["grounded"] and out.blurb["sources"] == ["https://collectaconusa.com/atlanta-2/"]
    assert [search for _, search in agent.researcher.gemini.calls] == [True, False]


def test_agent_retries_once_with_reason_then_gives_up(agent):
    agent.researcher.gemini = FakeGemini(["Too short.", "Still short."])
    out = agent.run(event_doc())
    assert out.status == "failed" and "words" in out.reason
    assert len(agent.researcher.gemini.calls) == 3  # research + 2 writes


def test_agent_skips_current_blurb(agent):
    doc = event_doc()
    doc["blurb"] = {"inputHash": input_hash(facts_packet(doc)), "grounded": True}
    agent.researcher.gemini = FakeGemini([])
    assert agent.run(doc).status == "skipped"
    assert agent.researcher.gemini.calls == []
