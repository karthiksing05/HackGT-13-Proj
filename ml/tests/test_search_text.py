"""Search texts: golden C, tags, who/pace/budget, timing in the viewer's zone, mood phrases."""

import json
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

from api.schemas.profile import SearchProfileRequest
from profiles import SearchInput, build_search_text, check, timing_bullets

FIXTURES = Path(__file__).parent / "fixtures"
FRIDAY_1410_NY = datetime(2026, 9, 25, 18, 10, tzinfo=timezone.utc)


def bullets(text: str, section: str) -> list[str]:
    out, current = [], None
    for line in text.splitlines():
        if line.endswith(":"):
            current = line[:-1]
        elif line.startswith("- ") and current == section:
            out.append(line[2:])
    return out


class GoldenTests(unittest.TestCase):
    def test_golden_c_jordan(self):
        golden = json.loads((FIXTURES / "search_jordan.json").read_text())
        text = build_search_text(SearchProfileRequest.model_validate(golden["request"]).to_input())
        self.assertEqual(text, golden["search_text"])
        self.assertEqual(bullets(text, "Timing"), ["friday afternoon", "weekday afternoon", "multi-hour outing"])
        self.assertEqual(bullets(text, "Social"), ["meeting new people", "small group of friends"])
        self.assertEqual(bullets(text, "Cost"), ["under $15 admission", "cheap food"])
        self.assertIsNone(check(text))
        self.assertFalse(text.endswith("\n"))


class BuilderTests(unittest.TestCase):
    def test_empty_request_is_empty_text(self):
        self.assertEqual(build_search_text(SearchInput()), "")
        self.assertEqual(build_search_text(SearchProfileRequest().to_input()), "")
        self.assertEqual(build_search_text(SearchInput(mood_text="  ", tags=[""])), "")

    def test_unknown_tag_goes_to_interests(self):
        self.assertEqual(build_search_text(SearchInput(tags=["Karaoke"])), "Interests:\n- karaoke")

    def test_tags_in_request_order_and_normalized(self):
        text = build_search_text(SearchInput(tags=["Meet people", "MEET_PEOPLE", "Music", "Outdoors"]))
        self.assertEqual(bullets(text, "Social"), ["meeting new people"])
        self.assertEqual(bullets(text, "Interests"), ["live music", "outdoor recreation"])
        self.assertEqual(bullets(text, "Activities"), ["social mixer"])

    def test_who_pace_budget(self):
        text = build_search_text(SearchInput(who="open", pace="packed", budget=0))
        self.assertEqual(bullets(text, "Social"), ["open group", "meeting new people"])
        self.assertEqual(bullets(text, "Pace"), ["fast-paced", "back-to-back activities"])
        self.assertEqual(bullets(text, "Cost"), ["free admission"])
        self.assertNotIn("Cost", build_search_text(SearchInput(budget=3, who="just_me")))
        self.assertEqual(build_search_text(SearchInput(who="just_me")), "Social:\n- solo outing")
        self.assertEqual(build_search_text(SearchInput(pace="chill")), "Pace:\n- relaxed rhythm\n- low-key")

    def test_timezone_changes_the_local_period(self):
        start = datetime(2026, 9, 26, 2, 30, tzinfo=timezone.utc)  # Fri 22:30 in New York, Sat 11:30 in Tokyo
        self.assertEqual(timing_bullets(start, None, "UTC"), ["saturday morning", "weekend morning"])
        self.assertEqual(timing_bullets(start, None, "America/New_York"), ["friday night", "weekday night"])
        self.assertEqual(timing_bullets(start, None, "Asia/Tokyo"), ["saturday morning", "weekend morning"])
        self.assertEqual(timing_bullets(start, None, "Not/AZone"), ["saturday morning", "weekend morning"])
        self.assertEqual(timing_bullets(start, None, None), ["saturday morning", "weekend morning"])

    def test_periods(self):
        # The design's buckets: morning < 12, afternoon 12-16, evening 17-20, night >= 21.
        expected = {0: "morning", 9: "morning", 11: "morning", 12: "afternoon", 16: "afternoon", 17: "evening", 20: "evening", 21: "night", 23: "night"}
        for hour, period in expected.items():
            with self.subTest(hour=hour):
                start = datetime(2026, 9, 28, hour, tzinfo=timezone.utc)  # a Monday
                self.assertEqual(timing_bullets(start, None, "UTC"), [f"monday {period}", f"weekday {period}"])

    def test_duration_buckets(self):
        start = datetime(2026, 9, 28, 9, tzinfo=timezone.utc)
        cases = {1: "short outing", 2: "multi-hour outing", 5: "multi-hour outing", 5.5: "full-day outing", 9: "full-day outing"}
        for hours, bullet in cases.items():
            with self.subTest(hours=hours):
                self.assertEqual(timing_bullets(start, start + timedelta(hours=hours), "UTC")[2], bullet)
        self.assertEqual(len(timing_bullets(start, start - timedelta(hours=1), "UTC")), 2)
        self.assertEqual(len(timing_bullets(start, start, "UTC")), 2)

    def test_no_timing_without_start_time(self):
        text = build_search_text(SearchInput(tags=["Chill"], back_by=FRIDAY_1410_NY + timedelta(hours=4), timezone="America/New_York"))
        self.assertNotIn("Timing", text)
        self.assertEqual(timing_bullets(None, FRIDAY_1410_NY, "UTC"), [])

    def test_mood_phrases_are_routed(self):
        text = build_search_text(SearchInput(mood_text="Rooftop bars and live jazz, nothing too pricey"))
        self.assertEqual(bullets(text, "Environment"), ["rooftop bars"])
        self.assertEqual(bullets(text, "Interests"), ["live jazz"])
        self.assertEqual(bullets(text, "Cost"), ["nothing too pricey"])

    def test_timing_cap_keeps_the_computed_bullets(self):
        text = build_search_text(SearchInput(mood_text="late night", start_time=FRIDAY_1410_NY, back_by=FRIDAY_1410_NY + timedelta(hours=3), timezone="America/New_York"))
        self.assertEqual(bullets(text, "Timing"), ["friday afternoon", "weekday afternoon", "multi-hour outing"])

    def test_output_passes_the_format_check(self):
        text = build_search_text(SearchInput(mood_text="Something chill and outside, then cheap food after.", tags=["Nerdy", "Games"], who="friends", pace="balanced", budget=2, start_time=FRIDAY_1410_NY, timezone="America/New_York"))
        self.assertIsNone(check(text))
        self.assertEqual(bullets(text, "Interests"), ["science and technology", "games and puzzles"])


if __name__ == "__main__":
    unittest.main()
