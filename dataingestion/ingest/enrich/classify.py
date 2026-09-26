"""Map source categories to our fixed vocabulary (§4.3) via seeds/category_maps.yaml.
Unmapped values return "other" and are left for the LLM classifier (§6.3)."""

from ..config import category_maps


def ticketmaster_category(segment: str | None, genre: str | None) -> str:
    m = category_maps()["ticketmaster"]
    return m["genre"].get(genre) or m["segment"].get(segment) or "other"


def google_category(primary_type: str | None, types: list[str]) -> str | None:
    """Classify by primaryType; fall back to types[] only when there is no primaryType.
    Returns None when unmapped: a hair salon tagged with a secondary `art_gallery`
    type is not a gallery."""
    m = category_maps()["google"]
    for t in [primary_type] if primary_type else types:
        if t in m:
            return m[t]
    return None


def ra_category(genre_slugs: list[str | None], is_festival: bool | None) -> str:
    """RA is mostly club nights: first mapped genre wins, else the source default."""
    m = category_maps()["resident_advisor"]
    if is_festival:
        return "festival"
    return next((m["genre"][g] for g in genre_slugs if g in m["genre"]), m["default"])
