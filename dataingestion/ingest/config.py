"""Global config (config.yaml), city config (cities/<slug>.yaml) and env (§3)."""

import os
from functools import lru_cache
from pathlib import Path

import yaml
from dotenv import load_dotenv
from pydantic import BaseModel, ConfigDict

ROOT = Path(__file__).resolve().parent.parent  # dataingestion/
CACHE_DIR = ROOT / "cache"
OUT_DIR = ROOT / "out"

# The team keeps one .env at the repo root; a local one in dataingestion/ also works.
load_dotenv(ROOT.parent / ".env")
load_dotenv(ROOT / ".env")


class MissingConfig(RuntimeError):
    pass


def env(name: str, default: str | None = None) -> str | None:
    return os.environ.get(name) or default


# Alternate names teammates have used for the same key.
ENV_ALIASES = {
    "GOOGLE_MAPS_API_KEY": ["GOOGLE_MAPS_API_KEYS", "GOOGLE_CLOUD_API_KEYS", "GOOGLE_CLOUD_API_KEY"],
    "GEMINI_API_KEY": ["GEMINI_API_KEYS"],
}


def require_env(name: str, hint: str = "") -> str:
    value = env(name) or next((env(a) for a in ENV_ALIASES.get(name, []) if env(a)), None)
    if not value:
        raise MissingConfig(f"{name} is not set in .env. {hint}".strip())
    return value


def require_keys(name: str, hint: str = "") -> list[str]:
    """A comma-separated list of API keys (one per account) for rotation; a single key works too."""
    keys = list(dict.fromkeys(k.strip().strip("'\"") for k in require_env(name, hint).split(",")))
    keys = [k for k in keys if k]
    if not keys:
        raise MissingConfig(f"{name} is empty in .env. {hint}".strip())
    return keys


class LatLng(BaseModel):
    lat: float | None
    lng: float | None


class BBox(BaseModel):
    south: float
    west: float
    north: float
    east: float


class City(BaseModel):
    model_config = ConfigDict(extra="allow")

    slug: str
    name: str
    country_code: str
    default_region: str | None
    language: str
    timezone: str
    currency: str
    price_tiers: list[float]
    center: LatLng
    bbox: BBox
    neighborhoods: list[str] = []
    hikes_radius_km: float = 60
    elevation_dataset: str | None = None  # OpenTopoData dataset; None = no elevation
    query_terms: dict[str, list[str]] = {}
    ticketmaster: dict = {}
    resident_advisor: dict = {}
    adapters: dict[str, bool] = {}


@lru_cache
def load_global() -> dict:
    return yaml.safe_load((ROOT / "config.yaml").read_text())


@lru_cache
def load_city(slug: str | None = None) -> City:
    slug = slug or load_global()["default_city"]
    path = ROOT / "cities" / f"{slug}.yaml"
    if not path.exists():
        raise MissingConfig(f"No city config at {path}")
    return City.model_validate(yaml.safe_load(path.read_text()))


@lru_cache
def category_maps() -> dict:
    return yaml.safe_load((ROOT / "seeds" / "category_maps.yaml").read_text())
