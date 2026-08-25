<!-- ABOUTME: Records the verified Phase 2 bounded-index and causal-query dogfood proof. -->
<!-- ABOUTME: Preserves immutable IDs, rebuild parity, cold gates, and the closeout review state. -->

# Phase 2 Dogfood Status

## Status

Phase 2 implementation and repository dogfood passed their product gates on
2026-08-24, and final independent review passed on 2026-08-25. The clean review
covered the exact Task 8 candidate based on `218536f`, from Phase 2 start
`a6c6c14`. That candidate landed on `wip/phase-2-dag-index-query` as closeout
commit `682c822` with every commit hook passing.

Two TDD fix waves resolved every broad-review finding. The final independent
re-review reports 0 Critical, 0 Important, and 0 Minor findings and approves
the package for commit. The Task 8 records and two canonical objects landed in
`682c822` (`chore: dogfood phase 2 index and query`).

## Signed proof

- Namespace: `org/2389/pact`
- Actor:
  `ed25519:sha256:bdf05ec0cd70191a1343dd4db0cd0c7b094794238a73e64bdcca06081053dd3a`
- Stable local ID: `phase-2-mvp`
- Proof object format: `pact/commit/v1`
- Kind: `assertion`
- Type: `build.capability.verified`
- Subject: `pact/mvp/phase-2`
- Schema: `pact:core/generic-object/v1`
- Tags: `dogfood`, `phase-2`
- Signed commit:
  `sha256:69d67237ecf856c5b72515cd1ac43322642a847c1fc0d35955cd30e911146743`
- Stable event reference:
  `pact:event:sha256:69d67237ecf856c5b72515cd1ac43322642a847c1fc0d35955cd30e911146743#phase-2-mvp`
- Signed checkpoint:
  `sha256:511f085606dc807fd3a67b2fd80afb37355d27f4b677b087cb337b32552eb485`
- Checkpoint object format: `pact/checkpoint/v1`
- Previous checkpoint:
  `sha256:2e97c1e773c4ff3c272ff736e2218c7c753946552d3e61b9d8f96fc1035d7e42`
- Checkpoint policy reference:
  `sha256:27ef52b038652366afcb48308326eca92da80c5598291ee0317454e3ab77fac0`
- Checkpoint schema references:
  `sha256:14b5fb39a48736dafb104e6232b230799e1f2d2ce021b4c0f6385cf77d36cded`
- Final namespace head: the signed Phase 2 commit above

The checkpoint reuses the existing decisions-document policy reference, event
schema reference, and `phase-0-1-root-v1` authority epoch because the trusted
root did not change. Its frontier contains only the Phase 2 proof commit.

## Exact format evidence

The canonical proof JSON at the path matching the signed commit ID has
`format=pact/commit/v1`. Event-level `show` confirmed the event fields above but
does not expose the enclosing commit format. Canonical checkpoint JSON and
checkpoint-level `show` both returned `format=pact/checkpoint/v1`, the exact
policy reference above, and a `schema_refs` array containing only the exact
schema reference above.

The final `index status` JSON reported `schema_version=1`. The implemented
SQLite schema constants and their real-database schema test establish the
fields that status does not expose: metadata format `pact/sqlite-index/v1`,
SQLite application ID `0x50414354`, and `PRAGMA user_version=1`.
`TestCreateSchemaInstallsExactSQLiteV1Schema` reads both pragmas from a real
SQLite database and compares them with `ApplicationID=0x50414354` and
`SchemaVersion=1`; `IndexFormat` is `pact/sqlite-index/v1` in the same checked-in
schema contract.

## Immutable baseline

The ignored machine-readable baseline is
`.scratch/task-8/pre-mutation-baseline.json`. Before mutation it recorded:

| Path | Bytes | SHA-256 |
|---|---:|---|
| `.pact/format.json` | 216 | `ee0e964dd9f805c85ee7f50c87a76b74217a79ab16f2fa4c8ef0fa9552cfe66d` |
| prior checkpoint object | 1,070 | `2e97c1e773c4ff3c272ff736e2218c7c753946552d3e61b9d8f96fc1035d7e42` |
| Phase 0/1 commit object | 1,148 | `5e7cf3a370a15ba838200ecef9fc8927a3855f3d27216466f1a5182598d804ce` |
| `.pact/trust.json` | 299 | `596449a6f3505161485e849d569448df3e7a15e4f4d36ea0a224eea81d1fe55c` |

