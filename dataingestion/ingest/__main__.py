"""CLI (§8). Run from dataingestion/:  python -m ingest --help"""

import logging
from pathlib import Path
from typing import Optional

import typer
from bson import json_util
from pymongo.errors import PyMongoError

from . import quota as quota_mod
from .adapters import ADAPTERS
from .config import load_city
from .db import ensure_indexes, get_db
from .pipeline import run_adapter

app = typer.Typer(no_args_is_help=True, add_completion=False)
snapshot_app = typer.Typer(no_args_is_help=True)
app.add_typer(snapshot_app, name="snapshot", help="Export/import activities as JSON.")
research_app = typer.Typer(no_args_is_help=True)
app.add_typer(research_app, name="research", help="Hand web research to an outside agent (Claude Code) and load its notes.")

CityOpt = typer.Option(None, "--city", help="City slug; defaults to config.yaml default_city.")


@app.callback()
def main(verbose: bool = typer.Option(False, "-v", "--verbose")):
    logging.basicConfig(level=logging.DEBUG if verbose else logging.INFO, format="%(levelname)s %(name)s: %(message)s")
    logging.getLogger("httpx").setLevel(logging.WARNING)


def _db_or_none():
    """Dry runs work without Mongo; they just can't report remaining quota."""
    try:
        db = get_db()
        db.command("ping")
        return db
    except PyMongoError:
        typer.echo("(Mongo not reachable; continuing without it)")
        return None


def _print_stats(stats) -> None:
    line = (
        f"{stats.source} [{stats.city}]{' (dry run)' if stats.dryRun else ''}: "
        f"fetched={stats.fetched} skipped={stats.skipped} inserted={stats.inserted} updated={stats.updated}"
    )
    typer.echo(line)
    for err in stats.errors:
        typer.secho(f"  error: {err}", fg=typer.colors.RED)


@app.command("init-db")
def init_db():
    """Create indexes (idempotent)."""
    names = ensure_indexes(get_db())
    typer.echo("indexes: " + ", ".join(names))


@app.command()
def run(
    adapter: str = typer.Argument(..., help=f"One of: {', '.join(ADAPTERS)}"),
    city: Optional[str] = CityOpt,
    dry_run: bool = typer.Option(False, "--dry-run", help="Free APIs: fetch + normalize to out/ without writing. Paid APIs: estimate calls only."),
    limit: Optional[int] = typer.Option(None, "--limit", help="Stop after N raw records."),
):
    """Run one adapter."""
    if adapter not in ADAPTERS:
        raise typer.BadParameter(f"unknown adapter {adapter!r}; choose from {', '.join(ADAPTERS)}")
    db = _db_or_none() if dry_run else get_db()
    stats = run_adapter(adapter, load_city(city), db, dry_run=dry_run, limit=limit)
    _print_stats(stats)
    if stats.errors and not (stats.inserted or stats.updated or stats.fetched):
        raise typer.Exit(1)


@app.command("run-all")
def run_all(city: Optional[str] = CityOpt, dry_run: bool = typer.Option(False, "--dry-run")):
    """Run every adapter enabled in the city config. A failing adapter doesn't stop the rest."""
    c = load_city(city)
    db = _db_or_none() if dry_run else get_db()
    for name in ADAPTERS:
        if c.adapters.get(name):
            _print_stats(run_adapter(name, c, db, dry_run=dry_run))


def _select(city: Optional[str], kind: str, limit: int, activity_id: Optional[str], research_provider: Optional[str] = None):
    from bson import ObjectId

    from .agent.research import CLAUDE

    c = load_city(city)
    db = get_db()
    q: dict = {"city": c.slug}
    if research_provider == CLAUDE:
        # No live research with `claude`: only activities whose notes were imported are worth writing.
        q["_id"] = {"$in": db.research.distinct("activityId", {"provider": CLAUDE})}
    if activity_id:
        q["_id"] = ObjectId(activity_id)
    elif kind != "all":
        q["kind"] = kind
    # Soonest events first (most likely to be planned); places by rating signal.
    cursor = db.activities.find(q, {"embedding": 0}).sort([("start", 1), ("ratingCount", -1)])
    if limit:
        cursor = cursor.limit(limit)
    return c, db, list(cursor)


