# PACT CLI Contract

The CLI is the stable agent-facing interface. Human output may evolve, but
machine-readable JSON fields and exit semantics should remain compatible within
a major format version.

Examples use:

```bash
pact="python3 <skill-directory>/scripts/pact.py"
```

## Global behavior

- `--repo PATH` points to the project root containing `.pact/`.
- Commands that mutate the ledger use atomic writes.
- Commands never print private key material.
- `--json` emits one JSON result object to stdout.
- Diagnostics go to stderr in human mode.
- No command edits an existing canonical object.
- Exit code `0` means the requested operation completed at its stated layer.

Recommended exit codes:

| Code | Meaning |
|---:|---|
| 0 | success |
| 2 | usage or input-validation error |
| 3 | store not initialized or unsupported format |
| 4 | integrity/signature/DAG verification failure |
| 5 | authorization failure or indeterminate authority when strict authority was required |
| 6 | schema/policy resolution failure |
| 7 | secret/sensitive-data safety refusal |
| 8 | sync/admission failure |
| 9 | incomplete operation or missing dependency |
| 10 | unexpected internal error |

The reference CLI currently uses a smaller compatible subset; a production CLI
should converge on this table.

## `init`

```bash
$pact init --repo PATH --namespace NAMESPACE [--json]
```

Creates `.pact/` only when absent. Refuses to overwrite an existing store.

Output:

```json
{
  "operation": "init",
  "store": "/path/.pact",
  "default_namespace": "org/example/project/widget",
  "format": "pact/store/v1"
}
```

## `keygen`

```bash
$pact keygen --actor ACTOR_LABEL --out KEY_FILE [--json]
```

Creates an Ed25519 key file with owner-only permissions. Refuses to overwrite.

Output includes actor label, key ID, public key, and path. It never includes the
private key.

## `trust-add`

```bash
$pact trust-add --repo PATH --key-file KEY_FILE [--json]
```

Adds the key's public identity to local root trust configuration. This is an
out-of-band bootstrap action, not a ledger commit.

Idempotent when the same key already exists. Hard-fails when the same key ID is
associated with different public bytes.

## `hash`

```bash
$pact hash FILE [--json]
```

Hashes exact file bytes for use in an evidence reference.

Output:

```json
{
  "path": "/path/file",
  "digest": "sha256:...",
  "size": 1234
}
```

## `commit`

```bash
$pact commit \
  --repo PATH \
  --key-file KEY_FILE \
  --events EVENT_BATCH.json \
  [--namespace NAMESPACE] \
  [--parent sha256:...]... \
  [--delegation-ref pact:event:...] \
  [--epoch EPOCH] \
  [--lease-ref pact:event:...] \
  [--correlation-id ID] \
  [--json]
```

Defaults:

- namespace from event batch, CLI, or store default in that precedence;
- parents to all locally computed heads in the exact namespace;
- observed time to current UTC, labeled advisory;
- actor label/key ID from key file.

Input event batch:

```json
{
  "namespace": "optional override",
  "observed_at": "optional advisory timestamp",
  "metadata": {},
  "events": []
}
```

Output:

```json
{
  "operation": "commit",
  "object_id": "sha256:...",
  "namespace": "...",
  "parents": [],
  "event_refs": ["pact:event:sha256:...#e1"],
  "integrity": "valid",
  "authenticity": "valid",
  "authorization": "authorized|unauthorized|indeterminate",
  "authorization_reasons": []
}
```

A production CLI may provide `--require-authorized` to refuse indeterminate or
unauthorized writes. Historical-admission policy must be explicit.

## `verify`

```bash
$pact verify --repo PATH [--strict] [--json]
```

Checks every canonical object, signatures, parent graph, references, and
available authority information.

`--strict` treats missing semantic refs and indeterminate structural conditions
as failures. It does not turn unqueried evidence availability into a failure.

Output includes separate counts/results for:

- objects;
- commits;
- checkpoints;
- integrity failures;
- authenticity failures;
- DAG failures;
- reference warnings/failures;
- authorization statuses;
- index status.

## `reindex`

```bash
$pact reindex --repo PATH [--json]
```

Deletes only the disposable SQLite index and recreates it from canonical objects.
It never changes objects or local trust roots.

## `heads`

```bash
$pact heads --repo PATH [--namespace PREFIX] [--json]
```

Reports current local heads grouped by exact namespace. When a prefix is used,
returns all matching descendant namespaces.

## `log`

```bash
$pact log \
  --repo PATH \
  [--namespace PREFIX] \
  [--type EVENT_TYPE] \
  [--subject SUBJECT] \
  [--actor KEY_ID] \
  [--limit N] \
  [--json]
```

Human ordering may use advisory time plus object ID for readability. Output must
state that this is not causal ordering. Machine output includes parents and
stable event refs.

## `show`

```bash
$pact show --repo PATH OBJECT_ID [--json]
$pact show --repo PATH EVENT_REF [--json]
```

Returns exact parsed object or event plus verification status. It does not
resolve external evidence unless a future explicit flag requests that action.

## `checkpoint`

```bash
$pact checkpoint \
  --repo PATH \
  --key-file KEY_FILE \
  --scope NAMESPACE_PREFIX \
  --policy-ref sha256:... \
  --authority-epoch EPOCH \
  [--schema-ref sha256:...]... \
  [--previous sha256:...] \
  [--purpose TEXT] \
  [--json]
```

Computes all local heads in selected namespaces, verifies the reachable store,
and signs a checkpoint.

A production command should support `--require-authorized` and refuse checkpoint
creation when the signer lacks checkpoint authority.

## `sync-dir`

```bash
$pact sync-dir --repo PATH --from OTHER_PROJECT [--json]
```

Reads `OTHER_PROJECT/.pact/objects`, verifies candidate bytes, copies only
missing valid objects, and rebuilds the local index.

Output:

```json
{
  "operation": "sync-dir",
  "source": "/other/.pact",
  "examined": 42,
  "imported": 7,
  "already_present": 35,
  "rejected": 0,
  "heads_after": {}
}
```

No existing object is overwritten. Invalid candidates cause failure and are not
admitted.

## Future production commands

A production implementation may add:

```text
pact delegate
pact revoke
pact epoch advance
pact schema register
pact policy register
pact policy activate
pact project
pact evidence check
pact bundle export/import
pact sync <adapter-url>
pact trust rotate
```

These must emit or consume the same underlying semantic objects rather than
introducing mutable side databases as authority.

## Machine-readable stability

Within `pact/store/v1`:

- object IDs and event refs are stable;
- field removal is breaking;
- new optional result fields are allowed;
- enum expansion must be tolerated only where the schema says so;
- unsupported object major versions fail closed;
- human prose is non-normative;
- scripts should use `--json` rather than parse human tables.
