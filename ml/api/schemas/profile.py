"""Request and response schemas for `POST /v1/user-profile` and `POST /v1/search-profile`.

The inputs are the app's own shapes (`Preferences`, `FacebookImport.interests`, ratings,
`PlanRequest`), so the Go backend forwards them and never builds a text itself. Enum values are
the contract's; ratings take any keys (unknown ones are ignored, values clamped to 1..5).
"""

from typing import Annotated, Literal

from pydantic import AwareDatetime, BaseModel, Field

from profiles import ProfileInput, RatedEventInput, SearchInput

RatingKey = Literal[
    "outdoors", "food", "museums", "live_music", "nightlife", "sports", "shopping", "big_crowds", "early_mornings", "long_walks"
]
Company = Literal["solo", "small_group", "big_group"]
Pace = Literal["relaxed", "chill", "balanced", "packed"]
Spend = Literal["free_only", "under_15", "15_to_40", "over_40"]
Flexibility = Literal["stick_to_budget", "bit_over_ok"]
Who = Literal["just_me", "friends", "open"]
Stars = Annotated[int, Field(ge=1, le=5)]
Budget = Annotated[int, Field(ge=0, le=3)]


class ProfileAnswers(BaseModel):
    """Setup step 5's open answers; `perfect_afternoon` and `plan_around` are likes, `never_do` dislikes."""

    perfect_afternoon: str = ""
    never_do: str = ""
    plan_around: str = ""


class RatedEvent(BaseModel):
    """One of the user's ratings with what is known about the activity. `embedding` is accepted
    for forward compatibility and currently unused by the text builder."""

    stars: Stars
    tags: list[str] = Field(default_factory=list)
    category: str | None = None
    activity_tags: list[str] = Field(default_factory=list)
    embedding: list[float] | None = None


class UserProfileRequest(BaseModel):
    ratings: dict[str, int] = Field(default_factory=dict)
    company: Company | None = None
    pace: Pace | None = None
    spend: Spend | None = None
    flexibility: Flexibility | None = None
    prefer_free: bool = False
    answers: ProfileAnswers = Field(default_factory=ProfileAnswers)
    facebook_interests: list[str] = Field(default_factory=list)
    rated_events: list[RatedEvent] = Field(default_factory=list)  # newest first; the first 20 are used
    embed: bool = True

    def to_input(self) -> ProfileInput:
        return ProfileInput(
            ratings=dict(self.ratings),
            company=self.company,
            pace=self.pace,
            spend=self.spend,
            flexibility=self.flexibility,
            prefer_free=self.prefer_free,
            perfect_afternoon=self.answers.perfect_afternoon,
            never_do=self.answers.never_do,
            plan_around=self.answers.plan_around,
            facebook_interests=list(self.facebook_interests),
            rated_events=[RatedEventInput(e.stars, list(e.tags), e.category, list(e.activity_tags)) for e in self.rated_events],
        )


class UserProfileResponse(BaseModel):
    """`negative_embedding` is all zeros when `negative_text` is empty; an all-empty profile has
    `positive_text == ""` and a zero positive vector, which the backend treats as "unranked"."""

    positive_text: str
    negative_text: str
    positive_embedding: list[float] | None
    negative_embedding: list[float] | None
    profile_text_hash: str
    template_version: str
    model: str
    dim: int
    provider: str


class SearchProfileRequest(BaseModel):
    """A `PlanRequest`'s preference part; `timezone` is the viewer's IANA zone for the timing bullets."""

    mood_text: str = ""
    tags: list[str] = Field(default_factory=list)
    who: Who | None = None
    pace: Pace | None = None
    budget: Budget | None = None
    start_time: AwareDatetime | None = None
    back_by: AwareDatetime | None = None
    timezone: str | None = None
    embed: bool = True

    def to_input(self) -> SearchInput:
        return SearchInput(
            mood_text=self.mood_text,
            tags=list(self.tags),
            who=self.who,
            pace=self.pace,
            budget=self.budget,
            start_time=self.start_time,
            back_by=self.back_by,
            timezone=self.timezone,
        )


class SearchProfileResponse(BaseModel):
    """`search_embedding` is null when the request has nothing to say (`search_text == ""`)."""

    search_text: str
    search_embedding: list[float] | None
    model: str
    dim: int
    provider: str
