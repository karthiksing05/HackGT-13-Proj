# Hackathon Event Recommendation System

## Purpose

Recommend events by representing both events and attendee preferences in the
same semantic space. The system should favor event-specific information that
answers a practical question: *would this person enjoy going?* It should be
robust to sparse or incomplete event records and provide a path from a
hackathon-ready baseline to learned compatibility models.

## 1. Event embedding-text schema

Convert each raw event record into the following compact text format before
embedding it. The generated text is an input to an embedding model; it is not
marketing copy or a user-facing description.

```text
Interests:
- <short semantic phrase>
- <short semantic phrase>

Activities:
- <short semantic phrase>

Social:
- <short semantic phrase>

Environment:
- <short semantic phrase>

Pace:
- <short semantic phrase>

Cost:
- <short semantic phrase>

Timing:
- <short semantic phrase>

Experience:
- <short semantic phrase>
```

### Section meanings

| Section | Include |
| --- | --- |
| `Interests` | Topics, genres, domains, or themes (for example, `machine learning`, `indie music`, `local food`). |
| `Activities` | What an attendee does (for example, `hands-on workshop`, `networking`, `live performance`). |
| `Social` | Group style, interaction level, audience structure, or social intent (for example, `small-group conversation`, `professional networking`). |
| `Environment` | Setting and sensory/logistical atmosphere (for example, `outdoor venue`, `quiet indoor setting`). |
| `Pace` | Intensity, duration character, or scheduling rhythm (for example, `relaxed drop-in`, `fast-paced competition`). |
| `Cost` | Attendance cost or cost framing when explicitly supported (for example, `free admission`, `ticketed`). |
| `Timing` | Time-of-day, date-related, recurrence, or duration information when relevant (for example, `weekday evening`, `weekend afternoon`). |
| `Experience` | Skill level, accessibility-relevant event format, or expected level of participation (for example, `beginner-friendly`, `expert discussion`, `family-friendly`). |

### Schema rules

- Every section is optional. Omit a section if it has no supported bullets.
- Each present section must contain at least one bullet.
- Each bullet is a short, normalized semantic phrase, not a sentence.
- Use consistent vocabulary across records; prefer specific concepts over broad
  labels such as `fun`, `community`, or `entertainment`.
- Do not repeat the same concept in multiple sections unless it changes the
  recommendation meaning.
- Do not include IDs, URLs, organizer/scraper metadata, or operational details.
- If a field is missing, `null`, empty, unavailable, or ambiguous, omit it.
  Never emit `unknown`, `not provided`, `N/A`, `unspecified`, or `null`.
- Do not infer a characteristic solely because data is absent. Inference is
  permitted only when directly supported by supplied event information.
- Resolve conflicting values conservatively: retain only information that is
  clearly supported; otherwise omit the disputed characteristic.

### Example output

```text
Interests:
- artificial intelligence
- startup entrepreneurship

Activities:
- founder presentations
- professional networking

Social:
- large-group networking

Environment:
- indoor venue

Pace:
- scheduled program

Cost:
- free admission

Timing:
- weekday evening

Experience:
- beginner-friendly
```

If the source has no reliable price, venue atmosphere, or skill-level detail,
the `Cost`, `Environment`, and `Experience` sections should simply be absent.

## 2. Event description generation prompt

Use the following prompt to transform a raw event record into embedding text.

```text
You convert raw event data into compact structured embedding text for an event
recommendation system.

The result will be embedded and compared with user-preference embeddings. Your
goal is to retain event-specific semantic signal relevant to whether someone
would enjoy attending, while minimizing boilerplate, filler, and irrelevant
metadata.

Output only the schema below. Do not include commentary, explanations, or prose
outside the schema.

Interests:
- <short semantic phrase>

Activities:
- <short semantic phrase>

Social:
- <short semantic phrase>

Environment:
- <short semantic phrase>

Pace:
- <short semantic phrase>

Cost:
- <short semantic phrase>

Timing:
- <short semantic phrase>

Experience:
- <short semantic phrase>

Rules:
- Every section is optional; omit any section with no supported information.
- Use short semantic phrases, not sentences or descriptive prose.
- Include only characteristics relevant to attendance preference.
- Use consistent, specific vocabulary. Avoid vague concepts such as “fun,”
  “exciting,” or “great atmosphere.”
- Do not use promotional language or calls to action.
- Do not add filler such as “this event is,” “attendees will,” or “great for.”
- Do not include event IDs, URLs, organizer IDs, scraper metadata, or other
  operational information.
- Do not repeat the same idea unnecessarily across sections.
- Do not infer sensitive demographic characteristics.

Missing, null, uncertain, or conflicting data:
- Omit values that are missing, null, empty, unavailable, or unsupported.
- Omit an entire section when no supported value exists.
- Never output placeholders, including “unknown,” “not provided,” “N/A,”
  “unspecified,” or “null.”
- Do not infer characteristics from missing data.
- When sources conflict or are uncertain, keep only clearly supported facts;
  otherwise omit the characteristic.

Example input:
Title: Intro to Python Workshop
Description: A hands-on beginner workshop covering Python basics. Bring a
laptop. Free, Saturday afternoon at the public library.

Example output:
Interests:
- python programming

Activities:
- hands-on workshop
- coding practice

Environment:
- library setting

Pace:
- guided learning

Cost:
- free admission

Timing:
- weekend afternoon

Experience:
- beginner-friendly
- laptop required

Example input:
Title: Friday Meetup
Description: Join us Friday night.

Example output:
Timing:
- friday night
```

