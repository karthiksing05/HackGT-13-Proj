"""Smoke test: does MUSE_API_KEY work against Meta's Model API (Muse Spark)?

Run directly:  python3 dataingestion/tests/test_muse_live.py
Or via pytest: pytest dataingestion/tests/test_muse_live.py

1. GET /models: checks the key is accepted (no tokens billed).
2. One short chat completion: checks the key can actually generate text
   (a few hundred tokens including reasoning, billed pay-as-you-go).

Stdlib only, like the Ticketmaster smoke test.
"""

import json
import os
import urllib.error
import urllib.request
from pathlib import Path

ENV_PATH = Path(__file__).resolve().parents[2] / ".env"
BASE_URL = "https://api.meta.ai/v1"
MODEL = os.environ.get("MUSE_MODEL", "muse-spark-1.3")


def load_api_key():
    key = os.environ.get("MUSE_API_KEY")
    if key:
        return key
    if ENV_PATH.exists():
        for line in ENV_PATH.read_text().splitlines():
            name, _, value = line.partition("=")
            if name.strip() == "MUSE_API_KEY":
                return value.strip().strip("'\"")
    raise RuntimeError("MUSE_API_KEY not set in environment or .env")


def call(path, body=None):
    req = urllib.request.Request(
        BASE_URL + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers={"Authorization": f"Bearer {load_api_key()}", "Content-Type": "application/json"},
        method="POST" if body is not None else "GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise AssertionError(f"{path} returned HTTP {e.code}: {e.read()[:300]!r}") from None


def check_models():
    body = call("/models")
    ids = [m["id"] for m in body.get("data", [])]
    assert ids, f"no models listed: {body!r:.300}"
    return ids


def check_generation():
    body = call(
        "/chat/completions",
        {
            "model": MODEL,
            # Muse Spark reasons before answering, and reasoning tokens count against
            # max_tokens; too small a budget returns content=None, finish_reason="length".
            "max_tokens": 1024,
            "messages": [
                {
                    "role": "user",
                    "content": "In one sentence, describe Piedmont Park in Atlanta for someone with a free afternoon.",
                }
            ],
        },
    )
    choice = body["choices"][0]
    text = choice["message"]["content"]
    assert text and text.strip(), f"empty completion (finish_reason={choice['finish_reason']}, usage={body.get('usage')})"
    return text


def test_muse_key_lists_models():
    check_models()


def test_muse_key_generates_text():
    check_generation()


if __name__ == "__main__":
    print("models:", ", ".join(check_models()))
    print(f"{MODEL} says:", check_generation())
