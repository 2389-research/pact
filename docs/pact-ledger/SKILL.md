---
name: pact-ledger
description: >-
  Operate PACT — Provenance, Authority, Causality, and Trust — as a local-first,
  append-only, content-addressed, cryptographically signed semantic ledger for
  agents, humans, and services. Use when work needs durable provenance,
  tamper-evident history, resumable agent state, scoped authority, concurrent
  writers, reproducible projections, signed checkpoints, or a real ledger
  beneath mutable CSVs, reports, dashboards, and other derived artifacts.
  PACT is domain-neutral: audit, verification, research, deployment, operations,
  and other skills may use it without PACT knowing their ontology.
metadata:
  version: "0.2.0"
---

# PACT Ledger

**PACT = Provenance, Authority, Causality, and Trust.**

## Mission

Maintain a durable project history in which authorized actors append atomic,
cryptographically signed commits containing domain-specific semantic events.
Commits form a content-addressed causal DAG. Evidence remains external and is
referenced by locator and digest. Policies and projections interpret the ledger
to produce current state and human-facing artifacts.

PACT records **what actors observed, asserted, decided, and did**. It does not
claim that an assertion is true merely because it is signed or present.

The governing separation is:

```text
immutable ledger history
        ↓
validation: integrity → authenticity → authorization
        ↓
projection policy
        ↓
accepted current state
        ↓
derived artifact
```

## Required project layout

The default project-local layout is:

```text
.pact/
  format.json                 # local format/configuration metadata
  trust.json                  # local verifier trust roots; bootstrap is out-of-band
  objects/sha256/aa/...       # canonical immutable signed objects
  index/pact-v1.sqlite3       # disposable, rebuildable query index
  refs/                       # mutable local labels/caches; never canonical proof
  tmp/                        # atomic-write staging; safe to clean
```

Private signing keys must live outside the repository and outside `.pact/`,
preferably under an OS-protected user configuration directory or hardware-backed
keystore. The bundled CLI uses an explicit key-file path and creates key files
with owner-only permissions.

The canonical record is the set of valid content-addressed objects. SQLite,
refs, labels, reports, CSVs, and dashboards are projections or caches and may be
rebuilt.

### Current implementation boundary

The `pact` CLI implements canonical objects, Ed25519 signing, atomic append, DAG
validation, root trust, signed checkpoints, indexing, and querying. It does
**not** execute arbitrary external payload schemas or projection policies,
provide trusted time, evaluate advanced delegation policy, synchronize
replicas, or act as a hardened multi-tenant key service.

When a requested operation needs one of those layers, follow this skill's
contract but report the layer as `indeterminate` or unavailable. Do not imply
that `pact` proved more than its command output shows.

## Non-negotiable invariants

1. **Append; never rewrite.** Do not edit, replace, delete, renumber, or mutate a
   persisted ledger object. Corrections, invalidations, revocations, evidence
   failures, and changed conclusions are new events.
2. **The ledger is not truth.** Presence proves that an actor made a signed
   assertion or recorded an observation. Acceptance is projection-relative.
3. **Commits are atomic.** A commit and every semantic event inside it become
   visible together or not at all. A projection must never consume half a
   commit.
4. **Content determines identity.** Object IDs are cryptographic digests of
   canonical object bytes. Never assign sequential ledger IDs as authority.
5. **Signatures prove authorship, not correctness.** Keep `authentic`,
   `authorized`, and `accepted` as separate states.
6. **Authority is delegated and scoped.** Validate namespace, event type,
   delegation ancestry, epoch, and lease policy before treating a commit as
   authorized.
7. **Causality outranks clocks.** Parent links and event references establish
   causal order. Actor-reported wall-clock timestamps are advisory metadata.
8. **Concurrency is normal.** Do not force a global sequence or silently discard
   a branch. Concurrent claims coexist until an explicit merge, supersession, or
   projection rule handles them.
9. **Evidence stays external.** Store evidence references, digests, media types,
   roles, and redaction metadata—not raw logs, source trees, credentials, private
   customer data, or large binary material—in semantic commits.
10. **Evidence loss is new information.** A failed resolution or digest mismatch
    never alters the original event. Record the later observation lazily when it
    is encountered.
