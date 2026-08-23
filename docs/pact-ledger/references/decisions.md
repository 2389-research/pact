# PACT Design Decisions

This file captures the v1 decisions that define the skill. Later changes should
be recorded as new design decisions rather than silently changing these
semantics.

## D1 — Semantic ledger plus external evidence

PACT stores small domain-meaningful events. Raw commands, logs, files, traces,
source snapshots, and binary captures remain outside the ledger and are
referenced by locator and digest.

**Reason:** semantic history remains queryable and durable without turning the
ledger into an immutable secret or blob warehouse.

## D2 — Append-only immutability

Persisted objects are never edited or deleted. Corrections, invalidations,
revocations, and evidence failures are later events.

**Reason:** the original assertion and the history of changing knowledge both
remain inspectable.

## D3 — Signed tamper-evident objects

Every commit and checkpoint is content-addressed and signed. Hashes detect byte
changes; signatures identify the asserting key.

**Reason:** tamper evidence and attribution are foundational rather than added
later as log metadata.

## D4 — Hierarchical namespaces

Events live under logical paths such as:

```text
org/example/project/widget
org/example/project/widget/audit
org/example/project/widget/release
```

Cross-namespace relationships use event references. Storage layout is not
required to mirror logical hierarchy.

**Reason:** authority and query boundaries remain precise while physical storage
can evolve independently.

## D5 — Delegated capabilities

Authority descends from bootstrapped human or organization roots to project,
service, and ephemeral agent keys. Delegations constrain namespace, event types,
epoch, lease policy, and subdelegation.

**Reason:** a valid signature alone should not grant every actor every semantic
power.

## D6 — Policy determines acceptance

The ledger records assertions and observations. A content-addressed projection
policy determines whether each event is accepted, rejected, irrelevant, or
unresolved for a specific output.

**Reason:** two workflows can interpret the same immutable history differently
without rewriting it.

## D7 — Policy registration and activation are ledger events

Policies are external content-addressed artifacts. Authorized actors register
and activate them with signed events.

**Reason:** a historical output must be reproducible under the exact policy that
was authoritative at the time.

## D8 — Native DAG, not global sequence

Commits may have multiple parents and multiple concurrent heads. Parent links
represent causal observation. No global append lock or total order is required.

**Reason:** independent agents can work locally and offline without manufacturing
false ordering.

## D9 — Atomic signed commits contain semantic events

The persistence unit is a signed commit. The domain unit is an event. All events
inside a commit become visible together.

**Reason:** workflows can record coherent multi-fact operations without exposing
misleading intermediate state.

## D10 — Local-first replication

Replicas create valid commits offline. Sync exchanges missing immutable objects,
verifies them, and unions the DAG.

**Reason:** the ledger remains useful inside local coding, research, and field
workflows without depending on a central service.

## D11 — External evidence may disappear

PACT does not require referenced evidence to remain available forever. A failed
future access is recorded lazily as a new observation.

**Reason:** evidence availability is part of evolving history; mutating the
original record would conceal that evolution.

## D12 — Causal time is authoritative; wall time is advisory

DAG ancestry proves causal order. Actor-supplied timestamps are useful for
humans but do not establish a global sequence or trusted existence time.

**Reason:** offline and distributed actors cannot safely rely on synchronized or
honest clocks.

## D13 — Rigid envelope, content-addressed payload schemas

PACT defines a small universal event envelope. Domain payloads reference exact
schemas. Optional compatibility declarations may describe evolution, but old
events are never migrated in place.

**Reason:** the core stays generic while historical meaning remains exact.

## D14 — Canonical objects plus disposable SQLite

The object store is canonical. SQLite is a derived local index that can be
recreated at any time.

**Reason:** queries can be fast without making a mutable database the historical
source of truth.

## D15 — Transport-neutral object exchange

Sync means exchanging missing objects by digest. Directory, SSH, HTTP, object
storage, or removable-media adapters may implement transport.

**Reason:** transport should not redefine ledger semantics.

## D16 — Signed checkpoints define official state boundaries

A checkpoint signs an exact frontier, policy, schema set, and authority context.
Replicas can operate without checkpoints, but an official output should name
one.

**Reason:** official state can be reproducible without making a central server
the sole writer.

## D17 — Derived artifacts may become later evidence

A report, CSV, model, or manifest is external and recorded by digest. Later work
may cite it as derived evidence while retaining the chain to its source
checkpoint and policy.

**Reason:** workflows compose without confusing an interpretation with a primary
observation.

## D18 — CLI is the integration contract

Skills and agents use a small command surface. A library sits underneath for
embedders and production implementations.

**Reason:** language-neutral automation remains simple, while storage and crypto
internals can evolve behind a stable interface.

## D19 — No encrypted immutable payload system in v1

PACT v1 refuses secrets and sensitive raw content rather than building envelope
encryption, key revocation, or cryptographic deletion.

**Reason:** evidence references solve the first-order need with much less
complexity. Confidential object payloads can be a later, explicit extension.

## D20 — Verification layers stay separate

Integrity, authenticity, authorization, acceptance, and evidence availability
are separate results.

**Reason:** a single `valid` flag obscures the exact guarantee and invites
incorrect trust decisions.
