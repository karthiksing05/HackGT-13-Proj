"""Profile texts: byte-exact goldens, the rating rules, free-text cleaning and the format check."""

import importlib.util
import json
import unittest
from pathlib import Path

from api.schemas.profile import UserProfileRequest
from profiles import (
    TEMPLATE_VERSION,
    ProfileInput,
    RatedEventInput,
    build_profile_texts,
    check,
    normalize_ratings,
    phrases,
    profile_hash,
    route,
)
from profiles.validate import CAPS, SECTIONS, text_hash

FIXTURES = Path(__file__).parent / "fixtures"


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text())


def render(request: dict):
    return build_profile_texts(UserProfileRequest.model_validate(request).to_input())


def bullets(text: str, section: str) -> list[str]:
    """The bullets under one section of a rendered text ([] when the section is absent)."""
    out, current = [], None
    for line in text.splitlines():
        if line.endswith(":"):
            current = line[:-1]
        elif line.startswith("- ") and current == section:
            out.append(line[2:])
    return out


class GoldenTests(unittest.TestCase):
    def test_golden_a_jordan(self):
        golden = fixture("profile_jordan.json")
        texts = render(golden["request"])
        self.assertEqual(texts.positive, golden["positive_text"])
        self.assertEqual(texts.negative, golden["negative_text"])
        self.assertEqual(profile_hash(texts.positive, texts.negative), golden["profile_text_hash"])
        self.assertEqual(golden["template_version"], TEMPLATE_VERSION)
        self.assertEqual(bullets(texts.positive, "Interests")[:2], ["outdoor recreation", "local food"])
        self.assertIn("indie rock", bullets(texts.positive, "Interests"))
        self.assertEqual(bullets(texts.negative, "Social"), ["large crowds"])

    def test_golden_b_seed_user(self):
        golden = fixture("profile_seed_user.json")
        texts = render(golden["request"])
        self.assertEqual(texts.positive, golden["positive_text"])
        self.assertEqual(texts.negative, golden["negative_text"])
        self.assertEqual(profile_hash(texts.positive, texts.negative), golden["profile_text_hash"])
        self.assertEqual(bullets(texts.negative, "Social"), ["large crowds", "huge crowds"])
        self.assertIn("only gets going after midnight", bullets(texts.negative, "Timing"))

    def test_golden_d_no_dislikes(self):
        texts = render({"ratings": {"outdoors": 5, "food": 4, "museums": 3}, "company": "solo", "pace": "relaxed", "spend": "free_only", "prefer_free": True})
        self.assertEqual(texts.negative, "")
        self.assertTrue(texts.positive.startswith("Interests:\n- outdoor recreation\n- local food\n\nActivities:\n- park visit\n- guided hike\n- food tasting"))
        self.assertEqual(bullets(texts.positive, "Cost"), ["free admission"])
        self.assertTrue(profile_hash(texts.positive, texts.negative).startswith("profile-v1:"))

    def test_goldens_pass_the_format_check(self):
        for name in ("profile_jordan.json", "profile_seed_user.json"):
            golden = fixture(name)
            for text in (golden["positive_text"], golden["negative_text"]):
                if text:
                    self.assertIsNone(check(text), (name, text))
                    self.assertFalse(text.endswith("\n"))

    def test_rendering_is_deterministic(self):
        request = fixture("profile_seed_user.json")["request"]
        self.assertEqual(render(request), render(json.loads(json.dumps(request))))


