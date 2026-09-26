// Package e2e is the end-to-end suite: it drives a running SideQuests API
// and its websocket over the network exactly as the app does, and decodes
// every response strictly into the pkg/contract types (unknown fields are a
// failure, every enum must be one the app knows, times are UTC).
//
// The tests live behind the e2e build tag, so `go test ./...` never runs
// them. They need a server that has run `sidequestz-admin seed-demo`:
//
//	cd Backend && E2E_BASE_URL=http://127.0.0.1:18080 E2E_DEMO_PASSWORD=… \
//	  go test -tags e2e -count=1 -timeout 20m -v ./e2e/
//
// Settings:
//
//	E2E_BASE_URL       the API root (required; without it every test skips)
//	E2E_DEMO_PASSWORD  Sandy Byte's password, the server's DEMO_PASSWORD (the demo tests skip without it)
//	E2E_DEMO_EMAIL     the demo account (default demo@sidequestz.tech)
//	E2E_WS_URL         the websocket (default: E2E_BASE_URL with ws(s):// and /ws)
//	E2E_TIME_ZONE      the X-Time-Zone every request sends (default America/New_York)
//
// What a run changes: it signs up three accounts (e2e.<time>.<n>@example.test,
// kept afterwards: there is no account deletion), and as the demo account it
// rates the Heron Creek stop, posts and deletes a free-now post, joins and
// leaves Marin's plan, sends "Plan together" to Marin once, sends messages to
// the crew and to Marin, and plans, saves and deletes a sidequest. Expenses,
// settlements, cards, statuses and the join it makes are undone before it
// ends. Run `sidequestz-admin seed-demo` afterwards to restore the rating.
// Password-reset steps need DEV_RESET_CODES=1 on the server and skip without it.
package e2e
