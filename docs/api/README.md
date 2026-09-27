# The app ↔ backend contract

The iPhone app talks to one HTTP + WebSocket API. What that API looks like is written down once, in
[`frontend/API_CONTRACT.md`](../../frontend/API_CONTRACT.md): every endpoint the app calls, the JSON
conventions (snake_case keys, ISO 8601 dates, integer cents, cursor pagination, `X-Time-Zone`), and
the fields of every shape. The Swift models in
[`frontend/SideQuestz/Models/`](../../frontend/SideQuestz/Models/) are the source of truth for field
names; [`Models/Decoding.swift`](../../frontend/SideQuestz/Models/Decoding.swift) lists which fields
are optional on the wire and what the app assumes when they're missing.

## `examples/`

[`examples/`](examples/) holds one JSON file per contract shape (`PlanBatch.json`, `User.json`, …)
plus [`index.json`](examples/index.json), which lists them. They are produced by the app's own
encoder, so they are exactly what the app sends and what it expects to read:

- `<Name>.json` is the payload of that Swift model (`Name` matches the type, or a variant of it:
  `PlanBatch.dag` is a batch with the DAG planner's extras, `PlanBatch.empty` an empty one with a
  `reason`, `RouteResult.dag` a re-timed route with `broken_at`, `UserHomeBase` an account with a
  home base).
- Every file is normalized the same way: keys sorted, 2-space indent, UTF-8 (no `\uXXXX` escapes), a
  trailing newline. A regeneration therefore only changes the shapes whose models changed.
- Requests (`PlanRequest`, `RouteRequest`, `CreateItineraryRequest`, `CreateCheckoutRun`, `SignupRequest`,
  `UserPatch`, `ItineraryUpdate`, `NewExpense`, `NewFreePost`, `ForumQuery`) are what the app sends; the
  rest are responses the app decodes.

## How the backend uses them

The Go server's contract tests (`Backend/pkg/contract`) decode every example into the matching
`contract` struct with unknown fields rejected, and re-encode it. So:

- Every key in an example must be a field of the Go struct. A key the app adds and the server doesn't
  know fails the test on the Go side; a key the server adds and the app doesn't know is fine for the
  app (unknown keys are ignored) but must still be listed in `API_CONTRACT.md`.
- Optional fields are the ones `Decoding.swift` defaults; the examples show them when the demo data
  has a value.

## Regenerating

From `frontend/`, with the simulator and Xcode 26.6 set up as in the README:

```sh
export DEVELOPER_DIR=/Applications/Xcode-26.6.app/Contents/Developer
DUMP=$(mktemp -d)
TEST_RUNNER_SQ_DUMP_CONTRACT=$DUMP xcodebuild test -project SideQuestz.xcodeproj -scheme SideQuestz \
  -destination "platform=iOS Simulator,name=<your simulator>" -only-testing:SideQuestzTests/ContractTests -quiet
python3 scripts/gen_contract_examples.py $DUMP API_CONTRACT.md
```

`ContractTests` round-trips every model through the live client's JSON settings and writes each
payload to `$DUMP/<name>.json`. The script then rewrites the "Example payloads" section of
`API_CONTRACT.md` and copies the listed dumps, normalized, into `docs/api/examples/` (or elsewhere with
`--examples-dir DIR`; `--no-copy` skips the copy). It deletes example files that are no longer listed,
writes `index.json`, and exits non-zero when a listed example wasn't dumped, so the two files never
drift apart. Commit `API_CONTRACT.md` and `docs/api/examples/` together.

## Changing the contract

1. Change the Swift model (and `Decoding.swift` when the field is optional or has an older spelling to
   accept), and the endpoint table in `API_CONTRACT.md`.
2. If the shape is new or a variant is worth showing, add a `roundTrip(…, "Name")` in
   `frontend/SideQuestzTests/ContractTests.swift` and the name to `GROUPS` in
   `scripts/gen_contract_examples.py`.
3. Regenerate (above) and review `git diff docs/api/examples`: only the shapes you touched should change.
4. Update the Go `contract` structs to match; `go test ./...` in `Backend/` runs the contract tests
   against the new examples.

Additive changes (a new optional field) are safe in either direction: the app ignores unknown keys and
defaults missing optional ones. Renames and removals are breaking on both sides and need the app and
the server to ship together.
