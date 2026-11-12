<!-- ABOUTME: Records task-six source, validation, review, and the rejected brief promotion gate. -->
<!-- ABOUTME: Leaves canonical PACT publication and Kata closure to the coordinating task. -->

# Development brief evaluation: w5cf

Result: tested negative evaluation; do not publish `pact brief`.
The final paired readers each used three tool calls and retained every rubric fact.
The prototype used fewer CLI processes and fewer bytes, but the promotion rule requires fewer tool calls.
See the [evaluation](../development-read-evaluation.md) for commands, captures, rubric, and interpretation limits.

## Source and scope

- Branch: `feat/development-brief`.
- Base: `576043e5a3035b95b971dbf0864ba3e57e71afa1`.
- Frozen source: `47fbc2f8e4024f7f045c8d00d25d39cfebaae03e`.
- Source commit: `test: verify development handoffs against public corpus`.
- Production Go sources, core wire formats, trust rules, raw commands, and query cursors are unchanged.
- The proof commit contains this report and retained evidence only.

The source adds compiled real-store read-recipe coverage using the canonical public corpus.
It exercises open findings, explicit correction, concurrent decisions, exact proof checks, one-item paging, and missing-index reads.
File snapshots prove those reads do not mutate the fixture.
Existing compiled tests cover stale-index refusal and intentional recovery.
Both fixture Git operations and existing evidence reads retain their Git-environment isolation.
Generated fixture keys stay outside exported stores and are not retained in evidence.

No public brief adapter, command registration, renderer, or budget flag was added.
Default 16 KiB, maximum 64 KiB, escaped-byte boundaries, and human output remain conditional unshipped requirements.
The tested prototype is the existing test driver, not a supported operator command.
The documented existing read recipe remains the default.
Task eight still owns the separate shipped-capability adoption trial.

## Findings and fixes

### Public corpus causality

The original correction fixture recorded local proof references without local causal edges.
The real projector correctly returned `unsupported`, with three reasons:
`invalid_check_refs`, `invalid_review_refs`, and `unobserved_resolution`.
This contradicted the corpus's declared positive expectation.

The fix adds explicit `caused_by` references in the correction and rejection acceptance events.
It also aligns declarative labels with `not_recorded`, `unresolved`, and `locally_closed`.
The compiled regression reads the canonical expected fixtures; it does not keep a workaround copy.
The earlier profile report remains immutable historical evidence of its narrower checks.

Parent-recorded finding:
`pact:event:sha256:13b9ff3e4b854b2052839df3803ef8a992503b4cbdb1ac80c5cf94f093a4eb32#corpus-review`,
finding ID `corpus-causality`, source `576043e5a3035b95b971dbf0864ba3e57e71afa1`.
The parent owns explicit PACT resolution and closure.

### Hook timeout and ignored arguments

The first normal source commit failed after the E2E package reached 30 seconds.
The running existing PTY test had started less than one second earlier.
The configured remote runner hardcoded `-tags=unit -timeout 30s -short -v` and ignored configured arguments.
It therefore never honored the intended `-race -count=1 ./...` arguments.
The full failed log is retained losslessly.

The coordinator approved replacing only that hook with a local system `go test` entry.
The hook keeps its ID/name and runs the intended full race suite with the normal Go timeout.
All other hooks remain enabled.
No hook was bypassed, no test was excluded, and no timeout exception was added.
The subsequent normal source commit passed every applicable hook.

## TDD and observed failures

`initial.log.gz` preserves the first evaluation run, including its cursor-helper mistake.
`fixture-debug.log.gz` preserves the corpus regression's exact unsupported reasons.
It also exposes the test helper's incorrect expected missing-index exit code.
Those helper errors were corrected from the actual query page and error contracts.
`recipe-debug.stderr` preserves the replay script's initial wrong object nesting assumption.
The script now reads the actual `object.body.events` field.
The initial partial recipe stdout was overwritten before retention; its complete error stream survives.
This diagnostic-only gap does not affect the retained final trials or corpus RED/GREEN evidence.

`corpus-green.log.gz` records all three public projection outcomes passing after the canonical fixture fix.
`read-recipe.log.gz` records the final regression plus existing index-state recovery scenarios.
The initial and controlled aggregate reader answers retain their transport failures.
The final per-scenario comparison retains zero errors and complete answers.
The evaluation describes why neither failed aggregate trial supports a promotion claim.

## Frozen-source verification

All commands below selected the managed Go toolchain with `env -u GOROOT mise exec --`.
The source hook used isolated `PREK_HOME=/tmp/pact-w5cf-prek-source`.
Lint used fresh task-specific caches to avoid cross-worktree cache results and locks.

| Actual command | Result | Retained evidence |
| --- | --- | --- |
| `go test ./cmd/pact ./internal/development ./tests/e2e` | Exit 0; cached after canonical gate | `47fbc2f-focused.log.gz`, `.exit` |
| `scripts/check` | Exit 0; Go checks and 18 reference tests passed | `47fbc2f-check.log.gz`, `.exit` |
| `go test -race ./...` | Exit 0; all packages passed | `47fbc2f-race.log.gz`, `.exit` |
| `GOLANGCI_LINT_CACHE=/tmp/pact-w5cf-lint-47fbc2f golangci-lint run` | Exit 0; zero issues | `47fbc2f-lint.log.gz`, `.exit` |
| Normal source `git commit` with isolated hook caches | Exit 0; all applicable hooks passed | `47fbc2f-source-hook.log.gz` |

The pre-freeze native oracle also passed:
`uv run --with-requirements docs/pact-ledger/requirements-dev.txt python tests/fixtures/development/oracle.py .`.
It checked 40 payloads in 16 public cases.
`sh -n` validated the replay script; real replay exercised its commands.
The full race gate ran after the source commit; its E2E package completed successfully in 44.852 seconds.
No successful gate was rerun without a source-boundary requirement or a concrete failure.

## Review and remaining limits

The self-review inspected every changed source, fixture, hook entry, and command path.
It checked real-store ownership, key exclusion, selected proof parity, complete paging, and read-only recovery behavior.
The source's focused tests and fresh lint passed before freezing.
The normal hook then exposed the separate runner defect, which was fixed and verified.
No material source finding remains open in this task's review.
The coordinating agent owns the independent integration review and canonical publication.

The configured evaluator model was inherited `gpt-6-astra`, `high` reasoning.
Its exact runtime deployment revision was not exposed.
One synthetic paired trial does not establish general efficiency or accuracy benefits.
The prototype exposes derived review support, not every raw review payload or parent edge.
Both readers state that limitation and preserve local-root and global-completeness uncertainty.
The strict gate rejection is the release recommendation for this task.

## Evidence and handoff

All artifacts under `w5cf-evidence/` are synthetic inputs, public outputs, or local validation logs.
The retained commands name temporary stores, never the canonical project ledger.
No private signing key, consumer transcript, or private application data was copied.
`SHA256SUMS` names and hashes every retained evidence file, excluding itself.
Retained runtime output and failure logs remain byte-exact; compressed logs are lossless.

The parent must inspect this source/proof pair, append the corpus finding resolution, and record the gate rejection.
The parent must verify canonical PACT state and close Kata only after its own integration review.
This task did not initialize or write the project ledger, merge main, push, deploy, or close Kata.
