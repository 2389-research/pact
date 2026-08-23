# PACT Production Implementation Plan

## Objective

Build a production-quality PACT engine that preserves the v1 invariants while
remaining easy for skills and agents to use through a stable CLI.

The bundled Python CLI is a reference implementation and conformance harness.
Production work should proceed in bounded phases with explicit gates rather than
attempting network replication, advanced authority, and policy execution at once.

## Guiding constraints

- local-first before networked;
- immutable object store before indexing;
- cryptographic integrity before authority;
- authority before official checkpoints;
- deterministic projections before distributed writers;
- evidence references, not evidence storage;
- no encrypted payload subsystem in v1;
- no global consensus or total ordering;
- no mutable database as canonical history;
- compatibility tests before implementation-language diversity.

## Phase 0 — Freeze the v1 contract

### Deliverables

- normative canonical JSON profile;
- commit and checkpoint JSON Schemas;
- object-ID and event-reference grammar;
- signature test vectors;
- canonicalization test vectors;
- namespace and event-type wildcard semantics;
- authority result model;
- projection manifest schema;
- threat model and resource limits;
- CLI exit-code contract.

### Required tests

- identical logical objects produce byte-identical canonical output;
- Unicode normalization cases are deterministic;
- duplicate normalized keys are rejected;
- floats and out-of-range integers are rejected;
- public-key IDs and signatures match fixed vectors;
- parent/head normalization is deterministic;
- event IDs remain stable across index rebuilds;
- malformed IDs and refs are rejected.

### Exit gate

At least two independent implementations or one implementation plus a separate
test-vector verifier agree on every conformance vector.

## Phase 1 — Single-replica immutable core

### Scope

Implement:

- project initialization;
- external key generation/loading;
- canonicalization;
- Ed25519 signing and verification;
- content-addressed atomic object writes;
- commit creation;
- checkpoint creation;
- object inspection;
- read-only verification;
- no authority beyond trusted roots.

### Non-goals

- network sync;
- delegated authority;
- projection execution;
- schema registry automation;
- evidence retrieval;
- multi-process write optimization.

### Required tests

- crash between temp write and rename leaves no canonical partial object;
- object collision with different bytes hard-fails;
- tampering changes object ID and fails verification;
- signature substitution fails;
- private key never appears under project root;
- secret scanner rejects representative credentials;
- concurrent independent genesis commits remain representable;
- correction creates a new object and leaves target bytes unchanged.

### Exit gate

A repository can initialize, append signed commits, verify from bytes alone, and
recover after index/ref loss.

## Phase 2 — DAG, index, and query

### Scope

Implement:

- parent validation;
- cycle detection;
- per-namespace head computation;
- stable event references;
- SQLite index and full rebuild;
- log/show/query commands;
- partial-replica diagnostics;
- graph and object resource limits.

### Required tests

- forks and merges preserve all branches;
- no timestamp is used to decide causal order;
- same-namespace parent rule is enforced;
- cross-namespace event references work;
- missing parent blocks completeness/checkpointing;
- index deletion followed by rebuild yields identical query results;
- malicious deep/wide DAGs hit explicit limits rather than exhausting the process.

### Exit gate

All canonical state and query indexes can be regenerated from object bytes, and
concurrent branches are visible without loss.

## Phase 3 — Schemas and semantic validation

### Scope

Implement:

- content-addressed schema artifacts or resolvers;
- `schema.registered` handling;
- exact payload validation;
- core event schemas;
- unavailable-schema status;
- optional compatibility metadata;
- schema-resolution cache that is not canonical.

### Required tests

- an old event always validates against its original schema;
- schema substitution with same name but different digest fails;
- unavailable schema preserves object integrity but blocks semantic acceptance;
- malformed domain payload is rejected before signing;
- compatibility metadata never silently migrates payloads.

### Exit gate

Domain event producers can rely on exact schemas, and projectors can distinguish
structural, schema, and policy failures.

## Phase 4 — Delegated authority

### Scope

Implement:

- trust roots;
- delegation-chain verification;
- namespace patterns;
- event-type patterns;
- control capabilities;
- subdelegation depth;
- causal revocation;
- authority epochs;
- advisory lease reporting;
- `authorized`, `unauthorized`, and `indeterminate` results.

### Required tests

- root and delegated commits validate independently;
- one uncovered event invalidates authorization for the atomic commit;
- delegation must be causally prior;
- wrong namespace/type/epoch fails with exact reason;
- causal revocation fails authorization;
- concurrent revocation produces an explicit policy-dependent/indeterminate case;
- subdelegation limits are enforced;
- partial replica cannot manufacture a complete authority chain;
- actor label changes do not affect key identity.

### Exit gate

Every commit receives a structured authorization result traceable to a configured
root or an exact blocker.

## Phase 5 — Projection engine and artifact provenance

### Scope

Implement:

- checkpoint/frontier traversal;
- deterministic topological batches;
- policy bundle loading by digest;
- schema-aware event stream;
- conflict/supersession decision API;
- projection diagnostics;
- deterministic artifact generation;
- projection manifest;
- `artifact.produced` helper.

