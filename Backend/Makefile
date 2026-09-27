# SideQuestz backend. Go 1.26; run from Backend/.
GO ?= go
BIN := bin

.PHONY: test test-db build build-native build-admin smoke fmt vet clean

## test: unit tests; Mongo-backed tests skip without a server (MONGO_TEST_URI).
test:
	$(GO) test ./...

## test-db: every test against the local sq-mongo container, race detector on, skips forbidden.
test-db:
	scripts/test-db.sh

## build: static Linux/amd64 server.
build:
	./build.sh

## build-native: server for this machine.
build-native:
	./build.sh --native

## build-admin: optional maintenance CLI alongside the server.
build-admin:
	./build.sh --native --admin

## smoke: curl + jq against BASE_URL (default http://127.0.0.1:8080).
smoke:
	scripts/smoke.sh

fmt:
	gofmt -l -w .

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN)
