<!-- ABOUTME: Records the final-review fixes, TDD evidence, and verification commands. -->
<!-- ABOUTME: Keeps each required finding tied to its design choice and observed test result. -->

# Final review fix report

Date: 2026-08-23
Branch: `wip/phase-0-1-core`
Scope: all eight findings in `.superpowers/sdd/final-review-findings.md`

## Oracle and contract evidence

Before choosing semantics, I read the complete CLI exit table and the matching
Python functions in `docs/pact-ledger/scripts/pact_core.py`:

- `repo_path`, `command_keygen`, `command_hash`, and `command_heads` report
  resolved absolute paths, including symlinked parents.
- `load_key_file` classifies key-ID/public and private/public mismatches as
  integrity exit 4.
- `load_trust` classifies malformed trust-file shape as store exit 3, while
  trusted-root public-byte mismatches are integrity exit 4.
- `read_object` classifies missing objects as exit 9 and digest mismatch as
  exit 4.
- `verify_object_file` retains parsed objects when signature verification alone
  fails, but stops object interpretation when exact bytes, digest, JSON, or
  canonical form fails.
- `scan_secret_hazards` reports hazard paths and classes without rejected
  values.

The production signing-key boundary remains intentionally stricter than the
Python oracle, as required by the final review.

## RED evidence

Tests were added before production changes.

### Signing keys, public trust keys, and typed identity integrity

Command:

```text
env -u GOROOT mise exec -- go test -count=1 ./internal/identity -run 'TestLoad(Signing|Public|KeyFileRejectsKeyID)'
```

Observed RED:

```text
undefined: ErrIntegrity
undefined: LoadSigningKey
undefined: ErrSecretSafety
undefined: LoadPublicKey
FAIL pact/internal/identity [build failed]
```

### Store rollback and cross-process mutation lock

Command:

```text
env -u GOROOT mise exec -- go test -count=1 ./internal/store -run 'TestPutCanonical(RollsBack|Serializes)|TestStoreMutationLock'
```

Observed RED:

```text
undefined: syncDirectoryFile
undefined: readCanonicalFile
undefined: mutationLockPath
st.WithMutationLock undefined
FAIL pact/internal/store [build failed]
```

The first GREEN attempt exposed a real shadowing bug in rollback: the deferred
closure captured the `linkFile` initializer's local `err`, which stayed nil.
All post-link tests showed the canonical link remained, and the identical
admission returned `created:false`. Renaming that local to `linkErr` made the
defer observe the named return error and enabled the intended rollback.

### Raw secret references and typed show failures

Command:

```text
env -u GOROOT mise exec -- go test -count=1 ./internal/ledger -run 'TestNormalizeEventBatch(ScansRaw|InvalidReferences)'
```

Observed RED:

```text
undefined: ErrSecretSafety
undefined: ShowError
undefined: ErrMissingDependency
FAIL pact/internal/ledger [build failed]
```

### JSON diagnostics and exit classification

Command:

```text
env -u GOROOT mise exec -- go test -count=1 ./cmd/pact -run 'TestRun(JSONFlag|TrustAddAllows|UsesTyped)'
```

Observed RED:

```text
undefined: exitMissingDependency
FAIL pact/cmd/pact [build failed]
```

### Real binary path and trust concurrency contract

Command:

```text
env -u GOROOT mise exec -- go test -count=1 ./tests/e2e -run 'TestCLIReviewSecurityAndResolvedPathContract|TestCLIConcurrentTrustAddPreservesDistinctAndIdenticalRoots' -v
```

Observed RED:

```text
keygen path = ".../key-link/operator.key.json", want resolved ".../keys/operator.key.json"
trusted distinct roots = 2, want 16
FAIL pact/tests/e2e
```

## Design and GREEN evidence

1. Private signing keys now use `identity.LoadSigningKey(path, st.Root())`.
   The loader resolves ancestor symlinks without following the final component
   for the lexical containment check, resolves the final target separately,
   checks both against the Store's resolved root, requires a regular file, and
   rejects every group/other permission bit. `LoadPublicKey` skips only these
   private-key rules and admits readable public-only files.
2. `Store.WithMutationLock` uses an advisory exclusive flock in a private real
   directory under the system temp directory, keyed by the resolved repo root.
   An unlocked stale file is safe. `ledger.AddRoot` holds it across load,
   collision check, sorted append, and atomic write.
3. `PutCanonical` holds the same mutation lock across admission. A defer tracks
   whether this call created the no-overwrite hard link; any later shard sync,
   hook, readback, or digest failure removes that link and syncs the shard.
   Serialization prevents another compliant admission from taking ownership of
   that path during rollback.
4. `run` sends each `FlagSet` to `io.Discard` in JSON mode, then emits exactly
   one structured command error.
5. `ledger.ShowError` carries a `ShowResult` and unwraps to `ErrIntegrity`.
   Exact-byte, digest, parse, canonical, and structure failures return typed
   details; authenticity-only failure still returns the parsed signed object.
6. Identity, store, and ledger packages now own typed integrity, store,
   authorization, secret-safety, and missing-dependency errors. The CLI maps
   those types to exits 3, 4, 5, 7, and 9 without matching prose.
7. Raw event input is scanned before detailed validation. Reference validators
   report field paths and hazard/error classes only.
8. Keygen uses the resolved path returned by key creation; hash resolves the
   input file; heads reports `st.Root()`.

