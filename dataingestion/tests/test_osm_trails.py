import math

import pytest

from ingest.adapters.osm_trails import JUNK_NAME, OsmTrailsAdapter, build_trails, parks
from ingest.enrich.trails import (
    Elevation, climb, hike_minutes, length_km, resample, smooth, tobler_minutes,
)
from ingest.pipeline import finalize

from .conftest import load_fixture

# ~1 km due north, starting in Sweetwater Creek State Park
LINE = [(-84.628, 33.754), (-84.628, 33.763)]


@pytest.fixture
def elements():
    return load_fixture("osm_trails/overpass_sweetwater.json")["elements"]


@pytest.fixture
def adapter(city):
    return OsmTrailsAdapter(city)


def raw_trail(t, elevations=None):
    samples = resample(t["coords"], 30, 200)
    return {**t, "samples": samples, "elevations": elevations}


class StubElevation:
    """Real OpenTopoData response shape, faked per point."""

    def __init__(self, dataset, db, rise_per_point=0.0):
        self.dataset, self.db, self.rise = dataset, db, rise_per_point
        self.disabled_reason = None

    def profile(self, points):
        return [200 + i * self.rise for i in range(len(points))]


# ---- trail math ----

def test_flat_tobler_speed_is_5_kmh():
    assert tobler_minutes(LINE) == pytest.approx(length_km(LINE) / (6 * math.exp(-0.175)) * 60)


def test_uphill_is_slower_than_downhill_and_out_and_back_counts_both():
    pts = resample(LINE, 30, 200)
    up = [i * 3.0 for i in range(len(pts))]  # ~10% grade
    assert tobler_minutes(pts, up) > tobler_minutes(pts[::-1], up[::-1])
    assert hike_minutes(pts, up, loop=False) == pytest.approx(
        tobler_minutes(pts, up) + tobler_minutes(pts[::-1], up[::-1]))
    assert hike_minutes(pts, up, loop=True) == pytest.approx(tobler_minutes(pts, up))


def test_resample_spacing_and_cap():
    pts = resample(LINE, 30, 200)
    assert pts[0] == LINE[0] and pts[-1] == LINE[-1]
    gaps = [length_km([a, b]) * 1000 for a, b in zip(pts, pts[1:])]
    assert max(gaps) <= 30 and min(gaps) > 29
    assert len(resample(LINE, 1, 50)) == 50  # long line: spacing grows instead


def test_smooth_removes_single_spikes_before_climb():
    noisy = [100, 100, 130, 100, 100]
    assert climb(noisy) == (30, 30)
    assert climb(smooth(noisy)) == (0, 0)


def test_elevation_without_mongo_never_calls_the_network():
    assert Elevation("ned10m", db=None).profile([(-84.1234, 33.4321)]) is None


def test_opentopodata_fixture_shape():
    data = load_fixture("osm_trails/opentopodata_ned10m.json")
    assert data["status"] == "OK"
    assert all(isinstance(r["elevation"], float) for r in data["results"])


# ---- building trails from Overpass ----

def test_parks_from_multipolygon_relation(elements):
    names = [n for _, n in parks(elements)]
    assert names == ["Sweetwater Creek State Park"]


def test_builds_named_trails_in_their_park(elements):
    trails = {t["name"]: t for t in build_trails(elements) if length_km(t["coords"]) >= 1}
    assert {"Red Trail", "Yellow Trail", "White Trail", "Orange Trail"} <= set(trails)
    assert all(t["park"] == "Sweetwater Creek State Park" for t in trails.values())
    for t in trails.values():
        lng, lat = t["coords"][0]
        assert -85 < lng < -84 and 33 < lat < 34.5, "coords must be (lng, lat)"
        assert t["id"].startswith("ways/")


def test_junk_names_are_not_trails(elements):
    names = {t["name"] for t in build_trails(elements)}
    assert not names & {"unmarked trail", "false trail", "Staff vehicles only", "Bronner Road", "Trail Connector"}
    assert JUNK_NAME.search("Old Orange Trail") and not JUNK_NAME.search("Group Area Trail")


def _way(id, nodes, coords, name="Ridge Trail"):
    return {"type": "way", "id": id, "nodes": nodes, "tags": {"highway": "path", "name": name},
            "geometry": [{"lon": x, "lat": y} for x, y in coords]}


