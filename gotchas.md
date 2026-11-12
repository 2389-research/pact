<!-- ABOUTME: Records project facts and traps that must survive across agent sessions. -->
<!-- ABOUTME: Add small entries in place; do not regenerate this shared file. -->

# PACT gotchas

- 2026-11-11: The development workflow direction is a small profile above the
  domain-neutral ledger, with exact source/proof, explicit finding resolution,
  and separate recorded decisions versus supported local acceptance. Planning
  lives in docs/plans/development-workflow/implementation-plan.md and epic
  pact#ns4y. Kata holds execution state; PACT holds signed provenance. Existing
  log/query/show already passed a fresh-context baseline, so a new brief must
  earn publication through the plan's measured comparison gate.

- 2026-11-06: When Doctor Biz requests the review-squad workflow, discover and
  invoke that specific workflow or state that it is unavailable. Ad hoc parallel
  reviewers do not establish that the named workflow ran.

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
- PACT's human operator is primarily a reader, reviewer, and troubleshooter;
  agents author and commit ledger events. Human CLI comfort should not add an
  event-authoring workflow unless a concrete human-authoring need appears.
- PACT's human terminal output must be visually polished as a product feature.
  Treat hierarchy, spacing, alignment, restrained color, and narrow-terminal
  behavior as tested CLI contracts rather than optional cleanup. Use native
  terminal patterns; do not import web-dashboard or card-layout conventions.
- Doctor Biz prefers Omakase for feature builds: compare distinct approaches
  in isolated worktrees, run one scenario contract, and keep the tested winner
  unless Doctor Biz asks for a different workflow.
- For a bounded command with an approved contract, skip separate spec and plan
  ceremony when Doctor Biz asks; implement directly with TDD and normal gates.
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
- `pact setup` is the canonical bootstrap. It performs store, external key,
  trust, strict verify, and derived-index actions in that order. Exact reruns
  converge without changing bytes; conflicts refuse overwrite, and the key
  must remain outside the project root.
- Canonical files under `.pact/objects/` must not gain a trailing newline; it
  changes their SHA-256 identity. The pre-commit EOF fixer excludes that path.
  Keep the exclusion and rely on PACT's canonical writer and verifier.
- 2026-11-06: `.pact/index/pact-v1.sqlite3` is derived and disposable. Explicit
  `pact index rebuild` recreates a missing index directory after a Git restore;
  it does not require `tmp` or `refs`. Existing non-directory or symlink paths
  still refuse recovery. Missing index contents never need canonical repair.
- A commit or checkpoint that publishes a new canonical object makes an
  existing index stale. An idempotent append of an existing object does not.
  Indexed reads refuse stale state until an explicit rebuild; `show` and
  canonical verification remain available.
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
- Phase 2 repository dogfood uses stable local ID `phase-2-mvp`. Its signed
  commit is `sha256:69d67237ecf856c5b72515cd1ac43322642a847c1fc0d35955cd30e911146743`;
  the chained checkpoint is
  `sha256:511f085606dc807fd3a67b2fd80afb37355d27f4b677b087cb337b32552eb485`.
  Full baseline, query, gate, and review evidence lives in
  `docs/status/phase-2-dogfood.md`. Rebuild after the checkpoint before indexed
  reads because every canonical append makes the derived index stale.
- Rebuild protects canonical state with the exclusive store lock and a second
  verified `ledger.Scan` source fingerprint. Do not invent numeric limits for
  ignored refs/trust paths; byte snapshots in tests prove no mutation.
- The 16 MiB log/query limit includes the JSON newline. Page assembly applies
  an exact skeleton-and-item budget before each append, including group fields,
  cursors, array commas, and the newline. CLI output uses the same streaming
  `QueryPage` serializer and must propagate writer errors.
- `errors.Join(err, nil)` wraps a lone error on current Go. When cancellation
  identity is a contract, join a cleanup error only when that error is non-nil.
- The tested standard-library CLI adapter won: one command catalog owns
  dispatch and help, and `golang.org/x/term` is the only remaining CLI
  dependency.
- The Go verifier authorizes locally trusted roots. It checks authority-field
  structure but does not evaluate delegation causality or revocation semantics;
  non-root authorization remains indeterminate.
- The animated seal belongs only to bare `pact`. Explicit `pact help` and
  `pact --help` must remain immediate; redirected bare output gets one static
  frame and follows the normal color policy.
- Real-PTY text assertions must select `--color never`; local `NO_COLOR`
  can hide ANSI-dependent failures that appear on GitHub runners.
- 2026-11-06: Missing query use in consumer logs does not prove a missing read
  API. A fresh agent resumed successfully with existing commands and skill
  0.2.1; the 0.2.2 guide also passed, without a demonstrated efficiency gain.
  `log`/`query` omit payloads and evidence; use exact `show` references. Cursors
  continue a fixed snapshot, not changes since a previous session.

- 2026-11-11: Git hooks export repository selectors such as `GIT_DIR`,
  `GIT_WORK_TREE`, `GIT_COMMON_DIR`, and `GIT_INDEX_FILE`. Setting a subprocess's
  working directory does not isolate Git: even `git init` can rewrite the common
  repository's `core.worktree`. Fixture Git processes must strip inherited
  `GIT_*` variables and select their temporary repository explicitly. Keep the
  real-repository regression that compares the outer config, refs, and index.
