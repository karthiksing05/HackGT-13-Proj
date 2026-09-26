"""Adapters from domain models to stable ranking contexts.

This is the only module that knows which fields exist on `User` and `Event`.
When those schemas change, update these functions; the Jev request building,
parsing, and ranking code does not need to change.
"""

from .models import Event, EventContext, SearchContext, User, UserContext


def user_to_context(user: User) -> UserContext:
    sections: list[str] = []

    if user.interests:
        sections.append(_bullets("Interests", user.interests))

    preferences: list[str] = []
    if user.preferred_environment:
        preferences.append(f"prefers {user.preferred_environment} activities")
    if user.budget is not None:
        if user.budget <= 0:
            preferences.append("prefers free events")
        else:
            preferences.append(f"prefers events costing at most {_format_price(user.budget)}")
    if preferences:
        sections.append(_bullets("Preferences", preferences))

    description = "\n\n".join(sections) or "No stated interests or preferences."
    return UserContext(user_id=user.id, description=description, dislikes=user_dislikes_description(user))


def user_dislikes_description(user: User) -> str | None:
    """Text describing what the user wants to avoid, or None if there is no negative signal.

    Kept separate from `user_to_context` so it can be embedded on its own.
    """
    if not user.dislikes:
        return None
    return _bullets("Dislikes", user.dislikes)


def search_to_context(text: str | None) -> SearchContext | None:
    """The search's preference text, or None when the user gave none (so no search is applied)."""
    if text is None or not text.strip():
        return None
    return SearchContext(description=text.strip())


def event_to_context(event: Event) -> EventContext:
    fields: list[tuple[str, str]] = [
        ("Name", event.name),
        ("Description", event.description),
        ("Category", event.category),
    ]
    if event.location:
        fields.append(("Location", event.location))
    if event.price is not None:
        fields.append(("Price", _format_price(event.price)))

    description = "\n\n".join(f"{label}:\n{value.strip()}" for label, value in fields if value)
    return EventContext(event_id=event.id, description=description)


def _bullets(title: str, items: list[str]) -> str:
    return f"{title}:\n" + "\n".join(f"- {item}" for item in items)


def _format_price(price: float) -> str:
    if price <= 0:
        return "Free"
    return f"${price:,.0f}" if price == int(price) else f"${price:,.2f}"
