# PACT v1 Object Model

This document is the normative human-readable model for the JSON Schemas in
`schemas/` and the bundled reference CLI.

## 1. Canonical JSON profile

PACT v1 signed objects use `pact-json-v1`:

1. input must parse as JSON with no duplicate object keys;
2. strings and object keys are normalized to Unicode NFC;
3. object keys are sorted lexicographically after normalization;
4. arrays retain semantic order except fields explicitly normalized below;
5. whitespace is omitted outside strings;
6. UTF-8 is used without a byte-order mark;
7. floating-point values, NaN, and infinities are forbidden;
8. integers must be within the interoperable signed range
   `[-9007199254740991, 9007199254740991]`;
9. `parents`, checkpoint `heads`, `schema_refs`, and event `tags` are sorted and
   deduplicated by the builder;
10. object IDs and signature fields are lowercase where their format requires it.

The reference CLI is the executable conformance definition for this profile in
v0.1. A production implementation should add cross-language canonicalization
vectors before claiming interoperability.

## 2. Digest and key identifiers

### SHA-256 digest

```text
sha256:<64 lowercase hexadecimal characters>
```

### Ed25519 key ID

```text
ed25519:sha256:<64 lowercase hexadecimal characters>
```

The key ID digest is computed over the raw 32-byte public key.

### Event reference

```text
pact:event:sha256:<commit-hex>#<event-local-id>
```

Within the same commit, `local:<event-local-id>` may be used in `caused_by`.
Persisted output should expand local references when presenting stable external
identifiers.

## 3. Signed commit object

```json
{
  "format": "pact/commit/v1",
  "body": {
    "namespace": "org/example/project/widget/audit",
    "parents": [
      "sha256:..."
    ],
    "actor": {
      "key_id": "ed25519:sha256:...",
      "label": "service/auditor"
    },
    "authority": {
      "delegation_ref": "pact:event:sha256:...#delegate",
      "epoch": "org/example/epoch/148",
      "lease_ref": "pact:event:sha256:...#lease"
    },
    "observed_at": "2026-08-23T15:04:05Z",
    "correlation_id": "run/01J...",
    "metadata": {
      "producer": "pact-reference-cli/0.1.0"
    },
    "events": []
  },
  "body_digest": "sha256:...",
  "signature": {
    "algorithm": "ed25519",
    "key_id": "ed25519:sha256:...",
    "public_key": "<unpadded base64url>",
    "value": "<unpadded base64url>"
  }
}
```

### Commit body fields

| Field | Required | Contract |
|---|---:|---|
| `namespace` | yes | Stable hierarchical namespace. |
| `parents` | yes | Sorted unique commit IDs in the same namespace. Empty only for a namespace genesis. |
| `actor.key_id` | yes | Must equal signature key ID. |
| `actor.label` | yes | Human-readable advisory label; not identity authority. |
| `authority` | yes | Object with nullable `delegation_ref`, `epoch`, and `lease_ref`. |
| `observed_at` | yes | RFC 3339-style actor timestamp; advisory only. |
| `correlation_id` | no | Domain/run grouping ID; not causal proof. |
| `metadata` | yes | Small non-secret producer metadata object. |
| `events` | yes | One or more semantic events, sorted by `local_id` by the builder. |

## 4. Semantic event envelope

```json
{
  "local_id": "e1",
  "kind": "observation",
  "type": "build.test.executed",
  "subject": "build/linux-amd64/42",
  "schema_ref": "sha256:...",
  "payload": {},
  "evidence": [],
  "caused_by": [],
  "supersedes": [],
  "tags": []
}
```

### Event fields

| Field | Required | Contract |
|---|---:|---|
| `local_id` | yes | Unique within commit; `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`. |
| `kind` | yes | `observation`, `assertion`, `action`, `decision`, or `control`. |
| `type` | yes | Namespaced semantic type; lowercase segments separated by dots, slashes, `_`, or `-`. |
| `subject` | yes | Stable domain identity; 1–512 UTF-8 characters after NFC. |
| `schema_ref` | yes | Exact `sha256:` artifact ID or versioned `pact:core/...` schema. |
| `payload` | yes | Domain object validated under `schema_ref`; no floats or secrets. |
| `evidence` | yes | Zero or more external evidence references. |
| `caused_by` | yes | Event refs or same-commit `local:` refs establishing domain causality. |
| `supersedes` | yes | Prior event refs explicitly corrected or replaced. |
| `tags` | yes | Sorted unique discovery labels; never authority or semantics. |

