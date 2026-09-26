# Event Embedding Text Generation

Convert raw event data into a compact structured text representation for an event recommendation system.

The text will be embedded using a text embedding model and compared against user preference embeddings using cosine similarity. It may later be used to train an event encoder using contrastive learning.

The goal is to maximize event-specific semantic signal while minimizing boilerplate, filler words, irrelevant metadata, and placeholder text.

## Rules

- Use short semantic phrases rather than sentences.
- Do not write descriptive prose.
- Do not use promotional language.
- Do not add filler such as "this event is," "attendees will," or "great for."
- Use consistent vocabulary across events.
- Include only characteristics relevant to whether someone would enjoy attending.
- Do not repeat the same concept across multiple sections unnecessarily.
- Prefer specific concepts over generic concepts.
- Do not include event IDs, URLs, scraper metadata, organizer IDs, or other operational information.
- Do not infer sensitive demographic characteristics.

## Missing, Null, or Uncertain Data

Raw event records may contain missing fields, `null` values, empty strings, empty arrays, incomplete descriptions, or conflicting information.

Handle these cases as follows:

- If a value is `null`, missing, empty, or unavailable, omit it.
- If an entire section has no supported information, omit the section entirely.
- Never output placeholders such as:
  - `unknown`
  - `not provided`
  - `N/A`
  - `unspecified`
  - `null`
- Do not infer a characteristic simply because a field is missing.
- Only infer semantic characteristics when they are strongly supported by other available event data.
- If evidence is weak or ambiguous, omit the characteristic.
- If multiple source fields provide the same information, include the concept only once.
- If source fields conflict, prefer the most explicit and reliable field. If the conflict cannot be resolved, omit the disputed characteristic.
- A sparse event should produce a sparse output rather than invented detail.

For example, if price is missing:

```text
Do not include a Cost section.
```

Do not write:

```text
Cost:
- unknown
```

If the event has no useful information about social setting:

```text
Do not include a Social section.
```

If the title says "Outdoor Yoga in Piedmont Park" but there is no explicit environment field, it is reasonable to infer:

```text
Environment:
- outdoors
```

because the event information directly supports it.

Do not infer:

```text
Social:
- meeting new people
```

unless the event description or structure actually supports that.

## Output Format

Use only the following sections when relevant:

```text
Interests:
- <topic>
- <topic>

Activities:
- <activity>
- <activity>

Social:
- <social characteristic>
- <group characteristic>

Environment:
- <environment characteristic>
- <atmosphere>

Pace:
- <pace or energy level>

Cost:
- <cost characteristic>

Timing:
- <time characteristic>

Experience:
- <other distinguishing event characteristics>
```

Omit any section for which there is no meaningful supported information.

Do not emit empty sections.

## Semantic Alignment

Use vocabulary that can naturally align with user preferences.

For example, if the event includes networking:

```text
Social:
- meeting new people
- professional networking
```

If it is a small acoustic concert:

```text
Interests:
- live music

Activities:
- live performance

Social:
- small group

Environment:
- intimate venue
- relaxed atmosphere

Pace:
- chill
```

If it is a basketball game:

```text
Interests:
- basketball
- sports

Activities:
- spectator sports
- live competition

Social:
- large group

Environment:
- crowded venue
- loud atmosphere
- high energy

Pace:
- high energy
```

The event representation should describe what the event actually contains, not make judgments about whether someone should enjoy it.

## Example With Complete Data

Input:

```text
Atlanta AI Builders Meetup
Monthly meetup featuring two short presentations from local AI engineers followed by demos and networking.
Location: Tech Square
Price: Free
Time: 7:00 PM
```

Output:

```text
Interests:
- artificial intelligence
- software engineering

Activities:
- technical talks
- interactive demonstrations
- networking

Social:
- meeting new people
- professional networking

Environment:
- indoor
- casual atmosphere

Pace:
- balanced

Cost:
- free

Timing:
- evening

Experience:
- technical discussion
- learning new technology
```

## Example With Missing Data

Input:

```text
Sunset Jazz Session
Live jazz performance at a local venue.
Price: null
Group size: null
Description: ""
Time: 8:00 PM
```

Output:

```text
Interests:
- live music
- jazz

Activities:
- live performance

Timing:
- evening

Experience:
- live jazz
```

Do not add Cost, Social, Environment, or Pace because the source does not support them.

## Example With Sparse Data

Input:

```text
Community Chess Night
```

Output:

```text
Interests:
- chess

Activities:
- board games
```

Do not invent crowd size, social intensity, cost, environment, pace, or timing.

## Bad Output

```text
Interests:
- exciting technology

Cost:
- unknown

Social:
- probably social

Environment:
- unspecified

Experience:
- a great way to meet people
```

This output contains unsupported inference, filler, and placeholder text that would pollute the embedding.

# Input

Generate the structured event text for:

{{EVENT_DATA}}

Return only the structured text.
