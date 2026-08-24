<!-- ABOUTME: Explains how operators build, use, and verify the PACT MVP tool. -->
<!-- ABOUTME: Defines the external-key rule, official checkpoint gate, and MVP limits. -->

# PACT

PACT is a local, append-only signed ledger CLI. This Go MVP can initialize a
store, create an external Ed25519 identity, trust that identity, append signed
event commits, inspect local heads and objects, verify the full store, and make
official signed checkpoints.

The Phase 0 contract and Phase 1 single-replica core are verified and now
dogfood this repository. The setup wrapper is deferred; use the explicit
bootstrap commands below. See the
[Phase 0 and Phase 1 status](docs/status/phase-0-1-dogfood.md) for exact gate,
ledger, checkpoint, recovery, and setup-review evidence.

## Install

Install `pact` at the exact destination `$HOME/.local/bin/pact`:

```sh
mkdir -p "$HOME/.local/bin"
env -u GOROOT mise exec -- env GOBIN="$HOME/.local/bin" go install ./cmd/pact
```

Add `$HOME/.local/bin` to `PATH` if it is not already present.

## Build and run an operator lifecycle

The commands below run from this repository and need `mise`, `go`, and `jq`.
They build the binary outside the project ledger, keep the private key outside
it, admit two commits, inspect an event and the current head, create a
checkpoint, and run strict verification.

```sh
work_dir="$(mktemp -d)"
repo="$work_dir/project"
key_dir="$(mktemp -d)"
pact="$key_dir/pact"
key_file="$key_dir/operator.key.json"

mkdir -p "$repo"
env -u GOROOT mise exec -- go build -o "$pact" ./cmd/pact

"$pact" init \
  --repo "$repo" \
  --namespace org/example/widget \
  --json

"$pact" keygen \
  --actor operator/alice \
  --out "$key_file" \
  --json

"$pact" trust-add \
  --repo "$repo" \
  --key-file "$key_file" \
  --json

cat >"$work_dir/first.json" <<'JSON'
{"events":[{"local_id":"e1","kind":"observation","type":"build.test.executed","subject":"build/linux/1","schema_ref":"pact:core/generic-object/v1","payload":{"result":"pass"},"evidence":[],"caused_by":[],"supersedes":[],"tags":["ci"]}]}
JSON

first_commit="$("$pact" commit \
  --repo "$repo" \
  --key-file "$key_file" \
  --events "$work_dir/first.json" \
  --json)"
first_id="$(printf '%s' "$first_commit" | jq -r .object_id)"
first_event="$(printf '%s' "$first_commit" | jq -r '.event_refs[0]')"

cat >"$work_dir/second.json" <<'JSON'
{"events":[{"local_id":"e2","kind":"observation","type":"build.package.created","subject":"build/linux/1","schema_ref":"pact:core/generic-object/v1","payload":{"artifact":"widget.tar"},"evidence":[],"caused_by":[],"supersedes":[],"tags":["ci"]}]}
JSON

"$pact" commit \
  --repo "$repo" \
  --key-file "$key_file" \
  --events "$work_dir/second.json" \
  --json

"$pact" show --repo "$repo" "$first_id" --json
"$pact" show --repo "$repo" "$first_event" --json
"$pact" heads --repo "$repo" --namespace org/example --json
"$pact" verify --repo "$repo" --strict --json

checkpoint="$("$pact" checkpoint \
  --repo "$repo" \
  --key-file "$key_file" \
  --scope org/example \
  --policy-ref sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --authority-epoch epoch-1 \
  --schema-ref sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  --purpose 'operator lifecycle' \
  --json)"
checkpoint_id="$(printf '%s' "$checkpoint" | jq -r .object_id)"

"$pact" show --repo "$repo" "$checkpoint_id" --json
"$pact" verify --repo "$repo" --strict --json
```

Private keys must stay outside every initialized project root. `pact keygen`
refuses an output path inside such a root. The project stores public identity
and signatures only. Keep external key files owner-readable only and back them
up through the operator's normal secret-storage process.

Official checkpoint creation has a stricter production rule than the bundled
Python reference: the signing key must already be configured as a local trusted
root with `trust-add`. Unknown signers cannot create a checkpoint, even when
their signature would be authentic. PACT performs this authorization check and
a full strict store verification before it persists checkpoint bytes.

## MVP limits

This cut supports `init`, `keygen`, `trust-add`, `hash`, `commit`, `heads`,
`show`, `verify`, and `checkpoint`. It uses one local filesystem store and
computes heads from canonical object bytes. It has no SQLite reindex command,
network or directory sync, delegated checkpoint authority, policy execution,
trusted timestamps, hardware key service, or global completeness claim.

## Repository gate

Run the full Go gate and all 17 bundled Python reference tests with:

```sh
env -u GOROOT mise exec -- ./scripts/check
```
