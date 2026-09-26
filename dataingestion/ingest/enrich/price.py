"""Price helpers shared by adapters."""

from ..models import Price


def tier_for(amount: float, tiers: list[float]) -> int:
    """tiers are upper bounds for tiers 0..len-1; anything above the last is the next tier."""
    for i, bound in enumerate(tiers):
        if amount <= bound:
            return i
    return len(tiers)


def price_from_range(lo: float | None, hi: float | None, currency: str, tiers: list[float]) -> Price | None:
    if lo is None and hi is None:
        return None
    lo = lo if lo is not None else hi
    return Price(min=lo, max=hi, currency=currency, tier=tier_for(lo, tiers), isFree=(hi or lo) == 0)
