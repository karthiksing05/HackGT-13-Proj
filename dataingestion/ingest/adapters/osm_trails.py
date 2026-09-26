"""OpenStreetMap trails (§5.10): hikes with length, climb and a computed duration.

One Overpass query around the city centre (city.hikes_radius_km) returns hiking route
relations, named paths/tracks/footways and park/reserve areas. Trails are built from:
- route relations: member ways merged into one line (the longest connected piece);
- other named ways: grouped by name, then split into connected networks (shared nodes, or
  ends within GAP_M: road crossings are separate unnamed ways), so two unrelated
  "Yellow Trail"s in different parks never merge but a greenway crossing parks stays whole.
Elevation comes from OpenTopoData; duration from Tobler's hiking function (enrich/trails.py).

Verified against a live response (fixtures/osm_trails/overpass_sweetwater.json): parks can be
multipolygon relations whose outer members carry their own geometry, many named ways are
not trails ("unmarked trail", "Staff vehicles only", "Bronner Road"), and greenways (PATH,
Stone Mountain Trail) are mostly highway=cycleway.
"""

import hashlib
import logging
import re
from collections import defaultdict
from datetime import datetime, timezone
from typing import Iterable

from shapely import LineString, STRtree
from shapely.ops import linemerge, polygonize, unary_union

from ..enrich.trails import Elevation, climb, hike_minutes, length_km, resample, smooth, trail_duration
from ..geo import haversine_km
from ..http import Http
from ..models import Activity, GeoPoint, HoursInterval, SourceRef, Trail
from .base import Adapter

log = logging.getLogger(__name__)

OVERPASS_URL = "https://overpass-api.de/api/interpreter"
CACHE_TTL = 7 * 24 * 3600
LOOP_M = 100  # endpoints this close = a loop
GAP_M = 50  # pieces of one trail this close are joined (road crossings, small mapping gaps)
SIMPLIFY_DEG = 0.00005  # ~5 m; keeps the stored geometry small

QUERY = """[out:json][timeout:180];
(
  relation["route"~"^(hiking|foot)$"](around:{r},{lat},{lng});
  way["highway"~"^(path|track|cycleway)$"]["name"]["access"!~"^(private|no)$"]["foot"!="no"](around:{r},{lat},{lng});
  way["highway"="footway"]["name"]["footway"!="sidewalk"](around:{r},{lat},{lng});
  way["leisure"~"^(park|nature_reserve)$"](around:{r},{lat},{lng});
  relation["leisure"~"^(park|nature_reserve)$"](around:{r},{lat},{lng});
  way["boundary"="protected_area"](around:{r},{lat},{lng});
  relation["boundary"="protected_area"](around:{r},{lat},{lng});
);
out geom;"""

AREA_TAGS = {"leisure": {"park", "nature_reserve"}, "boundary": {"protected_area"}}

# Named ways that aren't trails someone would plan a hike on: informal paths, street
# furniture, roads, generic labels and bare numbers.
JUNK_NAME = re.compile(
    r"\b(unmarked|unofficial|false|abandoned|closed|social|old|cut-?off|connector|staff|vehicles? only"
    r"|crossing|crosswalk|sidewalk|signal|plaza|underpass|overpass|access|school|academy)\b"
    r"|\b(road|rd|street|st|drive|dr|avenue|ave|lane|ln|parkway|pkwy|highway|hwy|way|circle|court|ct)\.?"
    r"(\s+(north|south|east|west|northeast|northwest|southeast|southwest|ne|nw|se|sw))?$"
    r"|^(multi-?use|shared[- ]use|foot|pedestrian|walking|bike|hiking|nature|connecting|paved)\s+(path|trail|pathway|walk)$"
    r"|^[^a-z]*$",
    re.I,
)


def daily_hours(open_hhmm: str, close_hhmm: str) -> list[HoursInterval]:
    """The same local hours every day, as minutes since Sunday 00:00."""
    def minutes(hhmm):
        h, m = hhmm.split(":")
        return int(h) * 60 + int(m)

    o, c = minutes(open_hhmm), minutes(close_hhmm)
    return [HoursInterval(open=d * 1440 + o, close=d * 1440 + c) for d in range(7)]


def _line(geometry: list[dict] | None) -> LineString | None:
    pts = [(p["lon"], p["lat"]) for p in geometry or [] if p]
    return LineString(pts) if len(pts) >= 2 else None


def _dist_m(a: tuple[float, float], b: tuple[float, float]) -> float:
    return haversine_km(a[1], a[0], b[1], b[0]) * 1000


