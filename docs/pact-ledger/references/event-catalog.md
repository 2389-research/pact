# PACT Core Event Catalog

Domain workflows should define their own event vocabularies. This catalog
reserves a small set of cross-cutting PACT control and provenance events.

All payloads should have exact schemas. Shapes below are recommendations for v1.

## `pact.authority.delegated`

**Kind:** `control`

Grants scoped authority to another key.

Required payload concepts:

- delegate key ID and label;
- namespace patterns;
- event-type patterns;
- authority epoch;
- lease metadata;
- subdelegation permission/depth;
- control capabilities.

## `pact.authority.revoked`

**Kind:** `control`

Records revocation of a delegation or key.

Required payload concepts:

- target delegation event reference and/or key ID;
- reason;
- optional replacement delegation;
- authority epoch.

## `pact.authority.epoch_advanced`

**Kind:** `control`

Declares a new scoped authority epoch.

Required payload concepts:

- previous epoch;
- new epoch;
- scope;
- carry-forward policy for delegations.

## `pact.schema.registered`

**Kind:** `control`

Registers a domain payload schema artifact.

Required payload concepts:

- schema name;
- artifact reference and digest;
- media type;
- supported event types;
- optional semantic version.

## `pact.schema.supersedes`

**Kind:** `control`

Declares that one schema succeeds another. This is metadata, not automatic
migration.

Required payload concepts:

- predecessor digest;
- successor digest;
- compatibility classification;
- migration note reference.

## `pact.schema.compatible_with`

**Kind:** `control`

Declares a compatibility relationship between schema artifacts.

Required payload concepts:

- schema digests;
- direction of compatibility;
- scope/limitations;
- evidence or conformance reference.

## `pact.policy.registered`

**Kind:** `control`

Registers a projection policy artifact.

Required payload concepts:

- policy name;
- artifact reference and digest;
- supported scopes/event schemas;
- projector/runtime requirements.

## `pact.policy.activated`

**Kind:** `decision`

Selects a registered policy for a namespace scope and authority epoch.

Required payload concepts:

- policy digest;
- scope;
- predecessor policy, if any;
- authority epoch;
- activation rationale.

## `pact.assertion.invalidated`

**Kind:** `decision` or `assertion`, depending on domain policy

States that a prior assertion should no longer be accepted under a named reason.

Required payload concepts:

- target event reference;
- reason code and explanation;
- evidence references;
- whether the target is contradicted, unsupported, stale, or outside scope.

The invalidating actor must have authority defined by the domain policy. This is
not a universal admin override.

## `pact.evidence.resolve_failed`

**Kind:** `observation`

Records an actual failed attempt to resolve prior evidence.

Required payload concepts:

- target event and evidence index or digest;
- attempted reference;
- failure class such as `not_found`, `access_denied`, `timeout`, or
  `unsupported_scheme`;
- non-secret diagnostic summary;
- attempt timestamp (advisory).

Do not append this merely because evidence might disappear someday.

## `pact.evidence.hash_mismatch`

**Kind:** `observation`

Records that resolved bytes did not match the recorded digest.

Required payload concepts:

- target event/evidence reference;
- expected digest;
- observed digest;
- locator;
- safe diagnostic summary.

Do not replace the original evidence digest.

## `pact.artifact.produced`

**Kind:** `action`

Records successful creation of an external derived artifact.

Required payload concepts:

- stable artifact subject and kind;
- artifact reference, digest, and media type;
- projection manifest reference/digest;
- source checkpoint/frontier;
- policy digest;
- projector identity.

## `pact.artifact.resolve_failed`

**Kind:** `observation`

Records an actual failed attempt to access a previously produced artifact.

Required payload concepts mirror `pact.evidence.resolve_failed`.

## Domain event naming

Recommended pattern:

```text
<domain>.<noun>.<past-tense-verb>
```

Examples:

```text
build.test.executed
release.candidate.approved
research.source.observed
audit.finding.proposed
operation.service.restarted
```

Names should describe what was recorded, not command the future. Prefer
`release.candidate.approved` over `approve_release`.

## Event-kind guidance

| Kind | Use for | Example |
|---|---|---|
| `observation` | directly witnessed result | command exited 1 |
| `assertion` | interpretation or claim | test failure indicates regression |
| `decision` | policy or human judgment | release rejected |
| `action` | completed operation | artifact written |
| `control` | ledger governance | delegation granted |
