# PACT Threat Model

## 1. Security goals

PACT aims to provide:

- tamper evidence for canonical object bytes;
- attribution of commits and checkpoints to signing keys;
- durable causal history;
- scoped authority evaluation;
- reproducible state boundaries;
- safe union of independently created history;
- explicit provenance for derived artifacts;
- clear separation between cryptographic validity and semantic acceptance.

## 2. Non-goals

PACT v1 does not provide:

- confidentiality for ledger payloads;
- secure deletion or cryptographic erasure;
- guaranteed availability of evidence or replicas;
- proof that an authorized assertion is factually correct;
- trusted real-world time;
- global consensus or total ordering;
- protection after all trusted roots are compromised;
- correctness of projection code or policy;
- automatic prevention of all sensitive metadata leakage;
- resistance to unlimited storage or query denial-of-service.

## 3. Assets

Protect:

- private signing keys;
- trusted-root configuration;
- canonical object bytes;
- namespace and authority boundaries;
- schema and policy artifact identity;
- checkpoint signer integrity;
- projection implementation and output digest;
- external evidence access controls;
- user understanding of each validation layer.

## 4. Adversaries and failures

Consider:

- an unauthenticated actor injecting objects;
- an authenticated but unauthorized agent;
- a buggy authorized agent signing false claims;
- a compromised delegated key;
- a compromised root key;
- a malicious or faulty sync peer;
- accidental object corruption;
- malicious schema or policy substitution;
- a projection that ignores concurrent contradictions;
- evidence disappearance or mutation;
- secret material accidentally placed in an immutable payload;
- clock skew or timestamp forgery;
- partial replica presented as complete;
- object flooding and pathological DAGs;
- hash or signature algorithm obsolescence.

## 5. Main controls

### Object tampering

Control: content-addressed paths, body digest, signature, post-write verification,
and immutable admission.

Residual risk: a compromised verifier or hash/signature implementation can
misreport validity.

### Unauthorized writes

Control: scoped delegations, explicit event-type capabilities, epochs, leases,
causal revocation, and policy rejection.

Residual risk: authentic unauthorized commits may consume storage and confuse
poorly designed queries. User interfaces must display authority status.

### False authorized assertions

Control: evidence references, independent agents, review/quorum projection
rules, correction events, and checkpoints.

Residual risk: PACT cannot cryptographically prove domain truth.

### Private-key compromise

Control: short-lived keys, narrow capability scope, no subdelegation by default,
hardware-backed roots, revocation, epoch advancement, and high-impact approval
policies.

Residual risk: commits signed before detection may be indistinguishable from
legitimate activity without additional evidence.

### Root compromise

Control: multi-root or quorum policy, offline/hardware roots, explicit rotation,
independent checkpoints, and out-of-band recovery.

Residual risk: a sole compromised root can delegate broad authority. PACT cannot
repair trust without an external recovery basis.

### Malicious sync peer

Control: recipient-side digest/signature/schema checks, atomic admission, no
overwrite, dependency validation, and canonical object IDs.

Residual risk: valid but unwanted objects and object-flooding attacks. Production
systems need quotas and admission policy.

### Evidence loss or mutation

Control: digest references and lazy failure/mismatch observations.

Residual risk: unavailable evidence may prevent later independent verification.
PACT preserves provenance, not availability.

### Secret leakage

Control: evidence externalization, secret scanning, payload minimization, and
hard refusal of obvious credentials/private keys.

Residual risk: heuristics cannot identify every sensitive value. Operators must
review schemas and payloads. Because v1 has no encrypted payload model, leaked
content is difficult to remediate without abandoning or access-restricting a
replica.

### Timestamp manipulation

Control: DAG causality is authoritative; timestamps are advisory; checkpoints
attest acceptance, not exact creation time.

Residual risk: human displays may still overinterpret timestamps. UI must label
clock data appropriately.

### Projection substitution

Control: hash policy and projector artifacts; include both in checkpoints and
output manifests; record artifact digest.

Residual risk: a malicious but authorized policy can accept bad history. Policy
approval is a governance problem.

### Partial replica confusion

Control: strict verification, missing-parent failures, explicit frontier labels,
and no global completeness claims without a checkpoint/sync basis.

Residual risk: a complete-looking subset can still omit independent namespaces
unless scope is explicit.

## 6. Secret handling policy

Never put these in PACT v1 objects:

- private keys;
- API secrets or bearer tokens;
- passwords;
- session cookies;
- authorization headers;
- unredacted customer records;
- evidence blobs containing sensitive data;
- credential-bearing URLs.

Use:

- environment-variable names;
- redacted identifiers;
- access-controlled external artifact locators;
- digests;
- non-sensitive summaries.

If a secret is discovered in a persisted immutable object:

1. stop replication;
2. rotate/revoke the exposed credential immediately;
3. record a non-secret incident/correction event in an uncompromised ledger when
   policy permits;
4. access-restrict or retire affected replicas;
5. do not pretend that appending a correction removed the secret bytes;
6. follow organizational incident-response and retention policy.

## 7. Denial-of-service controls for production

The reference implementation is intentionally small. Production deployments
should add:

- object-size and event-count limits;
- maximum parent count;
- maximum graph depth per request;
- schema and payload size limits;
- namespace quotas;
- per-actor rate limits;
- bounded signature verification concurrency;
- cycle-detection limits;
- sync object manifests and batching;
- quarantine limits;
- index transaction limits;
- cancellation and timeouts.

These controls must not mutate or silently drop already admitted canonical
history.

## 8. Algorithm agility

PACT v1 fixes SHA-256 and Ed25519 for simplicity. A later major format may add
new algorithms.

Migration should use signed bridge/checkpoint objects that identify both old and
new frontiers. Do not silently reinterpret old IDs under a new algorithm.

## 9. Audit checklist

Before production use, verify:

- keys are stored outside repositories and backups;
- root bootstrap process is documented;
- namespace patterns and event capabilities are tested;
- no default policy accepts `indeterminate` authorization;
- checkpoints require appropriate authority;
- projection artifacts include exact policy/projector IDs;
- evidence access is least-privilege;
- secret scanning runs before signing;
- sync imports verify before admission;
- object and graph resource limits exist;
- restore and reindex procedures are tested;
- root compromise and rotation drills exist.