class RatingRuleTests(unittest.TestCase):
    def test_thresholds(self):
        five = build_profile_texts(ProfileInput(ratings={"outdoors": 5}))
        self.assertEqual(bullets(five.positive, "Activities"), ["park visit", "guided hike"])
        four = build_profile_texts(ProfileInput(ratings={"outdoors": 4}))
        self.assertEqual(bullets(four.positive, "Activities"), ["park visit"])
        three = build_profile_texts(ProfileInput(ratings={"outdoors": 3}))
        self.assertEqual((three.positive, three.negative), ("", ""))
        two = build_profile_texts(ProfileInput(ratings={"big_crowds": 2}))
        self.assertEqual((two.positive, two.negative), ("", "Social:\n- large crowds\n\nEnvironment:\n- crowded venue"))
        one = build_profile_texts(ProfileInput(ratings={"nightlife": 1}))
        self.assertEqual(bullets(one.negative, "Environment"), ["loud nightclub setting", "crowded bars"])

    def test_likes_sorted_by_rating_then_key_order(self):
        texts = build_profile_texts(ProfileInput(ratings={"food": 5, "outdoors": 4}))
        self.assertEqual(bullets(texts.positive, "Interests"), ["local food", "outdoor recreation"])
        tie = build_profile_texts(ProfileInput(ratings={"food": 5, "outdoors": 5}))
        self.assertEqual(bullets(tie.positive, "Interests"), ["outdoor recreation", "local food"])
        dislikes = build_profile_texts(ProfileInput(ratings={"shopping": 2, "nightlife": 1}))
        self.assertEqual(bullets(dislikes.negative, "Interests"), ["nightlife", "shopping"])

    def test_alias_and_camel_case_keys(self):
        ratings = {"liveMusic": 5, "Big-Crowds": 1, "long walks": 4, "EarlyMornings": 2, "bogus": 5, "music": 3}
        self.assertEqual(
            normalize_ratings(ratings), {"live_music": 5, "big_crowds": 1, "long_walks": 4, "early_mornings": 2}
        )
        texts = build_profile_texts(ProfileInput(ratings=ratings))
        self.assertIn("live music", bullets(texts.positive, "Interests"))
        self.assertIn("long walks", bullets(texts.positive, "Activities"))
        self.assertEqual(bullets(texts.negative, "Social"), ["large crowds"])

    def test_values_clamped_and_junk_ignored(self):
        self.assertEqual(normalize_ratings({"outdoors": 9, "food": 0, "museums": "x", "sports": True, "shopping": 4.6}), {"outdoors": 5, "food": 1, "shopping": 5})
        self.assertEqual(normalize_ratings(None), {})


class BuilderTests(unittest.TestCase):
    def test_company_pace_cost_and_flexibility(self):
        texts = build_profile_texts(ProfileInput(company="big_group", pace="chill", spend="over_40", flexibility="stick_to_budget", prefer_free=True))
        self.assertEqual(bullets(texts.positive, "Social"), ["large-group outing", "big lively crowds"])
        self.assertEqual(bullets(texts.positive, "Pace"), ["relaxed rhythm", "low-key"])
        self.assertEqual(bullets(texts.positive, "Cost"), ["free admission", "premium tickets"])
        self.assertEqual(texts.negative, "Cost:\n- expensive tickets")
        unknown = build_profile_texts(ProfileInput(company="duo", pace="frantic", spend="lots", flexibility="whatever"))
        self.assertEqual((unknown.positive, unknown.negative), ("", ""))

    def test_rated_events_category_and_tags(self):
        events = [
            RatedEventInput(5, ["Would go again"], "museum", ["high_energy"]),
            RatedEventInput(1, [], "nightclub"),
            RatedEventInput(3, [], "cinema"),
            RatedEventInput(5, ["Too pricey"], "unknown_category"),
        ]
        texts = build_profile_texts(ProfileInput(rated_events=events))
        self.assertEqual(bullets(texts.positive, "Interests"), ["arts and culture"])
        self.assertEqual(bullets(texts.positive, "Pace"), ["high energy"])
        self.assertEqual(bullets(texts.positive, "Experience"), ["worth repeating"])
        self.assertEqual(bullets(texts.negative, "Interests"), ["nightlife"])
        self.assertEqual(bullets(texts.negative, "Cost"), ["expensive tickets"])
        self.assertNotIn("film", texts.positive + texts.negative)

    def test_only_the_first_twenty_rated_events_count(self):
        events = [RatedEventInput(5, [], "park")] * 20 + [RatedEventInput(5, [], "cinema")]
        texts = build_profile_texts(ProfileInput(rated_events=events))
        self.assertEqual(bullets(texts.positive, "Interests"), ["outdoor recreation"])

    def test_facebook_interests_are_cleaned(self):
        texts = build_profile_texts(ProfileInput(facebook_interests=["  Indie Rock ", "", "see http://x.example", "ok", "Hiking", "hiking"]))
        self.assertEqual(bullets(texts.positive, "Interests"), ["indie rock", "hiking"])

    def test_caps_and_dedupe(self):
        interests = [f"interest {i}" for i in range(15)]
        texts = build_profile_texts(ProfileInput(ratings={"outdoors": 5, "food": 5}, facebook_interests=interests + interests))
        self.assertEqual(len(bullets(texts.positive, "Interests")), CAPS["Interests"])
        self.assertEqual(bullets(texts.positive, "Interests")[:2], ["outdoor recreation", "local food"])
        self.assertEqual(len(set(bullets(texts.positive, "Interests"))), CAPS["Interests"])

    def test_answers_go_to_the_right_text(self):
        texts = build_profile_texts(ProfileInput(perfect_afternoon="rooftop bars", never_do="huge crowds", plan_around="the saturday market"))
        self.assertEqual(texts.positive, "Environment:\n- rooftop bars\n\nTiming:\n- saturday market")
        self.assertEqual(texts.negative, "Social:\n- huge crowds")

    def test_empty_profile(self):
        texts = build_profile_texts(ProfileInput())
        self.assertEqual((texts.positive, texts.negative), ("", ""))
        self.assertEqual(profile_hash("", ""), "profile-v1:" + text_hash("\n---\n"))

    def test_hash_prefix_and_formula(self):
        self.assertEqual(profile_hash("a", "b"), "profile-v1:" + text_hash("a\n---\nb"))
        self.assertNotEqual(profile_hash("a", "b"), profile_hash("b", "a"))


