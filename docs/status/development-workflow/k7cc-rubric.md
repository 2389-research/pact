<!-- ABOUTME: Freezes the fresh-context adoption protocol before the guide changes. -->
<!-- ABOUTME: Defines identical public inputs, answer requirements, and measurement limits for both readers. -->

# Adoption trial contract

Authored before editing the guide or running either reader.
Source base: `851bd51ef1531bae29141b4fad903fd915f35acd`.
Use one exported public fixture from `TestCLIDevelopmentReadEvaluation`.
Both readers use those same stores and the compiled public CLI.
Never expose fixture keys, expected outputs, prior transcripts, or broad reports.

The baseline receives the original embedded guide from the source base.
The candidate receives the updated original guide.
Both receive this goal and rubric, the same helper, and the same output budget.
The helper batches public query, verify, show, and evidence check commands.
Run one bounded tool response for each scenario: open, complete, conflict.
Forward raw output without filtering authoritative facts.
Retain each command, exit code, stdout, and stderr, including newlines.
Count reader tool calls separately from CLI calls and helper processes.
Retain failed or truncated responses; do not rerun to seek a favorable result.

## Reader goal and rubric

Recover the supported handoff for namespace `org/example/development`, subject `widget/task-1`.
The initial source goal is `62ba249ef5cafdee4860900220f826dfad0ba869`.
The corrected source goal is `fd0a35409b7f10ea96d6f2af192632bb79b5d810`.
Use the initial goal for open; use the corrected goal for complete and conflict.

For each scenario, report the exact heads, source, check refs, command, outcome, and exit code.
Report the Important finding ID, summary, original review ref, status, and exact resolving refs.
Report every recorded acceptance decision and its exact check/review refs.
Keep missing acceptance as `not_recorded` despite a green check.
Keep incompatible concurrent decisions as `unresolved`; select no winner.
Distinguish explicit resolution from merely passing a later check.
Report each checked evidence ref, digest, byte count, assessment, and supporting event ref.
State the declared next action and whether it remains applicable or historical.
Distinguish local closure from global completeness.
State the local-root authority basis and the limits of shared-key reviewer independence.
Distinguish schema validation, signatures, authority, evidence availability, recorded decisions, and interpreted support.
Do not claim the raw CLI ran the internal development projector.
Do not claim signed statements or matching bytes prove checks or reviews occurred.

## Assessment

Score each requirement against the retained raw outputs and public fixture.
Record missed blockers, missing required facts, unsupported assertions, and command failures.
Count actual reader tool calls, CLI calls, stdout bytes, and stderr bytes.
Record helper and measurement overhead separately.
Record the configured model identifier and reasoning for each reader.
Keep unavailable runtime deployment metadata explicitly unknown.
A single pair establishes one local observation only.
Publish ties, regressions, and omissions alongside any improvement.
The prior brief publication rejection remains in force.

## Guide verification contract

Preserve the existing byte-parity quickstart test.
Assert the guide names the shipped record and evidence commands before editing it.
Execute the authored shell example against a compiled CLI, temporary Git repository, external key, store, and index.
Bind every runtime variable to a real fixture value.
Verify the created record, exact retry identity, canonical event, matching Git evidence, and current index.
Check the README inventory against the actual command catalog.
Use existing product behavior; a documentation assertion failure is not a product regression.
