"""Does the service account reach our Qwen3-Embedding endpoint on Vertex AI, and how is it called?

    cd ml && python -m tools.vertex_probe [--json] [--credentials PATH] [--host HOST] [--keys inputs,prompt,text]

Credentials come from GOOGLE_APPLICATION_CREDENTIALS (a service-account key; a relative path is
resolved against the current directory) and the endpoint from VERTEX_PROJECT / VERTEX_LOCATION /
VERTEX_ENDPOINT_ID (defaults: our Model Garden deployment). Steps, each reported, as far as they go:

  1. credentials   the key's service account and project, and an access token (never printed)
  2. describe      GET the endpoint (needs aiplatform.endpoints.get): dedicated DNS, deployed model,
                   serving container
  3. hosts         --host / VERTEX_HOST, the described dedicated DNS, the conventional dedicated
                   name <endpoint>.<location>-<project>.prediction.vertexai.goog (if it resolves),
                   and the regional host; each resolved in DNS
  4. predict       the canary with each input key on each host through VertexEmbedder: outcome,
                   dimension, norm, latency and the cosine against the local golden

Prints the VERTEX_HOST and VERTEX_INPUT_KEY to configure when a 1024-d vector comes back (exit 0),
else exit 1. Both 403s mean the account lacks roles/aiplatform.user on the project.
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import sys
import time
from collections.abc import Callable, Mapping
from pathlib import Path

import httpx
import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # ml/, when run as a script

from embedding import CANARY_TEXT, DIM, EmbedderError, VertexEmbedder  # noqa: E402
from embedding.vertex import (  # noqa: E402
    DEFAULT_ENDPOINT_ID,
    DEFAULT_LOCATION,
    DEFAULT_PROJECT,
    INPUT_KEYS,
    regional_host,
    service_account_token_provider,
)
from tools.hf_probe import cosine, golden_vector  # noqa: E402

TokenProvider = Callable[[], str]
Resolver = Callable[[str], list[str]]


def resolve_dns(host: str) -> list[str]:
    return sorted({info[4][0] for info in socket.getaddrinfo(host, 443, proto=socket.IPPROTO_TCP)})


def key_identity(path: str) -> dict:
    """The service account and project named in a key file; the private key is never read out."""
    try:
        data = json.loads(Path(path).read_text())
    except (OSError, ValueError) as exc:
        return {"ok": False, "error": f"cannot read the key file: {type(exc).__name__}"}
    return {"ok": True, "client_email": data.get("client_email"), "project_id": data.get("project_id"), "type": data.get("type")}


def describe(client: httpx.Client, token: str, project: str, location: str, endpoint: str) -> dict:
    """The endpoint's dedicated DNS, deployed models and their containers (as far as permitted)."""
    base = f"https://{regional_host(location)}/v1"
    headers = {"Authorization": f"Bearer {token}"}
    try:
        response = client.get(f"{base}/projects/{project}/locations/{location}/endpoints/{endpoint}", headers=headers)
    except httpx.HTTPError as exc:
        return {"ok": False, "status": None, "detail": f"{type(exc).__name__}: {exc}"}
    if response.status_code != 200:
        return {"ok": False, "status": response.status_code, "detail": response.text[:300]}
    body = response.json()
    models = []
    for deployed in body.get("deployedModels") or []:
        row = {"id": deployed.get("id"), "display_name": deployed.get("displayName"), "model": deployed.get("model")}
        if deployed.get("model"):
            try:
                model = client.get(f"{base}/{deployed['model']}", headers=headers)
                if model.status_code == 200:
                    spec = model.json().get("containerSpec") or {}
                    row["container"] = spec.get("imageUri")
                    row["container_args"] = spec.get("args")
                else:
                    row["container"] = f"HTTP {model.status_code}"
            except httpx.HTTPError as exc:
                row["container"] = f"{type(exc).__name__}"
        models.append(row)
    return {
        "ok": True,
        "status": 200,
        "display_name": body.get("displayName"),
        "dedicated_endpoint_enabled": body.get("dedicatedEndpointEnabled"),
        "dedicated_dns": body.get("dedicatedEndpointDns"),
        "deployed_models": models,
    }


def candidate_hosts(explicit: str | None, described: str | None, project: str, location: str, endpoint: str, resolve: Resolver) -> list[dict]:
    guessed = f"{endpoint}.{location}-{project}.prediction.vertexai.goog"
    rows, seen = [], set()
    for host, source in ((explicit, "configured"), (described, "described"), (guessed, "conventional"), (regional_host(location), "regional")):
        if not host or host in seen:
            continue
        seen.add(host)
        try:
            addresses = resolve(host)
        except OSError as exc:
            rows.append({"host": host, "source": source, "resolves": False, "detail": str(exc)[:120]})
            continue
        rows.append({"host": host, "source": source, "resolves": bool(addresses), "addresses": addresses[:3]})
    return rows


def predict(host: str, key: str, token_provider: TokenProvider, project: str, location: str, endpoint: str,
            transport: httpx.BaseTransport | None, timeout: float) -> dict:
    embedder = VertexEmbedder(project=project, location=location, endpoint_id=endpoint, host=host, input_key=key,
                              token_provider=token_provider, transport=transport, timeout=timeout)
    started = time.monotonic()
    try:
        vector = embedder.embed([CANARY_TEXT])[0].astype(np.float64)
    except EmbedderError as exc:
        return {"host": host, "key": key, "ok": False, "kind": exc.kind, "detail": exc.message[:300], "ms": _ms(started)}
    finally:
        embedder.close()
    golden = golden_vector()
    return {
        "host": host, "key": key, "ok": vector.shape[0] == DIM, "dim": int(vector.shape[0]),
        "norm": round(float(np.linalg.norm(vector)), 6), "ms": _ms(started),
        "cosine_vs_local": None if golden is None or golden.shape != vector.shape else round(cosine(vector, golden), 6),
    }


