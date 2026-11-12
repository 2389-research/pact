#!/bin/sh
# ABOUTME: Captures the native bounded brief read recipe against an exact source.
# ABOUTME: Records raw streams and argv without interpreting acceptance or changing a store.
set -eu
if [ "$#" -ne 4 ]; then
  printf '%s\n' 'usage: candidate-read.sh BINARY REPO REVISION OUTPUT_DIR' >&2
  exit 2
fi
binary="$1" repo="$2" revision="$3" output_dir="$4"
mkdir -p "$output_dir"
jq -nc --args '{name:"brief",argv:$ARGS.positional}' -- "$binary" brief --repo "$repo" --profile development/v1 --namespace org/example/development --subject widget/task-1 --source-revision "$revision" --json > "$output_dir/commands.jsonl"
set +e
"$binary" brief --repo "$repo" --profile development/v1 --namespace org/example/development --subject widget/task-1 --source-revision "$revision" --json > "$output_dir/brief.stdout" 2> "$output_dir/brief.stderr"
capture_exit="$?"
set -e
printf '%s\n' "$capture_exit" > "$output_dir/brief.exit"
cat "$output_dir/brief.stdout"
cat "$output_dir/brief.stderr" >&2
exit "$capture_exit"