def _chain(geom) -> LineString:
    """One line from merged pieces: start with the longest and keep attaching the piece whose
    end is nearest either end of the line, while that gap is under GAP_M."""
    parts = [list(g.coords) for g in getattr(geom, "geoms", [geom])]
    parts.sort(key=length_km, reverse=True)
    line, rest = parts[0], parts[1:]
    while rest:
        best = None
        for i, part in enumerate(rest):
            for q in (part, part[::-1]):
                for at_end, d in ((True, _dist_m(line[-1], q[0])), (False, _dist_m(line[0], q[-1]))):
                    if d <= GAP_M and (best is None or d < best[0]):
                        best = (d, i, at_end, q)
        if best is None:
            break
        _, i, at_end, q = best
        line = line + q if at_end else q + line
        rest.pop(i)
    return LineString(line)


def _is_area(tags: dict) -> bool:
    return any(tags.get(k) in v for k, v in AREA_TAGS.items())


def parks(elements: list[dict]) -> list[tuple[object, str]]:
    """(polygon, name) for every named park/reserve/protected area."""
    out = []
    for el in elements:
        tags = el.get("tags") or {}
        if not tags.get("name") or not _is_area(tags):
            continue
        if el["type"] == "way":
            line = _line(el.get("geometry"))
            lines = [line] if line is not None and line.is_ring else []
        else:
            lines = [ln for m in el.get("members") or [] if m.get("role") in ("outer", "")
                     for ln in [_line(m.get("geometry"))] if ln is not None]
        if not lines:
            continue
        try:
            merged = linemerge(lines) if len(lines) > 1 else lines[0]
            poly = unary_union(list(polygonize(getattr(merged, "geoms", [merged]))))
        except Exception:  # broken multipolygon in OSM; skip the area, not the run
            continue
        if not poly.is_empty:
            out.append((poly, tags["name"]))
    return out


class _ParkIndex:
    def __init__(self, areas: list[tuple[object, str]]):
        self.areas = areas
        self.tree = STRtree([a for a, _ in areas]) if areas else None

    def name_at(self, line: LineString) -> str | None:
        """Smallest named area containing the line's midpoint."""
        if self.tree is None:
            return None
        mid = line.interpolate(0.5, normalized=True)
        hits = [self.areas[i] for i in self.tree.query(mid, predicate="within")]
        return min(hits, key=lambda h: h[0].area)[1] if hits else None


def _components(ways: list[dict]) -> list[list[dict]]:
    """Split ways into networks that share a node or have ends within GAP_M (union-find)."""
    parent = list(range(len(ways)))

    def find(i):
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i

    owner: dict[int, int] = {}
    for i, w in enumerate(ways):
        for node in w.get("nodes") or []:
            if node in owner:
                parent[find(i)] = find(owner[node])
            else:
                owner[node] = i
    ends = [(i, (p["lon"], p["lat"])) for i, w in enumerate(ways) for p in (w["geometry"][0], w["geometry"][-1])]
    for a, (i, pa) in enumerate(ends):
        for j, pb in ends[a + 1:]:
            if i != j and _dist_m(pa, pb) <= GAP_M:
                parent[find(i)] = find(j)
    groups: dict[int, list[dict]] = defaultdict(list)
    for i, w in enumerate(ways):
        groups[find(i)].append(w)
    return list(groups.values())


def build_trails(elements: list[dict]) -> list[dict]:
    """Overpass elements -> [{id, url, name, park, coords}], coords as (lng, lat)."""
    index = _ParkIndex(parks(elements))
    trails, in_route = [], set()

    for rel in (e for e in elements if e["type"] == "relation" and (e.get("tags") or {}).get("route")):
        members = [m for m in rel.get("members") or [] if m.get("type") == "way"]
        lines = [ln for m in members for ln in [_line(m.get("geometry"))] if ln is not None]
        name = ((rel.get("tags") or {}).get("name") or "").strip()
        if not lines or not name or JUNK_NAME.search(name):
            continue  # its ways can still become trails by their own names
        in_route.update(m["ref"] for m in members)
        line = _chain(linemerge(lines))
        trails.append({
            "id": f"relation/{rel['id']}", "url": f"https://www.openstreetmap.org/relation/{rel['id']}",
            "name": name, "park": index.name_at(line), "coords": list(line.coords),
        })

    groups: dict[str, list[dict]] = defaultdict(list)
    for way in elements:
        tags = way.get("tags") or {}
        name = (tags.get("name") or "").strip()
        if way["type"] != "way" or not tags.get("highway") or way["id"] in in_route or not name or JUNK_NAME.search(name):
            continue
        if _line(way.get("geometry")) is not None:
            groups[name].append(way)

    for name, ways in groups.items():
        for comp in _components(ways):
            line = _chain(linemerge([_line(w["geometry"]) for w in comp]))
            ids = sorted(w["id"] for w in comp)
            trails.append({
                "id": "ways/" + hashlib.sha1(",".join(map(str, ids)).encode()).hexdigest()[:16],
                "url": f"https://www.openstreetmap.org/way/{ids[0]}",
                "name": name, "park": index.name_at(line), "coords": list(line.coords),
            })
    return trails


