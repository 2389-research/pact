<!-- ABOUTME: Records the complete development lifecycle proof against one exact source revision. -->
<!-- ABOUTME: Separates existing production behavior, new journey coverage, verified gates, and remaining limits. -->

# Development lifecycle proof

Task: `pact#z895`, development workflow task 7.
Base: `a0d4a7540c8f5065803335244791833ebc32602b`.
Tested source: `42ffb83368b94481fbf934fefcf060f79cad4168`.
Branch: `feat/development-lifecycle`.

The complete recording journey passes on the existing production implementation.
This change adds compiled lifecycle coverage and documents the scenario.
It extracts the existing crash harness for use within the populated lifecycle store.
No production code, dependency, public command, wire format, trust rule, or cursor contract changed.

`scripts/check` already runs `go test ./...`, which includes the new journey.
Its existing production and Python reference gates remain unchanged.
No extra test invocation or exclusion was needed.

## Observed journey

`TestCLIDevelopmentLifecycle` uses one temporary real Git repository, external Ed25519 key, signed store, and SQLite index.
It imports the existing public Git fixture and checks both exact source revisions.
All writes use the compiled public CLI.
The causal competitor uses the existing raw commit command with an explicit parent.
State assertions use the compiled internal projector driver.
Public reads use complete query paging, exact show, evidence check, and strict verify.
No mock mode, network service, production credential, or exported key is involved.

| Step | Verified result |
| --- | --- |
| Record a failed review and passing check | The Important finding remains open; acceptance is `not_recorded`. |
| Advance Git and record the corrected-source check | The original finding remains open; acceptance is still absent. |
| Record a review resolving the exact finding | The finding names its resolving review; acceptance remains absent. |
| Record a separate matching acceptance | Exact check/review references support the corrected source; support becomes `supported`. |
| Submit `partial` as a check outcome | Validation refuses it; every store-file hash remains unchanged. |
| Change mutable proof | The mutable assessment becomes `changed`; the Git assessment remains `matching`. |
| Recover historical Git proof | Exact original bytes and their digest match; durable acceptance remains supported. |
| Kill the owned recorder after verified publication | Removing the index and retrying returns the original object and every original event reference. |
| Inspect recovered history | One canonical operation contains the full 1,024-event batch; the rebuilt index is current. |
| Add a large blocking finding | All 99,028 summary bytes survive; support becomes `unsupported`. |
| Follow all query cursors | All 1,031 event references appear once, including the required blocker and final recovery event. |
| Exceed the record input limit | The CLI refuses input larger than 4 MiB without changing any store-file hash. |
| Add a causally concurrent rejection | Both heads and both decisions remain visible; support becomes `unresolved`. |
| Verify final state | Seven canonical commits pass strict verification; all 1,034 event references remain pageable. |
| Compare protected bytes | Preexisting canonical object and trust hashes remain unchanged. |

The crash helper uses the existing blocked-output-pipe technique.
It waits for verified canonical publication before killing its owned process.
It now supports preexisting objects and compares the full admitted event-reference list after retry.
It also checks removal errors and reaps its process on failure paths.
No second crash harness or application test mode was added.

## Test-first evidence and review

The journey assertions preceded their first execution.
The first execution passed against unchanged production code.
There was no missing product behavior and no product RED result.
The retained first-run source hashes identify that uncommitted test candidate.
Later assertion changes strengthened public reads, exact retry identity, and cleanup.
The reviewed focused run and all exact-source gates passed.
No failed test, failed hook, warning, or diagnostic was discarded.

Self-review covered the three source-change files, the existing helper callers, and relevant production contracts.
It checked process ownership, Git isolation, cursor completion, fixture mutation, exact source/ref assertions, and protected hashes.
It also checked the diff and retained logs for credentials and private fixture data.
No unresolved implementation finding remained.
The parent session owns the independent review, canonical PACT evidence, and Kata closure.
This report does not claim that those parent operations have occurred.

## Exact-source verification

All commands below passed on `42ffb83368b94481fbf934fefcf060f79cad4168`.
The source commit also passed every applicable normal hook, including the full race/count=1 test hook.
Hooks used an isolated `PREK_HOME` and a separate lint cache.
The final lint command used another fresh cache.

| Check | Actual command | Result |
| --- | --- | --- |
| Focused compiled journey and recovery | `env -u GOROOT mise exec -- go test ./tests/e2e -run 'TestCLIDevelopmentLifecycle\|TestCLIRecordLostResponseAfterPublication' -count=1 -v` | Passed both tests. |
| Canonical gate | `env -u GOROOT mise exec -- scripts/check` | Passed all Go packages and 18 Python reference tests. |
| Full race suite | `env -u GOROOT mise exec -- go test -race -count=1 ./...` | Passed all packages. |
| Fresh-cache lint | `env -u GOROOT GOLANGCI_LINT_CACHE=<fresh-directory> mise exec -- golangci-lint run` | Zero issues. |
| Compiled usage | `env -u GOROOT mise exec -- sh .scratch/development-lifecycle/check-usage.sh` | Build and public command help passed. |

The retained [usage script](z895-evidence/check-usage.sh) shows each build and help command.
Observed tools were Go 1.26.6 on darwin/arm64, golangci-lint 2.13.2, and Apple Git 2.54.0.
Existing unit and real-store integration suites passed alongside the compiled E2E tests.
No production behavior changed, so this task added no redundant lower-layer unit tests.

Raw logs are losslessly compressed in [z895-evidence](z895-evidence/).
Each exact-source gate has a command record, exit-status file, and source-tagged log.
[SHA256SUMS](z895-evidence/SHA256SUMS) covers every retained evidence file.
[Source hashes](z895-evidence/42ffb83-source.sha256) identify the three tested source-change files.

## Limits and interpretation

The measured read-path evaluation rejected a public `pact brief` command.
The internal projector driver remains test tooling without a 16 KiB or 64 KiB output contract.
Those conditional CLI budget requirements remain unshipped and unclaimed.
This journey instead proves intact large facts, complete existing query paging, and the actual 4 MiB record-input refusal.
It also exercises the existing 1,024-event admission boundary without changing the core limit profile.

The synthetic fixture records reported check and review facts.
Matching signatures and proof bytes do not establish that the reported commands or reviews occurred.
One local trusted root does not establish reviewer independence or organizational approval.
Local closure does not establish global completeness.
No migration, deployment, release, push, main-branch merge, or production-ledger mutation occurred in this task worktree.
