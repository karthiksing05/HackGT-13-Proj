#!/usr/bin/env python3
"""Generate synthetic users: preference texts in the embedding-text format of the spec.

Each user starts from a random persona brief sampled in code (interests drawn from the event
taxonomy in generate.py, dislikes, social style, budget, availability, setting, energy,
experience, context and a detail level). The model turns the brief into two texts in the
spec's eight-section format (section 3 of ml/description_generation.md):

    positive_text  characteristics the user wants
    negative_text  characteristics the user wants to avoid ("" when the brief has no dislikes)

Both texts are checked with the same validator as event texts (generate.validate). One
process per GPU; each appends to its own JSONL shard after every round.
"""
from __future__ import annotations

import argparse
import json
import math
import random
import re
import secrets
import time
import uuid
from collections import Counter
from pathlib import Path

import generate as gen

# ----------------------------------------------------------------- persona briefs

TRAIT_DISLIKES = ["large crowds", "loud music", "late nights", "early mornings", "expensive tickets",
                  "alcohol-focused events", "competitive formats", "networking-heavy events", "long lectures",
                  "physically demanding activities", "outdoor events in bad weather", "online events",
                  "formal dress codes", "kid-heavy events", "sales pitches", "audience participation",
                  "rigid schedules", "crowded bars", "sitting still for hours", "very long events"]
SOCIAL = ["goes solo and likes meeting people there", "likes small groups", "enjoys big lively crowds",
          "goes with a partner or close friends", "wants to meet new people", "prefers quiet, low-key socializing"]
BUDGET = ["prefers free events", "keeps events under $20", "fine with moderate prices ($20-50)",
          "happy to pay premium prices for special events"]
AVAILABILITY = ["weekday evenings", "weekends only", "mornings", "weekend afternoons", "late nights",
                "flexible schedule"]
SETTING = ["prefers indoor venues", "loves being outdoors", "no strong setting preference", "open to online events"]
ENERGY = ["relaxed and low-key", "moderately active", "high-energy and intense"]
EXPERIENCE = ["beginner who wants to learn", "intermediate hobbyist", "experienced, wants advanced content"]
CONTEXT = ["university student", "busy working professional", "parent of young kids", "recently moved to the city",
           "remote worker looking for community", "retiree with free time", "shift worker with odd hours"]
DETAIL = {"detailed": 0.35, "moderate": 0.45, "brief": 0.20}
DETAIL_BULLETS = {"detailed": "12-20", "moderate": "6-12", "brief": "2-5"}
NO_DISLIKES = 0.15  # share of users with nothing to avoid: negative_text is empty


def sample_persona(rng: random.Random) -> dict:
    categories = list(gen.CATEGORIES)
    liked = rng.sample(categories, rng.choices([1, 2, 3], weights=[0.3, 0.45, 0.25])[0])
    interests = [t for c in liked for t in rng.sample(gen.CATEGORIES[c], min(len(gen.CATEGORIES[c]),
                                                                              rng.choice([1, 1, 2])))]
    persona = {"interests": interests, "interest_categories": liked, "detail": gen.weighted(rng, DETAIL)}
    if rng.random() >= NO_DISLIKES:
        pool = [f"{c} events" for c in categories if c not in liked] + TRAIT_DISLIKES
        persona["dislikes"] = rng.sample(pool, rng.choice([1, 1, 2, 2, 3]))
    optional = {"social": SOCIAL, "budget": BUDGET, "availability": AVAILABILITY, "setting": SETTING,
                "energy": ENERGY, "experience": EXPERIENCE, "context": CONTEXT}
    keep = {"detailed": 0.85, "moderate": 0.6, "brief": 0.3}[persona["detail"]]
    for key, options in optional.items():
        if rng.random() < keep:
            persona[key] = rng.choice(options)
    return persona


