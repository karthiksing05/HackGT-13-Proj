import httpx
import pytest

from ingest.agent.gemini import Result
from ingest.agent.muse import Muse, MuseUnavailable, parse_response
from ingest.agent.research import Researcher
from ingest.config import load_global

from .conftest import load_fixture
from .test_blurb import event_doc


def test_parse_documented_response():
    r = parse_response(load_fixture("muse/responses_web_search_documented_shape.json"), "muse-spark-1.3")
    assert r.text.startswith("- Camoufly is a masked electronic music producer")
    assert [s["url"] for s in r.sources] == [  # cited URLs, deduplicated, in order
        "https://open.spotify.com/artist/camoufly",
        "https://believemusichall.com/events/camoufly",
    ]
    assert r.search_queries == ["camoufly Wish Lounge Believe Music Hall Atlanta"]
    assert (r.tokens_in, r.tokens_out) == (412, 230)


def test_parse_live_response_skips_commentary():
    data = load_fixture("muse/web_search_live.json")
    res = parse_response(data, "muse-spark-1.3")
    assert res.text.startswith("- Live-music concert")
    assert "didn't open directly" not in res.text and "Researching your" not in res.text
    assert {s["url"] for s in res.sources} >= {"https://en.wikipedia.org/wiki/Castle_Rat"}
    assert len(res.search_queries) == 6


def test_parse_falls_back_to_raw_hits_when_nothing_cited():
    data = load_fixture("muse/responses_web_search_documented_shape.json")
    for item in data["output"]:
        for part in item.get("content") or []:
            part["annotations"] = []
    r = parse_response(data, "m")
    assert len(r.sources) == 2


def test_uncited_fallback_takes_top_hit_per_search_up_to_cap():
    data = load_fixture("muse/web_search_live.json")
    for item in data["output"]:
        for part in item.get("content") or []:
            part["annotations"] = []
    searches = [o for o in data["output"] if o["type"] == "web_search_call" and o.get("results")]
    r = parse_response(data, "m")
    assert len(r.sources) == 5
    assert r.sources[0]["url"] == searches[0]["results"][0]["url"]
    assert r.sources[1]["url"] == searches[1]["results"][0]["url"]


def test_billing_error_is_explained(city, monkeypatch):
    monkeypatch.setenv("MUSE_API_KEY", "test")
    resp = httpx.Response(402, json={"error": {"code": "billing_not_configured", "message": "Billing verification failed."}},
                          request=httpx.Request("POST", "https://api.meta.ai/v1/responses"))
    err = Muse._explain(httpx.HTTPStatusError("402", request=resp.request, response=resp))
    assert isinstance(err, MuseUnavailable) and "payment method" in str(err)


class FakeMuse:
    def __init__(self, fail: Exception | None = None):
        self.fail, self.prompts = fail, []

    def research(self, prompt):
        self.prompts.append(prompt)
        if self.fail:
            raise self.fail
        return Result(text="- notes", model="muse-spark-1.3", sources=[{"title": "t", "url": "https://x.example"}])


def muse_researcher(fake):
    r = Researcher(None, load_global()["blurb"], "Atlanta")
    assert r.provider == "muse"  # the configured default
    r._muse = fake
    return r


def test_researcher_uses_muse_and_tags_provider():
    fake = FakeMuse()
    r = muse_researcher(fake)
    res = r.research(event_doc(), {"name": "Collect-A-Con"}, "h")
    assert res["provider"] == "muse" and res["notes"] == "- notes" and res["sources"][0]["url"] == "https://x.example"
    assert "Search the web" in fake.prompts[0]


def test_muse_billing_error_turns_research_off_for_the_run():
    fake = FakeMuse(fail=MuseUnavailable("Muse 402 billing_not_configured"))
    r = muse_researcher(fake)
    assert r.research(event_doc(), {}, "h") is None
    assert r.grounded_off and "billing" in r.grounded_off
    assert r.research(event_doc(), {}, "h2") is None and len(fake.prompts) == 1  # no further calls
