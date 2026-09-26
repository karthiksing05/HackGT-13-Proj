# API endpoints

This file used to list the backend's routes as derived from the mock UI. The app-facing API is now
specified by the iOS contract, [`frontend/API_CONTRACT.md`](../frontend/API_CONTRACT.md): every endpoint,
body and response, with JSON examples generated from the app's tests into
[`docs/api/examples/`](../docs/api/examples/), mirrored field for field by `pkg/contract` and checked by
`pkg/contract/contract_test.go` ([`docs/api/README.md`](../docs/api/README.md) has the change protocol).
How the pieces fit, the token lifecycle and the realtime events are in
[`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md), the handler packages in
[`pkg/api/README.md`](pkg/api/README.md), and the planner's routes (`/plans/*`) in
[`docs/PLANNER.md`](../docs/PLANNER.md). Routes listed here in earlier versions that the app never used
(`/hello`, the `/activities` and `/events` catalog listings, `.ics` calendar feeds, host-side join
approval) were removed with the old handlers; see [`docs/ROADMAP.md`](../docs/ROADMAP.md).
