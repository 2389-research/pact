# ABOUTME: Developer entry points — build, test, coverage, install, cleanup.
# ABOUTME: `make check` runs the canonical scripts/check gate.
.PHONY: build check test test-race test-coverage install clean

build:
	go build -o bin/pact ./cmd/pact

check:
	./scripts/check

test:
	go test ./...

test-race:
	go test -race ./...

test-coverage:
	mkdir -p coverage
	go test -coverprofile=coverage/coverage.out -covermode=atomic ./...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html

install:
	go install ./cmd/pact

clean:
	rm -rf bin coverage
	go clean

.DEFAULT_GOAL := build
