"""Small geo helpers: geohash, haversine, bbox grid cells."""

import math
from dataclasses import dataclass

_GEOHASH = "0123456789bcdefghjkmnpqrstuvwxyz"
EARTH_KM = 6371.0088


def geohash(lat: float, lng: float, precision: int = 9) -> str:
    lat_rng, lng_rng = [-90.0, 90.0], [-180.0, 180.0]
    out, ch, bit, even = [], 0, 0, True
    while len(out) < precision:
        rng, val = (lng_rng, lng) if even else (lat_rng, lat)
        mid = (rng[0] + rng[1]) / 2
        if val > mid:
            ch, rng[0] = ch * 2 + 1, mid
        else:
            ch, rng[1] = ch * 2, mid
        even = not even
        bit += 1
        if bit == 5:
            out.append(_GEOHASH[ch])
            ch = bit = 0
    return "".join(out)


def haversine_km(lat1: float, lng1: float, lat2: float, lng2: float) -> float:
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dp, dl = p2 - p1, math.radians(lng2 - lng1)
    a = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * EARTH_KM * math.asin(math.sqrt(a))


@dataclass(frozen=True)
class Cell:
    south: float
    west: float
    north: float
    east: float

    @property
    def center(self) -> tuple[float, float]:
        return (self.south + self.north) / 2, (self.west + self.east) / 2

    @property
    def size_km(self) -> float:
        """Length of the cell's longer side."""
        lat, _ = self.center
        return max(
            haversine_km(self.south, self.west, self.north, self.west),
            haversine_km(lat, self.west, lat, self.east),
        )

    @property
    def radius_m(self) -> float:
        """Radius of the circle that covers the whole cell (half the diagonal)."""
        return haversine_km(self.south, self.west, self.north, self.east) * 1000 / 2

    def split(self) -> list["Cell"]:
        lat, lng = self.center
        return [
            Cell(self.south, self.west, lat, lng),
            Cell(self.south, lng, lat, self.east),
            Cell(lat, self.west, self.north, lng),
            Cell(lat, lng, self.north, self.east),
        ]


def grid(south: float, west: float, north: float, east: float, cell_km: float) -> list[Cell]:
    """Cover a bbox with roughly cell_km x cell_km cells."""
    mid_lat = (south + north) / 2
    rows = max(1, math.ceil(haversine_km(south, west, north, west) / cell_km))
    cols = max(1, math.ceil(haversine_km(mid_lat, west, mid_lat, east) / cell_km))
    dlat, dlng = (north - south) / rows, (east - west) / cols
    return [
        Cell(south + r * dlat, west + c * dlng, south + (r + 1) * dlat, west + (c + 1) * dlng)
        for r in range(rows)
        for c in range(cols)
    ]
