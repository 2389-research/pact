# PACT Authority Model

## 1. Guarantee boundary

PACT separates four questions:

1. **Did these bytes remain unchanged?** — content digest.
2. **Which key signed the body?** — Ed25519 signature.
3. **Was that key permitted to emit these events?** — trust and delegation.
4. **Should this workflow accept the events?** — projection policy.

Authority answers question 3 only.

## 2. Root of trust

A verifier begins with one or more out-of-band trusted public keys in
`.pact/trust.json` or an equivalent secure configuration.

The initial root cannot be established solely by a self-referential ledger
event. Operators must obtain it through a trusted installation, direct key
exchange, managed configuration, hardware root, or another explicit channel.

After bootstrap, the ledger may record:

- organization authority creation;
- project delegation;
- service delegation;
- ephemeral agent leases;
- root rotation;
- revocation;
- authority epoch changes.

## 3. Delegation event

Recommended event type:

```text
pact.authority.delegated
```

Recommended payload:

```json
{
  "delegate_key_id": "ed25519:sha256:...",
  "delegate_label": "agent/reviewer/01J...",
  "namespace_patterns": [
    "org/example/project/widget/audit/**"
  ],
  "event_type_patterns": [
    "audit.observation.*",
    "audit.finding.proposed"
  ],
  "epoch": "org/example/epoch/148",
  "lease": {
    "not_before": "2026-08-23T15:00:00Z",
    "not_after": "2026-08-23T18:00:00Z"
  },
  "allow_subdelegation": false,
  "max_subdelegation_depth": 0,
  "capabilities": [
    "commit"
  ]
}
```

Capabilities may include `commit`, `checkpoint`, `register_schema`,
`register_policy`, `activate_policy`, or project-defined control operations.

## 4. Namespace matching

Recommended semantics:

- exact string matches exact namespace;
- suffix `/**` matches the base namespace and all descendants;
- suffix `/*` matches exactly one descendant segment;
- no other wildcard syntax is recognized in v1.

Pattern evaluation must be deterministic and tested. Do not use host-language
regular expressions as the signed policy language unless the exact dialect is
part of the schema.

## 5. Event-type matching

Recommended semantics:

- exact type matches exactly;
- suffix `.*` matches all dot-delimited descendants;
- `*` may represent all event types only when explicitly granted.

A commit is authorized only when every event type in it is covered. Do not
partially authorize an atomic commit.

## 6. Causal requirement

The delegation event must be causally prior to the delegated commit:

- the delegation's commit is reachable from at least one parent of the delegated
  commit; and
- the commit explicitly references that delegation in `authority.delegation_ref`.

A concurrent or future delegation cannot retroactively authorize a prior commit
without an explicit later acceptance decision by policy.

## 7. Delegation chain

To validate a non-root signer:

1. verify the commit signature;
2. load the referenced delegation event;
3. verify the delegation event's enclosing commit;
4. verify that the delegation issuer was authorized to delegate;
5. match delegate key ID;
6. match namespace and every event type;
7. match epoch requirements;
8. inspect lease policy;
9. inspect causal revocations;
10. enforce subdelegation depth and capability constraints.

Stop at a locally trusted root.

## 8. Revocation

Recommended event type:

```text
pact.authority.revoked
```

Recommended payload:

```json
{
  "target_delegation_ref": "pact:event:sha256:...#delegate",
  "target_key_id": "ed25519:sha256:...",
  "reason": "service decommissioned",
  "replacement_delegation_ref": null
}
```

Revocation does not delete or make prior history unauthentic. Its effect is
interpreted relative to causality and checkpoint policy.

### Causally known revocation

If a commit's ancestry includes a revocation of its delegation, the commit is
unauthorized under the default policy.

### Concurrent offline commit

If revocation and a delegated commit are concurrent, the ledger alone cannot
prove which occurred first in real-world time. Default official checkpoint
policy should reject or quarantine the offline commit unless an authorized actor
explicitly accepts it.

## 9. Epochs

An authority epoch is a stable identifier for a bounded authority state, for
example:

```text
org/example/authority-epoch/148
```

Recommended event type:

```text
pact.authority.epoch_advanced
```

Epochs are not global sequence numbers for ordinary history. They are scoped
control identifiers used to limit stale authority and make checkpoint policy
explicit.

A new epoch may require new delegations or explicitly carry forward selected
ones. The policy must say which.

## 10. Leases and time

Actor timestamps are advisory. Therefore a delegation's `not_before` and
`not_after` cannot by themselves cryptographically prove that a commit was made
inside the interval.

Recommended v1 interpretation:

- the delegate uses the lease locally as an operational limit;
- the verifier reports whether the actor-reported time falls inside it, labeled
  advisory;
- an authorized checkpoint signer or acceptance policy decides whether to accept
  the commit under the lease;
- a commit accepted into a signed checkpoint gains an attestation that the
  checkpoint signer accepted it under that checkpoint's authority context;
- no claim is made that a trusted timestamp authority proved exact creation time.

## 11. Authorization result model

Use at least:

```text
authorized       complete valid chain to a trusted root; scope matches
unauthorized     a known rule fails: wrong key, scope, type, epoch, or causal revocation
indeterminate    proof is missing, replica is partial, schema unavailable, or lease policy needs checkpoint context
```

Include structured reasons. Do not map `indeterminate` to `authorized` for
convenience.

## 12. Authentic but unauthorized history

PACT may retain an authentic but unauthorized commit because it is still a valid
historical assertion by a known key.

Default projections should exclude it from accepted state while allowing audit
views to display it.

This is different from a corrupt object, which must not enter the canonical
store.

## 13. Root rotation

A safe root rotation should include:

1. new root public key established through a trusted channel;
2. old root signs a rotation/delegation event when available;
3. new root signs an acceptance event or checkpoint;
4. local trust configuration is updated explicitly;
5. old root revocation or retirement is recorded;
6. checkpoints state the authority epoch and accepted roots.

If the old key is compromised or unavailable, out-of-band recovery policy is
required. The ledger cannot manufacture trust from a compromised root.

## 14. Key compromise

PACT cannot prevent a stolen private key from signing authentic-looking commits.
Mitigations include:

- short-lived agent keys;
- narrow namespace and event-type scopes;
- no subdelegation by default;
- hardware-backed root/service keys;
- rapid epoch change and revocation;
- independent checkpoint approval for high-impact decisions;
- projection policies requiring multiple independent approvals.

## 15. Quorum as projection policy

PACT core does not implement universal consensus. A domain policy may require:

- two authorized reviewers;
- a human plus CI actor;
- independent evidence sources;
- a release-manager checkpoint;
- no unresolved contrary assertion.

These are acceptance rules over ledger history, not changes to the signed object
format.