## 3. Modeling approaches

User preferences should use the same eight-section vocabulary and formatting as
events, represented separately as positive and negative preference text:

```text
positiveText = characteristics the user wants
negativeText = characteristics the user wants to avoid
```

Embed these into `u_pos` and `u_neg`; embed the event text into `e`.

### A. Baseline: structured-text embeddings and cosine scoring

Use a pretrained text-embedding model without task-specific training. Generate
the structured event text, positive user-preference text, and negative
user-preference text, then score an event with:

```text
s(e, user) = cos(e, u_pos) - λ × cos(e, u_neg)
```

`λ` controls the penalty for characteristics the user wants to avoid. Start by
tuning it on a small validation set or through product testing.

**Use when:** building the first working product, data is scarce, and rapid
iteration/explainability matter most.

**Advantages:** no labeled interaction data required; simple to implement;
works for new events and new users; easy to inspect by looking at generated
text and similarity components.

**Tradeoffs:** the embedding model was not trained on the product’s definition
of compatibility; cosine similarity may miss nuanced preferences and feature
interactions; prompt quality has an outsized effect.

**Data requirement:** raw event data and user-provided likes/dislikes. A small
labeled set helps tune `λ` and evaluate ranking quality, but is not required to
launch.

### B. Intermediate: frozen pretrained embeddings plus a compatibility model

Keep the embedding model frozen and train a small compatibility model (for
example, an MLP) on top of event and user representations. Useful input
features include `e`, `u_pos`, `u_neg`, cosine similarities, and interaction
terms such as elementwise products or absolute differences. Train the head to
predict a click, save, RSVP, attendance, or curated relevance label.

**Use when:** the baseline is live or evaluated and interaction labels are
accumulating, but the data volume is not yet sufficient to safely fine-tune the
embedding encoder.

**Advantages:** learns product-specific nonlinear interactions while preserving
stable pretrained representations; lower training cost and lower overfitting
risk than full encoder fine-tuning; retains strong cold-start behavior.

**Tradeoffs:** representation quality is capped by the frozen encoder; the MLP
can be less transparent than the cosine score; careful negative sampling and
leakage prevention are needed.

**Data requirement:** a meaningful set of labeled user-event pairs, ideally
with impression context and both positive and negative outcomes. Use
time-based splits and evaluate ranking metrics such as Recall@K, NDCG@K, and
calibration.

### C. Later: contrastive / two-tower fine-tuning

Fine-tune event and user encoders jointly (or with partially shared weights) so
compatible user-event pairs are close in embedding space and incompatible pairs
are far apart. Use contrastive objectives such as in-batch negatives, pairwise
ranking loss, or sampled-softmax-style retrieval objectives. A two-tower design
allows event vectors to be precomputed and retrieved efficiently with nearest
neighbor search.

**Use when:** there is substantial, representative behavioral data and the
product needs the highest retrieval and ranking ceiling at scale.

**Advantages:** aligns the embedding space directly with event compatibility;
captures domain-specific language and preference patterns; supports efficient
large-catalog candidate retrieval.

**Tradeoffs:** highest engineering and evaluation complexity; susceptible to
selection bias, popularity bias, and weak/ambiguous implicit labels; requires
continual refreshes as events and user behavior change; can weaken general
semantic behavior if fine-tuned on narrow or noisy data.

**Data requirement:** a large, diverse set of user-event interactions with
reliable positive signals, carefully chosen negatives, and ideally exposure or
impression logs. Maintain held-out, time-based, cold-start, and fairness/slice
evaluations.

## Proposed staged rollout

1. **Hackathon / MVP — baseline.** Implement schema-constrained text
   generation, generate positive and negative user profiles, and rank with the
   cosine score. Establish offline examples and a small human relevance set to
   tune the prompt and `λ`.
2. **Early product learning — frozen encoder + MLP.** Log impressions and
   outcomes with consent-appropriate instrumentation. Train and compare a
   compact compatibility head against the baseline, retaining the baseline as a
   fallback for sparse users and events.
3. **Scaled retrieval — contrastive two-tower.** Once behavior data is broad
   and trustworthy, fine-tune two towers for retrieval, precompute event
   embeddings, and optionally use the MLP or another ranker as a second-stage
   reranker.

At every stage, maintain the structured text as an observable, debuggable
representation; measure performance separately for new users, new events, and
sparse records; and avoid using missing-data placeholders that could become
spurious shared embedding signal.
