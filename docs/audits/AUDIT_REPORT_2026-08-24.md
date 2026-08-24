<!-- ABOUTME: Audits the Phase 0 and Phase 1 operator and dogfood documentation. -->
<!-- ABOUTME: Records verified claims, corrected drift, and reviewed acceptance judgments. -->

# Documentation Audit Report

Generated: 2026-08-24 | Code revision: `9428ad1`

## Executive summary

| Metric | Count |
|---|---:|
| Documents scanned | 2 |
| Claims checked | 98 |
| Verified true | 90 |
| Verified false | 0 |
| Manually reviewed | 8 |

The audit covered `README.md` and
`docs/status/phase-0-1-dogfood.md` in the live dogfood worktree. It checked
paths, command inventory, flags, digests, object IDs, counts, permissions,
negative capability claims, and current store behavior against source and real
commands.

## Corrections made during audit

One initial status sentence said all parent directories of the external key
were mode `0700`. The key and PACT-owned `pact` and `keys` directories have the
documented `0600`, `0700`, and `0700` modes; the higher-level `~/.config`
directory is mode `0755`. The status page now names only the PACT-owned
directories.

The status page also now sends its standalone build check to
`.scratch/pact-build-check`. A bare `go build ./cmd/pact` passes but leaves a
root-level binary. The canonical `scripts/check` was already correct and builds
under a temporary directory with cleanup.

Detailed setup-history counts that were not all preserved on the core branch
were removed. The remaining setup claims point to the durable rejected review
on `setup-cli/omakase/setup-service`.

## Verified inventory

- All nine documented Phase 1 commands exist: `init`, `keygen`, `trust-add`,
  `hash`, `commit`, `heads`, `show`, `verify`, and `checkpoint`.
- Every documented long flag exists and was either invoked successfully or
  checked against its command adapter.
- All referenced source, schema, plan, status, and setup-branch files exist.
- The live store contains the documented commit and checkpoint IDs, and strict
  verification reports the documented counts and head.
- The policy and schema references equal the SHA-256 digests of their named
  checked-in files.
- The external key permissions and exact private-byte absence claim match the
  live filesystem.
- The README's Python test count is 17, matching the canonical gate.
- No setup, SQLite query/index, network, projection, hardware-key, or other
  later-phase command is present in the shipped command inventory.

## Reviewed acceptance and historical claims

Eight claims needed direct review because they express product decisions or
describe actions rather than static files:

- The Phase 0/Phase 1 naming, gate acceptance, setup deferral, and Phase 2 as
  the next planning goal come from the approved dogfood goal and canonical
  production plan.
- The Phase 1 recovery claim was observed directly during this goal: strict
  verification passed twice with `.pact/index` and `.pact/refs` moved aside;
  the two JSON results were byte-identical; canonical object hashes were
  unchanged; and the empty directories were restored.

These claims are accepted. No false, unresolved, Critical, or Important
documentation finding remains.

## Pattern expansion and gap detection

The second pass expanded every path and flag pattern found in either document,
compared the README command list with the command dispatcher, checked numeric
test and object counts, and compared MVP exclusions with the production file
inventory. It found no additional dead reference, unsupported flag, incorrect
count, or undocumented shipped command.