class OsmTrailsAdapter(Adapter):
    name = "osm_trails"

    def __init__(self, *args, **kw):
        super().__init__(*args, **kw)
        self.ot = self.cfg["osm_trails"]
        self.http = Http(self.name, timeout=240)  # Overpass may take up to the query's 180 s
        self.elevation = Elevation(self.city.elevation_dataset, self.db)

    # ---- fetch ----

    def query(self) -> str:
        c = self.city.center
        return QUERY.format(r=round(self.city.hikes_radius_km * 1000), lat=c.lat, lng=c.lng)

    def fetch(self) -> Iterable[dict]:
        data = self.http.get_json(OVERPASS_URL, {"data": self.query()}, cache_ttl=CACHE_TTL)
        if "error" in (data.get("remark") or "").lower():  # Overpass reports timeouts in a 200 body
            raise RuntimeError(f"Overpass: {data['remark']}")
        trails = build_trails(data.get("elements") or [])
        # Official routes and trails inside parks first, in case the elevation quota runs out.
        trails.sort(key=lambda t: (not t["id"].startswith("relation/"), t["park"] is None))
        for t in trails:
            km = length_km(t["coords"])
            if km < self.ot["min_length_km"]:
                self.skip("shorter than min_length_km")
                continue
            if km > self.ot["max_length_km"]:
                self.skip("longer than max_length_km")
                continue
            t["samples"] = resample(t["coords"], self.ot["sample_m"], self.ot["max_samples"])
            t["elevations"] = self.elevation.profile(t["samples"])
            yield t
        if self.elevation.disabled_reason:
            self.errors.append(f"elevation stopped early, later trails assume flat ground: {self.elevation.disabled_reason}")

    # ---- normalize ----

    def normalize(self, raw: dict) -> Iterable[Activity]:
        coords = raw["coords"]
        (x1, y1), (x2, y2) = coords[0], coords[-1]
        loop = haversine_km(y1, x1, y2, x2) * 1000 <= LOOP_M
        km = length_km(coords)
        elevations = smooth(raw["elevations"]) if raw.get("elevations") else None
        ascent, descent = climb(elevations) if elevations else (None, None)
        minutes = hike_minutes(raw["samples"], elevations, loop)
        geometry = LineString(coords).simplify(SIMPLIFY_DEG)

        return [
            Activity(
                kind="place",
                city=self.city.slug,
                name=raw["name"],
                description=self._description(km, loop, ascent),
                category="hike",
                sourceCategory="osm route relation" if raw["id"].startswith("relation/") else "osm named path",
                tags=["outdoors", "hiking", "loop" if loop else "out_and_back"],
                location=GeoPoint.at(y1, x1),  # trail start
                venueName=raw.get("park"),
                timezone=self.city.timezone,
                weeklyHours=daily_hours(*self.ot["hours"]),
                hoursSource="default",
                duration=trail_duration(minutes),
                trail=Trail(
                    lengthKm=round(km, 2),
                    ascentM=round(ascent) if ascent is not None else None,
                    descentM=round(descent) if descent is not None else None,
                    loop=loop,
                    geometry={"type": "LineString",
                              "coordinates": [[round(x, 5), round(y, 5)] for x, y in geometry.coords]},
                ),
                url=raw["url"],
                sourceKeys=[f"osm:{raw['id']}"],
                sources=[SourceRef(name=self.name, id=raw["id"], url=raw["url"], fetchedAt=datetime.now(timezone.utc))],
            )
        ]

    @staticmethod
    def _description(km: float, loop: bool, ascent: float | None) -> str:
        text = f"Loop trail, {km:.1f} km." if loop else f"Out-and-back trail, {km:.1f} km each way ({2 * km:.1f} km round trip)."
        if ascent is not None:
            text += f" About {round(ascent)} m of climbing{'' if loop else ' on the way out'}."
        return text