Focused GREEN commands:

```text
env -u GOROOT mise exec -- go test -count=1 ./internal/identity -run 'TestLoad(Signing|Public|KeyFileRejects)'
ok pact/internal/identity

env -u GOROOT mise exec -- go test -count=1 ./internal/store -run 'TestPutCanonical(RollsBack|Serializes|Verifies)|TestStoreMutationLock' -v
PASS: digest, shard-sync, hook, readback, identical-admission, and stale-lock cases

env -u GOROOT mise exec -- go test -count=1 ./internal/ledger -run 'TestNormalizeEventBatch(ScansRaw|InvalidReferences)|TestShow(ReturnsTyped|Allows|Missing)' -v
PASS: raw PEM/token paths, value-free refs, corrupt/unparseable show, authenticity-only show, and missing dependency

env -u GOROOT mise exec -- go test -count=1 ./cmd/pact -run 'TestRun(JSONFlag|TrustAddAllows|UsesTyped)' -v
PASS: one JSON diagnostic, public-only trust, typed exits, and show details

env -u GOROOT mise exec -- go test -count=1 ./tests/e2e -run 'TestCLIReviewSecurityAndResolvedPathContract|TestCLIConcurrentTrustAddPreservesDistinctAndIdenticalRoots' -v
PASS: real binary signing-key, resolved-path, JSON flag, and concurrent trust scenarios
```

## Final verification

Fresh-eyes review covered all 16 task files. It found one added security issue:
the first mutation-lock draft placed a predictable lock file directly under the
shared temp directory. A focused test reproduced that layout. The lock now
lives under a checked `0700` real directory; both init and mutation locking
reject a symlinked, non-directory, or group/other-accessible lock directory.

Commands and results before the final post-review rerun:

```text
env -u GOROOT mise exec -- go test -count=1 ./...
PASS: cmd/pact, canonical, conformance, identity, ledger, store, and real-binary E2E

env -u GOROOT mise exec -- go vet ./...
PASS

git diff --check
PASS

env -u GOROOT mise exec -- ./scripts/check
PASS: Go format, vet, tests, build, and all 17 Python oracle tests
```

Real usage built a fresh binary outside a fresh project, initialized through a
resolved path, generated and trusted an external key, committed the bundled
two-event example, inspected heads and the object, and ran strict verification.
The commit reported valid integrity/authenticity and authorized root signing;
strict verification reported `ok:true`, one commit, two events, and zero layer
failures.

Post-review verification reran from the final source state:

```text
env -u GOROOT mise exec -- go test -count=1 ./...
PASS: all seven Go packages/test trees, including real-binary E2E

env -u GOROOT mise exec -- go vet ./...
PASS

git diff --check
PASS

env -u GOROOT mise exec -- ./scripts/check
PASS: canonical Go gate and 17/17 Python oracle tests

env -u GOROOT mise exec -- go test -race -count=1 ./internal/store ./internal/ledger
PASS: store and ledger concurrency paths under the race detector
```

## Adopted lint baseline

Doctor Biz chose to adopt the full lint baseline after the new hook stack found
78 issues. No lint rule or hook was weakened. The baseline work:

- Applied every Go 1.26 `go fix` rewrite: `strings.SplitSeq`,
  `sync.WaitGroup.Go`, `maps.Copy`, range-over-integer, and `strings.Builder`.
- Replaced unchecked assertions on validated signed maps with typed internal
  commit, event, and checkpoint views. Inconsistent post-validation shapes now
  return errors instead of panicking.
- Split verification passes, commit/checkpoint option preparation, canonical
  normalization, event/evidence normalization, secret scanning, and canonical
  admission into small helpers. Wire fields, validation order, error text, and
  stored canonical bytes remain unchanged; the existing characterization,
  conformance, ledger, CLI, and E2E tests cover those contracts.
- Removed two unused authorization helpers and checked JSON encoding, flock
  unlock, and Ed25519 public-key assertions.
- Used one narrow `nolint:nilerr` where read failures are intentionally carried
  in `ObjectVerification`. Narrow documented `#nosec` lines cover only the
  required `private_key` wire field, public/user directory modes, and paths
  already restricted to a resolved store/key root or a digest-derived private
  lock location.

The lint count fell from 78 to zero:

```text
env -u GOROOT mise exec -- golangci-lint run --timeout=10m
0 issues.

env -u GOROOT mise exec -- go fix -diff ./...
PASS: no pending rewrites
```

Fresh-eyes review after the baseline refactor found a defer-order edge case:
temporary-file cleanup could create an error after the rollback defer had
already run. Canonical rollback is now registered first and therefore runs
last, so every error returned after this call creates the canonical link
removes and syncs that link.

Final full gate from the reviewed source state:

```text
env -u GOROOT mise exec -- go test -count=1 ./...
PASS: all Go packages, CLI integration, and real-binary E2E

env -u GOROOT mise exec -- go test -race -count=1 ./...
PASS: all packages under the race detector

env -u GOROOT mise exec -- go vet ./...
PASS

env -u GOROOT mise exec -- golangci-lint run --timeout=10m
0 issues.

env -u GOROOT mise exec -- go fix -diff ./...
PASS: no pending rewrites

git diff --check
PASS

env -u GOROOT mise exec -- ./scripts/check
PASS: Go gate and 17/17 Python oracle tests
```