A `supersedes` reference is an assertion made by this event. Projection policy
chooses how to interpret it.

## 5. Evidence reference

```json
{
  "ref": "file:///external/evidence/run-42.log",
  "digest": "sha256:...",
  "media_type": "text/plain",
  "role": "primary",
  "redacted": false,
  "description": "Exact command output captured by the test runner"
}
```

| Field | Required | Contract |
|---|---:|---|
| `ref` | yes | External locator with no embedded credentials. |
| `digest` | yes | Digest of exact bytes observed. |
| `media_type` | yes | Descriptive media type. |
| `role` | yes | `primary`, `supporting`, or `derived`. |
| `redacted` | no | Whether referenced content is a redacted representation. |
| `description` | no | Short non-secret description. |

PACT does not require the locator to be globally resolvable. A local file URI,
artifact-store URI, repository URI, or connector-specific opaque locator is
valid when the relevant workflow knows how to resolve it.

## 6. Signed checkpoint object

```json
{
  "format": "pact/checkpoint/v1",
  "body": {
    "scope": "org/example/project/widget",
    "frontier": [
      {
        "namespace": "org/example/project/widget/audit",
        "heads": ["sha256:..."]
      }
    ],
    "policy_ref": "sha256:...",
    "schema_refs": ["sha256:..."],
    "authority_epoch": "org/example/epoch/148",
    "previous_checkpoint": "sha256:...",
    "actor": {
      "key_id": "ed25519:sha256:...",
      "label": "human/release-manager"
    },
    "observed_at": "2026-08-23T15:04:05Z",
    "metadata": {
      "purpose": "release/1.4.0"
    }
  },
  "body_digest": "sha256:...",
  "signature": {
    "algorithm": "ed25519",
    "key_id": "ed25519:sha256:...",
    "public_key": "...",
    "value": "..."
  }
}
```

A checkpoint is not a commit and does not become a parent in a namespace DAG.
It is a signed statement about one or more DAG frontiers.

## 7. Key file

The reference CLI private key file is not a ledger object:

```json
{
  "format": "pact/key/v1",
  "algorithm": "ed25519",
  "actor": "human/operator",
  "key_id": "ed25519:sha256:...",
  "public_key": "...",
  "private_key": "...",
  "created_at": "2026-08-23T15:04:05Z"
}
```

It must be owner-readable/writable only and must never be committed to the
project.

## 8. Project format file

`.pact/format.json` is local configuration, not signed history:

```json
{
  "format": "pact/store/v1",
  "default_namespace": "org/example/project/widget",
  "created_at": "2026-08-23T15:04:05Z",
  "canonicalization": "pact-json-v1",
  "hash_algorithm": "sha256",
  "signature_algorithm": "ed25519"
}
```

## 9. Local trust file

`.pact/trust.json` bootstraps verification:

```json
{
  "format": "pact/trust/v1",
  "roots": [
    {
      "key_id": "ed25519:sha256:...",
      "actor": "human/operator",
      "public_key": "...",
      "added_at": "2026-08-23T15:04:05Z"
    }
  ]
}
```

This file is local policy. Its initial contents cannot be validated from the
ledger it is used to trust.

## 10. Normalization rules

Before signing, the builder:

- NFC-normalizes all strings and keys;
- sorts and deduplicates `parents`, `schema_refs`, checkpoint `heads`, tags,
  namespace patterns, and event-type patterns;
- sorts events by `local_id`;
- preserves `frontier` order by sorting on namespace;
- preserves payload array order because domain schemas may make it semantic;
- rejects duplicate keys after Unicode normalization;
- rejects unsupported numeric values and secret hazards.

A verifier verifies the stored canonical bytes; it does not silently normalize a
noncanonical object into validity.