def persona_text(p: dict) -> str:
    lines = [f"Interests: {', '.join(p['interests'])}",
             f"Dislikes: {'; '.join(p['dislikes'])}" if p.get("dislikes") else "Dislikes: none"]
    labels = {"social": "Social style", "budget": "Budget", "availability": "Availability", "setting": "Setting",
              "energy": "Energy", "experience": "Experience", "context": "Life context"}
    lines += [f"{label}: {p[key]}" for key, label in labels.items() if key in p]
    lines.append(f"Detail level: {p['detail']} ({DETAIL_BULLETS[p['detail']]} bullets in POSITIVE)")
    return "\n".join(lines)


# --------------------------------------------------------------------- generation

SYSTEM = """You write user preference profiles for an event recommendation app. A profile has two texts in \
the same format the app uses for events, so both can be embedded and compared with event embeddings:

- positive_text: characteristics the user wants
- negative_text: characteristics the user wants to avoid

Format (every section optional; omit a section with nothing to say; keep this order):

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

Sections: Interests = topics, genres, domains; Activities = what they like to do at events; Social = group \
style and interaction level; Environment = setting and atmosphere; Pace = intensity and rhythm; Cost = price \
expectations; Timing = when they can go; Experience = skill level and participation style.

Rules:
- Bullets are short normalized phrases, not sentences: lowercase, specific rather than vague ("indie rock \
concerts", not "music").
- Use the vocabulary events use, e.g. "hands-on workshop", "small-group conversation", "free admission", \
"weekday evening", "beginner-friendly".
- Base everything on the brief. Turn life context into preferences (e.g. "family-friendly events", \
"weekend daytime") instead of stating it.
- Never state demographics or sensitive traits: no age, gender, ethnicity, religion, health or similar.
- negative_text lists only what the user actively avoids; don't restate positive preferences as negatives.
- No placeholders such as "unknown", "N/A" or "none".
- Match the brief's detail level for the number of POSITIVE bullets.

Output exactly two blocks and nothing else: a line "POSITIVE" followed by the positive text, then a line \
"NEGATIVE" followed by the negative text. When the brief says "Dislikes: none", the NEGATIVE block is \
empty.

Example brief:
Interests: jazz, pottery
Dislikes: large crowds
Budget: keeps events under $20
Availability: weekday evenings
Detail level: moderate (6-12 bullets in POSITIVE)

Example output:
POSITIVE
Interests:
- live jazz
- pottery

Activities:
- hands-on workshop
- live music listening

Social:
- small-group setting

Cost:
- under $20 admission

Timing:
- weekday evening

NEGATIVE
Social:
- large crowds"""

SAMPLING = {"temperature": 0.7, "top_p": 0.9, "top_k": 40, "max_tokens": 700}
BLOCKS = re.compile(r"^\s*POSITIVE:?\s*$(.*?)^\s*NEGATIVE:?\s*$(.*)", re.S | re.M)
DEMOGRAPHIC = re.compile(r"\b(male|female|men|women|man|woman|gender|christian|muslim|jewish|hindu|buddhist|"
                         r"religious|ethnic|gay|lesbian|lgbtq\+?|\d0s|aged? \d+|years? old)\b", re.I)


def normalize(text: str) -> str:
    """Undo two harmless formatting slips before validating: escaped newlines, markdown bold."""
    text = text.replace("\\n", "\n")
    return re.sub(r"\*\*([^*]+)\*\*", r"\1", text)


def check_text(text: str, brief: str) -> tuple[str | None, dict | str]:
    body, sections = gen.validate(normalize(text), [], brief)
    if body is None:
        return None, sections
    if DEMOGRAPHIC.search(body):
        return None, "demographic"
    return body, sections


