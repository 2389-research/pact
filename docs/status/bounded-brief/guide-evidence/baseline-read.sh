#!/bin/sh
# ABOUTME: Captures the public query, object, verification, and selected-evidence read recipe.
# ABOUTME: Records raw streams and argv without interpreting acceptance or changing a store.
set -eu
if [ "$#" -ne 4 ]; then
  printf '%s\n' 'usage: baseline-read.sh BINARY REPO REVISION OUTPUT_DIR' >&2
  exit 2
fi
binary="$1" repo="$2" revision="$3" output_dir="$4"
mkdir -p "$output_dir"
capture() {
  capture_name="$1"
  shift
  jq -nc --arg name "$capture_name" --args '{name:$name,argv:$ARGS.positional}' -- "$@" >> "$output_dir/commands.jsonl"
  set +e
  "$@" > "$output_dir/$capture_name.stdout" 2> "$output_dir/$capture_name.stderr"
  capture_exit="$?"
  set -e
  printf '%s\n' "$capture_exit" > "$output_dir/$capture_name.exit"
  cat "$output_dir/$capture_name.stdout"
  cat "$output_dir/$capture_name.stderr" >&2
  test "$capture_exit" -eq 0
}
capture query "$binary" query --repo "$repo" --namespace org/example/development --subject widget/task-1 --limit 100 --json
if jq -e '.page.has_more' "$output_dir/query.stdout" >/dev/null; then
  printf '%s\n' 'Unread pages remain; this bounded exercise recipe stopped.' >&2
  exit 2
fi
capture verify "$binary" verify --repo "$repo" --strict --json
jq -r '[.batches[].items[].commit_id] | unique[]' "$output_dir/query.stdout" > "$output_dir/objects.txt"
while IFS= read -r object; do
  capture "show-${object#sha256:}" "$binary" show --repo "$repo" "$object" --json
  jq -r --arg object "$object" '.object.body.events[] | select((.payload.evidence_indexes | length) > 0 or any(.payload.findings[]?; (.evidence_indexes | length) > 0) or any(.payload.resolves[]?; (.evidence_indexes | length) > 0)) | "pact:event:" + $object + "#" + .local_id' "$output_dir/show-${object#sha256:}.stdout" >> "$output_dir/evidence-refs.txt"
done < "$output_dir/objects.txt"
while IFS= read -r event_ref; do
  capture "evidence-$(printf '%s' "$event_ref" | tr ':#' '__')" "$binary" evidence check --repo "$repo" --event-ref "$event_ref" --json
done < "$output_dir/evidence-refs.txt"
