"""Muse Spark (Meta Model API) web research: the Responses API with the built-in web_search tool.

Request/response shape from https://dev.meta.ai/docs/search-grounding (web_search is only on
POST /v1/responses, not chat completions). The answer is in output[type=message].content[
type=output_text].text, citations in its `annotations` (type url_citation: url, title), and
raw search hits in output[type=web_search_call].results when "web_search_call.results" is
requested. Verified against a live response on 2026-09-26 (fixtures/muse/web_search_live.json):
the answer comes as several `message` items, and all but the last carry `phase: "commentary"`
(progress notes between searches), so those are skipped.
"""

import logging

import httpx

from ..config import City, require_env
from ..http import Http
from .gemini import Result

log = logging.getLogger(__name__)

RESPONSES_URL = "https://api.meta.ai/v1/responses"
MAX_FALLBACK_SOURCES = 5


class MuseError(RuntimeError):
    pass


class MuseUnavailable(MuseError):
    """Billing, auth or quota problems: won't fix themselves mid-run, so research stops."""


def parse_response(data: dict, model: str) -> Result:
    texts: list[str] = []
    sources: dict[str, dict] = {}
    queries: list[str] = []
    for item in data.get("output") or []:
        kind = item.get("type")
        if kind == "message":
            if item.get("phase") == "commentary":
                continue  # progress chatter between searches ("That link didn't open..."), not findings
            for part in item.get("content") or []:
                if part.get("type") == "output_text" and part.get("text"):
                    texts.append(part["text"].strip())
                for ann in part.get("annotations") or []:
                    if ann.get("type") == "url_citation" and ann.get("url"):
                        sources.setdefault(ann["url"], {"title": ann.get("title"), "url": ann["url"]})
        elif kind == "web_search_call":
            query = (item.get("action") or {}).get("query")
            if query:
                queries.append(query)
    # Cited URLs are what the notes rest on; raw hits are only a fallback when nothing was cited.
    # A live call returned 68 hits (including spam pages), so keep the top hit of each search in
    # turn, up to MAX_FALLBACK_SOURCES.
    if not sources:
        per_search = [[h for h in item.get("results") or [] if h.get("url")]
                      for item in data.get("output") or [] if item.get("type") == "web_search_call"]
        for rank in range(max(map(len, per_search), default=0)):
            for hits in per_search:
                if rank < len(hits) and len(sources) < MAX_FALLBACK_SOURCES:
                    hit = hits[rank]
                    sources.setdefault(hit["url"], {"title": hit.get("title"), "url": hit["url"]})
    usage = data.get("usage") or {}
    return Result(
        text="\n\n".join(texts),
        model=data.get("model") or model,
        search_queries=queries,
        sources=list(sources.values()),
        tokens_in=usage.get("input_tokens") or 0,
        tokens_out=usage.get("output_tokens") or 0,
    )


class Muse:
    def __init__(self, city: City, cfg: dict):
        self.api_key = require_env("MUSE_API_KEY", "Get one from the Meta Model API dashboard.")
        self.model = cfg.get("model") or "muse-spark-1.3"
        self.search_context_size = cfg.get("search_context_size") or "medium"
        self.city = city
        self.http = Http("muse", timeout=cfg.get("timeout_s", 180))

    def research(self, prompt: str) -> Result:
        body = {
            "model": self.model,
            "input": prompt,
            "tools": [{
                "type": "web_search",
                "search_context_size": self.search_context_size,
                "user_location": {
                    "type": "approximate",
                    "country": self.city.country_code,
                    "city": self.city.name,
                    "timezone": self.city.timezone,
                },
            }],
            "include": ["web_search_call.results"],
        }
        headers = {"Authorization": f"Bearer {self.api_key}"}
        try:
            data = self.http.post_json(RESPONSES_URL, body, headers=headers)
        except httpx.HTTPStatusError as e:
            raise self._explain(e) from None
        return parse_response(data, self.model)

    @staticmethod
    def _explain(e: httpx.HTTPStatusError) -> MuseError:
        try:
            err = e.response.json().get("error") or {}
        except ValueError:
            err = {}
        code, msg = err.get("code") or e.response.status_code, err.get("message") or e.response.text[:200]
        if e.response.status_code in (401, 402, 403, 429):
            hint = " Add a payment method in the Meta developer dashboard." if code == "billing_not_configured" else ""
            return MuseUnavailable(f"Muse {e.response.status_code} {code}: {msg}{hint}")
        return MuseError(f"Muse {e.response.status_code} {code}: {msg}")
