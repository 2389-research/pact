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
- Doctor Biz prefers Omakase for feature builds: compare distinct approaches
  in isolated worktrees, run one scenario contract, and keep the tested winner
  unless Doctor Biz asks for a different workflow.
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
- Phase 0 and Phase 1 passed the full gate and dogfood this repository under
  namespace `org/2389/pact`; durable IDs and proof are in
  `docs/status/phase-0-1-dogfood.md`. The root key stays outside the repository.
- The setup CLI is deferred after all Omakase variants and a later repair plan
  failed review. Its rejected design and review remain on branch
  `setup-cli/omakase/setup-service`; do not merge setup into the core by
  accident.
- Canonical files under `.pact/objects/` must not gain a trailing newline; it
  changes their SHA-256 identity. The pre-commit EOF fixer excludes that path.
  Keep the exclusion and rely on PACT's canonical writer and verifier.
- `.pact/index/pact-v1.sqlite3` is derived and disposable. Missing or unusable
  index contents never need canonical repair. Restore `.pact/index` as a real
  directory and remove unsafe live paths or SQLite sidecars when needed, then
  run an explicit `pact index rebuild`.
- Every successful commit or checkpoint makes an existing index stale. Indexed
  reads refuse stale state until an explicit rebuild; `show` and canonical
  verification remain available.
- Causal batches encode known local dependency edges as a partial order.
  `observed_at` is advisory, numeric batch order is not a total order, and
  local closure never proves global completeness.
- Namespace rules depend on the relation: commit parents stay in one namespace,
  resolved `caused_by` references may cross namespaces and add causal order,
  while `supersedes` never adds a causal edge.
- Phase 2 queries hash bounded canonical bytes, and rebuild holds the exclusive
  store lock for its full scan and atomic publication. This O(canonical bytes)
  cost and writer pause are the accepted fixed-snapshot tradeoff.
- Keep long goal progress legible through small reviewed green commits. Do not
  let a broad E2E or documentation gate grow into one large uncommitted batch.