11. **No secrets in immutable payloads.** Never persist passwords, bearer tokens,
    session cookies, private keys, unredacted credentials, or credential-bearing
    URLs in commits, checkpoints, schemas, policies, or local refs.
12. **Schemas are exact.** The universal envelope is rigid. Domain payloads
    reference an exact content-addressed schema or a versioned PACT core schema.
    Do not reinterpret old payloads using a newer schema.
13. **Policies are artifacts.** Projection policy is content-addressed; policy
    registration and activation are signed events. A policy change does not
    rewrite history.
14. **Checkpoints define official cuts.** A signed checkpoint identifies an
    exact DAG frontier, policy, schema set, and authority context. A mutable
    server pointer or local ref is not an official state by itself.
15. **Replication is union, not overwrite.** Sync exchanges missing objects,
    verifies them, and unions valid history. It never performs last-writer-wins
    replacement.
16. **Unauthorized is not nonexistent.** A structurally valid, authentic commit
    may remain in the ledger even when its authority is absent, expired,
    revoked, or indeterminate. Default projections must not accept it silently.
17. **Corrupt objects are not ledger objects.** Hash failures, malformed
    envelopes, or invalid signatures must not enter the canonical object store.
    Preserve them only outside the ledger when diagnostic retention is needed.
18. **The index is disposable.** Never treat SQLite rows as canonical history.
    Rebuild the index from objects after uncertainty, import, or suspected drift.
19. **Derived artifacts retain provenance.** Record an `artifact.produced` event
    with artifact digest, external reference, input checkpoint, policy, and
    projector identity before another workflow relies on that artifact.
20. **Do not claim verification beyond the layer checked.** Report integrity,
    authenticity, authorization, evidence availability, and projection
    acceptance independently.

## Core vocabulary

| Term | Meaning |
|---|---|
| **Object** | Canonical content-addressed byte sequence stored under its digest. |
| **Commit** | Signed atomic object containing one or more semantic events and parent commit IDs. |
| **Event** | Domain-meaningful observation, assertion, action, decision, or control statement inside a commit. |
| **Event reference** | Stable reference derived from commit ID and event-local ID. |
| **Evidence reference** | External locator plus digest and descriptive metadata; never the evidence bytes themselves. |
| **Namespace** | Hierarchical authority and organization boundary, such as `org/acme/project/widget/audit`. |
| **Subject** | Stable domain identity the event concerns, such as `build/linux-amd64` or `requirement/REQ-42`. |
| **Parent** | A commit causally observed before the child commit. Parents form the DAG. |
| **Delegation** | Signed authority granted to a key for bounded namespaces, event types, epochs, and lease conditions. |
| **Projection** | Deterministic policy-driven interpretation of an exact ledger frontier. |
| **Checkpoint** | Signed declaration of an exact frontier and interpretation context. |
| **Artifact** | External derived output such as a CSV, report, release manifest, model, or dashboard snapshot. |

## Validation layers

Always keep these layers distinct:

```text
integrity       object bytes match the object ID and canonical format
authenticity    signature verifies against the embedded public key
authorization   a valid delegation/trust chain permits this actor and event scope
acceptance      a named projection policy incorporates the event
availability    referenced external evidence can currently be resolved and hashed
```

A commit can be:

- authentic but unauthorized;
- authorized but rejected by one projection;
- accepted by one policy and rejected by another;
- historically valid while some referenced evidence is now unavailable.

Never collapse these into a single `valid` boolean in user-facing reporting.

## Operating modes

Infer the smallest applicable mode from the request:

1. **Initialize** — create a project ledger and bootstrap local trust.
2. **Record** — append one atomic signed semantic commit.
3. **Correct or invalidate** — append a new event that targets prior history.
4. **Verify** — inspect object integrity, signatures, DAG structure, references,
   and available authority information without changing history.
5. **Query** — locate events, subjects, actors, branches, or evidence references.
6. **Project** — derive current state from a checkpoint and exact policy.
7. **Checkpoint** — sign an official frontier and interpretation context.
8. **Sync** — exchange missing objects with another replica and union the DAG.
9. **Integrate** — adapt another skill or workflow to emit PACT events and treat
   its mutable ledger/report as a projection.
10. **Design or extend** — define schemas, policies, adapters, or a production
    implementation while preserving the invariants in this skill.

## Startup procedure

Before any mutation:

