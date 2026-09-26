"""Rotation across API keys from several accounts (§7).

Keys are used in order. Each key has its own quota row (quota.key_api), and the ring moves to
the next key when the current one reaches our cap or the API refuses it (429/403). The first
key is the cache identity: responses are cached as if key 1 fetched them, so the disk cache
stays valid whichever key did.
"""

import logging

from pymongo.database import Database

from .config import require_keys
from .quota import QuotaExceeded, key_api, reserve

log = logging.getLogger(__name__)


class KeyRing:
    def __init__(self, env_name: str, db: Database | None, hint: str = ""):
        self.env_name = env_name
        self.keys = require_keys(env_name, hint)
        self.db = db
        self.index = 0

    @property
    def key(self) -> str:
        return self.keys[self.index]

    @property
    def cache_key(self) -> str:
        return self.keys[0]

    def label(self) -> str:
        return f"key {self.index + 1}/{len(self.keys)}"

    def reserve(self, api: str, n: int = 1) -> int:
        """Take n calls from the current key's quota, moving on to the next key when it's spent."""
        while True:
            try:
                return reserve(self.db, key_api(api, self.index), n)
            except QuotaExceeded as e:
                if not self.rotate(str(e)):
                    raise QuotaExceeded(f"{api}: every {self.env_name} key is at its cap") from None

    def rotate(self, reason: str) -> bool:
        """Switch to the next key for the rest of the run. False when there is none."""
        if self.index + 1 >= len(self.keys):
            return False
        self.index += 1
        log.warning("%s: switching to %s (%s)", self.env_name, self.label(), reason)
        return True
