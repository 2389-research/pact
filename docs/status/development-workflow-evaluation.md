<!-- ABOUTME: Records the fresh-context adoption result for the shipped development workflow. -->
<!-- ABOUTME: Preserves a tied read result, a guide transport regression, and the supported command boundaries. -->

# Development workflow adoption evaluation

Decision: publish the supported recorder and read recipe in the existing guide.
This pair demonstrates no read-effort improvement.
Both readers recovered the required handoff facts with the existing public commands.
The longer candidate guide also suffered one input truncation.
The prior decision against a public `pact brief` remains unchanged.

Source: `5782f2499825d6090f8838ed755cffb2edbf713e`.
Base and baseline guide: `851bd51ef1531bae29141b4fad903fd915f35acd`.
The candidate is the updated original `docs/pact-ledger/SKILL.md`, version 0.2.3.
`pact quickstart` embeds that same file; no duplicate guide was created.
See the [task report](development-workflow/k7cc-report.md) for exact-source checks and source/proof boundaries.

## One paired public-fixture observation

Both readers started with `fork_turns: none` and no prior transcript.
Both used explicitly configured `gpt-6.1-sol` with `high` reasoning.
The runtime deployment and build revision were unavailable.
[Spawn arguments](development-workflow/k7cc-evidence/evaluator-configuration.json) retain that distinction.
Both received the same source goals, [authored rubric](development-workflow/k7cc-rubric.md), helper, and output budgets.
The rubric and guide assertions preceded guide edits.
The only intended reader-input difference was the guide revision.

The existing exported public-store test prepared the fixtures once.
Both readers then used those identical three stores and the same compiled CLI.
Generated private keys stayed outside the exported stores and were not retained.
The fixture contains synthetic public data, not private consumer history.
The fixture preparation test uses the internal projector to verify its own setup.
Neither reader received its oracle, projector output, driver, prior reports, or expected files.
Their read path used only shipped `query`, `verify`, `show`, and `evidence check` commands.

The [capture helper](development-workflow/k7cc-evidence/read-adoption-trial.sh) batches one scenario per tool response.
It forwards raw stdout and stderr without JSON-encoding the returned stream.
It shows each unique object once and checks each selected evidence-bearing event.
It saves exact argv, individual exit codes, and both output streams before forwarding them.
This constrained recipe holds retrieval constant while comparing guide-assisted interpretation.
It does not measure unrestricted command discovery or establish general adoption benefits.

| Scenario | Tool reads each | Public CLI calls each | Baseline stdout bytes | Candidate stdout bytes |
| --- | ---: | ---: | ---: | ---: |
| Open Important finding; no acceptance | 1 | 5 | 8,734 | 8,734 |
| Explicit correction and acceptance | 1 | 9 | 16,864 | 16,863 |
| Later concurrent acceptance and rejection | 1 | 13 | 24,956 | 24,957 |
| Scenario total | 3 | 27 | 50,554 | 50,554 |

Both readers made four actual tool calls: one guide/rubric read and three scenario reads.
Each tool call contained one `exec_command` invocation.
Each reader used three helper-shell processes and 27 top-level public CLI processes.
Subordinate Git and operating-system processes were not instrumented.
All 54 public CLI invocations exited zero and emitted zero stderr bytes.
The [metrics](development-workflow/k7cc-evidence/metrics.json) count every CLI newline.
Small per-scenario differences come from local evidence observation timestamps.
The scenario totals tie exactly.
Each reader made one later archival tool call to save its completed answer.
Those post-trial saves performed no new fixture reads and are separate from trial counts.

## Correctness and transport results

Both answers retained exact heads, sources, check refs, finding refs, resolutions, decisions, and proof refs.
Both kept the open Important finding despite a passing check and absent acceptance.
Both distinguished explicit correction from a later green check.
Both preserved the concurrent accepted and rejected decisions without choosing a winner.
Both treated the old next action as historical after explicit resolution.
Both separated local-root authority, shared-key independence, evidence availability, and global uncertainty.
Neither claimed that a raw read ran the development projector or validated external domain schemas.
Neither claimed matching signatures or proof bytes established that the reported checks or reviews occurred.
The [rubric assessment](development-workflow/k7cc-evidence/rubric-results.json) records zero missed blockers and zero unsupported claims.
The baseline described positive support in prose without claiming a projector status.
The candidate explicitly described manual local support under the supplied guide.
These are supported manual interpretations, not public projector results.

