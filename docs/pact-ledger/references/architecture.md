# PACT Architecture

## 1. System boundary

PACT is a ledger substrate for domain workflows. It owns durable signed history,
causal relationships, object identity, actor identity, authority proofs,
checkpoints, and replication semantics.

PACT does **not** own:

- the domain ontology of an audit, test, deployment, or research workflow;
- the truth of an actor's claim;
- raw evidence retention;
- secret management;
- policy correctness;
- global consensus;
- trusted wall-clock time;
- a mandatory network service.

## 2. Logical components

```text
┌──────────────────────────────────────────────────────────────┐
│ Domain skill / agent                                         │
│ event vocabulary · schemas · projection policy · artifact    │
└──────────────────────────────┬───────────────────────────────┘
                               │ CLI / library
                               ▼
┌──────────────────────────────────────────────────────────────┐
│ PACT write path                                               │
│ validate → normalize → authorize preflight → sign → persist  │
└──────────────────────────────┬───────────────────────────────┘
                               ▼
┌──────────────────────────────────────────────────────────────┐
│ Canonical object store                                        │
│ signed commits · signed checkpoints · content-addressed DAG   │
└──────────────┬───────────────────────────────┬───────────────┘
               │                               │
               ▼                               ▼
┌──────────────────────────┐      ┌────────────────────────────┐
│ Rebuildable SQLite index │      │ Replica sync adapters       │
│ query only               │      │ directory / SSH / HTTP / S3 │
└──────────────┬───────────┘      └────────────────────────────┘
               ▼
┌──────────────────────────────────────────────────────────────┐
│ Verification and projection                                  │
│ integrity · signatures · authority · policy · deterministic  │
└──────────────────────────────┬───────────────────────────────┘
                               ▼
┌──────────────────────────────────────────────────────────────┐
│ External artifacts                                            │
│ CSV · report · dashboard · release manifest · model snapshot  │
└──────────────────────────────────────────────────────────────┘
```

## 3. Canonical versus derived data

### Canonical

- exact object bytes under their SHA-256 IDs;
- parent relationships encoded in signed commit bodies;
- event envelopes and payloads inside signed commits;
- signed checkpoint envelopes.

### Derived or local

- SQLite rows;
- head caches;
- friendly labels;
- trust-root configuration;
- query result ordering;
- projection output;
- artifact files;
- sync inventories;
- evidence availability status until recorded as an event.

Local trust roots are not canonical ledger history because initial trust must be
bootstrapped from outside the ledger. After bootstrap, rotations and delegations
can be recorded inside the ledger.

## 4. Namespace model

Namespaces are slash-delimited logical paths:

```text
org/<organization>/project/<project>/<domain>/<subdomain>
```

PACT does not require that exact prefix vocabulary, but a project should select
one convention and keep it stable.

A commit belongs to exactly one namespace. Its parents must belong to the same
namespace in v1. Cross-namespace semantic causality uses event references rather
than parent links.

This yields independent per-namespace DAGs that can be checkpointed together at
a higher scope.

## 5. Causal DAG

A commit has zero or more parent commit IDs:

```text
        B ──────┐
       ↗         ▼
A ────            D
       ↘         ▲
        C ──────┘
```

- `A → B` means B's author had incorporated A into the causal history used for B.
- B and C are concurrent when neither is reachable from the other.
- D is a merge commit because it acknowledges both branches.
- Array order of parents is not semantic; canonical encoding sorts IDs.

A deterministic topological ordering may be used for processing, but only DAG
reachability establishes causal order. A hash or timestamp tie-breaker must not
be treated as proof that one concurrent event happened first.

## 6. Atomicity

A commit may contain multiple events:

```text
commit K
  e1 observation.command_finished
  e2 assertion.test_passed
  e3 action.artifact_written
```

The object store admits K atomically. A projection must process all three events
or none. Events may reference earlier events in the same commit using local
references such as `local:e1`.

Unrelated events should not be batched solely to save signatures. Atomicity is a
semantic boundary, not a transport optimization.

## 7. Object identity

PACT v1 uses:

```text
object ID = sha256(canonical signed object bytes)
body digest = sha256(canonical unsigned body bytes)
```

The signature covers the body digest. The complete signed envelope determines
the stored object ID.

An event reference is derived:

```text
pact:event:<commit-object-id>#<local-id>
```

This avoids assigning mutable sequence numbers while giving each semantic event
a stable address.

## 8. Write path

1. Parse the domain event batch.
2. Reject unsupported values, floats, malformed refs, duplicate IDs, and secret
   hazards.
3. Normalize strings and deterministic collections.
4. Select current observed parents.
5. Build body with namespace, actor, authority hints, advisory timestamp,
   metadata, and events.
6. Compute body digest.
7. Sign body digest.
8. Build signed envelope and compute object ID.
9. Write to a temporary file.
10. Atomically move into the content-addressed path.
11. Re-read and verify.
12. Update the disposable index.

The object must remain valid even if the final index or ref update fails.

## 9. Read and verification path

Verification is layered:

```text
bytes/path → envelope → body digest → signature → DAG → references → authority
```

Semantic acceptance is not part of basic cryptographic verification. It belongs
to a named projection policy.

A replica may be partial. Missing parents make a commit unverifiable as a
complete causal object and should block checkpointing. Missing non-parent event
references may be warnings in a deliberately partial replica, but strict mode
must fail them.

## 10. Evidence path

Evidence stays outside PACT:

```text
external bytes ── sha256 ──► evidence reference inside event
```

PACT does not proactively crawl evidence. When a workflow later needs it:

1. resolve the locator;
2. read the exact bytes;
3. recompute digest;
4. use them when the digest matches;
5. append `resolve_failed` or `hash_mismatch` only if the attempt fails.

The original evidence reference remains unchanged.

## 11. Checkpoints

A checkpoint contains a frontier grouped by namespace:

```text
scope: org/example/project/widget
frontier:
  - namespace: .../audit
    heads: [sha256:..., sha256:...]
  - namespace: .../release
    heads: [sha256:...]
policy_ref: sha256:...
schema_refs: [...]
authority_epoch: ...
previous_checkpoint: sha256:...
```

The checkpoint is signed and content-addressed. Friendly names such as `latest`
are mutable local refs to checkpoint IDs and are never sufficient provenance by
themselves.

## 12. Projection boundary

A projection's complete identity is a tuple:

```text
(frontier/checkpoint, policy, schema set, projector implementation)
```

The artifact output digest is a function of that tuple plus exact reachable
history. Reproducing the artifact requires all four.

## 13. Replication

A replica advertises object IDs or checkpoint/frontier roots. The peer computes
missing reachable objects and transfers exact bytes. Admission is based on the
recipient's verifier, not trust in the transport.

Replication does not resolve semantic conflicts. It merely makes more history
available.

## 14. Extensibility

A production implementation may replace:

- local files with an immutable object service;
- SQLite with another rebuildable index;
- file keys with hardware or cloud signing;
- directory sync with a network protocol;
- Python with another language.

It must preserve canonical encoding, object IDs, signature semantics, event
references, atomic visibility, verification layers, and projection/checkpoint
contracts for interoperability.