def _run_batch(agent, docs, force: bool, dry_run: bool, out_name: str, record) -> None:
    """Run an agent over docs, failing soft per activity. `record(doc, outcome)` -> dry-run row."""
    import json

    from .config import OUT_DIR

    counts = {"written": 0, "skipped": 0, "failed": 0}
    grounded, rows = 0, []
    for i, doc in enumerate(docs, 1):
        label = f"[{i}/{len(docs)}] {doc['name'][:50]}"
        try:
            res = agent.run(doc, force=force)
        except Exception as e:  # one bad activity doesn't stop the batch
            counts["failed"] += 1
            typer.secho(f"{label}: {type(e).__name__}: {str(e)[:200]}", fg=typer.colors.RED)
            continue
        counts[res.status] += 1
        if res.status == "written":
            n_sources = len((res.research or {}).get("sources") or [])
            grounded += bool(res.research)
            typer.echo(f"{label} ({n_sources} sources)")
            rows.append(record(doc, res))
        elif res.status == "failed":
            typer.secho(f"{label}: failed checks ({res.reason})", fg=typer.colors.YELLOW)
    if dry_run and rows:
        OUT_DIR.mkdir(exist_ok=True)
        path = OUT_DIR / out_name
        path.write_text(json.dumps(rows, indent=1, default=str))
        typer.echo(f"wrote {len(rows)} results to {path}")
    typer.echo(f"written={counts['written']} (grounded={grounded}) skipped={counts['skipped']} failed={counts['failed']}")
    if agent.grounded_off:
        typer.secho(f"grounded research stopped early: {agent.grounded_off}", fg=typer.colors.YELLOW)


KindOpt = typer.Option("event", "--kind", help="event, place or all")
LimitOpt = typer.Option(0, "--limit", help="0 = every matching activity")
IdOpt = typer.Option(None, "--id", help="One activity by _id")
ResearchOpt = typer.Option(None, "--research-provider", help="muse, gemini or claude; overrides config.yaml. "
                           "claude = only activities with notes from `ingest research import`")


def _agent_cfg(research_provider: Optional[str]) -> dict:
    from .config import load_global

    cfg = load_global()["blurb"]
    return {**cfg, "research_provider": research_provider} if research_provider else cfg


@app.command()
def blurb(
    city: Optional[str] = CityOpt,
    kind: str = KindOpt,
    limit: int = LimitOpt,
    activity_id: Optional[str] = IdOpt,
    force: bool = typer.Option(False, "--force", help="Rewrite even if the blurb is current"),
    dry_run: bool = typer.Option(False, "--dry-run", help="Write blurbs to out/ instead of the DB (research is still cached)"),
    research_provider: Optional[str] = ResearchOpt,
):
    """Research activities on the web and write their detail-screen paragraph (§6.6)."""
    from .agent.blurb import BlurbAgent

    c, db, docs = _select(city, kind, limit, activity_id, research_provider)
    agent = BlurbAgent(db, _agent_cfg(research_provider), c.name, dry_run=dry_run)
    _run_batch(agent, docs, force, dry_run, f"blurbs_{c.slug}_dryrun.json", lambda doc, res: {
        "_id": str(doc["_id"]), "name": doc["name"], **res.blurb,
        "researchNotes": (res.research or {}).get("notes"),
    })


@app.command("embed-text")
def embed_text(
    city: Optional[str] = CityOpt,
    kind: str = KindOpt,
    limit: int = LimitOpt,
    activity_id: Optional[str] = IdOpt,
    force: bool = typer.Option(False, "--force", help="Regenerate even if current"),
    dry_run: bool = typer.Option(False, "--dry-run", help="Write results to out/ instead of the DB (research is still cached)"),
    research_provider: Optional[str] = ResearchOpt,
):
    """Web-research activities and write structured embedding text for the ML model."""
    from .agent.embed_text import EmbedTextAgent

    c, db, docs = _select(city, kind, limit, activity_id, research_provider)
    agent = EmbedTextAgent(db, _agent_cfg(research_provider), c.name, dry_run=dry_run)
    _run_batch(agent, docs, force, dry_run, f"embed_text_{c.slug}_dryrun.json", lambda doc, res: {
        "_id": str(doc["_id"]), "name": doc["name"], "embeddingText": res.text,
        "eventData": res.event_data, "sources": [s["url"] for s in (res.research or {}).get("sources", [])],
    })