The baseline guide contained 35,463 bytes; the candidate contained 42,710 bytes.
The common rubric contained 3,835 bytes.
Guide/rubric input grew from 39,298 to 46,545 bytes, a 7,247-byte regression.
The first-call inner tool budget was 18,000 tokens.
The outer `functions.exec` budget remained its default 10,000 tokens for that context read.
The candidate reported `Warning: truncated output (original token count: 11637)`.
A middle portion of its guide was omitted; the rubric and scenario streams remained visible.
The baseline reported no truncation.
All scenario responses used the same explicit 14,000-token outer and 12,000-token inner budgets.
Neither scenario path was truncated, failed, or retried.
No second pair sought a favorable result.

This transport regression prevents a claim that the entire candidate guide reached its reader.
The retained [candidate answer](development-workflow/k7cc-evidence/candidate-answer.json) reports that limit directly.
The [baseline answer](development-workflow/k7cc-evidence/baseline-answer.json) retains its own uncertainty and observations.
Future full-guide trials need an explicit outer context-read budget for both readers.
This is a local synthetic observation, not a general accuracy, cost, or performance result.

## Source goals and proof contract

The namespace is `org/example/development`; the subject is `widget/task-1`.
The initial source is `62ba249ef5cafdee4860900220f826dfad0ba869`.
The corrected source is `fd0a35409b7f10ea96d6f2af192632bb79b5d810`.
Both revisions come from the public `tests/fixtures/development/source.fast-import` input.
Every reported check names `go test ./...`, outcome `passed`, and exit zero.
Those remain synthetic signed claims; the readers did not run that reported command.

| Required result | Open | Complete | Conflict |
| --- | --- | --- | --- |
| Frontier | One initial head | One corrected head | Both corrected-source heads |
| `finding-1` | Important and open | Explicitly resolved by corrected review | Explicitly resolved on both branches |
| Recorded acceptance | None | Accepted with exact check/review refs | Accepted and rejected with both proof chains |
| Manual support | `not_recorded` | Supported locally | `unresolved`; no winner |
| Checked immutable evidence | Two matching assessments | Five matching assessments | Eight matching assessments |
| Declared action | Correct and review the finding | Historical | Historical; no declared conflict action |
| Authority/completeness | Local root; shared key; locally closed; global unknown | Same limits | Same limits |

Every checked blob contains 77 bytes with digest
`sha256:c3f58fa85abd4403a63dc44eb205a4be351054f86f0f748865d4094160d3f446`.
Equal proof digests do not independently prove the source correction.
The answer dictionaries retain every exact object, event, source, signer, and proof reference.
The local policy digest is
`sha256:e9d050d6b7170e608677c0c56b338a888c2ec001499b0119adca52cf0c8fd437`.
It identifies local interpretation, not organizational approval or a signed global cut.

## Supported operator workflow

Use the [single canonical guide](../pact-ledger/SKILL.md#record-development-facts) for recording and recovery.
The real example checks a committed Git blob, records the result, retries exactly,
then uses public reads to inspect the event and evidence.
Its executable test binds a compiled CLI, temporary Git repository, external key, store, and index.
The [usage script](development-workflow/k7cc-evidence/check-usage.sh) also executes the authored README commands with real bindings.

An exact retry preserves operation identity within namespace and signer.
Changed facts require a different operation ID.
An admitted operation can still have a failed derived-index refresh.
Keep its original IDs and retry exactly or rebuild the index explicitly.
Raw writes require explicit index rebuilding before indexed reads.
Keep schema validation, signatures, authority, evidence availability, recorded decisions, and supported acceptance separate.

Recreate the same public stores in a new nonexistent temporary directory:

```sh
PACT_READ_EVALUATION_DIR=/tmp/pact-adoption-fresh env -u GOROOT mise exec -- go test ./tests/e2e -run '^TestCLIDevelopmentReadEvaluation$' -count=1 -v
sh docs/status/development-workflow/k7cc-evidence/read-adoption-trial.sh /tmp/pact-adoption-fresh open /tmp/pact-adoption-open-capture
```

These commands create temporary test stores and diagnostic captures only.
They do not target the canonical project ledger.
The internal test driver remains unpublished; no brief catalog entry or budget flags were added.
