<!-- ABOUTME: Records the measured decision against publishing a development brief command. -->
<!-- ABOUTME: Preserves the public fixture, full answer rubric, raw outputs, and working read recipe. -->

# Development read-path evaluation

Decision: do not publish `pact brief`.
The final paired trial used three tool calls for each path.
The prototype returned fewer bytes and used fewer CLI processes.
The required strict reduction in tool calls did not occur.

Source: `47fbc2f8e4024f7f045c8d00d25d39cfebaae03e`.
Base: `576043e5a3035b95b971dbf0864ba3e57e71afa1`.
The candidate was the existing compiled `internal/development/testdata/projectdriver`.
It emits the existing `development.Project` result and is not a public command.
Production Go sources did not change between these revisions.
The trial binaries were built before the source freeze from those unchanged sources.

## Method and results

Both readers received the same goal, exact source revisions, local policy, and rubric.
Both used the same three public stores and the same transport granularity.
Each reader made one shell tool call per scenario.
Each call retained raw stdout and stderr, then counted their bytes.
The count includes every output newline.
Measurement metadata and post-trial answer writes are reported separately.

| Scenario | Baseline tool calls | Prototype tool calls | Baseline CLI calls | Prototype processes | Baseline stdout bytes | Prototype stdout bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Open Important finding; no acceptance | 1 | 1 | 5 | 1 | 8,718 | 3,083 |
| Explicit correction and acceptance | 1 | 1 | 9 | 1 | 16,844 | 5,153 |
| Concurrent acceptance and rejection | 1 | 1 | 13 | 1 | 24,932 | 7,771 |
| Total | 3 | 3 | 27 | 3 | 50,494 | 16,007 |

Both paths returned zero stderr bytes and exited successfully.
Neither reader needed extra reads.
Both readers retained every required rubric fact.
Neither reader missed a blocker, changed a required fact, or asserted unsupported acceptance.
The byte reduction is 34,487 bytes across this fixture.
It does not convert a tool-call tie into a passing gate.

The baseline batches existing commands, including dependent discovery within the shell call.
It queries the subject, verifies local authority, and shows each unique containing object once.
It checks the selected evidence for equal observed-proof coverage.
This avoids redundant shows for events in the same commit.
The raw baseline does not execute a development projector.
The prototype provides derived conclusions; its raw review payloads and parent edges are not displayed.
Both answers state those limits.

The configured model was `gpt-6-astra`, with `high` reasoning.
The root supplied that configuration for this task agent.
Both final evaluators used `fork_turns: "none"` and omitted model and reasoning overrides.
The tool contract therefore inherited this task agent's configured model.
The exact deployment/build revision was not exposed and remains unknown.
The readers correctly reported that missing runtime metadata.
[Spawn metadata](development-workflow/w5cf-evidence/evaluator-configuration.json) records this distinction.
One paired trial does not establish a general efficiency or cost improvement.

## Complete answer rubric

The namespace is `org/example/development`; the subject is `widget/task-1`.
The initial source is `62ba249ef5cafdee4860900220f826dfad0ba869`.
The corrected source is `fd0a35409b7f10ea96d6f2af192632bb79b5d810`.
Both are retained by the public `source.fast-import` fixture.

| Required fact | Open | Complete | Conflict |
| --- | --- | --- | --- |
| Source and frontier | Initial; one head | Corrected; one head | Corrected; both heads |
| Scoped checks | One passed unit check; `go test ./...`; exit 0 | Same facts on corrected source | Two same-source passed checks |
| Important finding | `finding-1` remains open | Original finding explicitly resolved | Original finding resolved on both branches |
| Resolution refs | Empty | Exact corrected review ref | Both exact corrected review refs |
| Recorded decisions | None | Explicit accepted decision | Explicit accepted and rejected decisions |
| Supported conclusion | `not_recorded` | `supported` | `unresolved`; no winner |
| Checked evidence | Two matching immutable assessments | Five matching immutable assessments | Eight matching immutable assessments |
| Declared action | Correct finding and review correction | Same declaration is historical | Same declaration is historical; no declared conflict action |
| Completeness and authority | Locally closed; global unknown; local root; independence unproved | Same | Same |

Every finding, decision, check, and evidence assessment must retain its exact supporting ref.
The shared proof digest is
`sha256:c3f58fa85abd4403a63dc44eb205a4be351054f86f0f748865d4094160d3f446`.
Each checked artifact contains 77 bytes.
Signed claims and matching evidence do not prove that a check or review ran.

