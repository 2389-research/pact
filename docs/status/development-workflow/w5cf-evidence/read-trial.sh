#!/bin/sh
# ABOUTME: Replays either development read path against the same retained public fixtures.
# ABOUTME: Batches dependent reads without hiding canonical command stdout or errors.
set -eu
if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  echo 'usage: read-trial.sh baseline|prototype FIXTURE_DIRECTORY [open|complete|conflict]' >&2
  exit 2
fi
mode="$1"
fixture_dir="$2"
case "$mode" in baseline|prototype) ;; *) echo 'expected baseline or prototype' >&2; exit 2 ;; esac
scenarios='open complete conflict'
if [ "$#" = 3 ]; then
  case "$3" in open|complete|conflict) scenarios="$3" ;; *) echo 'unknown public scenario' >&2; exit 2 ;; esac
fi
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
for scenario in $scenarios; do
  manifest="$fixture_dir/$scenario-trial.json"
  binary="$(jq -r .binary "$manifest")"
  repo="$(jq -r .repo "$manifest")"
  if [ "$mode" = prototype ]; then
    prototype="$(jq -r .prototype "$manifest")"
    "$prototype" "$repo" "$(jq -c .request "$manifest")"
    continue
  fi
  "$binary" query --repo "$repo" --namespace org/example/development --subject widget/task-1 --json > "$scratch/query.json"
  cat "$scratch/query.json"
  if [ "$(jq -r .page.has_more "$scratch/query.json")" != false ]; then
    echo 'fixture exceeds one query page; follow next_cursor before interpreting facts' >&2
    exit 2
  fi
  "$binary" verify --repo "$repo" --strict --json
  jq -r '[.batches[].items[].commit_id] | unique[]' "$scratch/query.json" > "$scratch/objects"
  while IFS= read -r object; do
    "$binary" show --repo "$repo" "$object" --json > "$scratch/show.json"
    cat "$scratch/show.json"
    jq -r --arg object "$object" '.object.body.events[] | select((.payload.evidence_indexes | length) > 0) | "pact:event:" + $object + "#" + .local_id' "$scratch/show.json" > "$scratch/refs"
    while IFS= read -r ref; do
      "$binary" evidence check --repo "$repo" --event-ref "$ref" --json
    done < "$scratch/refs"
  done < "$scratch/objects"
done