def _ms(started: float) -> float:
    return round((time.monotonic() - started) * 1000.0, 1)


def run(
    argv: list[str] | None = None, *, env: Mapping[str, str] = os.environ, token_provider: TokenProvider | None = None,
    transport: httpx.BaseTransport | None = None, resolve: Resolver = resolve_dns, out=sys.stdout,
) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--json", action="store_true", help="print the report as JSON")
    ap.add_argument("--credentials", default=env.get("GOOGLE_APPLICATION_CREDENTIALS"))
    ap.add_argument("--project", default=env.get("VERTEX_PROJECT") or DEFAULT_PROJECT)
    ap.add_argument("--location", default=env.get("VERTEX_LOCATION") or DEFAULT_LOCATION)
    ap.add_argument("--endpoint", default=env.get("VERTEX_ENDPOINT_ID") or DEFAULT_ENDPOINT_ID)
    ap.add_argument("--host", default=env.get("VERTEX_HOST"))
    ap.add_argument("--keys", default=",".join(INPUT_KEYS))
    ap.add_argument("--timeout", type=float, default=60.0)
    args = ap.parse_args(argv)

    report: dict = {"project": args.project, "location": args.location, "endpoint": args.endpoint, "ok": False,
                    "credentials": None, "describe": None, "hosts": [], "predict": [], "use": None}
    if token_provider is None:
        if not args.credentials:
            report["credentials"] = {"ok": False, "error": "GOOGLE_APPLICATION_CREDENTIALS is not set"}
            return _finish(report, args.json, out)
        path = os.path.abspath(args.credentials)
        report["credentials"] = key_identity(path)
        try:
            token_provider = service_account_token_provider(path)
        except EmbedderError as exc:
            report["credentials"] = {"ok": False, "error": exc.message}
            return _finish(report, args.json, out)
    else:
        report["credentials"] = {"ok": True, "client_email": "(injected)"}
    try:
        token = token_provider()
    except Exception as exc:
        report["credentials"] = {**(report["credentials"] or {}), "ok": False, "error": f"token refresh failed: {type(exc).__name__}: {exc}"[:300]}
        return _finish(report, args.json, out)
    report["credentials"]["token"] = "ok"

    with httpx.Client(timeout=args.timeout, transport=transport) as client:
        report["describe"] = describe(client, token, args.project, args.location, args.endpoint)
    report["hosts"] = candidate_hosts(args.host, report["describe"].get("dedicated_dns"), args.project, args.location, args.endpoint, resolve)
    keys = [k.strip() for k in args.keys.split(",") if k.strip()]
    for host in [h["host"] for h in report["hosts"] if h["resolves"]]:
        for key in keys:
            result = predict(host, key, token_provider, args.project, args.location, args.endpoint, transport, args.timeout)
            report["predict"].append(result)
            if result["ok"]:
                report["ok"], report["use"] = True, {"VERTEX_HOST": host, "VERTEX_INPUT_KEY": key}
                return _finish(report, args.json, out)
            if result["kind"] in ("auth", "timeout", "unavailable"):
                break  # the input key cannot fix these; try the next host
    return _finish(report, args.json, out)


def _finish(report: dict, as_json: bool, out) -> int:
    if as_json:
        print(json.dumps(report, indent=2), file=out)
    else:
        _print(report, out)
    return 0 if report["ok"] else 1


def _print(report: dict, out) -> None:
    print(f"endpoint: projects/{report['project']}/locations/{report['location']}/endpoints/{report['endpoint']}", file=out)
    c = report["credentials"] or {}
    if c.get("ok"):
        print(f"credentials: {c.get('client_email')} (project {c.get('project_id')}), token {c.get('token', 'not fetched')}", file=out)
    else:
        print(f"credentials: {c.get('error')}", file=out)
    d = report["describe"]
    if d is not None:
        if d["ok"]:
            print(f"describe: {d['display_name']!r} dedicated={d['dedicated_endpoint_enabled']} dns={d['dedicated_dns']}", file=out)
            for m in d["deployed_models"]:
                print(f"  deployed model {m['id']} {m['display_name']!r} container={m.get('container')}", file=out)
        else:
            print(f"describe: HTTP {d['status']}: {d['detail']}", file=out)
    for h in report["hosts"]:
        state = f"resolves to {', '.join(h.get('addresses', []))}" if h["resolves"] else f"does not resolve ({h.get('detail', '')})"
        print(f"host ({h['source']}): {h['host']} {state}", file=out)
    for p in report["predict"]:
        if p["ok"]:
            print(f"predict {p['host']} key={p['key']}: OK dim={p['dim']} norm={p['norm']} {p['ms']} ms cosine_vs_local={p['cosine_vs_local']}", file=out)
        else:
            print(f"predict {p['host']} key={p['key']}: FAILED ({p['kind']}) {p['ms']} ms: {p['detail']}", file=out)
    if report["use"]:
        print(f"result: works; set VERTEX_HOST={report['use']['VERTEX_HOST']} VERTEX_INPUT_KEY={report['use']['VERTEX_INPUT_KEY']}", file=out)
    else:
        print("result: no 1024-d vector", file=out)


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
