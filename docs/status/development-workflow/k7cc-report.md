<!-- ABOUTME: Records task-eight source, fresh-context observations, verification, and known limits. -->
<!-- ABOUTME: Leaves independent review, canonical PACT publication, and Kata closure to the coordinator. -->

# Development workflow adoption proof

Task: `pact#k7cc`, development workflow task 8.
Branch: `feat/development-adoption`.
Base: `851bd51ef1531bae29141b4fad903fd915f35acd`.
Frozen source: `5782f2499825d6090f8838ed755cffb2edbf713e`.

The original guide now documents the shipped validated recorder and evidence checker.
The README points to that single guide and lists the actual public commands.
The quickstart parity test remains intact.
A compiled E2E test executes the authored guide example with real runtime bindings.
It verifies created admission, exact retry identity, canonical event facts, matching Git proof, and a current index.
No production source, runtime dependency, wire format, trust rule, query cursor, or security control changed.

The [adoption evaluation](../development-workflow-evaluation.md) records one paired fresh-context observation.
Both readers recovered the required facts using only shipped public reads.
Both used four trial tool calls, 27 public CLI invocations, and 50,554 CLI stdout bytes.
Neither missed a blocker or selected a winner for concurrent conflicting decisions.
The candidate's longer guide input was truncated once by the outer tool budget.
Its scenario outputs were complete; no pair was rerun to seek a win.
This is a documented transport regression and no demonstrated read-effort improvement.
The result does not justify publishing the rejected public brief command.

## Contracts, review, and evidence boundaries

The [rubric](k7cc-rubric.md) and guide assertions preceded the guide edits.
The first contract execution failed because the old guide lacked the new command and executable example.
[The retained log](k7cc-evidence/guide-contract-before.log.gz) preserves both expected failures.
These were documentation contract failures, not a manufactured product regression.
The authored example passed against unchanged product behavior after the guide update.
The [reviewed run](k7cc-evidence/guide-reviewed.log.gz) preserves parity and runtime checks.

Self-review inspected all six source-change files and the relevant command, evidence, retry, schema, and policy owners.
It checked shell quoting, Git environment isolation, temporary ownership, exact retry identity, and layer boundaries.
It also checked changed documents and retained output for secret material and private data.
No material implementation finding remains open from that review.
The parent session owns the independent task and whole-branch reviews.

The existing export helper prepared each public store once before the source freeze.
Both readers used those same stores and that same executable.
Public runtime command owners were unchanged between the base and frozen source.
The preparation executable was built before the final guide freeze; readers did not invoke its embedded quickstart.
Its SHA-256 is retained in evaluator configuration.
Exact-source focused and usage gates subsequently rebuilt the CLI from the frozen guide source.
Readers received the full exact fixture-source goals and abbreviated evaluation source `5782f24`.
The full evaluation source is recorded here; both answers correctly report that its full form was not supplied to them.

Both reader spawns explicitly selected `gpt-6.1-sol`, `high`, and `fork_turns: none`.
The exact runtime deployment/build metadata was unavailable and remains unknown.
The guide input truncation, call counts, and archival overhead remain visible in the retained answers.
Raw CLI captures retain every argv, exit status, stdout byte, and stderr byte in a lossless archive.
The source guides remain available through their exact Git revisions; no second embedded guide was added.

## Exact-source verification

Every check below passed on `5782f2499825d6090f8838ed755cffb2edbf713e`.
The source commit passed all applicable normal hooks, including full `go test -race -count=1 ./...`.
Hooks used isolated `PREK_HOME=/tmp/pact-k7cc-prek-source` and a separate lint cache.
The final lint gate used another fresh cache.

| Check | Actual command | Result |
| --- | --- | --- |
| Guide and quickstart | `env -u GOROOT mise exec -- go test ./cmd/pact ./tests/e2e -run 'TestQuickstart\|TestCLIDevelopmentGuideExample' -count=1 -v` | All selected tests passed. |
| Real authored examples | `sh docs/status/development-workflow/k7cc-evidence/check-usage.sh` | Compiled CLI, guide example, bound README commands, and byte parity passed. |
| Canonical gate | `env -u GOROOT mise exec -- scripts/check` | All Go checks and 18 Python reference tests passed. |
| Full race suite | `env -u GOROOT mise exec -- go test -race -count=1 ./...` | All packages passed. |
| Fresh lint | `env -u GOROOT GOLANGCI_LINT_CACHE=/tmp/pact-k7cc-lint-5782f24 mise exec -- golangci-lint run` | Zero issues. |

The [verification runner](k7cc-evidence/verify-source.sh) retains exact commands and exit statuses.
Source-tagged logs are compressed losslessly in [k7cc-evidence](k7cc-evidence/).
No failed test, hook result, or trial was overwritten by a retry.
[SHA256SUMS](k7cc-evidence/SHA256SUMS) covers every evidence file except itself.
The proof commit adds the reports, answers, metrics, and logs separately from source.
No successful source check was repeated without a source-boundary requirement or new change.

## Remaining limits and handoff

The candidate guide grew by 7,247 bytes and exceeded the first context-read outer budget.
Future full-guide comparisons must set both inner and outer input budgets explicitly.
This pair is one local observation with constrained retrieval, not a general adoption or efficiency result.
The raw reads do not execute the internal development projector or external domain schema validators.
Both answers preserve that boundary while interpreting the observed facts manually.
Matching proof and signatures do not establish check execution, reviewer independence, or organizational approval.
Local closure does not establish global completeness.

No canonical project-ledger write, tracker update, second project ledger, main merge, push, release, or deployment occurred here.
Only temporary test stores were written.
The coordinator must review the source/proof pair, publish canonical PACT evidence, and close Kata after verification.
