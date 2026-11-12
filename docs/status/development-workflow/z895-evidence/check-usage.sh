#!/bin/sh
# ABOUTME: Builds the recorded source and exercises the shipped development command help.
# ABOUTME: Captures the actual tool versions and public read/write command usage.
set -eu
go version
golangci-lint version
git version
go build -o .scratch/development-lifecycle/pact ./cmd/pact
.scratch/development-lifecycle/pact help
.scratch/development-lifecycle/pact help record
.scratch/development-lifecycle/pact help evidence check
.scratch/development-lifecycle/pact help query
.scratch/development-lifecycle/pact help show
