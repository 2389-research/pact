#!/bin/sh
# ABOUTME: Retains source-tagged checks for the bounded brief command implementation.
# ABOUTME: Stops on a failed gate and records each exact argv, log, and exit separately.
set -eu

source_commit="$(git rev-parse HEAD)"
evidence_dir="docs/status/bounded-brief/evidence"
native_dir="/tmp/pact-bounded-brief-command-native-$source_commit"
lint_cache="$(mktemp -d /tmp/pact-bounded-brief-command-final-lint.XXXXXX)"

run_check() {
	check_name="$1"
	shift
	check_log="$evidence_dir/$source_commit-$check_name.log"
	check_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	set +e
	"$@" >"$check_log" 2>&1
	check_exit="$?"
	set -e
	printf '%s\n' "$check_exit" >"$evidence_dir/$source_commit-$check_name.exit"
	jq -nc --arg source "$source_commit" --arg name "$check_name" --arg started "$check_start" --arg ended "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg cwd "$PWD" --arg log "$source_commit-$check_name.log.gz" --argjson exit "$check_exit" --args '{source_commit:$source,name:$name,started:$started,ended:$ended,cwd:$cwd,argv:$ARGS.positional,exit_code:$exit,log:$log}' -- "$@" >>"$evidence_dir/check-events.jsonl"
	gzip -n "$check_log"
	printf '%s: exit %s; %s.gz\n' "$check_name" "$check_exit" "$check_log"
	if [ "$check_exit" -ne 0 ]; then
		exit "$check_exit"
	fi
	test "$(git rev-parse HEAD)" = "$source_commit"
	git diff --quiet HEAD -- cmd internal tests scenarios.jsonl
}

run_check focused env -u GOROOT "PACT_BOUNDED_BRIEF_DIR=$native_dir" mise exec -- go test ./cmd/pact ./internal/development ./tests/e2e -count=1 -v
run_check check env -u GOROOT mise exec -- scripts/check
run_check race env -u GOROOT mise exec -- go test -race -count=1 ./...
run_check lint env -u GOROOT "GOLANGCI_LINT_CACHE=$lint_cache" mise exec -- golangci-lint run
printf '%s\n' "$native_dir" >"$evidence_dir/native-directory.txt"
