#!/bin/sh
# ABOUTME: Retains an exact-source verification command, exit status, and lossless output log.
# ABOUTME: Keeps successful and failed checks without allowing log hooks to alter raw streams.
set -eu
if [ "$#" != 2 ]; then
  echo 'usage: verify-source.sh focused|usage|check|race|lint SOURCE_ID' >&2
  exit 2
fi
check="$1"
source_id="$2"
directory=docs/status/development-workflow/k7cc-evidence
base="$directory/$source_id-$check"
case "$check" in
  focused) command="env -u GOROOT mise exec -- go test ./cmd/pact ./tests/e2e -run 'TestQuickstart|TestCLIDevelopmentGuideExample' -count=1 -v" ;;
  usage) command='sh docs/status/development-workflow/k7cc-evidence/check-usage.sh' ;;
  check) command='env -u GOROOT mise exec -- scripts/check' ;;
  race) command='env -u GOROOT mise exec -- go test -race -count=1 ./...' ;;
  lint) command="env -u GOROOT GOLANGCI_LINT_CACHE=/tmp/pact-k7cc-lint-$source_id mise exec -- golangci-lint run" ;;
  *) echo 'unknown verification check' >&2; exit 2 ;;
esac
printf '%s\n' "$command" > "$base.command"
status=0
sh -c "$command" > "$base.log" 2>&1 || status=$?
printf '%s\n' "$status" > "$base.exit"
gzip "$base.log"
printf '%s: exit %s\n' "$check" "$status"
exit "$status"
