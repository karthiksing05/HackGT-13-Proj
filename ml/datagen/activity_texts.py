#!/usr/bin/env python3
"""Write the missing embedding texts of MongoDB activities (step 2 of mongo_backfill.py).

Each prompt is the ingestion pipeline's event_embedding_text.md filled in with one activity's data
(built by `mongo_backfill.py export`). Generation follows the pipeline's EmbedTextAgent: the prompt
as a single user message, temperature 0.2, the output cleaned and held to the prompt's format rules
(activity_text.check), and a rejected text regenerated with the pipeline's feedback appended. The
pipeline retries once; with no API quota to save we allow --attempts in total (default 3).
Inputs already written to --out are skipped, so a rerun resumes (and retries the failures).
"""
from __future__ import annotations

import argparse
import json
import re
import secrets
import time
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

import activity_text as at
import generate as gen

SAMPLING = {"temperature": 0.2, "top_p": 0.9, "top_k": 20, "max_tokens": 400}


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model", default="Qwen/Qwen3.5-9B")
    ap.add_argument("--todo", required=True, help="texts_todo.jsonl from mongo_backfill.py export")
    ap.add_argument("--out", required=True)
    ap.add_argument("--failed", required=True, help="inputs still rejected after the last attempt")
    ap.add_argument("--stats", required=True)
    ap.add_argument("--num-workers", type=int, default=1)
    ap.add_argument("--worker-index", type=int, default=0)
    ap.add_argument("--attempts", type=int, default=3)
    ap.add_argument("--limit", type=int, default=0, help="only the first N inputs (testing)")
    ap.add_argument("--max-model-len", type=int, default=8192)
    ap.add_argument("--gpu-memory-utilization", type=float, default=0.90)
    a = ap.parse_args()

    from vllm import LLM, SamplingParams, __version__ as vllm_version

    rows = [json.loads(line) for line in open(a.todo)]
    rows = (rows[:a.limit] if a.limit else rows)[a.worker_index::a.num_workers]
    out = Path(a.out)
    done = {json.loads(line)["input_hash"] for line in out.open()} if out.exists() else set()
    rows = [r for r in rows if r["input_hash"] not in done]
    llm = LLM(model=a.model, max_model_len=a.max_model_len, gpu_memory_utilization=a.gpu_memory_utilization,
              enable_prefix_caching=True, seed=secrets.randbits(31), **gen.text_only_kwargs())
    params = SamplingParams(**SAMPLING)
    gen.log(f"texts: {len(rows)} inputs to write ({len(done)} already done), vllm {vllm_version}")

    passed, reasons, n_failed, tokens, started = Counter(), Counter(), 0, 0, time.time()
    pending = [(row, "") for row in rows]  # (input, the pipeline's retry suffix)
    with out.open("a") as sink, open(a.failed, "w") as failed:
        for attempt in range(1, a.attempts + 1):
            if not pending:
                break
            outs = llm.chat([[{"role": "user", "content": row["prompt"] + suffix}] for row, suffix in pending],
                            params, use_tqdm=False, chat_template_kwargs={"enable_thinking": False})
            retry = []
            for (row, _), o in zip(pending, outs):
                c = o.outputs[0]
                tokens += len(c.token_ids)
                text = at.clean(c.text)
                reason = "the output was cut off" if c.finish_reason == "length" else at.check(text)
                if reason is None:
                    passed[attempt] += 1
                    sink.write(json.dumps({"input_hash": row["input_hash"], "text": text, "text_hash": at.text_hash(text),
                                           "model": a.model, "attempts": attempt,
                                           "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds")},
                                          ensure_ascii=False) + "\n")
                    continue
                reasons[re.sub(r"'[^']*'", "'...'", reason)] += 1
                if attempt < a.attempts:
                    retry.append((row, at.retry_suffix(reason, text)))
                else:
                    n_failed += 1
                    failed.write(json.dumps({"input_hash": row["input_hash"], "name": row["name"], "reason": reason,
                                             "last_text": text}, ensure_ascii=False) + "\n")
            sink.flush()
            gen.log(f"attempt {attempt}: {sum(passed.values())}/{len(rows)} written, {len(retry)} to retry | "
                    f"{tokens / (time.time() - started):.0f} generated tok/s")
            pending = retry

    stats = {"inputs": len(rows), "written": sum(passed.values()), "failed": n_failed,
             "written_at_attempt": dict(passed), "rejections": dict(reasons.most_common()), "output_tokens": tokens,
             "seconds": round(time.time() - started, 1), "model": a.model, "vllm": vllm_version, "sampling": SAMPLING}
    Path(a.stats).write_text(json.dumps(stats, indent=2))
    gen.log(f"done: {stats['written']} written, {stats['failed']} failed")


if __name__ == "__main__":
    main()
