<!-- ABOUTME: Explains how operators build, index, query, and verify the PACT tool. -->
<!-- ABOUTME: Defines external-key, recovery, completeness, cursor, and resource contracts. -->

# PACT

PACT is a local, append-only signed ledger CLI. This Go tool initializes a
store, creates an external Ed25519 identity, trusts that identity, appends
signed event commits, inspects local heads and objects, verifies the full
store, makes official signed checkpoints, and queries a disposable SQLite
index in deterministic causal batches.

The Phase 0 contract, Phase 1 single-replica core, and Phase 2 bounded index and
query surface are implemented. The setup wrapper is deferred; use the explicit
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

## Operator CLI foundation

Running bare `pact` in a terminal plays a brief rotating signed-and-sealed
welcome, then prints top-level help. Redirected bare output prints one static
frame, plain under the default automatic color policy. `pact help` and
`pact --help` skip the animation and remain immediate. `pact help index` shows
the nested index commands, and `pact COMMAND --help` remains seal-free.

Commands that need an existing repository, including `status`, `heads`,
`show`, `verify`, `log`, `query`, and `index`, discover it from the working
directory: PACT walks up through parent directories until it finds `.pact`.
An explicit `--repo PATH` is authoritative. PACT resolves that exact path and
does not search its parents.

`pact status` performs strict verification and checks whether indexed reads
are ready; it never creates or rebuilds an index. A `healthy` status writes its
summary to stdout and exits 0. An `attention` status means canonical state is
valid but the index needs work; it writes the summary and `pact index rebuild`
to stderr, then exits 9. A `broken` status writes its summary to stderr and
uses the matching verification exit code: 4 for integrity or authorization
failure, or 9 when missing dependencies are the only failure. A missing PACT
store is a store error with exit code 3. With `--json`, healthy output is one
JSON result on stdout; a non-healthy result is one JSON error envelope on
stderr.

Human output accepts `--color auto|always|never`. `auto` is the default and
adds color only on a suitable terminal; `NO_COLOR` and `TERM=dumb` disable it.
`always` forces color, `never` disables it, and JSON output never contains ANSI
escapes.

Setup remains deferred: this foundation does not ship `pact setup`. Use the
explicit bootstrap lifecycle below to create a repository, key, and trusted
root. Rebuild the derived index explicitly with `pact index rebuild` after a
canonical write makes it stale.

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

"$pact" index status --repo "$repo" --json
"$pact" index rebuild --repo "$repo" --json
"$pact" log --repo "$repo" --namespace org/example --limit 100 --json
"$pact" query \
  --repo "$repo" \
  --namespace org/example \
  --type build.test.executed \
  --tag ci \
  --limit 100 \
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

## Index and query operations

The shipped command inventory is `init`, `keygen`, `trust-add`, `hash`,
`commit`, `heads`, `show`, `verify`, `checkpoint`, `index status`,
`index rebuild`, `log`, and `query`.

The live SQLite file is `.pact/index/pact-v1.sqlite3`. It is derived,
disposable, and never canonical ledger history. Removing it does not remove an
event, trust root, head, or checkpoint. Read commands never repair it as a side
effect. Inspect the state, then rebuild after the derived filesystem shape is
safe:

```sh
pact index status --repo /path/to/project --json
pact index rebuild --repo /path/to/project --json
```

`missing`, `stale`, `corrupt`, `incompatible`, and `partial-build` all require
operator action. `index rebuild` repairs an absent live file or unusable regular
database when `.pact/index` is a real directory with no SQLite sidecars. If
`.pact/index` is missing or unsafe, recreate it as a real directory. Remove or
repair unsafe live paths and SQLite sidecars before rebuilding. A successful
canonical write makes an existing index stale; this is expected. Rebuild scans
and verifies canonical bytes, creates and validates a same-directory temporary
SQLite file, then replaces the live file. Log and query refuse every state
except `current`. `show` and canonical verification do not need a usable index.

