"""Duration resolution (§6.2). Only event times and category priors so far;
source fields, Google typical time, trails and the LLM slot in above the prior."""

from ..models import Activity, Duration

# category -> (median minutes, sigma)
CATEGORY_PRIORS: dict[str, tuple[float, float]] = {
    "museum": (120, 0.35), "gallery": (50, 0.40), "park": (60, 0.50), "garden": (90, 0.35),
    "zoo_aquarium": (180, 0.30), "landmark": (30, 0.50), "viewpoint": (20, 0.50), "cafe": (45, 0.40),
    "restaurant": (75, 0.30), "bar": (90, 0.40), "nightclub": (180, 0.35), "community_event": (90, 0.40),
    "live_music": (150, 0.30), "comedy": (100, 0.20), "theater": (150, 0.20), "cinema": (140, 0.15),
    "sports_event": (180, 0.20), "rec_venue": (90, 0.30), "market": (60, 0.45), "shopping": (60, 0.50),
    "festival": (150, 0.45), "class_workshop": (120, 0.25), "tour": (90, 0.30), "other": (60, 0.50),
    "hike": (120, 0.40),  # placeholder until the trail model (§5.10)
}

MAX_FIXED_SPAN_MIN = 4 * 60
EVENT_TIMES_SIGMA = 0.1  # people mostly stay for the whole show


def prior(category: str) -> Duration:
    median, sigma = CATEGORY_PRIORS.get(category, CATEGORY_PRIORS["other"])
    return Duration.lognormal(median, sigma, "category_prior")


def resolve(act: Activity) -> None:
    """Set act.duration (and attendance for long events) unless already set."""
    if act.duration is not None:
        return
    if act.kind == "event" and act.start and act.end and act.end > act.start:
        span_min = (act.end - act.start).total_seconds() / 60
        if span_min <= MAX_FIXED_SPAN_MIN:
            act.duration = Duration.lognormal(span_min, EVENT_TIMES_SIGMA, "event_times")
            act.attendance = "fixed_start"
            return
        act.attendance = "drop_in"  # long window: come and go, stay for the category's usual time
    act.duration = prior(act.category)
