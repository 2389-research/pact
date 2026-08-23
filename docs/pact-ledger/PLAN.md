# PACT Build Plan

## Product definition

PACT is a domain-neutral, local-first ledger substrate for agents, humans, and
services. It stores atomic signed semantic commits in an immutable
content-addressed DAG. External policies project that history into current state
and human-facing artifacts.

The key promise is narrow and strong:

> Preserve exactly who asserted or observed what, under which authority, in what
> causal context, with references to the evidence they used.

PACT does not make assertions true and does not store raw evidence or secrets.

## v1 architecture

```text
domain skill / agent
        ↓ CLI or library
semantic events + external evidence digests
        ↓
signed atomic commits
        ↓
content-addressed per-namespace DAG
        ├── rebuildable SQLite index
        ├── object-union replica sync
        └── signed checkpoints
                ↓
content-addressed policy + deterministic projector
                ↓
current-state artifact + provenance manifest
```

## v1 product surfaces

1. **Object core** — canonical JSON, SHA-256 IDs, Ed25519 signatures, atomic
   persistence, stable event references.
2. **Graph** — same-namespace parents, native forks/merges, causal traversal,
   cycle and dependency checks.
3. **Identity and authority** — out-of-band roots, scoped delegation,
   event/namespace capabilities, revocation, epochs, advisory leases.
4. **Evidence contract** — external locator + digest + role; lazy failure and
   mismatch events.
5. **Checkpoints** — signed official frontiers with policy/schema/authority
   context.
6. **Projection contract** — deterministic interpretation with explicit
   acceptance and conflict rules.
7. **Replication** — transport-neutral exchange and union of missing objects.
8. **Agent interface** — stable CLI backed by an importable library.

## Build sequence

### Milestone 0 — Freeze the wire contract

Freeze canonicalization, IDs, event refs, commit/checkpoint schemas, wildcard
semantics, status vocabulary, and test vectors. Do not build multiple language
implementations before this gate.

### Milestone 1 — Local immutable core

Ship initialization, external key handling, signing, object storage, append,
verification, and inspection. The store must survive loss of every cache and
index.

### Milestone 2 — DAG and query index

Add forks/merges, head calculation, causal traversal, stable event lookup,
SQLite rebuild, and bounded graph verification.

### Milestone 3 — Exact schemas

Add content-addressed schema resolution, signed registration, core control-event
schemas, and semantic validation without migrating old events.

### Milestone 4 — Full authority evaluator

Complete recursive subdelegation, capability depth, epochs, causal revocation,
lease/checkpoint policy, root rotation, and machine-readable authorization
explanations.

### Milestone 5 — Projection engine

Load exact policy bundles, feed verified partial-order event batches, expose
conflict/supersession decisions, generate deterministic artifacts, and record
artifact provenance.

### Milestone 6 — Replication

Stabilize directory/bundle sync first. Then add replaceable SSH/HTTP/object-store
adapters. A transport never decides object identity or acceptance.

### Milestone 7 — Operational hardening

Add hardware/OS-backed keys, quotas, graph limits, fuzzing, crash tests, network
backpressure, root recovery, and multi-approver checkpoint policies.

## First production release gate

A first credible production release requires Milestones 0–6 plus one real
workflow adapter. It must demonstrate:

- two replicas can write independently and union history without loss;
- tampering and malformed signatures are detected;
- direct and recursive authority decisions are explainable;
- an index can be deleted and rebuilt exactly;
- a signed checkpoint identifies an official frontier;
- one existing mutable artifact can be regenerated deterministically;
- raw evidence and private keys remain outside the project ledger;
- unauthorized history is visible but excluded by default policy.

## Deliberate v1 non-goals

- blockchain or global consensus;
- a universal total order;
- encrypted immutable payloads or cryptographic deletion;
- raw evidence/blob storage;
- trusted timestamping;
- universal truth or automatic fact adjudication;
- a mandatory daemon or central server;
- a universal domain event ontology.

## Workstream ownership

The object/crypto contract is the dependency for all other work. After it is
frozen, work can proceed in parallel across graph/index, authority,
projection/policy, replication, conformance/security, and workflow adapters.

The detailed phase gates and test matrix are in
`references/implementation-plan.md`.