class FreeTextTests(unittest.TestCase):
    def test_cleaning_table(self):
        table = {
            "I'd love something chill and outside, then cheap food after.": ["chill", "outside", "cheap food"],
            "Maybe a long walk; or live jazz!": ["long walk", "live jazz"],
            "Packed clubs, huge crowds, or anything that only gets going after midnight.": ["packed clubs", "huge crowds", "only gets going after midnight"],
            "n/a": [],
            "Unknown": [],
            "see https://example.com/x, www.example.org/y": ["see"],
            "one two three four five six seven eight nine ten": ["one two three four five six seven eight"],
            "": [],
            "   \n ": [],
            "a, an, the": [],
            "Live music. Live music. LIVE MUSIC": ["live music"],
            '"Quiet" cafés (with wifi)': ["quiet cafés with wifi"],
        }
        for raw, expected in table.items():
            with self.subTest(raw=raw):
                self.assertEqual(phrases(raw), expected)

    def test_at_most_eight_phrases(self):
        raw = ", ".join(f"thing number {i}" for i in range(12))
        self.assertEqual(len(phrases(raw)), 8)

    def test_routing(self):
        expected = {
            "late night jazz": "Timing",
            "sunrise swims": "Timing",
            "meeting new people": "Social",
            "whoever's free to wander": "Social",
            "rooftop bars": "Environment",
            "chill": "Pace",
            "cheap food": "Cost",
            "long hikes": "Activities",
            "indie rock": "Interests",
        }
        for phrase, section in expected.items():
            with self.subTest(phrase=phrase):
                self.assertEqual(route(phrase), section)
                self.assertIn(section, SECTIONS)


class CheckMirrorTests(unittest.TestCase):
    """`profiles.validate.check` must agree with the backfill's copy of the pipeline checker."""

    GOOD = "Interests:\n- live music\n- jazz\n\nActivities:\n- live performance\n\nTiming:\n- evening"
    TABLE = [
        GOOD,
        GOOD + "\n\nCost:\n- unknown",
        GOOD + "\n\nVibe:\n- chill",
        GOOD + "\n\nSocial:",
        "This is a lovely jazz evening.",
        GOOD + "\n\nExperience:\n- https://example.com",
        GOOD + "\n\nInterests:\n- blues",
        GOOD + "\n\nExperience:\n- a relaxed evening of classic standards played by a quartet",
        "- orphan bullet",
        "",
        "Interests:\n- not provided here",
        "Interests:\n- n/a",
    ]

    @classmethod
    def setUpClass(cls):
        path = Path(__file__).resolve().parents[1] / "datagen" / "activity_text.py"
        spec = importlib.util.spec_from_file_location("datagen_activity_text", path)
        cls.datagen = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.datagen)

    def test_agrees_with_the_datagen_copy(self):
        for text in self.TABLE:
            with self.subTest(text=text[:40]):
                self.assertEqual(check(text), self.datagen.check(text))
        self.assertEqual(SECTIONS, self.datagen.SECTIONS)

    def test_rejections(self):
        self.assertIsNone(check(self.GOOD))
        for text, fragment in [
            (self.GOOD + "\n\nCost:\n- unknown", "placeholder"),
            (self.GOOD + "\n\nVibe:\n- chill", "not an allowed section"),
            (self.GOOD + "\n\nSocial:", "empty sections"),
            ("This is a lovely jazz evening.", "neither"),
            (self.GOOD + "\n\nExperience:\n- https://example.com", "URL"),
            (self.GOOD + "\n\nInterests:\n- blues", "twice"),
            (self.GOOD + "\n\nExperience:\n- a relaxed evening of classic standards played by a quartet", "sentence"),
            ("", "empty"),
        ]:
            with self.subTest(fragment=fragment):
                self.assertIn(fragment, check(text))


if __name__ == "__main__":
    unittest.main()