1. Determine the project root and requested namespace. Do not guess an
   organization or project namespace when the user supplied one.
2. Search for an existing `.pact/` directory at the project root.
   - If exactly one exists, use it in place.
   - If nested or competing ledgers exist, stop mutation and report their paths
     and namespace declarations. Do not merge by guessing.
   - If none exists and the task requires recording, initialize one.
3. Read `.pact/format.json` and refuse unsupported major versions.
4. Verify the current object store before appending:

   ```bash
   pact verify --repo . --strict
   ```

5. If integrity, signature, parent, or cycle errors exist, stop mutation. Do not
   append onto a corrupted or structurally ambiguous store.
6. Rebuild the SQLite index after import, uncertain state, or index failure:

   ```bash
   pact index rebuild --repo .
   ```

7. Resolve the signing actor and private key without exposing key material.
8. Determine whether the actor is a root, has a delegation, or will produce an
   authentic-but-authorization-indeterminate commit.
9. Inspect current heads for the exact namespace. Parent all locally observed
   heads by default; intentionally parent a subset only when preserving an
   offline or concurrent branch is part of the workflow.
10. Identify the applicable event schema and evidence references before creating
    a commit.

## Initialize a ledger

Use the PACT CLI:

```bash
pact setup \
  --repo . \
  --namespace org/example/project/widget \
  --actor human/operator \
  --key-file ~/.config/pact/keys/operator.json
```

Initialization rules:

- Treat trust-root installation as out-of-band bootstrap configuration. A ledger
  cannot prove its own initial root of trust.
- Never create a private key inside the repository.
- Do not silently replace an existing key or trust root.
- Record later root rotation, project delegation, and authority changes as
  signed semantic events.
- Keep `.pact/index/`, `.pact/tmp/`, and local refs out of version control when
  the project uses Git. Whether immutable objects are committed to Git is a
  project policy decision; PACT sync does not require Git.

## Prepare an atomic commit

### 1. Choose semantic events

Use the smallest event set that forms one coherent atomic operation. Split
unrelated observations or decisions into separate commits. Keep events together
when partial visibility would create a misleading state.

Each event uses this rigid envelope:

```text
local_id
kind                 observation | assertion | action | decision | control
type                 namespaced domain event type
subject              stable domain identity
schema_ref           exact payload schema
payload              domain-specific object
evidence[]           external refs + digests
caused_by[]           causal event references
supersedes[]          explicit correction/replacement targets
tags[]                optional non-authoritative discovery labels
```

### 2. Preserve semantic distinctions

Do not collapse these into one event:

```text
observation  what was directly seen
assertion    what an actor claims the observation means
decision     what an authorized process chooses to accept or do
action       what was actually performed
```

For example, a command exit status is an observation; “the capability works” is
an assertion; “ship the release” is a decision; deployment is an action.

### 3. Use stable subjects

Subjects identify domain entities, not transient prose. Prefer:

```text
requirement/REQ-42
build/linux-amd64/2026-08-23
subsystem/authentication
artifact/release-manifest/1.4.0
```

Do not use timestamps, array positions, or changing display names as the only
identity when a stable domain identifier exists.

### 4. Reference evidence safely

For evidence that supports an event, record:

```json
{
  "ref": "file:///external/evidence/run-42.log",
  "digest": "sha256:<hex>",
  "media_type": "text/plain",
  "role": "primary"
}
```

Rules:

- Hash the exact bytes observed.
- Use redacted or access-controlled external evidence when content is sensitive.
- Do not place raw evidence bytes in the payload.
- Do not include credentials in a URI.
- Use role `derived` when the evidence is itself a report or projection.
- If evidence cannot be retained, the original event remains valid as history;
  later resolution failure becomes a new observation.

### 5. Express causality explicitly

Use commit parents for commit-level causal history in the same namespace. Use
`caused_by` for domain causality and cross-namespace event relationships.

Use `supersedes` only when an event explicitly replaces or corrects a prior
assertion for a projection that recognizes supersession. Supersession never
removes the target from history.

### 6. Preflight authority

Before signing:

- confirm the key ID;
- identify the delegation event, when any;
- confirm namespace patterns cover the commit namespace;
- confirm event-type patterns cover every event in the commit;
- record the authority epoch and lease reference expected by policy;
- detect a known causal revocation;
- distinguish an authorization failure from missing local proof.

