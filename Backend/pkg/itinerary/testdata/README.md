# Itinerary fixtures

Real activities for 26 Sep 2026, used by `fixture_test.go`:

- `atlanta_2026-09-26.json`: events between 12:00 and midnight, plus the 60
  best-rated places within 6 km of Tech Square.
- `nyc_2026-09-26.json`: events between 12:00 and midnight (NYC has no
  Google places yet).

`score` is a deterministic stand-in for ranker output (0.35–0.95, from a
hash of the id), so results are stable between runs.

Regenerate from the ingestion output, running from the repo root:

    python3 Backend/pkg/itinerary/testdata/make_fixtures.py
