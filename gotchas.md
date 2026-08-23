<!-- ABOUTME: Records project facts and traps that must survive across agent sessions. -->
<!-- ABOUTME: Add small entries in place; do not regenerate this shared file. -->

# PACT gotchas

- The bundled Python package under `docs/pact-ledger/` is the v1 reference and
  conformance oracle. Production code must match its frozen wire bytes, IDs,
  signatures, and event references; do not "improve" those formats in isolation.
- Build the production engine in bounded gates. Phase 0 conformance must pass
  before Phase 1 object storage, and the object/crypto contract must freeze
  before later workstreams run in parallel.
- On this machine, run Go through `env -u GOROOT mise exec -- go ...`; the
  inherited `GOROOT` is stale.
- `docs/pact-ledger-skill.zip.sha256` records the original `/mnt/data` path.
  Verify the digest value against the local ZIP; do not rely on that path.
- Doctor Biz's product order is tool-first: build a usable `pact` CLI against
  the bundled Python behavior, then use the tool to refine its own contract.
  Do not block the MVP on completing every later protocol decision.
