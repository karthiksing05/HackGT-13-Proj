from datetime import datetime, timezone

from ingest.agent.blurb import BlurbAgent
from ingest.agent.research import CLAUDE, facts_packet, import_research, input_hash, research_todo
from ingest.config import load_global

from .test_blurb import GOOD, NOTES, FakeGemini, event_doc

NOW = datetime(2026, 9, 26, 12, 0, tzinfo=timezone.utc)


def claude_cfg() -> dict:
    return {**load_global()["blurb"], "research_provider": CLAUDE}


def seed(db):
    """Two showings of one event (different ticket URLs), a different event, and one already over."""
    docs = [
        event_doc(city="atlanta", start=datetime(2026, 9, 26, 14, 0, tzinfo=timezone.utc)),
        event_doc(city="atlanta", start=datetime(2026, 9, 27, 14, 0, tzinfo=timezone.utc), url="https://www.ticketmaster.com/y"),
        event_doc(city="atlanta", name="Castle Rat", venueName="The Masquerade - Heaven",
                  start=datetime(2026, 9, 26, 22, 0, tzinfo=timezone.utc)),
        event_doc(city="atlanta", name="Yesterday", start=datetime(2026, 9, 25, 14, 0, tzinfo=timezone.utc)),
    ]
    db.activities.insert_many(docs)
    return docs


def test_todo_lists_upcoming_unresearched_once_per_event(test_db):
    seed(test_db)
    rows = research_todo(test_db, "atlanta", now=NOW)
    assert [r["name"] for r in rows] == ["Collect-A-Con", "Castle Rat"]
    assert rows[0]["facts"]["venueName"] == "Georgia World Congress Center"
    assert rows[0]["notes"] == "" and rows[0]["sources"] == []
    assert len(research_todo(test_db, "atlanta", limit=1, now=NOW)) == 1


def test_import_shares_notes_across_showings_and_clears_todo(test_db):
    docs = seed(test_db)
    rows = research_todo(test_db, "atlanta", now=NOW)
    rows[0].update(notes=NOTES, sources=["https://collectaconusa.com/atlanta-2/"])
    counts = import_research(test_db, rows)
    assert counts == {"imported": 1, "empty": 1, "missing": 0, "facts_changed": 0}
    saved = list(test_db.research.find({"provider": CLAUDE}))
    assert {r["activityId"] for r in saved} == {docs[0]["_id"], docs[1]["_id"]}
    assert saved[0]["sources"] == [{"title": None, "url": "https://collectaconusa.com/atlanta-2/"}]
    assert [r["name"] for r in research_todo(test_db, "atlanta", now=NOW)] == ["Castle Rat"]


def test_import_flags_facts_changed_since_export(test_db):
    docs = seed(test_db)
    rows = research_todo(test_db, "atlanta", now=NOW)
    test_db.activities.update_many({"name": "Collect-A-Con"}, {"$set": {"url": "https://example.com/new"}})
    rows[0]["notes"] = NOTES
    assert import_research(test_db, rows)["facts_changed"] == 1
    current = input_hash(facts_packet(test_db.activities.find_one({"_id": docs[0]["_id"]})))
    assert test_db.research.find_one({"activityId": docs[0]["_id"]})["inputHash"] == current


def test_claude_provider_writes_from_imported_notes_without_searching(test_db):
    doc = seed(test_db)[0]
    import_research(test_db, [{"activityId": str(doc["_id"]), "notes": NOTES, "sources": []}])
    agent = BlurbAgent(test_db, claude_cfg(), "Atlanta", dry_run=True)
    agent.researcher.gemini = FakeGemini([GOOD])
    out = agent.run(doc)
    assert out.status == "written" and out.blurb["grounded"]
    assert agent.researcher.gemini.calls == [(agent.researcher.write_models[0], False)]


def test_claude_provider_never_calls_an_api_when_notes_are_missing():
    agent = BlurbAgent(None, claude_cfg(), "Atlanta", dry_run=True)
    agent.researcher.gemini = FakeGemini([GOOD, GOOD])  # GOOD's numbers aren't in the bare facts, so it retries
    out = agent.run(event_doc())
    assert out.research is None
    assert [search for _, search in agent.researcher.gemini.calls] == [False, False]
