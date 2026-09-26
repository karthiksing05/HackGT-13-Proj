"""Does a provider produce the local model's vectors? The gate before a remote provider is switched on.

    cd ml && python -m tools.parity_check --ml-url http://127.0.0.1:8000   # what a running service returns
    cd ml && python -m tools.parity_check --provider hf|vertex|local        # one provider, settings from the env

Embeds the five texts of tests/fixtures/golden_texts.json and compares every vector with
tests/fixtures/qwen3_golden.json (local Qwen3-Embedding-0.6B, fp32, CPU, no prompt). Passes when
every cosine is at least --min-cosine (0.995). A provider is checked alone, with no cache and no
local fallback, so a failing remote provider fails here instead of passing through the local
model. Through a service, texts it serves from its cache were embedded by whichever provider saw
them first (the report shows `cached`). The golden also keeps the production vector of one
activity; its cosine is reported for reference. Exit 0 on pass, 1 on a parity failure, 2 when
nothing could be embedded.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections.abc import Mapping
from pathlib import Path

import httpx
import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # ml/, when run as a script

from embedding import EmbedderError, EmbeddingSettings, NullCache, build_embedder  # noqa: E402

FIXTURES = Path(__file__).resolve().parents[1] / "tests" / "fixtures"
MIN_COSINE = 0.995


def load_goldens(fixtures: Path = FIXTURES) -> tuple[list[dict], dict]:
    texts = json.loads((fixtures / "golden_texts.json").read_text())["texts"]
    golden = json.loads((fixtures / "qwen3_golden.json").read_text())
    if [t["name"] for t in texts] != golden["texts"]:
        raise SystemExit("golden_texts.json and qwen3_golden.json disagree; rerun tools.make_golden")
    return texts, golden


def cosines(vectors: np.ndarray, expected: np.ndarray) -> np.ndarray:
    vectors = np.asarray(vectors, dtype=np.float64)
    expected = np.asarray(expected, dtype=np.float64)
    norms = np.linalg.norm(vectors, axis=1) * np.linalg.norm(expected, axis=1)
    return np.divide(np.sum(vectors * expected, axis=1), norms, out=np.zeros(len(vectors)), where=norms > 0)


def embed_via_service(ml_url: str, texts: list[str], transport: httpx.BaseTransport | None = None) -> dict:
    with httpx.Client(timeout=300, transport=transport) as client:
        response = client.post(f"{ml_url.rstrip('/')}/v1/embed", json={"texts": texts, "kind": "activity"})
    if response.status_code != 200:
        raise EmbedderError("unavailable", f"HTTP {response.status_code}: {response.text[:200]}", retryable=True, provider="service")
    body = response.json()
    return {"vectors": np.asarray(body["embeddings"], dtype=np.float64), "provider": body.get("provider"),
            "cached": body.get("cached", 0), "model": body.get("model")}


def embed_via_provider(provider: str, texts: list[str], env: Mapping[str, str]) -> dict:
    settings = EmbeddingSettings.from_env(env)
    settings.provider = provider  # type: ignore[assignment]
    settings.local_fallback = False
    settings.cache_path = None
    settings.warmup = False
    embedder = build_embedder(settings, cache=NullCache())
    result = embedder.embed_result(texts, "activity")
    return {"vectors": np.asarray(result.vectors, dtype=np.float64), "provider": result.provider, "cached": 0, "model": settings.model}


def run(argv: list[str] | None = None, *, env: Mapping[str, str] = os.environ,
        transport: httpx.BaseTransport | None = None, out=sys.stdout) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    source = ap.add_mutually_exclusive_group(required=True)
    source.add_argument("--ml-url", help="a running ML service")
    source.add_argument("--provider", choices=["vertex", "hf", "local"], help="one provider, configured from the env")
    ap.add_argument("--min-cosine", type=float, default=MIN_COSINE)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args(argv)

    texts, golden = load_goldens()
    try:
        got = embed_via_service(args.ml_url, [t["text"] for t in texts], transport) if args.ml_url \
            else embed_via_provider(args.provider, [t["text"] for t in texts], env)
    except (EmbedderError, httpx.HTTPError) as exc:
        report = {"ok": False, "error": f"{type(exc).__name__}: {exc}"[:400]}
        print(json.dumps(report, indent=2) if args.json else f"cannot embed: {report['error']}", file=out)
        return 2

    expected = np.asarray(golden["embeddings"], dtype=np.float64)
    if got["vectors"].shape != expected.shape:
        report = {"ok": False, "error": f"got vectors of shape {got['vectors'].shape}, the golden is {expected.shape}"}
        print(json.dumps(report, indent=2) if args.json else report["error"], file=out)
        return 1
    values = cosines(got["vectors"], expected)
    report = {
        "ok": bool(values.min() >= args.min_cosine),
        "source": args.ml_url or args.provider,
        "provider": got["provider"],
        "model": got["model"],
        "cached": got["cached"],
        "min_cosine": round(float(values.min()), 6),
        "threshold": args.min_cosine,
        "cosines": {t["name"]: round(float(c), 6) for t, c in zip(texts, values)},
    }
    reference = golden.get("reference_activity")
    if reference:
        stored = np.asarray(reference["embedding"], dtype=np.float64)[None, :]
        report["reference_activity_vs_stored"] = round(float(cosines(got["vectors"][-1:], stored)[0]), 6)

    if args.json:
        print(json.dumps(report, indent=2), file=out)
    else:
        print(f"source: {report['source']} (provider {report['provider']}, model {report['model']}, cached {report['cached']})", file=out)
        for name, value in report["cosines"].items():
            print(f"  {value:.6f}  {name}", file=out)
        if "reference_activity_vs_stored" in report:
            print(f"  {report['reference_activity_vs_stored']:.6f}  reference activity vs its stored production vector", file=out)
        print(f"min cosine {report['min_cosine']:.6f} vs threshold {args.min_cosine}: {'PASS' if report['ok'] else 'FAIL'}", file=out)
    return 0 if report["ok"] else 1


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
