<!-- ABOUTME: Records the compiled setup dogfood, stable hashes, gates, and whole-branch review. -->
<!-- ABOUTME: Keeps private key bytes out of durable setup delivery evidence. -->

# Operator CLI Setup Status

## Scope and shipped artifacts

`pact setup` is the canonical bootstrap for a usable local ledger. It applies
five ordered actions: store, external key, trust root, strict verification,
and derived index. Fully flagged automation and guided real-terminal flows use
the same typed setup service.

The shipped verification surface includes `scenarios.jsonl` and the test-only
`github.com/creack/pty v1.1.24` dependency in `go.mod` and `go.sum`. The PTY
dependency does not enter the production command path.

## Canonical lifecycle TDD

The script contract first required the fully flagged setup call, five ordered
actions, and a current initial index. With the old `init`, `keygen`, and
`trust-add` bootstrap still present, the focused E2E test failed because
`scripts/check` lacked the setup call. After replacing those three commands,
the focused test and canonical check passed. The later commit, query, rebuild,
and strict-verify lifecycle and secret scan remain in the script.

## Fresh compiled-command dogfood

The dogfood ran on 2026-08-26 in one fresh `mktemp -d` root. The compiled
binary, project, captured output, snapshots, and external key were all inside
that scratch root, with the key as a sibling of the project rather than below
it. An exit trap removed the exact scratch root and the external key.

The command sequence was:

```sh
env -u GOROOT mise exec -- go build -o "$scratch/pact" ./cmd/pact
"$scratch/pact" setup --repo "$repo" \
  --namespace org/2389/pact/setup-dogfood \
  --actor setup-dogfood --key-file "$external_key" --json
"$scratch/pact" index status --repo "$repo" --json
"$scratch/pact" setup --repo "$repo" \
  --namespace org/2389/pact/setup-dogfood \
  --actor setup-dogfood --key-file "$external_key" --json
ln -s "$repo" "$repo_alias"
"$scratch/pact" setup --repo "$repo_alias" \
  --namespace org/2389/pact/setup-dogfood \
  --actor setup-dogfood --key-file "$external_key" --json
"$scratch/pact" verify --repo "$repo_alias" --strict --json
"$scratch/pact" index status --repo "$repo_alias" --json
```

Fresh actions were `store created`, `key created`, `trust created`, `verify
valid`, and `index created`. Both the exact rerun and the symlink-alias rerun
reported `store existing`, `key existing`, `trust existing`, `verify valid`,
and `index current`.

The external key mode was `0600`. `cmp` proved that `format.json`, the external
key file, `trust.json`, the canonical object-file listing, and
`pact-v1.sqlite3` were unchanged after both reruns. Setup created zero
canonical objects. Strict verification returned `ok=true` with
`index_status=current`; index status returned `state=current` and
`coverage=complete`.

Safe durable identifiers and hashes:

- public key ID:
  `ed25519:sha256:eb5026f620fee5d6ecbad71b13cf4951398710e4b71e1382e1e0914ec44b8d8a`;
- format SHA-256:
  `2f15dfa57f3913243b2f1fb026959d7114a05ff70a4aea2479d2ed9fdd98ef18`;
- external key-file SHA-256:
  `8719eccbdd84b4c64772f68474292d2f3e39fa7dcf2b9891506a43c8f661e1b3`;
- trust SHA-256:
  `db76cd8e00246cb96509b36403438e07a2800dcc2a9d9be888771315bc73162d`;
- index-file SHA-256:
  `8f1d56bfc41aee4bba8bb807e24a8edbffd1f4873bd7c691686d60a3d7a4e7f4`;
- index logical digest:
  `sha256:03f03e8a64cabe2cf76aad5d5f0ba934f63efc7aac6b03526922bc8207b3d481`;
- index source fingerprint:
  `sha256:239a46f0a3514bf5f07eaea4b834fbe96ba805b502c0664d510886dc3a52fe87`.

The resolved project lived under the macOS private temporary-directory alias;
no durable document depends on its removed random path. Searches across every
captured stdout/stderr file and every scratch project file found neither the
private-key field name nor the actual base64 private seed. The external key
was excluded from that scan because it is the intended secret container; its
bytes were never printed or copied into this document.

## Final gates

After the review fixes, every exact final gate ran through the project
toolchain and exited 0:

```sh
env -u GOROOT mise exec -- gofmt -w internal/identity/keyfile.go internal/identity/keyfile_test.go cmd/pact/setup_command_test.go tests/e2e/cli_test.go
env -u GOROOT mise exec -- go test ./...
env -u GOROOT mise exec -- go test -race ./...
env -u GOROOT mise exec -- ./scripts/check
env -u GOROOT mise exec -- pre-commit run --all-files
env -u GOROOT mise exec -- go build ./cmd/pact
git status --short
```