PIPELINE_SOURCES = ["ticketmaster", "google_places", "resident_advisor", "osm_trails"]


@app.command()
def pipeline(
    city: Optional[str] = CityOpt,
    sources: str = typer.Option(",".join(PIPELINE_SOURCES), "--sources", help="Comma-separated adapters to fetch"),
    blurbs: bool = typer.Option(True, "--blurbs/--no-blurbs", help="Generate Gemini blurbs"),
    embed_texts: bool = typer.Option(True, "--embed-text/--no-embed-text", help="Generate ML embedding text"),
    blurb_kind: str = typer.Option("event", "--blurb-kind", help="event, place or all (places need a lot of research quota)"),
    blurb_limit: int = typer.Option(0, "--blurb-limit", help="0 = all"),
):
    """Full ingestion: fetch sources -> geocode/fill missing fields -> Gemini blurbs + embedding text -> coverage report."""
    from .agent.blurb import BlurbAgent
    from .agent.embed_text import EmbedTextAgent
    from .agent.research import Researcher
    from .config import load_global
    from .pipeline import backfill, coverage

    c = load_city(city)
    db = get_db()
    ensure_indexes(db)

    typer.secho("1/4 fetch", bold=True)
    for name in [s.strip() for s in sources.split(",") if s.strip()]:
        if name not in ADAPTERS:
            typer.secho(f"  unknown source {name!r}; skipping", fg=typer.colors.RED)
            continue
        if not c.adapters.get(name, True):
            typer.echo(f"  {name}: disabled in cities/{c.slug}.yaml")
            continue
        stats = run_adapter(name, c, db)  # fails soft: errors land in `runs`, the pipeline continues
        _print_stats(stats)
        if stats.skipReasons:
            typer.echo("    skipped: " + ", ".join(f"{k}={v}" for k, v in stats.skipReasons.items()))

    typer.secho("2/4 fill missing fields (geocode coordinates/address, region, duration, expiry)", bold=True)
    counts = backfill(db, c)
    typer.echo("  " + " ".join(f"{k}={v}" for k, v in counts.items()))

    typer.secho("3/4 Gemini: web research, blurbs, embedding text", bold=True)
    if blurbs or embed_texts:
        cfg = load_global()["blurb"]
        researcher = Researcher(db, cfg, c.name, city=c)  # one research cache + quota tracker for both writers
        _, _, docs = _select(c.slug, blurb_kind, blurb_limit, None)
        if blurbs:
            typer.echo("  blurbs:")
            _run_batch(BlurbAgent(db, cfg, c.name, researcher=researcher), docs, False, False, "", lambda d, r: {})
        if embed_texts:
            typer.echo("  embedding text:")
            _run_batch(EmbedTextAgent(db, cfg, c.name, researcher=researcher), docs, False, False, "", lambda d, r: {})
    else:
        typer.echo("  skipped")

    typer.secho("4/4 coverage", bold=True)
    for label, n, total in coverage(db, c):
        pct = f"{100 * n / total:.0f}%" if total else "-"
        colour = typer.colors.GREEN if total and n == total else typer.colors.YELLOW if total and n / total >= 0.8 else typer.colors.RED
        typer.secho(f"  {label:<26} {n:>5}/{total:<5} {pct:>4}", fg=colour)


@app.command()
def quota():
    """Show quota usage for limited APIs this period."""
    for row in quota_mod.usage(get_db()):
        typer.echo(f"{row['api']:<22} {row['period']:<10} {row['used']:>5} / {row['cap']}")


