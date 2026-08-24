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
- The Go module path is `pact` until a real hosting location exists. Do not
  invent a remote import path; change it once publication is concrete.
- The v1 filesystem boundary blocks static symlink mistakes. It does not claim
  defense against a concurrent local process that can already replace owned
  path components; operators must protect repo and key directories.
- Production checkpoint admission is stricter than the Python oracle: the
  signing key must be a configured local trusted root before any checkpoint
  bytes persist. Authentic but unknown checkpoint signers are refused.
- Dev tooling (Makefile, golangci, goreleaser, pre-commit, GitHub workflows)
  was ported from ../portal on 2026-08-23. The full lint baseline is zero, and
  the strict hooks are usable when commits run under the project toolchain:
  `env -u GOROOT mise exec -- git commit ...`. Goreleaser's release target
  (`2389-research/pact`) is the presumed future home; no git remote exists yet.