Standard and race tests passed every Go package. `scripts/check` passed every
Go package, the compiled canonical lifecycle, and all 18 frozen Python oracle
tests. Every pre-commit hook passed, including format, imports, module tidy,
lint, unit tests, fix diff, module verification, and vet. The exact build
created the expected untracked root binary; that generated file was removed
before staging. Status then listed only the closeout files.

## Independent whole-branch review

Both reviewers audited base `06cb31c` through the final working tree against
the approved design and implementation plan.

The requirements review found three Important gaps: a stale setup JSON example,
a writer test that proved the rerun but not the original in-memory action
result, and a scenario reference to an ignored scratch script. Focused tests
first failed on the stale artifacts. The writer test was strengthened and a
deliberate mutation proved that it fails if renderer failure changes the
original action slice. The design now documents `ready`, `key_file`, and action
`name`; the writer test proves exit 10 while retaining the original
`created/created/created/valid/created` result for both modes; and the scenario
names only tracked E2E tests. Requirements re-review found no Critical or
Important issue and approved the fixes.

Fresh-eyes review found one Important bug: combined post-publication directory
sync and temporary-key cleanup failures dropped the cleanup error, which could
hide a leftover secret-bearing temporary file. Its new test failed because
only the sync identity survived. The key writer now joins both non-nil errors
while retaining `GenerateCreated`; the focused and owner-package tests passed.
The same review found two Minor doc errors, an incomplete key-ID prefix and a
missing `status` command in the README inventory; both are fixed. Fresh-eyes
re-review found no new Critical or Important issue and returned `Ready`.

The reviewers explicitly confirmed:

1. every caller changed with the `store.Init`, `GenerateKeyFile`, and `AddRoot`
   signatures;
2. no ordinary error can be mistaken for convergence;
3. init and mutation alias tests prove final bytes and statuses;
4. human and JSON writer failures return 10 while preserving the original
   completed actions after durable publication;
5. the fifth derived-index action is implemented and current on success.

No known shipping concern remains. The PTY tests target the Unix PTY API used
by the supported development and CI systems; they do not claim Windows PTY
coverage. The branch has not been merged.

## Later final-review repairs

A later whole-branch pass found three Important gaps after the earlier
approval: byte-stability failures could print the external key snapshot,
setup routed several typed owner failures to exit 10, and directory-close or
staging-removal errors could be discarded. Focused TDD repairs now report only
changed snapshot paths and SHA-256 values, preserve store exit 3 and index or
resource exit 9 with stable details plus partial actions, and retain cleanup
error identity after key, store, and trust publication.

Controller audit and fresh-eyes review then found two mixed-error edges: an
`ErrNotExist` member could hide a real store-staging or key-temporary cleanup
fault. Tests first reproduced both losses. The classifiers now ignore a
not-found cleanup only when every error-tree leaf is not-found. Repair commits
are `bcf954d`, `0d71c62`, and `eb4dc5f`.

Independent rereview of `06cb31c..eb4dc5f` reported 0 Critical, 0 Important,
and 0 Minor findings. After the last source fix, the full race suite,
`scripts/check` with 18 Python tests, 20-run store-alias and setup-concurrency
race tests, human and JSON writer tests, compiled E2E and real PTY tests, a
fresh compiled setup lifecycle, and the build gate all passed. Exact commands
were:

```sh
env -u GOROOT mise exec -- go test -race ./... -count=1
env -u GOROOT mise exec -- ./scripts/check
bash .scratch/setup-lifecycle.sh
env -u GOROOT mise exec -- go test ./tests/e2e -run 'TestSetupRealPTYPromptContract|TestSetupCompiledLifecycle' -count=1
env -u GOROOT mise exec -- go test -race ./internal/store -run 'TestInitSerializesAliases|TestMutationLockSerializesAliases' -count=20
env -u GOROOT mise exec -- go test -race ./internal/setup -run 'TestApplyConcurrentIdenticalRequestsConverge|TestApplyConcurrentConflictingRequestsPreserveWinner' -count=20
env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestSetupTerminalWriterFailuresPrecedeAllMutation|TestSetupWriterFailuresReturnUnexpectedAfterDurableApply' -count=1
env -u GOROOT mise exec -- go build -o /tmp/pact-setup-final-eb4dc5f/pact ./cmd/pact
env -u GOROOT mise exec -- pre-commit run --all-files
```

The branch has not been merged.
