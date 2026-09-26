from datetime import datetime, timezone

import pytest
from bson import ObjectId

from ingest.agent.embed_text import EmbedTextAgent, check, clean, event_data
from ingest.agent.gemini import Result
from ingest.config import load_global

from .conftest import gemini_cfg

GOOD = """Interests:
- live music
- jazz

Activities:
- live performance

Timing:
- evening"""


def doc(**kw):
    d = {
        "_id": ObjectId(), "kind": "event", "name": "Sunset Jazz Session", "category": "live_music",
        "timezone": "America/New_York", "venueName": "Blind Willie's",
        "address": {"locality": "Atlanta", "region": "GA"},
        "start": datetime(2026, 9, 27, 0, 0, tzinfo=timezone.utc),   # Sat 8:00 PM EDT
        "end": datetime(2026, 9, 27, 3, 0, tzinfo=timezone.utc),
        "price": {"min": 12, "max": 14, "currency": "USD"}, "tags": ["21_plus"],
        "url": "https://example.com/jazz", "sourceKeys": ["ra:1"],
    }
    d.update(kw)
    return d


def test_event_data_has_facts_but_no_ids_urls_or_dates():
    text = event_data(doc(), {"notes": "- Quartet led by a local saxophonist"})
    assert "Starts: Saturday 8:00 PM" in text and "(3 hours later)" in text
    assert "Price: $12-$14" in text and "Age: 21+" in text
    assert "Quartet led by a local saxophonist" in text
    assert "http" not in text and "ra:1" not in text and "2026" not in text


def test_event_data_omits_missing_fields():
    text = event_data(doc(price=None, tags=[], end=None, venueName=None, category="other"), None)
    assert "Price" not in text and "Age" not in text and "Ends" not in text and "Venue" not in text
    assert "Category" not in text and "Web research" not in text


def test_clean_strips_code_fence():
    assert clean("```text\n" + GOOD + "\n```") == GOOD


def test_check_accepts_valid():
    assert check(GOOD) is None


@pytest.mark.parametrize("text, fragment", [
    (GOOD + "\n\nCost:\n- unknown", "placeholder"),
    (GOOD + "\n\nVibe:\n- chill", "not an allowed section"),
    (GOOD + "\n\nSocial:", "empty sections"),
    ("This is a lovely jazz evening.", "neither"),
    (GOOD + "\n\nExperience:\n- https://example.com", "URL"),
    (GOOD + "\n\nInterests:\n- blues", "twice"),
    (GOOD + "\n\nExperience:\n- a relaxed evening of classic standards played by a quartet", "sentence"),
])
def test_check_rejects(text, fragment):
    assert fragment in check(text)


class FakeGemini:
    def __init__(self, writes):
        self.writes, self.prompts = list(writes), []

    def generate(self, model, prompt, **kw):
        if kw.get("search"):
            return Result(text="- Quartet", model=model, sources=[{"title": "x", "url": "https://a.example"}])
        self.prompts.append(prompt)
        return Result(text=self.writes.pop(0), model=model)


def test_agent_fills_template_and_retries_with_reason():
    agent = EmbedTextAgent(None, gemini_cfg(), "Atlanta", dry_run=True)
    agent.researcher.gemini = FakeGemini(["Cost:\n- unknown", GOOD])
    out = agent.run(doc())
    assert out.status == "written" and out.text == GOOD
    first, second = agent.researcher.gemini.prompts
    assert "{{EVENT_DATA}}" not in first and "Sunset Jazz Session" in first and "- Quartet" in first
    assert "rejected because it uses a placeholder" in second
