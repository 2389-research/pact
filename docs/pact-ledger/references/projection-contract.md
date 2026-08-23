# PACT Projection Contract

## 1. Purpose

A projection derives a current, useful interpretation from immutable ledger
history. It may produce a CSV, report, dashboard model, release state, queue,
knowledge graph, or another deterministic artifact.

A projection is where PACT answers:

> Given this exact history, authority context, and policy, what should this
> workflow currently accept?

The ledger itself does not answer that question.

## 2. Projection identity

A reproducible projection is identified by:

```text
frontier or checkpoint ID
policy artifact digest
schema artifact digests
projector implementation digest/version
projection parameters
```

Do not identify a projection merely as “latest.” Friendly labels may resolve to
an exact checkpoint, but the output manifest must store the immutable IDs.

## 3. Required inputs

A projection invocation must declare:

- project/store location;
- scope and namespace filter;
- checkpoint ID or explicit frontier;
- policy artifact reference and digest;
- schema set or schema-resolution rules;
- projector implementation identity;
- parameters that can change output;
- expected output format and destination.

## 4. Preflight

Before interpretation:

1. resolve every frontier head;
2. load all reachable commit ancestors;
3. verify object IDs and canonical encoding;
4. verify body digests and signatures;
5. verify the DAG is acyclic and parents are complete;
6. validate event envelopes;
7. resolve exact schemas needed for semantic validation;
8. evaluate authorization or label it indeterminate;
9. verify policy bytes match the requested digest;
10. refuse to run when required inputs are missing or ambiguous.

A policy may explicitly permit authentic-but-unauthorized or partially validated
history for an audit view. That permission must be visible in the output
manifest.

## 5. Partial-order processing

The commit graph is a partial order. A projector may create deterministic
processing batches:

```text
batch 0: all genesis commits
batch 1: commits whose parents are in earlier batches
...
```

Concurrent commits in the same batch may be sorted by object ID only to make
execution deterministic. That tie-breaker is not semantic ordering.

Policies that depend on “latest” must define latest using explicit causal rules,
checkpoints, supersession, domain sequence values, or another signed domain
fact—not wall-clock comparison alone.

## 6. Atomic commit rule

A projection either incorporates all accepted events in a commit or rejects the
commit as a unit when authorization or envelope validity applies at commit
scope.

A policy may mark individual event semantics irrelevant, but it must not process
an event from a structurally invalid or unauthorized atomic commit while
pretending the commit itself was accepted.

## 7. Event-state model

A useful internal decision record is:

```text
event_ref
integrity_status
authenticity_status
authorization_status
schema_status
policy_disposition     accepted | rejected | irrelevant | unresolved
reason_codes[]
causal_context[]
supersession_context[]
```

Keep this decision trace available for audit, even when the human-facing
artifact contains only current state.

## 8. Supersession and invalidation

PACT core records `supersedes` links and correction events but does not assign
universal meaning.

A projection policy must define:

- which event types may supersede which other types;
- whether the superseding actor needs additional authority;
- whether a supersession must be causally after the target;
- how concurrent corrections are represented;
- whether invalidated history remains visible in audit output;
- whether a correction replaces, qualifies, or merely disputes the target.

Default safe behavior: retain both claims and mark the subject unresolved when
conflicting authorized corrections are concurrent.

## 9. Conflict handling

A policy should define behavior for:

- concurrent incompatible assertions;
- authorized versus unauthorized claims;
- multiple accepted decisions on one subject;
- missing schemas;
- unavailable evidence;
- digest-mismatched evidence;
- causal revocation;
- expired or indeterminate leases;
- checkpoint policy changes;
- duplicated semantic facts in separate commits.

Never silently select the event with the newest timestamp.

## 10. Evidence use

A projection may operate without resolving evidence when policy accepts signed
semantic assertions as input.

When policy requires evidence:

1. resolve the exact reference;
2. compute the digest;
3. compare to the recorded digest;
4. record the resolution result in projection diagnostics;
5. append a ledger observation only when the workflow is authorized to record
   the failed access or mismatch.

Evidence availability at projection time does not alter the original event.

## 11. Policy artifact

A policy artifact should state:

- policy name and semantic version;
- exact scope and supported event schemas;
- required validation gates;
- authorization rules;
- event dispositions;
- subject reducers;
- concurrency and conflict rules;
- supersession rules;
- evidence requirements;
- artifact output schema;
- deterministic ordering rules used only for serialization;
- failure and incomplete-output behavior.

Policy source code may be the artifact. A manifest may hash the executable,
configuration, and dependencies as one bundle.

## 12. Policy activation

Recommended events:

```text
pact.policy.registered
pact.policy.activated
```

Activation should include:

- policy digest;
- namespace scope;
- activation authority;
- optional predecessor policy;
- migration or compatibility note;
- effective authority epoch.

A checkpoint names the policy actually used. Historical projections do not
change when a new policy becomes active.

## 13. Deterministic output

For identical inputs, a conforming projector produces byte-identical output or a
byte-identical canonical data model before presentation formatting.

Avoid nondeterminism from:

- current time embedded in core output;
- unordered map iteration;
- filesystem enumeration order;
- random IDs;
- locale-dependent formatting;
- network lookups not represented as evidence;
- environment variables not captured as parameters;
- version-floating dependencies.

Advisory generation time belongs in a separate manifest field and should not
change the canonical artifact digest unless policy explicitly requires it.

## 14. Output manifest

Recommended manifest:

```json
{
  "format": "pact/projection-manifest/v1",
  "scope": "org/example/project/widget",
  "checkpoint": "sha256:...",
  "frontier_digest": "sha256:...",
  "policy_ref": "sha256:...",
  "schema_refs": ["sha256:..."],
  "projector_ref": "sha256:...",
  "parameters": {},
  "artifact": {
    "ref": "file:///external/output/report.json",
    "digest": "sha256:...",
    "media_type": "application/json"
  },
  "counts": {
    "commits_seen": 0,
    "events_seen": 0,
    "accepted": 0,
    "rejected": 0,
    "irrelevant": 0,
    "unresolved": 0
  },
  "generated_at": "2026-08-23T15:04:05Z"
}
```

`generated_at` is advisory. The manifest should separate deterministic core
fields from nondeterministic presentation metadata if byte reproducibility is
required.

## 15. Recording the artifact

After successful generation, append an event such as:

```text
pact.artifact.produced
```

Payload should include the artifact digest/reference, manifest digest/reference,
checkpoint, policy, projector, output kind, and stable artifact subject.

Do not record `artifact.produced` before the file is fully written, hashed, and
available at the stated reference.

## 16. Projection evolution

When a new policy or projector produces a different result from the same
checkpoint:

- retain both artifacts;
- record each with its own policy/projector identity;
- do not rewrite the older artifact event;
- optionally append a domain decision selecting one artifact for current use;
- preserve comparison diagnostics as external evidence.

## 17. Incomplete projections

A projection that intentionally tolerates missing or indeterminate inputs must
label itself `INCOMPLETE` and list:

- missing objects;
- missing schemas;
- unresolved authorization;
- unavailable required evidence;
- unresolved concurrent claims;
- policy rules skipped;
- subjects whose current state cannot be derived.

Never emit a normal-looking complete artifact and hide these conditions in logs.
