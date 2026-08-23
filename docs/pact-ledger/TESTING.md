# Testing PACT

## Install development dependencies

```bash
python3 -m pip install -r requirements-dev.txt
```

## Run the complete suite

```bash
python3 -m unittest discover -s tests -v
```

## Current conformance coverage

The suite exercises:

- canonical Unicode normalization and numeric restrictions;
- namespace and event-capability wildcard semantics;
- secret-hazard detection;
- JSON Schema validity and example conformance;
- initialization, external key generation, and trust bootstrap;
- signed append, stable event references, querying, and head calculation;
- append-only correction without changing original bytes;
- native forks and explicit multi-parent merge;
- scoped direct delegation;
- causal revocation while retaining authentic history;
- forbidden subdelegation;
- external evidence references;
- signed checkpoints;
- SQLite rebuild;
- directory replica union and idempotent resync;
- object tamper detection.

## Test boundary

These tests establish the behavior of the bundled reference implementation.
They do not claim completion of later production phases such as arbitrary
content-addressed schema resolution, policy execution, trusted time, hardware
key storage, network sync, or multi-tenant resource controls.

## Recommended production additions

Before production release, add:

- frozen cross-language canonicalization and signature vectors;
- property tests and parser fuzzing;
- crash injection around every persistence boundary;
- pathological DAG/resource-limit tests;
- recursive subdelegation and concurrent-revocation policy matrices;
- deterministic projection golden files;
- bundle and network-sync interruption tests;
- root compromise/rotation drills;
- performance tests on realistic object/event counts.
