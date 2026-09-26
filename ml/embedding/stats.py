"""Thread-safe counters about embedding calls, reported on `/healthz`."""

from __future__ import annotations

import threading
import time
from collections import Counter


class EmbedStats:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.calls = 0
        self.texts = 0
        self.cached = 0
        self.fallbacks = 0
        self.errors = 0
        self.latency_ms_total = 0.0
        self.by_provider: Counter[str] = Counter()  # uncached texts served, per provider
        self.by_kind: Counter[str] = Counter()
        self.last_provider: str | None = None
        self.last_call_at: float | None = None

    def record(self, *, kind: str, n: int, cached: int, provider: str, ms: float) -> None:
        with self._lock:
            self.calls += 1
            self.texts += n
            self.cached += cached
            self.latency_ms_total += ms
            self.by_kind[kind] += n
            if n > cached:
                self.by_provider[provider] += n - cached
                self.last_provider = provider
            self.last_call_at = time.time()

    def fallback(self) -> None:
        with self._lock:
            self.fallbacks += 1

    def error(self) -> None:
        with self._lock:
            self.errors += 1

    def snapshot(self) -> dict:
        with self._lock:
            return {
                "calls": self.calls,
                "texts": self.texts,
                "cached": self.cached,
                "fallbacks": self.fallbacks,
                "errors": self.errors,
                "avg_ms": round(self.latency_ms_total / self.calls, 1) if self.calls else None,
                "by_provider": dict(self.by_provider),
                "by_kind": dict(self.by_kind),
                "last_provider": self.last_provider,
                "last_call_at": self.last_call_at,
            }
