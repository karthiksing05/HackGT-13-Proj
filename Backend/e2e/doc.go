// Package e2e is the end-to-end suite: it drives a running SideQuests API
// and its websocket over the network exactly as the app does, and decodes
// every response strictly into the pkg/contract types (unknown fields are a
// failure, every enum must be one the app knows, times are UTC).
//
// The tests live behind the e2e build tag, so `go test ./...` never runs
// them. The demo cases need a server with the existing demo fixture records:
//
//	cd Backend && E2E_BASE_URL=http://127.0.0.1:18080 E2E_DEMO_PASSWORD=… \
//	  go test -tags e2e -count=1 -timeout 20m -v ./e2e/
//
// Settings:
//
//	E2E_BASE_URL       the API root (required; without it every test skips)
//	E2E_DEMO_PASSWORD  Sandy Byte's existing account password (the demo tests skip without it)
//	E2E_DEMO_EMAIL     the demo account (default demo@sidequestz.tech)
//	E2E_WS_URL         the websocket (default: E2E_BASE_URL with ws(s):// and /ws)
//	E2E_TIME_ZONE      the X-Time-Zone every request sends (default America/New_York)
//	E2E_DEMO_DATE      the server's DEMO_DATE, when it runs the demo on a fixed date: adds the demo-clock checks
//	                   (the suite reads the demo account's date from /me either way)
//
// What a run changes: it signs up three accounts (e2e.<time>.<n>@example.test,
// named Casey, Alice and Bob; kept afterwards, since there is no account
// deletion), and as the demo account it rates the Heron Creek stop, posts and
// deletes a free-now post, joins and leaves Marin's plan (its chat keeps her
// message), sends "Plan together" to Marin once, posts messages in the crew
// chat, and plans, saves, books and deletes a sidequest. Expenses,
// settlements, photos, preferences and the join are undone before it ends.
// Restore the fixture database afterwards if the original rating is needed.
// Password-reset steps need DEV_RESET_CODES=1 on the server and skip without it.
package e2e