def parse(text: str, persona: dict, brief: str) -> tuple[dict | None, str]:
    blocks = BLOCKS.search(re.sub(r"<think>.*?</think>", "", text, flags=re.S))
    if not blocks:
        return None, "no_blocks"
    positive, negative = (b.strip() for b in blocks.groups())
    pos, pos_sections = check_text(positive, brief)
    if pos is None:
        return None, f"positive_{pos_sections}"
    if persona.get("dislikes"):
        neg, neg_sections = check_text(negative, brief)
        if neg is None:
            return None, f"negative_{neg_sections}"
    else:
        neg, neg_sections = "", {}  # no dislikes: whatever the model wrote is ignored
    return {"positive_text": pos, "negative_text": neg,
            "positive_sections": {s.lower(): pos_sections.get(s, []) for s in gen.SECTIONS},
            "negative_sections": {s.lower(): neg_sections.get(s, []) for s in gen.SECTIONS}}, "ok"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model", default="Qwen/Qwen3.5-9B")
    ap.add_argument("--total", type=int, required=True, help="valid users wanted across all workers")
    ap.add_argument("--num-workers", type=int, default=1)
    ap.add_argument("--worker-index", type=int, default=0)
    ap.add_argument("--chunk", type=int, default=2048)
    ap.add_argument("--out", required=True)
    ap.add_argument("--stats", required=True)
    ap.add_argument("--max-model-len", type=int, default=4096)
    ap.add_argument("--gpu-memory-utilization", type=float, default=0.90)
    a = ap.parse_args()

    from vllm import LLM, __version__ as vllm_version

    target = math.ceil(a.total / a.num_workers)
    out = Path(a.out)
    done = sum(1 for _ in out.open()) if out.exists() else 0
    rng = random.Random(secrets.randbits(64))
    from vllm import SamplingParams

    llm = LLM(model=a.model, max_model_len=a.max_model_len, gpu_memory_utilization=a.gpu_memory_utilization,
              enable_prefix_caching=True, seed=secrets.randbits(31), **gen.text_only_kwargs())
    params = SamplingParams(**SAMPLING)
    gen.log(f"users worker {a.worker_index}/{a.num_workers}: target {target} (have {done}), vllm {vllm_version}")

    rejects, tokens, attempted, started = Counter(), 0, 0, time.time()
    reject_path = out.with_name(out.name.replace("users-raw-", "users-rejects-"))
    with out.open("a") as sink, reject_path.open("a") as reject_sink:
        while done < target:
            if attempted >= 500 and done == 0:
                raise SystemExit(f"no valid users after {attempted} attempts; see {reject_path}")
            n = min(a.chunk, math.ceil((target - done) * 1.25) + 16)
            personas = [sample_persona(rng) for _ in range(n)]
            briefs = [persona_text(p) for p in personas]
            outs = llm.chat([[{"role": "system", "content": SYSTEM}, {"role": "user", "content": b}] for b in briefs],
                            params, use_tqdm=False, chat_template_kwargs={"enable_thinking": False})
            kept = 0
            for persona, brief, o in zip(personas, briefs, outs):
                tokens += len(o.outputs[0].token_ids)
                if o.outputs[0].finish_reason != "stop":
                    rejects["truncated"] += 1
                    continue
                texts, reason = parse(o.outputs[0].text, persona, brief)
                if texts is None:
                    if sum(rejects.values()) < 300:
                        reject_sink.write(json.dumps({"reason": reason, "brief": brief,
                                                      "output": o.outputs[0].text}, ensure_ascii=False) + "\n")
                    rejects[reason] += 1
                    continue
                if done + kept >= target:
                    break
                row = {"uid": uuid.uuid4().hex[:12], "persona": persona, "persona_text": brief, **texts}
                sink.write(json.dumps(row, ensure_ascii=False) + "\n")
                kept += 1
            sink.flush()
            reject_sink.flush()
            done += kept
            attempted += n
            gen.log(f"round: kept {kept}/{n} | total {done}/{target} | "
                    f"{tokens / (time.time() - started):.0f} generated tok/s | rejects {dict(rejects)}")

    Path(a.stats).write_text(json.dumps({"worker": a.worker_index, "valid": done, "attempted": attempted,
                                         "rejects": dict(rejects), "output_tokens": tokens,
                                         "seconds": round(time.time() - started, 1), "model": a.model,
                                         "vllm": vllm_version}, indent=2))
    gen.log(f"done: {done}/{target} users")


if __name__ == "__main__":
    main()
