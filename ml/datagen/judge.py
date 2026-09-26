#!/usr/bin/env python3
"""Rate (user, event) candidate pairs 0-3 with the LLM; the classifier's label is rating / 3.

The judge sees exactly what the classifier sees: the user's positive and negative preference
texts and the event's embedding text. It answers in constrained JSON: a one-sentence reason,
then the rating (writing the reason first works as a short rationale before the score).
Tasks come from select_candidates.py; finished pairs are skipped, so reruns resume.
"""
from __future__ import annotations

import argparse
import json
import secrets
import time
from collections import Counter
from pathlib import Path

import generate as gen

SYSTEM = """You judge how well an event suits a user of an event recommendation app. The user's likes \
and dislikes and the event are all written in the app's eight-section format (Interests, Activities, \
Social, Environment, Pace, Cost, Timing, Experience).

Rate how much this user would enjoy attending:
3 = strong match: the event clearly fits the user's core interests and nothing conflicts with what they \
avoid or their constraints (budget, timing, setting, social style, pace, skill level).
2 = good match: real overlap with their interests, with at most minor mismatches.
1 = weak match: little overlap, or an appealing event with a notable conflict.
0 = poor match: no relevant overlap, or it clearly hits something they want to avoid.

Judge only from the text given. Details the event doesn't state are neutral, not negative, and \
preferences the user doesn't state don't count against the event. Reply with JSON: a one-sentence \
reason (at most 25 words), then the rating."""

SCHEMA = {"type": "object",
          "properties": {"reason": {"type": "string"}, "rating": {"type": "integer", "enum": [0, 1, 2, 3]}},
          "required": ["reason", "rating"], "additionalProperties": False}
SAMPLING = {"temperature": 0.2, "top_p": 0.9, "top_k": 20, "max_tokens": 120}


def prompt(task: dict) -> str:
    likes = task["positive_text"] or "(nothing stated)"
    dislikes = task["negative_text"] or "(nothing stated)"
    return f"User likes:\n{likes}\n\nUser dislikes:\n{dislikes}\n\nEvent:\n{task['event_text']}"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model", default="Qwen/Qwen3.5-9B")
    ap.add_argument("--tasks", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--stats", required=True)
    ap.add_argument("--chunk", type=int, default=8192)
    ap.add_argument("--max-model-len", type=int, default=4096)
    ap.add_argument("--gpu-memory-utilization", type=float, default=0.90)
    a = ap.parse_args()

    from vllm import LLM, __version__ as vllm_version

    out = Path(a.out)
    done = set()
    if out.exists():
        done = {(r["user_id"], r["event_id"]) for r in map(json.loads, out.open())}
    tasks = [t for t in map(json.loads, open(a.tasks)) if (t["user_id"], t["event_id"]) not in done]
    tasks.sort(key=lambda t: t["user_id"])  # neighbours share the user's prompt prefix (prefix cache)
    llm = LLM(model=a.model, max_model_len=a.max_model_len, gpu_memory_utilization=a.gpu_memory_utilization,
              enable_prefix_caching=True, seed=secrets.randbits(31), **gen.text_only_kwargs())
    params, mode = gen.json_sampling(SCHEMA, **SAMPLING)
    gen.log(f"judge: {len(tasks)} pairs to rate ({len(done)} already done), vllm {vllm_version}, decoding {mode}")

    ratings, failures, tokens, started = Counter(), Counter(), 0, time.time()
    with out.open("a") as sink:
        for start in range(0, len(tasks), a.chunk):
            chunk = tasks[start:start + a.chunk]
            pending = chunk
            for attempt in range(2):  # one retry for unparseable answers
                outs = llm.chat([[{"role": "system", "content": SYSTEM}, {"role": "user", "content": prompt(t)}]
                                 for t in pending], params, use_tqdm=False,
                                chat_template_kwargs={"enable_thinking": False})
                retry = []
                for task, o in zip(pending, outs):
                    tokens += len(o.outputs[0].token_ids)
                    try:
                        ans = json.loads(o.outputs[0].text)
                        rating = int(ans["rating"])
                        assert rating in (0, 1, 2, 3)
                    except (ValueError, KeyError, TypeError, AssertionError):
                        retry.append(task)
                        continue
                    ratings[rating] += 1
                    sink.write(json.dumps({"user_id": task["user_id"], "event_id": task["event_id"],
                                           "rating": rating, "label": round(rating / 3, 4),
                                           "reason": str(ans.get("reason", ""))[:300]}, ensure_ascii=False) + "\n")
                pending = retry
                if not pending:
                    break
            failures["unparseable"] += len(pending)
            sink.flush()
            gen.log(f"rated {min(start + a.chunk, len(tasks))}/{len(tasks)} | "
                    f"{tokens / (time.time() - started):.0f} generated tok/s | ratings {dict(sorted(ratings.items()))}"
                    f" | failures {dict(failures)}")

    Path(a.stats).write_text(json.dumps({"rated": sum(ratings.values()), "ratings": dict(ratings),
                                         "failures": dict(failures), "output_tokens": tokens,
                                         "seconds": round(time.time() - started, 1), "model": a.model,
                                         "vllm": vllm_version}, indent=2))
    gen.log(f"done: {sum(ratings.values())} ratings")


if __name__ == "__main__":
    main()
