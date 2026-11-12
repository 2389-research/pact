#!/bin/sh
# ABOUTME: Captures one bounded adoption scenario using only shipped public read commands.
# ABOUTME: Retains exact CLI streams and exit codes while forwarding all raw output to the reader.
set -eu
if [ "$#" != 3 ]; then
  echo 'usage: read-adoption-trial.sh FIXTURE_DIRECTORY open|complete|conflict CAPTURE_DIRECTORY' >&2
  exit 2
fi
fixture_dir="$1"
scenario="$2"
capture="$3"
case "$scenario" in open|complete|conflict) ;; *) echo 'unknown public scenario' >&2; exit 2 ;; esac
mkdir "$capture"
manifest="$fixture_dir/$scenario-trial.json"
binary="$(jq -r .binary "$manifest")"
repo="$(jq -r .repo "$manifest")"
sequence=0
run() {
  sequence=$((sequence + 1))
  printf '%s\n' "$@" > "$capture/$sequence.args"
  status=0
  "$@" > "$capture/$sequence.stdout" 2> "$capture/$sequence.stderr" || status=$?
  printf '%s\n' "$status" > "$capture/$sequence.exit"
  cat "$capture/$sequence.stdout"
  cat "$capture/$sequence.stderr" >&2
  if [ "$status" != 0 ]; then exit "$status"; fi
}
run "$binary" query --repo "$repo" --namespace org/example/development --subject widget/task-1 --limit 100 --json
query="$capture/1.stdout"
if [ "$(jq -r .page.has_more "$query")" != false ]; then
  echo 'fixture exceeded one page; follow next_cursor before interpreting facts' >&2
  exit 2
fi
run "$binary" verify --repo "$repo" --strict --json
jq -r '[.batches[].items[].commit_id] | unique[]' "$query" > "$capture/objects"
while IFS= read -r object; do
  run "$binary" show --repo "$repo" "$object" --json
  shown="$capture/$sequence.stdout"
  jq -r --arg object "$object" '.object.body.events[] | select((.payload.evidence_indexes | length) > 0) | "pact:event:" + $object + "#" + .local_id' "$shown" > "$capture/refs"
  while IFS= read -r ref; do
    run "$binary" evidence check --repo "$repo" --event-ref "$ref" --json
  done < "$capture/refs"
done < "$capture/objects"
printf '%s\n' "$sequence" > "$capture/cli-calls"