`log` accepts repeated `--namespace` and `--actor` filters. `query` requires at
least one filter and accepts repeated `--namespace`, `--type`, `--kind`,
`--subject`, `--actor`, `--tag`, `--schema-ref`, `--event-ref`, `--caused-by`,
and `--supersedes` filters. Repeated values in one family are OR; different
families are AND. Namespace prefixes match the exact namespace or a
slash-delimited descendant, so `org/example` does not match `org/example2`.
Other filters are exact and case-sensitive after validation and field
normalization. Subject and tag values use Unicode NFC normalization.

```sh
pact log \
  --repo /path/to/project \
  --namespace org/example \
  --namespace org/another-team \
  --limit 100 \
  --json

pact query \
  --repo /path/to/project \
  --type build.test.executed \
  --type build.package.created \
  --tag ci \
  --actor ed25519:sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  --limit 100 \
  --json
```

The query means “either listed type, and tag `ci`, and that actor.” Results use
stored causal batches based on known local dependency edges. `observed_at` is
advisory metadata and never sorts results. A batch is part of a partial order,
not a total sequence; a lower batch number does not prove a path to every later
batch.

Replica status separates local closure from global knowledge. `locally_closed`
means all dependencies named by the local object set are present. It does not
mean no other object exists elsewhere. Missing dependencies yield stable
blockers and local completeness `incomplete`; global completeness always
remains `unknown`. A valid current index built from that incomplete replica has
`coverage=partial`. An unavailable index has `coverage=unavailable`, while
`partial-build` means SQLite diverges from its own declared source and cannot
answer queries. Strict verification and official checkpoints refuse
completeness blockers.

Pagination uses an opaque cursor bound to the command, normalized filters,
page limit, schema, source fingerprint, logical digest, and causal order.
Cursors survive process restart and a deterministic same-source rebuild. A
changed query fails with `cursor_query_mismatch`. When canonical source changes,
the old index first fails as `index_stale`; after rebuilding against the changed
source, an old cursor fails with `cursor_stale`. A page may split a causal batch,
in which case each affected page reports `complete_in_page=false`. Unresolved
events remain a separate transport group and do not claim to occur later.

## Phase 2 limits

The fixed profile is `pact/resource-limits/phase2-v1`.

| Resource | Maximum |
|---|---:|
| One canonical object | 4,194,304 bytes |
| Canonical objects per scan | 100,000 |
| Canonical bytes per scan | 1,073,741,824 bytes |
| Events per commit | 1,024 |
| Events per scan | 250,000 |
| Parents per commit | 64 |
| Longest known causal path | 4,096 signed dependency edges |
| Graph frontier | 4,096 nodes |
| Total graph edges | 1,000,000 |
| Query results per page | default 100; maximum 1,000 |
| Values in one filter family | 64 |
| Values across filter families | 256 |
| Encoded cursor | 4,096 bytes |
| Decoded cursor | 3,072 bytes |
| Encoded JSON result, including the `--json` newline | 16,777,216 bytes |
| Stored SQLite file | 2,147,483,648 bytes |
| Diagnostic samples per axis | 100 |
| One diagnostic text field | 512 UTF-8 bytes |

Exact-bound inputs pass. With `--json`, the first excess fails safely with a
bounded, machine-readable diagnostic instead of truncating authoritative
results. Log and query use one streaming serializer for both the bound and CLI
output, so this limit does not require a full-page encoding buffer. The code
names the input contract: graph, scan, and filter excesses use
`resource_limit`; an invalid page limit is a usage error; an oversized or
malformed cursor is `cursor_invalid`; and an oversized stored index is
`corrupt`.

Every query hashes the bounded canonical object set and verifies selected
canonical commits. Rebuild scans all bounded canonical bytes and holds the
exclusive store lock through publication, so large repositories make queries
costly and pause writers during rebuild. This simple fixed-snapshot tradeoff is
intentional for the local Phase 2 scope.

Phase 3 payload and schema meaning, setup automation, network sync, policy
execution, and delegated authority remain out of scope. PACT also has no
trusted timestamps, hardware key service, or global completeness claim.

## Repository gate

Run the full Go gate, compiled scratch index lifecycle, and bundled Python
reference suite with:

```sh
env -u GOROOT mise exec -- ./scripts/check
```
