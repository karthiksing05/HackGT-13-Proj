"""Adapter contract: fetch() yields raw source records, normalize() turns one into Activities."""

from collections import Counter
from typing import Iterable

from pymongo.database import Database

from ..config import City, load_global
from ..models import Activity


class Adapter:
    name: str = ""

    def __init__(self, city: City, db: Database | None = None, dry_run: bool = False):
        self.city = city
        self.db = db
        self.dry_run = dry_run
        self.cfg = load_global()
        self.errors: list[str] = []
        self.skipped = 0
        self.skip_reasons: Counter[str] = Counter()

    def estimate(self) -> dict[str, int] | None:
        """Calls per quota-limited API this run would make, or None if the source is free.
        Adapters that return a dict are not fetched on --dry-run (§5.2 budget check)."""
        return None

    def fetch(self) -> Iterable[dict]:
        raise NotImplementedError

    def normalize(self, raw: dict) -> Iterable[Activity]:
        raise NotImplementedError

    def skip(self, reason: str = "other") -> list:
        self.skipped += 1
        self.skip_reasons[reason] += 1
        return []
