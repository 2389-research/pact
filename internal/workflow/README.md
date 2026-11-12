<!-- ABOUTME: Defines reliable development recording and the signed operation protocol. -->
<!-- ABOUTME: Documents retry recovery, layered outcomes, and local concurrency limits. -->

# Development recording

`pact record --repo PATH --profile development/v1 --input FILE --key-file FILE --json`
records one `development.RecordInput` object. It never runs reported commands,
creates keys, or installs trust. Arrays receive the expected-object diagnostic.
The input file is bounded by the existing 4 MiB canonical object limit; signed
admission still applies all core limits. Use an existing external signing key.

```go
type RecordRequest struct {
    Repo, KeyFile string
    Input development.RecordInput
}
func Record(context.Context, RecordRequest) (RecordResult, error)
```

Always inspect both returns. Admission and index publication are separate.
`development.ValidateInput` runs before key access. Source revisions receive
shape validation as reported facts; recording neither proves commit existence
nor establishes that a command ran against that source. `evidence.ResolveAll`
checks all supplied evidence in normalized event order, with 16 MiB per item
and 64 MiB per operation. New admission requires every item to match.

## Signed operation protocol

The operation key is `(namespace, signer key ID, development/v1, operation_id)`.
After domain/core normalization, hash the canonical object with fields `profile`,
`namespace`, and `events`. Events contain the complete normalized core envelope,
including injected operation/source, exact schema identity, evidence, and refs.
This digest excludes observation time, actor label, parents, and machine paths.
Equivalent normalizations share identity; different normalized facts conflict.

Batch metadata stores:

```json
{"workflow":{"profile":"development/v1","operation_id":"example","request_digest":"sha256:..."}}
```

The core may also supply its ordinary producer metadata. No wire format changes
or mutable operation journal are involved. Reconciliation scans verified signed
canonical history under a store read lock and checks signed namespace/signer,
metadata, and a freshly recomputed digest of the stored event facts. Copied
metadata cannot certify different raw events. Multiple raw claims, even identical
ones, report conflict and all candidate object IDs; recording never chooses a
winner. `ErrConflict` and `ErrEvidence` support `errors.Is`.

A cooperative operation holds `.pact/tmp/record-<hash>.lock`, whose hash covers
the canonical operation-key array. The directory must be real and the lock file
regular, with no symlink following. Flock serializes processes and releases on
exit. Files remain after use: unlinking an active inode would break exclusion.
Operators may remove abandoned lock files only when recorders are stopped.
Missing tmp/index directories and lost lock files do not erase admission history.

The operation lock covers reconciliation, evidence reads, and admission. It
releases before index rebuild. There is no enclosing store mutation lock around
`ledger.Commit`/`Store.PutCanonical` or `index.Manager.Rebuild`; those owners
acquire their own locks. Raw writes bypass operation locking. They may produce
visible duplicate/conflicting claims or advance the source after a rebuild.
This is local cooperative idempotency, not distributed exactly-once delivery.

## Result fields and recovery

The CLI adds `operation: "record"` to these `RecordResult` JSON fields:

| Field | Meaning |
| --- | --- |
| `admission` | `created`, `existing`, `conflict`, or `unknown`; unknown does not assert absence |
| `object_id` | Verified admitted object ID, or empty when no unique identity is established |
| `event_refs` | Original immutable refs; always an array |
| `schema_validation` | `valid`, `invalid`, or `unchecked` for the submitted development facts |
| `integrity`, `authenticity` | Core verification layers, initially `unchecked` |
| `authorization` | Core root-authority result, initially `indeterminate`; separate from schema and signature |
| `index_state` | `current` for the returned rebuild snapshot, `pending` after admitted recovery failure, or `unchecked` |
| `source_fingerprint` | Last observed canonical set; on success the rebuilt index snapshot, never a Git source revision |
| `next_action` | Recovery instruction, empty after successful index publication |
| `evidence` | Current assessment array, flattened in normalized event/evidence order |
| `evidence_state` | `matching`, `not_matching`, `unchecked`, `unavailable`, `limit_exceeded`, `interrupted`, or `process_failed` |
| `conflicting_object_ids` | Sorted array of every matching-key candidate when conflict prevents choosing an object |

A created object with an unsuccessful index rebuild leaves `source_fingerprint`
empty rather than returning the earlier pre-admission set. An existing operation
may retain the fingerprint observed during reconciliation. Readiness is an
observation of that returned snapshot; a later raw writer may make it stale.

Exact retries reconcile admission before requiring fresh evidence. Missing,
changed, unsupported, oversized, or failed evidence reads remain visible in the
result but do not erase admission or prevent index recovery. Conflicting inputs
append nothing. Index failures preserve the object ID and advise an explicit
rebuild or exact retry. Cancellation before admission refuses the request;
publication is not rolled back when cancellation arrives later. The existing
store opener, signing-key loader, and commit API have no context parameter:
cancellation cannot interrupt those owner calls. A commit error triggers a
bounded canonical reconciliation independent of caller cancellation, and an
unresolved outcome remains `unknown`.

Exit zero means recording/reconciliation and index publication succeeded. It does
not claim all current retry evidence matches, execution occurred, or acceptance
is supported. Usage/domain, evidence mismatch/limit, and operation conflict use
exit 2; existing owner errors keep their existing classifications. Unexpected
post-admission/index failures use exit 10 unless the owner supplies a more
specific classification. JSON errors carry the result in `details` on stderr;
human post-admission errors include the object ID and recovery action. Success
JSON goes to stdout. A lost or partial response is recovered with the exact input.