If authorization cannot be established but preserving the assertion is useful,
record the commit only when project policy allows authentic-but-indeterminate
history. State that it will not be accepted by default projections.

### 7. Scan for immutable-secret hazards

Before signing, inspect payloads, metadata, evidence locators, and labels for:

- private keys or PEM blocks;
- passwords and bearer/session credentials;
- API tokens and secret-bearing environment values;
- cookies and authorization headers;
- URLs containing userinfo or secret query parameters;
- unnecessary personal or customer data.

Replace values with redacted descriptors or environment-variable names. Do not
use encryption as an excuse to put secrets into the v1 ledger.

## Sign and persist

The commit procedure is:

1. Normalize the event batch according to the canonical JSON profile.
2. Resolve and sort parent IDs.
3. Build the unsigned commit body.
4. Hash the canonical body.
5. Sign the body digest with Ed25519.
6. Build the signed commit envelope.
7. Hash the complete canonical envelope to obtain the object ID.
8. Write to a temporary file in `.pact/tmp/`.
9. `fsync` when supported.
10. Atomically rename into `.pact/objects/sha256/...`.
11. Re-read the object, recompute its ID, and verify its signature.
12. Update or rebuild the disposable index.

Use:

```bash
pact commit \
  --repo . \
  --key-file ~/.config/pact/keys/operator.json \
  --events event-batch.json
```

Do not update a head pointer before the object has been durably written and
verified. Local refs are conveniences; if a ref update fails, the object still
exists and heads can be recomputed.

## Correct, invalidate, or add later knowledge

Never edit the original object.

Use a domain correction event or a PACT control event that references the target:

```text
assertion.invalidated
observation.corrected
decision.superseded
authority.revoked
evidence.resolve_failed
evidence.hash_mismatch
artifact.unavailable
```

A correction should state:

- the exact target event reference;
- what new observation or reasoning changed the conclusion;
- supporting evidence references when available;
- whether the new event supersedes, invalidates, or merely qualifies the target.

Do not claim that a correction makes the original event disappear. A projection
must be able to show both the historical assertion and its later disposition.

## Verify a ledger

Run verification after append, sync, key changes, and before checkpointing:

```bash
pact verify --repo . --strict
```

Verification must check, at minimum:

1. object path and content digest agree;
2. canonical envelope and format version are valid;
3. body digest agrees with the canonical body;
4. Ed25519 signature verifies and key ID matches the embedded public key;
5. event-local IDs are unique;
6. parent commits exist and share the commit namespace;
7. the parent graph is acyclic;
8. referenced local events exist;
9. cross-commit event references are well formed and resolvable when the replica
   claims completeness;
10. delegation references are structurally valid and causally prior;
11. known causal revocations are reported;
12. trusted-root and delegation-based authorization status is reported
    separately from signature status;
13. checkpoint frontiers reference existing heads and exact policy/schema IDs;
14. index rows can be regenerated from objects.

Do not resolve every external evidence reference during ordinary verification.
Evidence checking is lazy unless the user explicitly requests evidence access.
When an attempted resolution fails or a digest differs, append a new observation
only after the failed check itself has been completed and captured.

## Query history

Queries operate on objects or the rebuildable index. They do not mutate history.

Typical commands:

```bash
pact heads --repo .
pact log --repo . --namespace org/example/project/widget
pact query --repo . --type build.test.executed
pact show --repo . sha256:<object-id>
```

A query result must identify its view boundary:

- current local replica;
- named checkpoint;
- exact frontier supplied by the caller;
- namespace filter;
- policy, if acceptance is being reported.

Never describe a local replica’s incomplete frontier as globally complete.

## Projection contract

A projection converts immutable history into current interpreted state.

A conforming projection:

1. names an exact checkpoint or explicit frontier;
2. names an exact content-addressed policy artifact;
3. identifies the projector implementation/version or digest;
4. verifies every reachable commit before interpreting it;
5. preserves commit atomicity;
6. treats the DAG as a partial order;
7. does not infer semantic order from arbitrary hash or timestamp tie-breakers;
8. evaluates authorization and policy acceptance explicitly;
9. retains unresolved concurrent claims rather than silently selecting one;
10. emits a deterministic output for identical inputs;
11. creates an output manifest containing source frontier, policy, schemas,
    projector, output digest, and generation metadata;
