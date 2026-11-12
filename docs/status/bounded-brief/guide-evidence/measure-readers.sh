#!/bin/sh
# ABOUTME: Counts retained reader input and raw public command output without semantic grading.
# ABOUTME: Keeps context bytes, CLI calls, stream bytes, and failures separate.
set -eu
if [ "$#" -ne 2 ]; then
  printf '%s\n' 'usage: measure-readers.sh FIXTURE_DIR CANDIDATE_GUIDE' >&2
  exit 2
fi
fixture_dir="$1" candidate_guide="$2"
goals_bytes="$(wc -c < "$fixture_dir/goals.json" | tr -d ' ')"
rubric_bytes="$(wc -c < "$fixture_dir/fact-rubric.md" | tr -d ' ')"
for reader in baseline candidate; do
  guide="$candidate_guide"
  if [ "$reader" = baseline ]; then guide="$fixture_dir/baseline-SKILL.md"; fi
  guide_bytes="$(wc -c < "$guide" | tr -d ' ')"
  recipe_bytes="$(wc -c < "$fixture_dir/$reader-read.sh" | tr -d ' ')"
  context_bytes="$((guide_bytes + goals_bytes + rubric_bytes + recipe_bytes))"
  for scenario in open complete conflict; do
    capture_dir="$fixture_dir/$reader-$scenario"
    calls="$(jq -s length "$capture_dir/commands.jsonl")"
    stdout_bytes=0 stderr_bytes=0 failures=0
    for stream in "$capture_dir/"*.stdout; do
      size="$(wc -c < "$stream" | tr -d ' ')"
      stdout_bytes="$((stdout_bytes + size))"
    done
    for stream in "$capture_dir/"*.stderr; do
      size="$(wc -c < "$stream" | tr -d ' ')"
      stderr_bytes="$((stderr_bytes + size))"
    done
    for exit_file in "$capture_dir/"*.exit; do
      if [ "$(cat "$exit_file")" -ne 0 ]; then failures="$((failures + 1))"; fi
    done
    jq -nc --arg reader "$reader" --arg scenario "$scenario" --argjson guide "$guide_bytes" --argjson context "$context_bytes" --argjson calls "$calls" --argjson stdout "$stdout_bytes" --argjson stderr "$stderr_bytes" --argjson failures "$failures" '{reader:$reader,scenario:$scenario,guide_input_bytes:$guide,context_stdout_bytes:$context,top_level_cli_invocations:$calls,stdout_bytes:$stdout,stderr_bytes:$stderr,cli_failures:$failures}'
  done
done
