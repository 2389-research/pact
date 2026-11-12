#!/bin/sh
# ABOUTME: Runs the real signed-store bounded brief scenarios during local development.
# ABOUTME: Uses the committed public corpus and compiled CLI without mock dependencies.
set -eu
exec env -u GOROOT mise exec -- go test ./cmd/pact ./tests/e2e -run 'TestBrief|TestCLIBoundedBrief' -count=1 -v
