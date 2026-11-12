<!-- ABOUTME: Defines the bounded brief CLI kata and its observable acceptance checks. -->
<!-- ABOUTME: Keeps projection semantics with their existing owner and measures the real public command. -->

# Implement and verify the bounded brief command

Read ../implementation-plan.md for the approved shared contract.
Base: the controller's committed planning cut on feat/bounded-brief.

## Files and interfaces

- Create cmd/pact/brief_command.go and cmd/pact/brief_render.go.
- Create cmd/pact/brief_command_test.go.
- Modify cmd/pact/command_catalog.go and the document-output adapter only as needed.
- Modify the Project request-error type and its tests for explicit owner classification.
- Create tests/e2e/bounded_brief_test.go; reuse the existing public corpus/read helpers.
- Add the recurring scenario to scenarios.jsonl.
- Write docs/status/bounded-brief/command-report.md and retained evidence.

Consume development.Project(ctx, store, ProjectRequest) and WorkState without changing their meaning.
Produce runBrief(args []string, stderr io.Writer, config runConfig) (commandResult, error).
Use one state representation and serialize each chosen output exactly once.
The existing document result can carry pre-encoded bounded output; preserve shared writer behavior.
Inspect index.Manager.Status and owner errors before choosing the readiness guard.
Do not nest owner locks or introduce a second snapshot/policy implementation.
The controller approved ProjectRequestError for the existing identity, duplicate-frontier, and namespace request failures.
Preserve their messages and semantics; classify unexpected read failures separately.

## Test-first steps

- [ ] Add failing catalog/usage, real-store JSON, human, and writer-error scenarios.
- [ ] Add actual encoded-byte boundaries at required-1, required, and required+1.
- [ ] Include quotes, backslashes, control characters, Unicode, and the final newline.
- [ ] Prove valid small budgets return a bounded limit error with empty stdout.
- [ ] Prove an oversized blocking finding cannot become a partial accepted view.
- [ ] Exercise missing/stale indexes, explicit frontier stability, and protected file hashes.
- [ ] Exercise open Important findings, absent decisions, explicit resolutions, and concurrent decisions through compiled CLI.
- [ ] Implement the minimal catalog adapter and shared-state renderer to pass those cases.
- [ ] Measure the actual command against efficient query/show/verify/evidence reads on identical public stores.
- [ ] Report every required fact/ref, stdout byte, top-level CLI count, outer call count, and failure separately.
- [ ] Self-review, commit with hooks, run exact-source focused/canonical/race/fresh-lint gates, and retain proof.

The public JSON must preserve the complete WorkState, with operation=brief added.
Human output must retain meaningful proof and blocker fields, not merely an overall status.
Do not change the engine to make a renderer test pass.
Source existence and reported execution remain distinct from matching proof.
All reads must leave canonical objects, trust, index, and evidence unchanged.

## Verification

```sh
env -u GOROOT mise exec -- go test ./cmd/pact ./internal/development ./tests/e2e
env -u GOROOT mise exec -- scripts/check
env -u GOROOT mise exec -- go test -race -count=1 ./...
env -u GOROOT GOLANGCI_LINT_CACHE=TASK_FRESH_CACHE mise exec -- golangci-lint run
```

The final command must pass the shared revised gate before task closure.
Report DONE, DONE_WITH_CONCERNS, BLOCKED, or NEEDS_CONTEXT with exact commits and evidence paths.
The controller owns independent review, PACT appends, and Kata closure.
