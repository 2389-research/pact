<!-- ABOUTME: Records the approved bounded brief follow-up and revised promotion evidence. -->
<!-- ABOUTME: Coordinates two independently verified katas above the existing development projector. -->

# Bounded brief implementation plan

Use subagent-driven development for each task and one final integration review.

Goal: expose reliable development state with bounded output and a short operator guide.
Architecture: adapt development.Project through the current command catalog and presentation pipeline.
Keep canonical history, domain policy, and derived-index ownership unchanged.
Stack: existing Go module and standard-library CLI; no new dependency.

## Authorization and source

Doctor Biz approved this follow-up on 2026-11-12 with "lets do it".
The approval covers the bounded command, revised measurement gate, shorter guide, and their tests.
Base: 082729690e107a66faaff4c94323d34c40e3d7cd, after the v1.1.1 release receipts.
Branch: feat/bounded-brief; worktree /Users/harper/.codex/worktrees/add2/pact.
Canonical PACT store: /Users/harper/Public/src/2389/pact/.pact, namespace org/2389/pact.
The previous epic and negative brief evaluation remain completed historical evidence.
This follow-up does not reopen or rewrite them.

## Operator contract

```text
pact brief --repo PATH --profile development/v1 --namespace NAME --subject SUBJECT --source-revision FULL [--frontier ID] [--max-bytes N] [--json]
```

Require the fixed profile, exact namespace, subject, and full source revision.
Repeat --frontier for an explicit cut; omission captures current namespace heads once.
Use repository discovery and the existing read-only index readiness/error owners.
Missing, stale, corrupt, or incompatible indexes require explicit recovery; never rebuild on a read.
Derive state only with Project; never reimplement acceptance, authority, or finding resolution in the adapter.
Keep the exact captured cut visible if another writer advances history.
Do not describe a separate index observation as an atomic guarantee about a later cut.

JSON contains operation=brief plus every WorkState field at the top level.
Human output uses that same state and preserves every required fact and reference.
Show source/frontier/profile/policy/projector, checks, findings, decisions, evidence, uncertainty, and declared actions.
Escape untrusted control characters in human text.
Do not read or print evidence contents beyond existing Project behavior.
Do not infer a next action or acceptance that the engine did not report.

Default maximum encoded output: 16,384 bytes.
Valid --max-bytes range: 1,024 through65,536 bytes.
Count the actual chosen format, escaping, and trailing newline before writing stdout.
Never emit a partial successful view.
If required facts do not fit, return exit2 with details.code=brief_output_limit and bounded recovery guidance.
Include requested and required byte counts in that error.
Keep the diagnostic under1,024 bytes; do not echo payload text into it.
Use existing owner exit classifications for other errors.
Keep writer failures visible as brief output failures, while preserving quickstart's existing diagnostic.

## Revised promotion gate

Correctness is mandatory on the same open, corrected, and conflicting public histories.
Require zero missed or changed required facts and zero unsupported acceptance.
Require fewer top-level public CLI invocations and no increase in returned bytes for each scenario.
Report outer tool calls separately; their count may tie when both paths batch shell work.
Report failures, truncation, input bytes, and uninstrumented subprocess work explicitly.
Do not claim total process, time, token, or general efficiency improvements from these measurements.
Retain the prior3:3 shell-call rejection as valid under its original rule.
Run the new gate against the actual public brief command, not the test driver.

## Execution

1. [Task1: bounded public command and same-fixture measurement](tasks/01-command.md).
2. [Task2: short single-source guide and fresh-context exercise](tasks/02-guide.md).

Task2 depends on task1's approved implementation.
The controller owns Kata, PACT, progress, integration, and final publication records.
Each implementer owns its task files; reviewers remain read-only.

## Common checks

Write meaningful failing scenarios before each behavior change.
Use real stores, keys, Git, SQLite, and compiled processes; add no mocks or test modes.
Strip inherited GIT_* variables in fixture Git processes.
Preserve partial and failed output; compress raw logs losslessly when formatting hooks would alter them.
Run focused tests, scripts/check, full race, fresh-cache lint, and normal hooks.
Use env -u GOROOT mise exec -- for the toolchain.
Go tests require uv; authored shell examples also require jq.
Record one event per actual check argv; do not combine separate programs into one command array.
Keep private keys outside the project and out of evidence.
Review actual source after tests; fix material findings and rerun affected checks.
Close only verified katas, with fresh PACT and Kata readback.