12. records `artifact.produced` before another workflow treats the result as
    ledger-backed evidence.

A mutable CSV or report may be the canonical **human interface** for a workflow,
but it is a projection, not the immutable historical source.

## Create a signed checkpoint

A checkpoint identifies an exact official cut without requiring a central write
service.

```bash
pact checkpoint \
  --repo . \
  --key-file ~/.config/pact/keys/operator.json \
  --scope org/example/project/widget \
  --policy-ref sha256:<policy-digest> \
  --authority-epoch 148
```

Before signing a checkpoint:

- verify the entire reachable frontier;
- enumerate all namespaces included by scope;
- include every selected head, not merely the newest timestamp;
- record the exact policy and schema set;
- record the authority epoch or authority context;
- link the previous checkpoint when maintaining a checkpoint series;
- ensure the signer has checkpoint authority for the scope.

The checkpoint signature means the actor attests to this frontier and context.
It does not make every contained assertion true.

## Sync replicas

Sync is set reconciliation over immutable objects:

```text
exchange known object IDs / frontiers
        ↓
identify missing objects
        ↓
transfer exact bytes
        ↓
verify digest, envelope, signature, and dependencies
        ↓
atomically admit valid objects
        ↓
union DAG and rebuild index
```

The current CLI has no replica synchronization command. If a task requires
sync, stop and report that operation as unavailable; do not copy canonical
object files by hand and imply that PACT admitted them.

Sync rules:

- never overwrite an existing object ID with different bytes;
- never delete a local branch because a remote lacks it;
- admit authentic-but-unauthorized objects when local policy permits historical
  retention, but mark them accordingly;
- reject corrupt or malformed objects from the canonical store;
- fetch parent dependencies before declaring a frontier complete;
- do not assume evidence objects travel with ledger objects;
- rebuild the index after import;
- verify before creating a checkpoint that includes imported history.

The transport may be a directory, SSH, HTTP, S3-compatible store, removable
media, or another adapter. Transport must not redefine object identity or
validation semantics.

## Authority model

The trust hierarchy may be:

```text
human/admin roots
      ↓
organization authority
      ↓
project authority
      ↓
service identity
      ↓
ephemeral agent key
```

Delegation payloads should bound:

- delegate key ID;
- namespace patterns;
- event-type patterns;
- authority epoch;
- lease or acceptance window;
- whether subdelegation is allowed;
- maximum subdelegation depth;
- optional checkpoint or policy capabilities.

Revocation is a new signed event. It does not erase prior commits. Offline
commits created after authority becomes stale may remain authentic but should be
rejected by default checkpoint/projection policy unless an authorized actor
explicitly accepts them.

Because wall-clock timestamps are advisory, lease enforcement is ultimately an
acceptance-policy decision made by an authorized checkpointing or projection
context. Do not present actor-supplied time as cryptographic proof of when a
commit existed.

## Schema and policy lifecycle

The core envelope is versioned by PACT. Domain payload schemas and projection
policies are external content-addressed artifacts.

Use signed events such as:

```text
pact.schema.registered
pact.schema.supersedes
pact.schema.compatible_with
pact.policy.registered
pact.policy.activated
```

For v1 simplicity:

- registration with exact digest is required before a domain schema or policy is
  treated as governed;
- compatibility declarations are optional metadata;
- the core does not attempt automatic schema migration;
- old events remain interpreted under their original schema;
- unknown or unavailable schemas do not invalidate object integrity, but they
  can block semantic validation and projection acceptance.

## Derived artifacts

A derived artifact remains external. Record it with an event containing:

```text
artifact kind and stable subject
external reference
sha256 digest
media type
source checkpoint or frontier
policy digest
projector digest/version
derivation timestamp (advisory)
```

Another workflow may use that artifact as `derived` evidence. It must retain the
chain back to the underlying checkpoint and policy rather than presenting the
artifact as a primary observation.

If the artifact later disappears, append `pact.artifact.resolve_failed` or a
domain equivalent when an actual access attempt fails.

## Integrating another skill or workflow

PACT must remain domain-neutral. An integrating skill owns its event vocabulary,
payload schemas, projection policy, and human-facing artifact.

Use this migration sequence:

