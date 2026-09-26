"""An on-disk embedding cache keyed by (model, text), shared by the uvicorn workers.

SQLite in WAL mode with one connection per thread: several processes and threads read and write it
concurrently, and a busy timeout covers the rare write collision. Blank texts are never cached
(they are zero rows by definition, see `embed_with_blanks`).
"""

from __future__ import annotations

import hashlib
import sqlite3
import threading
import time
from abc import ABC, abstractmethod
from pathlib import Path

import numpy as np

DEFAULT_MAX_ENTRIES = 100_000
_CHUNK = 500  # keys per SELECT ... IN (...)


class EmbeddingCache(ABC):
    @abstractmethod
    def get_many(self, model: str, texts: list[str]) -> list[np.ndarray | None]:
        """One entry per text, in order: the cached float32 vector or None."""

    @abstractmethod
    def put_many(self, model: str, texts: list[str], vectors: np.ndarray) -> None: ...

    @abstractmethod
    def info(self) -> dict: ...


class NullCache(EmbeddingCache):
    def get_many(self, model: str, texts: list[str]) -> list[np.ndarray | None]:
        return [None] * len(texts)

    def put_many(self, model: str, texts: list[str], vectors: np.ndarray) -> None:
        return None

    def info(self) -> dict:
        return {"enabled": False}


class SqliteEmbeddingCache(EmbeddingCache):
    def __init__(self, path: str | Path, max_entries: int = DEFAULT_MAX_ENTRIES) -> None:
        if max_entries < 1:
            raise ValueError(f"max_entries must be >= 1, got {max_entries}")
        self.path = Path(path)
        self.max_entries = max_entries
        self._local = threading.local()
        self._lock = threading.Lock()
        self._connections: set[sqlite3.Connection] = set()  # every thread's, for close()
        self.hits = 0
        self.misses = 0
        self.path.parent.mkdir(parents=True, exist_ok=True)
        conn = self._conn()
        conn.execute("PRAGMA journal_mode=WAL")
        conn.execute("PRAGMA synchronous=NORMAL")
        conn.execute(
            "CREATE TABLE IF NOT EXISTS embeddings ("
            " key TEXT PRIMARY KEY, model TEXT NOT NULL, dim INTEGER NOT NULL,"
            " vector BLOB NOT NULL, created_at REAL NOT NULL)"
        )
        conn.execute("CREATE INDEX IF NOT EXISTS embeddings_created_at ON embeddings(created_at)")
        conn.commit()

    @staticmethod
    def key(model: str, text: str) -> str:
        return hashlib.sha1(f"{model}\x00{text}".encode()).hexdigest()

    def _conn(self) -> sqlite3.Connection:
        conn = getattr(self._local, "conn", None)
        if conn is None:
            # Each thread keeps its own connection; `check_same_thread=False` only lets `close()`
            # (called from whichever thread shuts the service down) close the others' connections.
            conn = sqlite3.connect(str(self.path), timeout=5.0, check_same_thread=False)
            self._local.conn = conn
            with self._lock:
                self._connections.add(conn)
        return conn

    def close(self) -> None:
        """Close every thread's connection (tests and shutdown; a closed cache must not be reused)."""
        with self._lock:
            connections, self._connections = self._connections, set()
        for conn in connections:
            conn.close()
        self._local = threading.local()

    def get_many(self, model: str, texts: list[str]) -> list[np.ndarray | None]:
        out: list[np.ndarray | None] = [None] * len(texts)
        keyed = [(i, self.key(model, t)) for i, t in enumerate(texts) if t and t.strip()]
        if not keyed:
            return out
        conn = self._conn()
        found: dict[str, np.ndarray] = {}
        for start in range(0, len(keyed), _CHUNK):
            chunk = [k for _, k in keyed[start : start + _CHUNK]]
            marks = ",".join("?" * len(chunk))
            for key, dim, blob in conn.execute(
                f"SELECT key, dim, vector FROM embeddings WHERE key IN ({marks})", chunk
            ):
                vector = np.frombuffer(blob, dtype=np.float32)
                if vector.shape[0] == dim:
                    found[key] = vector.copy()
        hits = 0
        for i, key in keyed:
            if key in found:
                out[i] = found[key]
                hits += 1
        with self._lock:
            self.hits += hits
            self.misses += len(keyed) - hits
        return out

    def put_many(self, model: str, texts: list[str], vectors: np.ndarray) -> None:
        vectors = np.asarray(vectors, dtype=np.float32)
        if vectors.shape[0] != len(texts):
            raise ValueError(f"{len(texts)} texts but {vectors.shape[0]} vectors")
        rows = [
            (self.key(model, t), model, int(vectors.shape[1]), vectors[i].tobytes(), time.time())
            for i, t in enumerate(texts)
            if t and t.strip()
        ]
        if not rows:
            return
        conn = self._conn()
        with conn:
            conn.executemany(
                "INSERT OR REPLACE INTO embeddings (key, model, dim, vector, created_at) VALUES (?, ?, ?, ?, ?)", rows
            )
            excess = conn.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0] - self.max_entries
            if excess > 0:
                conn.execute(
                    "DELETE FROM embeddings WHERE rowid IN"
                    " (SELECT rowid FROM embeddings ORDER BY created_at ASC LIMIT ?)",
                    (excess,),
                )

    def size(self) -> int:
        return int(self._conn().execute("SELECT COUNT(*) FROM embeddings").fetchone()[0])

    def info(self) -> dict:
        with self._lock:
            hits, misses = self.hits, self.misses
        return {
            "enabled": True,
            "path": str(self.path),
            "entries": self.size(),
            "max_entries": self.max_entries,
            "hits": hits,
            "misses": misses,
        }
