#!/bin/sh
# ABOUTME: Executes the authored guide and README commands with real temporary runtime bindings.
# ABOUTME: Uses public Git input, an external disposable signer, and a compiled CLI without exporting keys.
set -eu
for git_variable in $(env | sed -n 's/^\(GIT_[A-Za-z0-9_]*\)=.*/\1/p'); do
  unset "$git_variable"
done
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM
repo="$work_dir/project"
pact="$work_dir/pact"
key_file="$work_dir/key.json"
export repo pact key_file work_dir
mkdir "$repo"
env -u GOROOT mise exec -- go build -o "$pact" ./cmd/pact
git -C "$repo" init --quiet --object-format=sha1
git -C "$repo" fast-import --quiet < tests/fixtures/development/source.fast-import
git -C "$repo" symbolic-ref HEAD refs/heads/main
"$pact" setup --repo "$repo" --namespace org/example/development --actor 'Guide usage' --key-file "$key_file" --json
awk '/^<!-- development-record-example -->$/ {section=1; next} section && /^```sh$/ {code=1; next} code && /^```$/ {exit} code {print}' docs/pact-ledger/SKILL.md > "$work_dir/guide.sh"
test -s "$work_dir/guide.sh"
sh -eu "$work_dir/guide.sh"
object_id="$(jq -r .object_id "$work_dir/record-result.json")"
event_ref="$(jq -r '.event_refs[0]' "$work_dir/record-result.json")"
awk '/^## Development recording and handoffs$/ {section=1; next} section && /^```sh$/ {code=1; next} code && /^```$/ {exit} code {print}' README.md |
  sed -e "s|/path/to/project|$repo|g" -e "s|/path/to/record.json|$work_dir/record.json|g" \
    -e "s|/path/outside/project/operator.key.json|$key_file|g" \
    -e "s|EXACT_OBJECT_OR_EVENT_REF|$object_id|g" -e "s|EXACT_EVENT_REF|$event_ref|g" \
    -e "s|^pact |\"$pact\" |" > "$work_dir/readme.sh"
test -s "$work_dir/readme.sh"
sh -eu "$work_dir/readme.sh"
"$pact" quickstart > "$work_dir/quickstart.md"
cmp docs/pact-ledger/SKILL.md "$work_dir/quickstart.md"
