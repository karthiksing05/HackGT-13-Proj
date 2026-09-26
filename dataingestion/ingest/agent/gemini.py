"""Thin Gemini wrapper for the agents: per-model pacing, 429/503 backoff, grounding metadata.

Verified on our key (2026-09-25): Google Search grounding works on gemini-2.5-flash only;
gemini-flash-latest / flash-lite-latest / 3.5-flash return 429 with the search tool (no
free-tier grounding), but work without tools. A 429 body doesn't say whether the
per-minute or per-day limit was hit, so we back off a couple of times and then give up.

GEMINI_API_KEY may hold several comma-separated keys (one per account). Each key has its own
free-tier limits, so each (key, model) is paced separately and a call goes to whichever key is
free soonest; a key that runs out for a model is dropped for that model for the rest of the run.
"""

import logging
import re
import threading
import time
from dataclasses import dataclass, field

import httpx
from google import genai
from google.genai import errors, types

from ..config import env, require_keys

log = logging.getLogger(__name__)

RETRY_WAITS_429 = (20, 60)   # seconds; after these, treat the quota as spent for this run
RETRY_WAITS_5XX = (5,)  # then the caller falls back to another model


class GeminiQuotaExhausted(RuntimeError):
    pass


@dataclass
class Result:
    text: str
    model: str
    search_queries: list[str] = field(default_factory=list)
    sources: list[dict] = field(default_factory=list)  # [{title, url}] with redirects resolved
    tokens_in: int = 0
    tokens_out: int = 0


class Gemini:
    def __init__(self, rpm: dict[str, float] | None = None):
        self.clients = [
            genai.Client(
                api_key=key,
                # Our own retry loop below handles 429/503; the SDK's would hammer a spent quota.
                http_options=types.HttpOptions(retry_options=types.HttpRetryOptions(attempts=1)),
            )
            for key in require_keys("GEMINI_API_KEY", "Get one from AI Studio.")
        ]
        self.rpm = rpm or {}
        self._next: dict[tuple[int, str], float] = {}  # (key, model) -> monotonic time it's free again
        self._spent: set[tuple[int, str]] = set()
        self._lock = threading.Lock()
        self._http = httpx.Client(timeout=10, follow_redirects=False)

    def generate(self, model: str, prompt: str, *, search: bool = False, url_context: bool = False,
                 system: str | None = None, temperature: float | None = None) -> Result:
        tools = []
        if search:
            tools.append(types.Tool(google_search=types.GoogleSearch()))
        if url_context:
            tools.append(types.Tool(url_context=types.UrlContext()))
        config = types.GenerateContentConfig(
            tools=tools or None, system_instruction=system, temperature=temperature,
        )
        resp = self._call(model, prompt, config)
        cand = (resp.candidates or [None])[0]
        usage = resp.usage_metadata
        out = Result(
            text=(resp.text or "").strip(),
            model=model,
            tokens_in=(usage.prompt_token_count or 0) if usage else 0,
            tokens_out=(usage.candidates_token_count or 0) if usage else 0,
        )
        if cand is not None:
            out.search_queries, out.sources = self._grounding(cand)
        return out

    def _call(self, model: str, prompt: str, config: types.GenerateContentConfig):
        waits_429 = {i: list(RETRY_WAITS_429) for i in range(len(self.clients))}
        waits_5xx = list(RETRY_WAITS_5XX)
        while True:
            i = self._acquire(model)  # raises GeminiQuotaExhausted once every key is spent
            try:
                return self.clients[i].models.generate_content(model=model, contents=prompt, config=config)
            except errors.APIError as e:
                if e.code == 429:
                    daily = "PerDay" in str(e.details)  # daily cap: waiting won't help today
                    if daily or not waits_429[i]:
                        self._spent.add((i, model))
                        why = "daily free-tier limit reached" if daily else e.message or "quota exhausted"
                        log.warning("%s: key %d/%d out of quota (%s)", model, i + 1, len(self.clients), why)
                        continue
                    wait = waits_429[i].pop(0)
                    self._hold(i, model, wait)  # other keys keep working meanwhile
                elif e.code and e.code >= 500:
                    if not waits_5xx:
                        raise
                    wait = waits_5xx.pop(0)
                    time.sleep(wait)
                else:
                    raise
                log.info("%s: key %d HTTP %s, retrying in %ss", model, i + 1, e.code, wait)

    def _acquire(self, model: str) -> int:
        """The live key that is free soonest for this model; waits out its pacing gap."""
        with self._lock:
            live = [i for i in range(len(self.clients)) if (i, model) not in self._spent]
            if not live:
                raise GeminiQuotaExhausted(f"{model}: every key is out of quota")
            i = min(live, key=lambda k: self._next.get((k, model), 0))
            wait = self._next.get((i, model), 0) - time.monotonic()
            if wait > 0:
                time.sleep(wait)
            rpm = self.rpm.get(model)
            self._next[(i, model)] = time.monotonic() + (60 / rpm if rpm else 0)
            return i

    def _hold(self, i: int, model: str, seconds: float) -> None:
        with self._lock:
            self._next[(i, model)] = max(self._next.get((i, model), 0), time.monotonic() + seconds)

    def _grounding(self, cand) -> tuple[list[str], list[dict]]:
        sources: dict[str, dict] = {}
        gm = cand.grounding_metadata
        for ch in (gm.grounding_chunks or []) if gm else []:
            if ch.web and ch.web.uri:
                url = self._resolve(ch.web.uri)
                if not _is_search_page(url):
                    sources.setdefault(url, {"title": ch.web.title, "url": url})
        um = cand.url_context_metadata
        for m in (um.url_metadata or []) if um else []:
            if m.retrieved_url and "SUCCESS" in str(m.url_retrieval_status):
                sources.setdefault(m.retrieved_url, {"title": None, "url": m.retrieved_url})
        queries = list(gm.web_search_queries or []) if gm else []
        return queries, list(sources.values())

    def _resolve(self, uri: str) -> str:
        """Search-grounding URIs are vertexaisearch redirects; store where they point."""
        if "grounding-api-redirect" not in uri:
            return uri
        try:
            return self._http.head(uri).headers.get("location") or uri
        except httpx.HTTPError:
            return uri


def _is_search_page(url: str) -> bool:
    """Grounding sometimes cites a Google search results page (e.g. a time-zone lookup)."""
    return bool(re.match(r"https?://(www\.)?google\.[a-z.]+/search", url))


def write_models(cfg: dict) -> list[str]:
    """Primary write model, then fallbacks for when it's overloaded (503)."""
    first = cfg.get("write_model") or env("GEMINI_MODEL") or "gemini-flash-latest"
    return [first] + [m for m in cfg.get("write_fallback_models") or [] if m != first]