The prototype reports profile digest
`sha256:b89dd93eb851ca4db63e9e29fbd1d060310f2268c8d37ed00c46004f2eafeab9`,
policy digest
`sha256:e9d050d6b7170e608677c0c56b338a888c2ec001499b0119adca52cf0c8fd437`,
and projector `development/projector/v1`, digest
`sha256:2bbf4768d020ecc4d3009aa89f297b519577d67d25f312cd0f9651b2bf8e4d2e`.
These identify local interpretation, not organizational approval or a global checkpoint.
The raw path reports that no projector ran; it does not invent a projector identity.

The [baseline answer](development-workflow/w5cf-evidence/pact-w5cf-final-baseline-answer.json)
and [prototype answer](development-workflow/w5cf-evidence/pact-w5cf-final-prototype-answer.json)
retain full structured answers and exact refs.
The [rubric results](development-workflow/w5cf-evidence/rubric-results.json) record the comparison.
Raw captures, actual command arguments, and all failed trials remain in the evidence directory.

## Failed trials and corpus correction

The first aggregate trial returned 50,495 baseline bytes and 16,007 prototype bytes.
Its baseline needed three calls after output truncation; its prototype needed two calls, including counting.
That comparison included avoidable harness calls and was not used for promotion.

The second aggregate trial requested larger inner and outer output budgets.
The runtime still omitted 1,852 baseline tokens.
That reader correctly withheld a complete acceptance conclusion and marked missing facts.
The prototype fit in one call, but the unequal transport failure invalidated that comparison.
After those two failures, the final protocol used one bounded response per scenario for both paths.
No further trials sought a favorable result.
All original answers and raw command streams are retained.

The initial public correction fixture also failed its claimed supported-acceptance outcome.
Its acceptance payload named checks and reviews, but its causal envelope observed neither local event.
The projector reported `invalid_check_refs`, `invalid_review_refs`, and `unobserved_resolution`.
The correction adds explicit local `caused_by` links in the canonical correction and rejection fixtures.
No workaround fixture, historical proof rewrite, or projector relaxation was introduced.
Declarative labels now match `not_recorded`, `unresolved`, and `locally_closed`.
The new compiled real-store regression consumes those fixture expectations.

## Existing read recipe

Use the existing guide and supported commands:

```sh
pact query --repo PATH --namespace NAMESPACE --subject SUBJECT --json
pact show --repo PATH EXACT_OBJECT_ID --json
pact evidence check --repo PATH --event-ref EXACT_EVENT_REF --json
pact verify --repo PATH --strict --json
```

Follow every `page.next_cursor` while `page.has_more` is true.
Keep the same query filters and limit while continuing a cursor.
A cursor continues a fixed snapshot; it is not a changes-since marker.
Show each unique returned object once to inspect all its events atomically.
Alternatively, show an exact event ref when only that event is needed.
Read payloads and evidence; query summaries alone do not support acceptance.
Resolve `local:` payload refs within their containing commit.
Preserve concurrent claims and historical findings until explicit resolution.
Check selected immutable evidence and state which bytes were actually checked.

If indexed reads return `index_missing` or `index_stale`, preserve the error.
They do not rebuild or mutate the store.
An explicit recovery command is:

```sh
pact index rebuild --repo PATH --json
```

Retry the query after that intentional write.
Exact `show` remains available while the index is missing or stale.
The compiled regression snapshots files around reads and verifies one-item cursor paging.
Existing compiled index tests cover stale, corrupt, incompatible, and partial indexes.

The [replay script](development-workflow/w5cf-evidence/read-trial.sh) batches this fixture recipe.
Recreate the public stores in a fresh, nonexistent temporary directory:

```sh
PACT_READ_EVALUATION_DIR=/tmp/pact-read-trial-fresh env -u GOROOT mise exec -- go test ./tests/e2e -run TestCLIDevelopmentReadEvaluation -count=1 -v
sh docs/status/development-workflow/w5cf-evidence/read-trial.sh baseline /tmp/pact-read-trial-fresh open
```

The test retains no private signing key in the exported fixture.
Do not use the recorder or setup against a real repository for this evaluation.

## Scope of the negative result

No public command, catalog row, human renderer, or output-budget flag was added.
The existing tested projection library remains available to internal callers.
The unpublished driver has no 16 KiB/64 KiB output contract.
Its measured small outputs do not prove escaped-byte boundaries or overflow handling.
Those conditional CLI requirements were not implemented or claimed after the gate failed.
The existing query byte limits and cursor behavior remain unchanged.

This task evaluates a candidate before publication.
The later adoption task must separately evaluate the capabilities actually shipped.
