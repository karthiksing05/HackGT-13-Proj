import json
import os
from pathlib import Path

import pytest
from pymongo import MongoClient
from pymongo.errors import PyMongoError

from ingest.config import load_city, load_global

FIXTURES = Path(__file__).resolve().parent.parent / "fixtures"


def load_fixture(rel: str):
    return json.loads((FIXTURES / rel).read_text())


@pytest.fixture
def city():
    return load_city("atlanta")


@pytest.fixture
def test_db():
    """A throwaway database on local Mongo; tests using it skip if Mongo isn't running."""
    client = MongoClient(os.environ.get("TEST_MONGODB_URI", "mongodb://localhost:27017"), tz_aware=True, serverSelectionTimeoutMS=1000)
    try:
        client.admin.command("ping")
    except PyMongoError:
        pytest.skip("local MongoDB not running")
    db = client["freetime_test"]
    client.drop_database(db.name)
    yield db
    client.drop_database(db.name)


def gemini_cfg() -> dict:
    """Agent config with Gemini as the research provider (tests fake the Gemini client)."""
    return {**load_global()["blurb"], "research_provider": "gemini"}
