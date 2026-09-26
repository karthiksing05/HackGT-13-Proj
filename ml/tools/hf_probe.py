"""Does HF_TOKEN reach Qwen3-Embedding through the Hugging Face Inference Providers router?

    cd ml && python -m tools.hf_probe [--json] [--routes deepinfra,hf-inference]

Never prints the token. Reports, going as far as it can:
  token    the account and the token's role; for a fine-grained token, whether it may "Make calls to
           Inference Providers" (the `inference.serverless.write` permission the router needs)
  mapping  the Hub's inference-provider mapping for the model: provider, status, provider model id
  routes   one canary embed per route through HFRouterEmbedder (the production client, no retries):
           outcome, detail, dimension, norm, latency and the cosine against the local golden
Exit 0 when a route returned a 1024-d vector, else 1. Enable the provider only after
`tools.parity_check --provider hf` passes as well.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from collections.abc import Callable, Mapping
from pathlib import Path

import httpx
import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # ml/, when run as a script

from embedding import CANARY_TEXT, DIM, MODEL, EmbedderError, HFRouterEmbedder, RouteMapping  # noqa: E402
from embedding.hf_router import DEFAULT_ROUTER_BASE, DEFAULT_ROUTES  # noqa: E402

GOLDEN = Path(__file__).resolve().parents[1] / "tests" / "fixtures" / "qwen3_golden.json"
INFERENCE_PERMISSION = "inference.serverless.write"


def golden_vector(name: str = "canary") -> np.ndarray | None:
    """A text's vector from the local golden (None when the fixture is missing)."""
    try:
        golden = json.loads(GOLDEN.read_text())
        return np.asarray(golden["embeddings"][golden["texts"].index(name)], dtype=np.float64)
    except (OSError, KeyError, ValueError):
        return None


def cosine(a: np.ndarray, b: np.ndarray) -> float:
    return float(a @ b / (np.linalg.norm(a) * np.linalg.norm(b)))


def token_info(token: str, whoami: Callable[[str], dict] | None = None) -> dict:
    """The account and permissions behind the token (never the token itself)."""
    try:
        if whoami is None:
            from huggingface_hub import HfApi

            info = HfApi(token=token).whoami()
        else:
            info = whoami(token)
    except Exception as exc:
        return {"ok": False, "error": f"{type(exc).__name__}: {str(exc)[:200]}"}
    access = (info.get("auth") or {}).get("accessToken") or {}
    role = access.get("role")
    out = {"ok": True, "account": info.get("name"), "token_name": access.get("displayName"), "role": role}
    if role == "fineGrained":
        granted = (access.get("fineGrained") or {}).get("global") or []
        out["inference_providers_permission"] = INFERENCE_PERMISSION in granted
    return out


def provider_mapping(token: str, lookup: Callable[[], list[dict]] | None = None) -> dict:
    """The Hub's inference-provider mapping for the model."""
    try:
        if lookup is None:
            from huggingface_hub import HfApi

            info = HfApi(token=token).model_info(MODEL, expand=["inferenceProviderMapping"])
            raw = getattr(info, "inference_provider_mapping", None) or []
            entries = raw.values() if isinstance(raw, dict) else raw
            rows = [
                {"provider": m.provider, "status": m.status, "provider_id": m.provider_id, "task": getattr(m, "task", None)}
                for m in entries
            ]
        else:
            rows = lookup()
    except Exception as exc:
        return {"ok": False, "error": f"{type(exc).__name__}: {str(exc)[:200]}", "providers": []}
    return {"ok": True, "providers": rows}


def probe_route(
    token: str, route: str, mapping: list[dict], *, router_base: str = DEFAULT_ROUTER_BASE,
    transport: httpx.BaseTransport | None = None,
) -> dict:
    routes = {m["provider"]: RouteMapping(m.get("provider_id") or MODEL, m.get("status") or "unknown") for m in mapping}
    embedder = HFRouterEmbedder(
        token, routes=(route,), router_base=router_base, max_retries=0, transport=transport,
        mapping_lookup=lambda: routes, sleep=lambda seconds: None,
    )
    started = time.monotonic()
    try:
        result = embedder.embed_result([CANARY_TEXT], "activity")
    except EmbedderError as exc:
        return {"route": route, "ok": False, "kind": exc.kind, "detail": exc.message[:300], "ms": _ms(started)}
    finally:
        embedder.close()
    vector = result.vectors[0].astype(np.float64)
    golden = golden_vector()
    return {
        "route": route,
        "ok": vector.shape[0] == DIM,
        "dim": int(vector.shape[0]),
        "norm": round(float(np.linalg.norm(vector)), 6),
        "ms": _ms(started),
        "cosine_vs_local": None if golden is None or golden.shape != vector.shape else round(cosine(vector, golden), 6),
    }


def _ms(started: float) -> float:
    return round((time.monotonic() - started) * 1000.0, 1)


def run(
    argv: list[str] | None = None, *, env: Mapping[str, str] = os.environ, whoami=None, mapping_lookup=None,
    transport: httpx.BaseTransport | None = None, out=sys.stdout,
) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--json", action="store_true", help="print the report as JSON")
    ap.add_argument("--routes", default=",".join(DEFAULT_ROUTES))
    ap.add_argument("--router-base", default=env.get("HF_ROUTER_BASE") or DEFAULT_ROUTER_BASE)
    args = ap.parse_args(argv)

    report: dict = {"model": MODEL, "token": None, "mapping": None, "routes": []}
    token = env.get("HF_TOKEN")
    if not token:
        report["token"] = {"ok": False, "error": "HF_TOKEN is not set"}
    else:
        report["token"] = token_info(token, whoami)
        report["mapping"] = provider_mapping(token, mapping_lookup)
        for route in [r.strip() for r in args.routes.split(",") if r.strip()]:
            report["routes"].append(probe_route(token, route, report["mapping"]["providers"], router_base=args.router_base, transport=transport))
    report["ok"] = any(r["ok"] for r in report["routes"])

    if args.json:
        print(json.dumps(report, indent=2), file=out)
    else:
        _print(report, out)
    return 0 if report["ok"] else 1


def _print(report: dict, out) -> None:
    print(f"model: {report['model']}", file=out)
    t = report["token"]
    if not t["ok"]:
        print(f"token: {t['error']}", file=out)
    else:
        line = f"token: account={t['account']} name={t['token_name']!r} role={t['role']}"
        if "inference_providers_permission" in t:
            line += f" 'Make calls to Inference Providers'={'granted' if t['inference_providers_permission'] else 'MISSING'}"
        print(line, file=out)
    m = report["mapping"]
    if m is not None:
        if not m["ok"]:
            print(f"mapping: {m['error']}", file=out)
        for row in m["providers"]:
            print(f"mapping: {row['provider']:<14} status={row['status']:<8} provider_id={row['provider_id']} task={row.get('task')}", file=out)
    for r in report["routes"]:
        if r["ok"]:
            print(f"route {r['route']}: OK dim={r['dim']} norm={r['norm']} {r['ms']} ms cosine_vs_local={r['cosine_vs_local']}", file=out)
        else:
            print(f"route {r['route']}: FAILED ({r['kind']}) {r['ms']} ms: {r['detail']}", file=out)
    print("result: " + ("a route works" if report["ok"] else "no route works"), file=out)


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