`.pact/refs/` contained no files. The only head was the Phase 0/1 commit. A
throwaway repository let the built CLI consume the external root key and
report only safe public metadata. It matched the actor above, and the key file
mode was `0600`; no private material entered the repository or logs.

## Pre-mutation query and recovery parity

The initial index state was `missing`. The first rebuild returned:

- source fingerprint
  `sha256:c057555e08261de323603c053d2b3813b7444b644acccfad530ddd5d6294e16a`;
- logical digest
  `sha256:b673e993507589aa01c2de6a8094fce0a56521c1350ce05d422e87b02666e4e5`;
- 2 objects, 1 commit, 1 checkpoint, 1 event, 3 edges, and 2,218
  canonical bytes.

The exact namespace/type/subject/actor/tag intersection returned only the
existing `#phase-0-1-mvp` event. The live derived SQLite file was removed,
status returned `missing`, and an explicit rebuild returned the same source
fingerprint, logical digest, counts, and byte-identical normalized query JSON.
The immutable baseline comparison still had no missing, changed, or added file
and no head change.

## Phase 2 lifecycle

Appending the proof commit made the index `stale`. The exact Phase 2 query
refused with exit 9 and detail code `index_stale`, while direct `show` returned
the new event with valid integrity and authenticity. Explicit rebuild returned:

- source fingerprint
  `sha256:874c5e0041f62871777654c8ceee7789e66010d30ade1f8656b8bd5071b44b2e`;
- logical digest
  `sha256:38db2f19ae0c968398e7429665fd35240a1801205ea0ee0cec547e48b3354459`;
- 3 objects, 2 commits, 1 checkpoint, 2 events, 6 edges, and 3,473
  canonical bytes.

The exact Phase 2 intersection then returned only `#phase-2-mvp`; query output
contained no payload or evidence. Strict verification passed before checkpoint
creation.

The one chained checkpoint again made the index stale. Direct checkpoint
inspection and strict verification worked in that state. A final explicit
rebuild returned:

- source fingerprint
  `sha256:f73a0bb2f272f2b12f32f4323fe31967371e359de74d77f88b7edb39dfb43ef3`;
- logical digest
  `sha256:f096b1a612d86077666f1f7196a15c0af04737db4e610975569d26e536f2f0ec`;
- 4 objects, 2 commits, 2 checkpoints, 2 events, 8 edges, and 4,624
  canonical bytes.

Final strict verification reported `ok=true`, `current` index state, locally
closed completeness, the Phase 2 limits profile within limits, two authorized
commits, and zero integrity, structure, authenticity, DAG, reference,
unauthorized, or indeterminate failures.

## Final immutability result

Every baseline file remains byte-identical. Exactly these two files were added:

- `.pact/objects/sha256/69/d67237ecf856c5b72515cd1ac43322642a847c1fc0d35955cd30e911146743.json`
  — 1,255 bytes, raw SHA-256 equals the signed commit ID;
- `.pact/objects/sha256/51/1f085606dc807fd3a67b2fd80afb37355d27f4b677b087cb337b32552eb485.json`
  — 1,151 bytes, raw SHA-256 equals the signed checkpoint ID.

Neither canonical file has a trailing newline. Trust, format, prior objects,
the prior checkpoint, and refs did not change. The head changed only from the
Phase 0/1 commit to the Phase 2 commit.

## Gate evidence

The full suite below passed before mutation and again from fresh Go and linter
caches after the two intentional objects were present:

```sh
cache="$PWD/.scratch/task-8/final-gocache"
lint_cache="$PWD/.scratch/task-8/final-lint-cache"
gate_binary="$PWD/.scratch/task-8/final-gate-pact"

env -u GOROOT mise exec -- env GOCACHE="$cache" ./scripts/check
env -u GOROOT mise exec -- env GOCACHE="$cache" go test -race ./...
env -u GOROOT mise exec -- env GOCACHE="$cache" go vet ./...
env -u GOROOT mise exec -- env GOCACHE="$cache" go build -o "$gate_binary" ./cmd/pact
env -u GOROOT mise exec -- env GOCACHE="$cache" GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run
env -u GOROOT mise exec -- go mod verify
env -u GOROOT mise exec -- env GOCACHE="$cache" go test ./tests/e2e -run 'Index|Log|Query|Cursor|Partial' -count=1 -v
env -u GOROOT mise exec -- env GOCACHE="$cache" go test ./tests/e2e -run 'Limit|Concurrent|Interrupted|Symlink|Cycle' -count=1 -v
git diff --check
```

`scripts/check` passed every Go package and all 18 frozen Python reference
tests. The race suite passed every package. The linter reported `0 issues`,
module verification reported `all modules verified`, both compiled E2E groups
passed, and no command emitted a new warning or error.

The implementer self-audit covered all 64 files in `a6c6c14..218536f` against
the approved design and scenario matrix and found no issue. The later
independent broad review found six issues in the working implementation:
unbounded protected-state proof work, uncancellable projection/digest work,
allocating result sizing, duplicate CLI JSON representation, weak concurrent
success/interruption assertions, and no exact-2-GiB SQLite proof. The first
unstaged fix wave addressed all six.

The final-review fix wave then passed its focused tests and current
working-tree gate:

```sh
fresh_cache="$(mktemp -d)"
env -u GOROOT mise exec -- ./scripts/check
env -u GOROOT GOCACHE="$fresh_cache/go" mise exec -- go test -race ./... -count=1
env -u GOROOT GOCACHE="$fresh_cache/go" mise exec -- go vet ./...
env -u GOROOT GOCACHE="$fresh_cache/go" mise exec -- go build -o "$fresh_cache/pact" ./cmd/pact
env -u GOROOT GOCACHE="$fresh_cache/go" GOLANGCI_LINT_CACHE="$fresh_cache/lint" mise exec -- golangci-lint run
env -u GOROOT mise exec -- go mod verify
env -u GOROOT GOCACHE="$fresh_cache/go" mise exec -- go test ./tests/e2e -run 'Index|Log|Query|Cursor|Partial' -count=1 -v
env -u GOROOT GOCACHE="$fresh_cache/go" mise exec -- go test ./tests/e2e -run 'Limit|Concurrent|Interrupted|Symlink|Cycle' -count=1 -v
git diff --check
```

The canonical check passed all Go packages and 18 frozen Python tests. The
fresh-cache race suite, vet, scratch build, both compiled E2E groups, and diff
check passed; the final linter reported `0 issues`, and module verification
reported `all modules verified`. Hash-only comparison reconfirmed the two new
objects, two prior objects, and trust file as `511f0856...b485`,
`69d67237...6743`, `2e97c1e7...7e42`, `5e7cf3a3...04ce`, and
`596449a6...e55c` respectively. No canonical writer ran during the fix wave.
The first re-review found three follow-up Important issues and two stale-doc
Minor issues, all fixed through a second TDD wave.

The second review found three remaining Important issues and two stale design
statements. The second TDD fix wave charges the exact complete page
before retaining each hydrated candidate, reports JSON stdout failures with a
safe exit-10 diagnostic, and removes the invented refs/trust manifest limits.
Rebuild still holds the exclusive store lock, validates fixed paths, and runs a
second bounded canonical scan. Focused and full gate results for this wave are
recorded in `.superpowers/sdd/final-review-v2-fix-report.md`. The final
independent re-review on 2026-08-25 reports 0 Critical, 0 Important, and 0
Minor findings and approves the package for commit.

## Known weakness and recommendation

The known Medium weakness remains: each query hashes bounded canonical bytes,
and rebuild pauses writers and queries while it holds the exclusive store lock.
Ship the simple fixed snapshot. Do not add incremental indexing until measured
use shows that the clear freshness contract is too costly and a new design is
approved.

Setup CLI work and Phase 3 remain out of scope and did not enter this branch.
