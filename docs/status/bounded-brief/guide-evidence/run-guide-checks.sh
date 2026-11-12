#!/bin/sh
# ABOUTME: Retains each exact-source guide and runtime check with complete compressed logs.
# ABOUTME: Stops on failure and records one command event per actual gate invocation.
set -eu
source_commit="$(git rev-parse HEAD)"
evidence_dir=docs/status/bounded-brief/guide-evidence
lint_cache="$(mktemp -d /tmp/pact-guide-final-lint.XXXXXX)"
run_check() {
  check_name="$1"
  shift
  check_log="$evidence_dir/$source_commit-$check_name.log"
  check_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  set +e
  "$@" > "$check_log" 2>&1
  check_exit="$?"
  set -e
  printf '%s\n' "$check_exit" > "$evidence_dir/$source_commit-$check_name.exit"
  jq -nc --arg source "$source_commit" --arg name "$check_name" --arg started "$check_start" --arg ended "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg log "$source_commit-$check_name.log.gz" --argjson exit "$check_exit" --args '{source_commit:$source,name:$name,started:$started,ended:$ended,argv:$ARGS.positional,exit_code:$exit,log:$log}' -- "$@" >> "$evidence_dir/check-events.jsonl"
  gzip -n "$check_log"
  printf '%s: exit %s; %s.gz\n' "$check_name" "$check_exit" "$check_log"
  test "$check_exit" -eq 0
  test "$(git rev-parse HEAD)" = "$source_commit"
  git diff --quiet HEAD -- README.md cmd internal tests docs/pact-ledger/SKILL.md gotchas.md
}
run_check focused env -u GOROOT mise exec -- go test ./cmd/pact ./internal/development ./tests/e2e -run 'TestQuickstart|TestCLIDevelopmentGuideExample|TestCLIDevelopmentRecord|TestCLIRecord|TestCLIDevelopmentEvidence|TestCLIBoundedBriefEncoded|TestCLIBoundedBriefOversized' -count=1 -v
run_check check env -u GOROOT mise exec -- scripts/check
run_check race env -u GOROOT mise exec -- go test -race -count=1 ./...
run_check lint env -u GOROOT "GOLANGCI_LINT_CACHE=$lint_cache" mise exec -- golangci-lint run
