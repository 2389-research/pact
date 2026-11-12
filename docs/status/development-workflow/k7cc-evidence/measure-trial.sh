#!/bin/sh
# ABOUTME: Measures preserved public read captures without rerunning either adoption trial.
# ABOUTME: Counts exact CLI output bytes, newline bytes, failed exits, and top-level CLI invocations.
set -eu
if [ "$#" != 2 ]; then
  echo 'usage: measure-trial.sh CAPTURE_ROOT OUTPUT_FILE' >&2
  exit 2
fi
capture_root="$1"
output="$2"
rows="$(mktemp)"
trap 'rm -f "$rows"' EXIT HUP INT TERM
for reader in baseline candidate; do
  for scenario in open complete conflict; do
    capture="$capture_root/$reader-$scenario"
    calls="$(cat "$capture/cli-calls")"
    stdout_bytes=0
    stderr_bytes=0
    failures=0
    number=1
    while [ "$number" -le "$calls" ]; do
      stdout_bytes=$((stdout_bytes + $(wc -c < "$capture/$number.stdout")))
      stderr_bytes=$((stderr_bytes + $(wc -c < "$capture/$number.stderr")))
      if [ "$(cat "$capture/$number.exit")" != 0 ]; then failures=$((failures + 1)); fi
      number=$((number + 1))
    done
    jq -nc --arg reader "$reader" --arg scenario "$scenario" \
      --argjson cli_calls "$calls" --argjson stdout_bytes "$stdout_bytes" \
      --argjson stderr_bytes "$stderr_bytes" --argjson failures "$failures" \
      '{reader:$reader,scenario:$scenario,cli_calls:$cli_calls,stdout_bytes:$stdout_bytes,stderr_bytes:$stderr_bytes,failures:$failures}' >> "$rows"
  done
done
jq -s . "$rows" > "$output"