### Required tests

- identical inputs produce identical canonical artifact bytes;
- concurrent events are not given false semantic order;
- policy changes produce separate reproducible outputs;
- unresolved conflicts appear in diagnostics/artifact status;
- unauthorized/indeterminate events follow explicit policy;
- output manifest contains exact immutable inputs;
- artifact hash is recorded only after successful durable write.

### Exit gate

At least one real workflow can regenerate its existing mutable artifact from PACT
and achieve byte- or semantic-equivalence across representative histories.

## Phase 6 — Directory and bundle replication

### Scope

Implement:

- object inventory exchange;
- missing-object calculation;
- transfer bundles/manifests;
- recipient-side verification;
- atomic admission;
- dependency completion;
- index rebuild;
- sync summaries;
- quotas and object-size limits.

### Required tests

- sync is idempotent;
- sync never overwrites an existing object;
- forks union cleanly;
- corrupt remote object is rejected;
- authentic unauthorized object is retained or rejected according to explicit
  local admission policy, never silently accepted;
- missing parent is reported and can be fetched;
- interrupted sync resumes without canonical corruption;
- evidence is not assumed to be transferred.

### Exit gate

Two offline replicas can create independent histories, exchange objects, verify,
and produce the same heads and projection from the same checkpoint.

## Phase 7 — Network sync service

### Scope

Only after directory sync is stable, add:

- authenticated object inventory endpoint;
- object fetch/push by digest;
- checkpoint discovery;
- namespace filtering;
- rate limits and quotas;
- resumable batched transfer;
- server-side read-only verification cache;
- no server authority implied by storage alone.

### Required tests

- untrusted transport cannot change object identity;
- clients verify every fetched object;
- server cannot force branch deletion;
- authorization to upload is distinct from event authorization;
- partial scopes cannot be presented as full project history;
- replay and duplicate uploads are harmless;
- backpressure and cancellation work.

### Exit gate

The network service is replaceable by another transport without changing ledger
or projection semantics.

## Phase 8 — Hardened key and trust operations

### Scope

- hardware-backed or OS keystore signing;
- root rotation workflows;
- multi-root/quorum checkpoint policy;
- ephemeral key issuance;
- automated lease renewal;
- compromise recovery tooling;
- audit reports for authority chains.

### Required tests

- keys are never exported when hardware-backed;
- root rotation preserves historical verification;
- compromised-key revocation does not rewrite prior objects;
- quorum policies fail closed;
- recovery requires explicit out-of-band trust changes.

### Exit gate

Operational key lifecycle is documented, tested, and separate from ordinary
agent commit logic.

## Phase 9 — Ecosystem and compatibility

### Scope

- language SDKs generated from the frozen contract;
- conformance test suite;
- projection SDK;
- schema/policy packaging convention;
- transport adapters;
- observability and metrics;
- migration tooling for legacy ledgers.

### Required tests

- cross-language object IDs and signatures match;
- sync between implementations preserves exact bytes;
- projection input stream is equivalent;
- unknown minor fields obey forward-compatibility policy;
- unsupported major versions fail safely.

## Reference CLI to production migration

The bundled CLI should remain useful as:

- a conformance oracle;
- a forensic verifier;
- an offline recovery tool;
- a test-fixture generator;
- a minimal integration path.

A production implementation may be in Go, Rust, or another language, but should
run the same test vectors and expose compatible commands or machine-readable
results.

## Workstreams

Parallelize only after Phase 0:

1. **Object/crypto core** — canonicalization, signing, storage.
2. **Graph/index/query** — DAG traversal and SQLite.
3. **Authority** — delegation evaluator and trust tooling.
4. **Projection** — policy API and deterministic output.
5. **Replication** — manifests, directory sync, then network adapters.
6. **Conformance/security** — vectors, fuzzing, threat tests, limits.
7. **Workflow adapters** — event schemas and projections for real use cases.

The object/crypto contract is the shared dependency. Do not allow workstreams to
invent incompatible IDs or authority semantics.

## Quality gates across all phases

- property tests for canonicalization and DAG invariants;
- fuzz parsing and malformed-object handling;
- deterministic golden tests;
- crash/recovery tests;
- mutation tests for verifier strength;
- static secret scanning and runtime secret fixtures;
- performance tests on large histories and branch counts;
- cross-version upgrade tests;
- repository-integrity checks ensuring tests never mutate canonical fixtures;
- explicit incomplete/indeterminate states rather than optimistic defaults.

## Initial production milestone

A credible first production milestone includes Phases 0–6 and one real workflow
adapter. It does not require a daemon, global service, encrypted payloads, trusted
timestamps, or universal policy language.

Success means:

- independent agents can write offline;
- history unions without loss;
- every object is tamper-evident and attributable;
- authority is scoped and inspectable;
- a signed checkpoint identifies an official state;
- a deterministic projection reproduces a useful existing artifact;
- the whole index can be destroyed and rebuilt from objects.
