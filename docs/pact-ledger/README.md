# PACT Ledger Skill

PACT is a reusable agent skill and reference implementation for a **local-first,
append-only, content-addressed, signed semantic ledger**.

**PACT = Provenance, Authority, Causality, and Trust.**

It is designed to sit underneath mutable workflow artifacts such as CSVs,
reports, dashboards, release manifests, research notebooks, and verification
matrices. Those artifacts become deterministic projections of immutable history.

## Core idea

```text
signed atomic semantic commits
            ↓
content-addressed causal DAG
            ↓
verification + scoped authority
            ↓
policy-driven projection
            ↓
current state / human-facing artifact
```

PACT is intentionally domain-neutral. A workflow defines its own event types,
payload schemas, and projection policy. PACT supplies the common machinery:

- immutable content-addressed objects;
- Ed25519 signatures;
- hierarchical namespaces;
- atomic multi-event commits;
- causal DAG history and concurrent branches;
- external evidence references and digests;
- capability delegation, epochs, leases, and revocation semantics;
- signed checkpoints;
- transport-neutral object sync;
- deterministic projection contracts;
- rebuildable SQLite indexing;
- CLI-first integration.

## What PACT is not

PACT is not a blockchain, cryptocurrency, global consensus system, secret store,
blob archive, or universal truth engine. It has no proof-of-work, global total
order, or requirement for a central service.

A signed event proves that a key asserted something. A projection policy decides
whether that assertion is accepted for a particular purpose.

## Package contents

- `SKILL.md` — complete operating contract for agents.
- `PLAN.md` — concise product/build plan; the detailed phase gates live under
  `references/`.
- `TESTING.md`, `TEST-RESULTS.txt`, and `PACKAGE-AUDIT.md` — conformance scope
  and recorded package verification.
- `scripts/pact.py` — stable CLI entrypoint.
- `scripts/pact_core.py` — importable reference library and conformance helper.
- `references/` — architecture, object model, authority, projection, threat
  model, adoption, and staged implementation plan.
- `schemas/` — JSON Schemas for the core signed envelopes.
- `examples/` — generic event batches and walkthrough.
- `tests/` — reference CLI tests.

## Quick start

Install the only runtime dependency used for signing:

```bash
python3 -m pip install cryptography
```

Initialize a project ledger and a root key:

```bash
python3 scripts/pact.py init \
  --repo /path/to/project \
  --namespace org/example/project/widget

python3 scripts/pact.py keygen \
  --actor human/operator \
  --out ~/.config/pact/keys/operator.json

python3 scripts/pact.py trust-add \
  --repo /path/to/project \
  --key-file ~/.config/pact/keys/operator.json
```

Create `event-batch.json`:

```json
{
  "events": [
    {
      "local_id": "e1",
      "kind": "observation",
      "type": "build.test.executed",
      "subject": "build/linux-amd64/42",
      "schema_ref": "pact:core/generic-object/v1",
      "payload": {
        "command": "make test",
        "exit_code": 0,
        "result": "pass"
      },
      "evidence": [
        {
          "ref": "file:///external/evidence/build-42.log",
          "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
          "media_type": "text/plain",
          "role": "primary"
        }
      ],
      "caused_by": [],
      "supersedes": [],
      "tags": ["ci"]
    }
  ]
}
```

Append an atomic signed commit:

```bash
python3 scripts/pact.py commit \
  --repo /path/to/project \
  --key-file ~/.config/pact/keys/operator.json \
  --events event-batch.json
```

Verify and inspect:

```bash
python3 scripts/pact.py verify --repo /path/to/project --strict
python3 scripts/pact.py heads --repo /path/to/project
python3 scripts/pact.py log --repo /path/to/project
```

Create a signed checkpoint:

```bash
python3 scripts/pact.py checkpoint \
  --repo /path/to/project \
  --key-file ~/.config/pact/keys/operator.json \
  --scope org/example/project/widget \
  --policy-ref sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --authority-epoch org/example/epoch/1
```

Sync another directory-backed replica:

```bash
python3 scripts/pact.py sync-dir \
  --repo /path/to/project \
  --from /mounted/other-project
```

## Validate the package

```bash
python3 -m pip install -r requirements-dev.txt
python3 -m unittest discover -s tests -v
```

The suite covers canonicalization vectors, JSON Schemas, immutable correction,
DAG forks and merges, scoped delegation, causal revocation, blocked
subdelegation, secret refusal, tamper detection, signed checkpoints, index
rebuilds, and idempotent directory sync.

## Reference implementation boundary

The bundled CLI implements the storage, canonicalization, signing, DAG,
checkpoint, indexing, querying, and directory-sync mechanics needed to exercise
the design. It performs conservative structural authority checks and clearly
reports indeterminate authorization.

It is a reference and conformance tool, not a hardened multi-tenant key service
or final network protocol. The staged production plan is in
`references/implementation-plan.md`.

## Design principle to keep

> The ledger preserves every valid historical assertion. Projections determine
> what is currently believed.