def test_same_name_disconnected_ways_stay_separate():
    a = _way(1, [1, 2], [(-84.40, 33.70), (-84.40, 33.71)])
    b = _way(2, [2, 3], [(-84.40, 33.71), (-84.40, 33.72)])  # shares node 2 with a
    c = _way(3, [8, 9], [(-84.30, 33.70), (-84.30, 33.71)])  # elsewhere
    trails = build_trails([a, b, c])
    assert sorted(round(length_km(t["coords"]), 1) for t in trails) == [1.1, 2.2]


def test_pieces_split_by_a_road_crossing_are_joined():
    a = _way(1, [1, 2], [(-84.40, 33.70), (-84.40, 33.71)])
    b = _way(2, [3, 4], [(-84.40, 33.7102), (-84.40, 33.72)])  # ~22 m gap, no shared node
    (t,) = build_trails([a, b])
    assert length_km(t["coords"]) == pytest.approx(2.2, abs=0.05)


def test_route_relation_wins_over_its_member_ways():
    a = _way(1, [1, 2], [(-84.40, 33.70), (-84.40, 33.71)], name="Blue Blaze")
    b = _way(2, [2, 3], [(-84.40, 33.71), (-84.40, 33.72)], name="Blue Blaze")
    rel = {"type": "relation", "id": 42, "tags": {"route": "hiking", "name": "Mountain Loop"},
           "members": [{"type": "way", "ref": w["id"], "role": "", "geometry": w["geometry"]} for w in (a, b)]}
    (t,) = build_trails([rel, a, b])
    assert t["id"] == "relation/42" and t["name"] == "Mountain Loop"
    assert t["url"] == "https://www.openstreetmap.org/relation/42"


# ---- adapter ----

def test_normalize_with_elevation(adapter, elements):
    t = next(t for t in build_trails(elements) if t["name"] == "Yellow Trail")
    raw = raw_trail(t)
    raw["elevations"] = StubElevation("ned10m", None, rise_per_point=1.0).profile(raw["samples"])
    (act,) = adapter.normalize(raw)
    assert (act.kind, act.category, act.venueName) == ("place", "hike", "Sweetwater Creek State Park")
    assert act.sourceKeys == [f"osm:{t['id']}"]
    assert act.trail.ascentM == len(raw["samples"]) - 1 and act.trail.descentM == 0
    assert act.duration.source == "trail_model" and act.duration.sigma == 0.25
    assert "out_and_back" in act.tags and "round trip" in act.description
    assert act.trail.geometry["type"] == "LineString"
    assert act.hoursSource == "default" and len(act.weeklyHours) == 7
    assert (act.weeklyHours[0].open, act.weeklyHours[0].close) == (6 * 60, 18 * 60)  # Sunday 06:00-18:00
    assert (act.weeklyHours[6].open, act.weeklyHours[6].close) == (6 * 1440 + 360, 6 * 1440 + 1080)
    finalize(act)
    assert act.expiresAt is None and act.duration.source == "trail_model"  # places don't expire


def test_normalize_without_elevation_assumes_flat(adapter, elements):
    t = next(t for t in build_trails(elements) if t["name"] == "Orange Trail" and length_km(t["coords"]) > 1)
    (act,) = adapter.normalize(raw_trail(t))
    assert act.trail.loop and act.trail.ascentM is None
    assert "climbing" not in act.description
    assert act.duration.medianMin == pytest.approx(tobler_minutes(resample(t["coords"], 30, 200)), abs=0.1)


def test_fetch_filters_by_length(adapter, elements, monkeypatch):
    monkeypatch.setattr(adapter.http, "get_json", lambda *a, **k: {"elements": elements})
    adapter.elevation = StubElevation("ned10m", None)
    trails = list(adapter.fetch())
    assert trails and all(length_km(t["coords"]) >= 1 for t in trails)
    assert adapter.skip_reasons["shorter than min_length_km"] > 0
    assert all(t["elevations"] for t in trails)


def test_query_uses_city_radius_and_center(adapter, city):
    q = adapter.query()
    assert f"around:{round(city.hikes_radius_km * 1000)},{city.center.lat},{city.center.lng}" in q