1. **Shadow** — keep the current mutable ledger authoritative while emitting PACT
   events for the same operations.
2. **Compare** — replay PACT into a projection and compare it to the existing
   artifact; record mismatches rather than hiding them.
3. **Cut over history** — make PACT the authoritative history while retaining the
   existing CSV/report/dashboard as the canonical human-facing projection.
4. **Enable concurrency** — allow independent agents to append signed commits;
   keep projection writes single-output and deterministic.
5. **Checkpoint** — publish signed official cuts for releases, audits, or handoff.

Do not force all integrating skills to share event types. Share only the PACT
envelope, identity, authority, DAG, evidence, checkpoint, and projection
contracts.

## Stop conditions and safety gates

Stop mutation and report the exact blocker when any of these occurs:

- existing object digest mismatch;
- invalid signature on a canonical object;
- unsupported major format version;
- missing parent on a replica expected to be complete;
- cycle in the commit graph;
- two different byte sequences claiming the same object ID;
- private key found inside the project ledger;
- secret-like material in a proposed immutable payload;
- ambiguous project root or competing ledgers;
- signing key does not match the intended actor;
- required delegation is absent, expired by policy, revoked, or outside scope;
- schema or policy bytes cannot be identified by exact digest when semantic
  validation is required;
- checkpoint would omit a known selected head;
- projection cannot deterministically resolve its inputs;
- sync source contains corrupt objects.

When safe, read-only queries and diagnostics may continue after a mutation gate
fails. Do not “repair” immutable history by editing object bytes.

## Completion criteria

### Record operation

Complete only when:

- event batch is schema-valid;
- no secret hazard remains;
- parents represent the actor’s observed frontier;
- body digest and signature verify;
- object ID matches persisted bytes;
- object is atomically present in the canonical store;
- post-write verification passes;
- authorization status is reported without conflating acceptance.

### Verification operation

Complete only when:

- every scanned canonical object has an integrity result;
- signatures, parents, cycles, and event references are checked;
- incomplete-replica assumptions are explicit;
- authorization is reported as authorized, unauthorized, or indeterminate;
- evidence availability is not claimed unless actually checked;
- index rebuild or comparison succeeds when requested.

### Projection operation

Complete only when:

- frontier/checkpoint, policy, schema set, and projector are exact;
- all reachable inputs pass required verification gates;
- concurrent unresolved claims are visible or policy-resolved explicitly;
- output is deterministic and hashed;
- provenance manifest is produced;
- `artifact.produced` is appended when the output becomes reusable evidence.

### Sync operation

Complete only when:

- no existing object was overwritten;
- every imported object passed admission checks;
- missing dependencies are identified;
- DAG union and index rebuild complete;
- resulting heads are reported per namespace;
- no claim of global completeness exceeds the replicas actually compared.

## Final response contract

Return the smallest applicable report containing:

1. operation performed and project namespace;
2. object, commit, event, or checkpoint IDs created or inspected;
3. frontier or checkpoint boundary;
4. integrity and authenticity result;
5. authorization result and proof source;
6. projection policy and acceptance result, when relevant;
7. evidence resolution status only for evidence actually accessed;
8. imported/exported object counts for sync;
9. blockers, rejected objects, unresolved concurrency, or indeterminate states;
10. exact paths to generated artifacts or external refs without exposing secrets.

Never summarize all layers as merely “valid.”

## Anti-patterns

Never:

- use a mutable CSV, SQLite row, server pointer, or Git branch as the immutable
  ledger itself;
- update a prior event to correct it;
- let latest timestamp win by default;
- assume a signature makes an assertion true;
- assume an authenticated actor was authorized;
- assume authorization means a projection accepted the event;
- place raw evidence or secrets in immutable payloads;
- use one global sequence number to serialize unrelated writers;
- discard concurrent branches during sync;
- create a checkpoint from an unverified or partial frontier without saying so;
- silently migrate old payloads to a new schema;
- let a projection infer causality from lexical or hash ordering;
- treat local trust configuration as self-authenticating ledger history;
- delete authentic history because authority later changed;
- admit corrupt objects merely to preserve them;
- claim evidence remains available without attempting resolution;
- claim the local replica represents all history without a completeness basis;
- make another skill adopt PACT’s domain ontology instead of defining its own.
