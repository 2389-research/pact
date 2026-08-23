# Adopting PACT in an Existing Workflow

## 1. Identify the current “ledger”

Find the mutable artifact currently carrying history and current state:

- CSV verification matrix;
- report with finding statuses;
- database table;
- JSON state file;
- issue tracker;
- task board;
- release manifest;
- agent scratchpad.

Separate what it currently mixes:

```text
historical observations
current interpretation
human presentation
raw evidence
workflow control
```

PACT should replace only the historical observation/assertion layer. Existing
artifacts can remain excellent projections and user interfaces.

## 2. Define stable subjects

Create a stable identity scheme for domain entities before defining events.
Examples:

```text
requirement/<stable-id>
finding/<stable-id>
job/<stable-id>
release/<version>
source/<normalized-id>
artifact/<kind>/<stable-id>
```

Do not derive subject identity from row position or current display text.

## 3. Define a minimal event vocabulary

Start with events corresponding to actual state-changing observations and
judgments. Avoid mirroring every function call.

A good first vocabulary usually includes:

- entity discovered/registered;
- evidence observed;
- execution attempted/completed;
- result asserted;
- defect/finding proposed;
- correction or invalidation;
- decision accepted/rejected;
- artifact produced.

Keep control events such as authority and policy under PACT's reserved catalog.

## 4. Define payload schemas

For each event type:

- specify required fields;
- distinguish IDs from display text;
- prohibit raw secrets and evidence bytes;
- define enum values exactly;
- state which fields are semantic;
- state whether arrays are ordered;
- include schema version and content digest;
- provide positive and negative examples.

Do not over-generalize into one “everything event” payload.

## 5. Define projection policy

Document:

- which events affect current state;
- required authorization;
- supersession and correction rules;
- conflict behavior;
- evidence requirements;
- incomplete-state behavior;
- deterministic output order;
- artifact schema.

The projection policy replaces hidden “latest row wins” assumptions.

## 6. Migrate in four stages

### Stage 1 — Shadow

Continue writing the existing artifact. Also emit PACT events for each operation.
Do not change workflow authority yet.

Measure:

- event coverage;
- schema friction;
- secret hazards;
- whether stable subjects work;
- object and index growth.

### Stage 2 — Compare

Build a projection that regenerates the current artifact from PACT. Compare it
against the existing writer after every run.

Record mismatches as diagnostics. Do not “normalize them away” until the cause is
understood.

### Stage 3 — Cut over history

Make PACT the source of historical truth. The former ledger becomes a generated
projection.

Keep one deterministic projection writer. Multiple workers may append events,
but they should not independently overwrite the human-facing artifact.

### Stage 4 — Enable distributed actors

Add service and ephemeral-agent keys, scoped delegations, offline commits,
replication, and signed checkpoints.

Do this only after single-replica projection equivalence is stable.

## 7. Map mutable updates to append-only events

Examples:

| Mutable operation | PACT representation |
|---|---|
| change status from pending to pass | append execution observation + pass assertion |
| edit defect text | append correction/supersession event |
| delete obsolete row | append retirement/out-of-scope decision |
| replace report | produce a new artifact with new digest/policy |
| revoke agent access | append authority revocation/epoch event |
| evidence file disappears | append lazy resolve-failed observation |

## 8. Multi-agent pattern

Each worker:

1. receives a scoped namespace and event capability;
2. creates external evidence safely;
3. appends signed commits;
4. does not mutate the projection artifact;
5. returns commit and event references to the coordinator.

The coordinator:

1. syncs/collects commits;
2. verifies and evaluates authority;
3. runs deterministic projection;
4. records the derived artifact;
5. creates a checkpoint when an official cut is needed.

## 9. Legacy import

Do not fabricate detailed event history that the old artifact never recorded.

Recommended import:

1. hash and externally retain the exact legacy artifact;
2. append one `legacy.snapshot.imported` observation with artifact reference,
   digest, source date, and known limitations;
3. create one domain assertion per stable current subject only when needed;
4. label imported state as reconstructed;
5. begin native event history from the migration checkpoint.

Never imply reconstructed events occurred at their original real-world times
unless reliable source evidence establishes that.

## 10. Cutover gate

Cut over only when:

- every current domain entity maps to a stable subject;
- every mutating workflow emits the necessary events;
- schemas reject malformed/secret-bearing payloads;
- projection output matches the existing artifact across representative runs;
- correction, concurrency, and incomplete-history cases are tested;
- index rebuild reproduces query results;
- checkpoint generation works;
- rollback means switching the projection consumer, not rewriting ledger objects.

## 11. Common migration mistakes

Avoid:

- copying every log line into semantic events;
- preserving row numbers as permanent identity;
- making one enormous generic payload schema;
- treating existing timestamps as causal truth;
- enabling distributed writers before projection equivalence;
- calling old mutable data “immutable” because it was imported;
- using a Git commit alone as actor authorization;
- putting raw legacy files inside signed event payloads;
- deleting PACT history when the old artifact no longer shows it.
