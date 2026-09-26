"""The backfill's additions to the pipeline's embedding-text contract (activity_text.py)."""
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import activity_text as at  # noqa: E402

DAY = 1440


def daily(open_min, close_min, days=range(7)):
    return [{"open": d * DAY + open_min, "close": d * DAY + close_min} for d in days]


def test_hours_groups_days_monday_first():
    hours = daily(660, 1260, range(1, 6)) + daily(600, 1380, (0, 6))
    assert at.hours_text(hours) == "Mon-Fri 11:00 AM-9:00 PM; Sat-Sun 10:00 AM-11:00 PM"
    assert at.hours_text(daily(360, 1080)) == "daily 6:00 AM-6:00 PM"


def test_hours_past_midnight_and_past_saturday():
    # Saturday 5 PM until Sunday 1 AM is stored with close < open (wraps past the week's end)
    hours = [{"open": 4 * DAY + 1020, "close": 4 * DAY + 1380},
             {"open": 5 * DAY + 1020, "close": 6 * DAY + 60},
             {"open": 6 * DAY + 1020, "close": 60}]
    assert at.hours_text(hours) == "Thu 5:00 PM-11:00 PM; Fri-Sat 5:00 PM-1:00 AM"


def test_hours_round_the_clock():
    assert at.hours_text([{"open": 0, "close": 7 * DAY}]) == "open 24 hours every day"
    assert at.hours_text([{"open": DAY, "close": 2 * DAY}]) == "Mon open 24 hours"
    assert at.hours_text([]) is None and at.hours_text(None) is None


def place(**kw):
    doc = {"name": "Red Oak Victory", "kind": "place", "category": "museum", "sourceCategory": "museum",
           "address": {"locality": "Richmond", "region": "CA"}, "timezone": "America/Los_Angeles",
           "rating": 4.7, "ratingCount": 175, "weeklyHours": daily(600, 960, (0, 6)), "hoursSource": "google"}
    return {**doc, **kw}


def test_backfill_input_adds_sourced_hours_after_the_area():
    lines = at.backfill_input(place()).split("\n")
    assert lines[lines.index("Area: Richmond, CA") + 1] == "Opening hours: Sat-Sun 10:00 AM-4:00 PM"
    assert lines[-1] == "Rating: 4.7 from 175 reviews"


def test_backfill_input_is_the_pipeline_input_otherwise():
    for doc in (place(hoursSource="default"), place(weeklyHours=None), place(kind="event")):
        assert at.backfill_input(doc) == at.event_data(doc, None)
    event = {"name": "House Rules", "kind": "event", "timezone": "America/Los_Angeles",
             "start": datetime(2026, 9, 25, 5, tzinfo=timezone.utc), "end": datetime(2026, 9, 25, 10, tzinfo=timezone.utc)}
    assert "Starts: Thursday 10:00 PM" in at.backfill_input(event)


def test_check_and_hashes():
    good = "Interests:\n- maritime history\n\nActivities:\n- ship tour"
    assert at.check(good) is None
    assert "sentence" in at.check("Interests:\n- a very long phrase that keeps going on and on and on")
    assert at.check("Cost:\n- unknown").startswith("it uses a placeholder")
    assert at.input_hash("T", "data") != at.input_hash("T2", "data")
    assert len(at.text_hash(good)) == 40