@app.command()
def stats(city: Optional[str] = CityOpt):
    """Counts by kind/category/source and field coverage."""
    c = load_city(city)
    acts = get_db().activities
    q = {"city": c.slug}
    total = acts.count_documents(q)
    typer.echo(f"{total} activities in {c.slug}")
    if not total:
        return
    for field in ("kind", "category"):
        rows = acts.aggregate([{"$match": q}, {"$group": {"_id": f"${field}", "n": {"$sum": 1}}}, {"$sort": {"n": -1}}])
        typer.echo(f"by {field}: " + ", ".join(f"{r['_id']}={r['n']}" for r in rows))
    rows = acts.aggregate([{"$match": q}, {"$unwind": "$sources"}, {"$group": {"_id": "$sources.name", "n": {"$sum": 1}}}])
    typer.echo("by source: " + ", ".join(f"{r['_id']}={r['n']}" for r in rows))
    coverage = {
        "weeklyHours (places)": ({"kind": "place", "weeklyHours": {"$ne": None}}, {"kind": "place"}),
        "duration": ({"duration": {"$ne": None}}, {}),
        "price tier or isFree": ({"$or": [{"price.tier": {"$ne": None}}, {"price.isFree": {"$ne": None}}]}, {}),
        "address.region": ({"address.region": {"$ne": None}}, {}),
        "embedding": ({"embedding": {"$exists": True}}, {}),
        "blurb": ({"blurb": {"$ne": None, "$exists": True}}, {}),
    }
    for label, (num, den) in coverage.items():
        d = acts.count_documents({**q, **den})
        n = acts.count_documents({**q, **den, **num})
        typer.echo(f"  {label:<22} {n}/{d}" + (f" ({100 * n / d:.0f}%)" if d else ""))


@research_app.command("todo")
def research_todo(
    city: Optional[str] = CityOpt,
    kind: str = KindOpt,
    limit: int = typer.Option(10, "--limit", help="0 = every activity still missing notes"),
    out: Optional[Path] = typer.Option(None, "--out", help="Default: out/research_todo_<city>.json"),
):
    """Write upcoming activities that need research (facts + empty notes/sources) to a JSON file."""
    import json

    from .agent.research import research_todo as todo
    from .config import OUT_DIR

    c = load_city(city)
    rows = todo(get_db(), c.slug, kind, limit)
    path = out or OUT_DIR / f"research_todo_{c.slug}.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(rows, indent=1, ensure_ascii=False, default=str))
    typer.echo(f"wrote {len(rows)} activities to {path}; fill in notes + sources, then `ingest research import {path}`")


@research_app.command("import")
def research_import(path: Path):
    """Load filled-in notes (from `research todo`) as `claude` research, shared by blurb and embed-text."""
    import json

    from .agent.research import import_research

    counts = import_research(get_db(), json.loads(path.read_text()))
    typer.echo(" ".join(f"{k}={v}" for k, v in counts.items()))
    if counts["facts_changed"]:
        typer.secho("some activities changed since the todo was exported; check their notes still apply", fg=typer.colors.YELLOW)
    typer.echo("next: ingest blurb --research-provider claude  (and/or embed-text)")


@snapshot_app.command("export")
def snapshot_export(
    path: Path,
    city: Optional[str] = CityOpt,
    limit: int = typer.Option(0, "--limit", help="0 = all. Use ~50 for a team sample."),
    kind: Optional[str] = typer.Option(None, "--kind", help="event or place"),
):
    """Write activities to a JSON file (Extended JSON, relaxed)."""
    q = {"city": load_city(city).slug}
    if kind:
        q["kind"] = kind
    docs = list(get_db().activities.find(q, {"embedding": 0}).sort("start", 1).limit(limit))
    path.write_text(json_util.dumps(docs, indent=1, json_options=json_util.RELAXED_JSON_OPTIONS))
    typer.echo(f"wrote {len(docs)} activities to {path}")


@snapshot_app.command("import")
def snapshot_import(path: Path):
    """Load a snapshot back (upserts by _id)."""
    db = get_db()
    docs = json_util.loads(path.read_text())
    for d in docs:
        db.activities.replace_one({"_id": d["_id"]}, d, upsert=True)
    typer.echo(f"imported {len(docs)} activities")


if __name__ == "__main__":
    app()
