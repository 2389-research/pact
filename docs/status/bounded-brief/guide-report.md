<!-- ABOUTME: Records the short single-source operator guide and fresh-reader exercise. -->
<!-- ABOUTME: Separates state correctness, original proof scope, guide bytes, streams, and calls. -->

# Short guide and native handoff evidence

Doctor Biz approved this task in the bounded-brief implementation plan.
Source commit: `b1e00dd1c9b2280c18af0ce50a65191dc24480d0`.
Source tree: `3ccab6608dbbdacc9a1b2dc64c0cb82d1fef2608`.
Base: `48bc63640fbd41028eb4a1bf5bf59625070834b9`.
The controller owns independent review, integration, PACT, and Kata closure.

The original `docs/pact-ledger/SKILL.md` remains the only embedded operator guide.
It and raw `pact quickstart` now contain 12,284 UTF-8 bytes, below the 12,288-byte limit.
The historical source contained 42,710 bytes.
Metadata advances from 0.2.3 to 0.2.4; no release, tag, or package ZIP changed.
No command, projector, profile, schema, policy, or canonical wire owner changed.

The guide retains all 19 shipped commands, canonical ledger selection, and external key/trust boundaries.
It keeps exact retries, admission/index separation, explicit rebuilds, and read-only errors.
It preserves blockers, explicit resolution, conflicting decisions, exact refs, and actually checked evidence.
It distinguishes exit success from acceptance and old declared actions from current work.
The native example reads the exact source recorded by the executable record example.
Both comment markers remain available to the real E2E extractor.

Long protocol detail remains discoverable through eight existing reference links:

| Removed detail | Existing reference |
| --- | --- |
| Mission, layout, object DAG, operating model | `references/architecture.md` |
| Canonical bytes, vocabulary, evidence, checkpoints | `references/object-model.md` |
| Trust bootstrap, delegation, epochs, revocation | `references/authority-model.md` |
| Validation layers, partial order, correction, conflicts, manifests | `references/projection-contract.md` |
| Core events, schema and policy registration | `references/event-catalog.md` |
| Reference CLI and future operations | `references/cli-contract.md` |
| Immutable secret risks and safety limits | `references/threat-model.md` |
| Workflow adoption and integration | `references/adoption-guide.md` |

The short guide labels broader reference operations as protocol detail, not shipped commands.
The E2E checks every authored reference link and actual native brief help flags.
The inventory test reads the real command catalog, rather than a second copied command list.
Existing raw and JSON source-parity checks remain intact.

## Test-first proof

[The RED log](guide-evidence/guide-red.log.gz) shows the expected 42,710-byte budget failure and missing brief marker.
The first implementation attempt retained 12,312 bytes; its [failed log](guide-evidence/guide-green-over-budget.log.gz) remains intact.
Its real record and brief examples already passed.
The final [GREEN log](guide-evidence/guide-green.log.gz) passes both contracts.
The tests execute the extracted authored snippets through a compiled CLI, real Git, an external key, a store, and SQLite.
They retain exact retry identity, exact Git blob evidence, source/event refs, and `not_recorded` acceptance.
Fixture Git processes strip all inherited `GIT_*` variables through the existing helper.

[Iteration events](guide-evidence/iteration-events.jsonl) retain each actual test or commit argv separately.
A harmless preparation-shell typo occurred after successful setup; its [diagnostic](guide-evidence/preparation-shell-error.txt) remains recorded.
No fixture preparation or reader scenario was repeated.

## One paired fresh-context exercise

Both fresh readers used configured model `gpt-6.1-sol`, reasoning `high`, and `fork_turns=none`.
The runtime deployment/build version was unexposed; it is not the configured model identifier.
[Baseline spawn arguments](guide-evidence/baseline-spawn-arguments.json) and [candidate spawn arguments](guide-evidence/candidate-spawn-arguments.json) retain the actual messages and configuration.
Both received the full evaluation source SHA above, identical goals, and the same pre-read [fact rubric](guide-evidence/fact-rubric.md).
Neither received tests, oracle, expected fixtures, prior transcripts, or broad reports.

One fixture preparation ran at the frozen source through the existing public-fixture helper.
It built one actual executable and prepared open, corrected, and conflicting public stores once.
Both readers then used that executable and those exact same store paths.
The generated keys stayed outside each store and disappeared with test cleanup.
The [binary digest](guide-evidence/reader-binary.sha256), [build metadata](guide-evidence/reader-binary-build-info.txt.gz), and [source hashes](guide-evidence/reader-source-files.sha256) bind the preparation.
The Go build metadata exposes no VCS revision field; provenance comes from the frozen checkout, recorded preparation argv, and unchanged source hashes.
Only untracked proof files existed during preparation.

The baseline received the historical guide from `082729690e107a66faaff4c94323d34c40e3d7cd`.
It used public query, one show per discovered object, strict verification, and selected evidence checks.
The candidate received the entire updated original guide and used the actual public brief with its default limit.
The [baseline recipe](guide-evidence/baseline-read.sh) and [candidate recipe](guide-evidence/candidate-read.sh) show every public argv.
They capture streams and exits without computing acceptance.
Neither reader invokes the internal project driver.

Each initial context call and each scenario call used explicit 16,000-token outer and inner output budgets.
Both forwarded raw stdout with `text(result.output)`.
Both readers reported complete guide delivery, zero tool truncations, zero CLI failures, and zero reruns.
The [baseline tool calls](guide-evidence/baseline-tool-calls.json) and [candidate tool calls](guide-evidence/candidate-tool-calls.json) retain actual call arguments.

| Scenario | Baseline CLI calls | Brief CLI calls | Baseline stdout bytes | Brief stdout bytes | Stderr bytes, each |
| --- | ---: | ---: | ---: | ---: | ---: |
| Open Important finding | 5 | 1 | 8,742 | 3,103 | 0 |
| Explicit correction and acceptance | 9 | 1 | 16,874 | 5,173 | 0 |
| Concurrent incompatible decisions | 13 | 1 | 24,968 | 7,791 | 0 |

Every stream count includes its final newline.
The [retained metrics](guide-evidence/reader-stream-metrics.jsonl) are recomputable with [the measurement script](guide-evidence/measure-readers.sh).
Guide inputs were 42,710 versus 12,284 bytes.
The complete initial file deliveries were 46,716 versus 15,279 bytes, including goals, rubric, and recipe.
Each reader used five outer `functions.exec` and five inner `exec_command` calls.
Those calls comprise one context read, three scenario reads, and one accounting read.
Thus scenario shell calls tie at three each; total retrieval/accounting calls tie at five each.
Final accounting stdout was 7,451 baseline bytes and 851 candidate bytes.
These are separate from scenario CLI output and guide input.

After the exercise, artifact delivery added one baseline outer/apply-patch call.
Candidate delivery added two outer calls, one apply-patch call, and one shell validation call.
Neither delivery phase ran a CLI or reread a scenario.
Fixture creation, builds, controller verification, and report assembly are outside reader retrieval counts.
Internal shell, OS, Git, jq, and Ruby subprocesses are uninstrumented.
No total process, time, token, or general efficiency claim follows from these measurements.

## Fact retention and limits

[The baseline handoff](guide-evidence/baseline-handoff.md.gz) labels its interpretation as manual.
[The candidate handoff](guide-evidence/candidate-handoff.md.gz) labels WorkState as derived engine state.
Controller comparison against the shared rubric found zero missed or changed required selected-source state facts.
Both preserve exact source/cut/refs, passing checks, Important finding identity, explicit resolution refs, and every selected evidence assessment.
Both preserve decision dependencies, declared action source/blockers, local authority, and uncertainty limits.
Open remains `not_recorded`; corrected is locally `supported`; concurrent accepted/rejected decisions remain `unresolved`.
Both preserve the historical action after correction without calling it current work.
Unsupported acceptance, invented winners, and changed blockers: zero in both handoffs.
The [grading scope](guide-evidence/expected-facts.md) records the exact required state facts and excluded original fields.
That file documents the controller comparison after the reads; the common rubric predates them.

The candidate did not retrieve original review verdicts, `resolves` payload fields, parent payloads, or older check payloads.
Its handoff explicitly leaves those fields unread rather than inventing them.
WorkState filters checks to the requested source and retains finding/resolution state separately.
Original proof inspection still requires exact `show`; this pair does not establish full original-payload equivalence.
The baseline read those extra fields but did not execute schema or projection policy validation.
The bounded baseline recipe refuses unexpected unread pages instead of silently claiming completeness.
It does not filter history by source revision, so the reader must preserve source scope manually.
Discovery beyond the supplied namespace/subject and unbounded histories was not evaluated.

Both paths state local-root authority limits, unestablished reviewer independence, unknown global completeness, and unproven reported execution.
All protected Git, store, trust, index, and evidence bytes remained unchanged across the reads.
The three `*-readonly-verification.log` files retain all 102 successful file comparisons.
The [public archive](guide-evidence/reader-public-captures.tar.gz) retains signed stores, goals, historical guide, recipes, raw command streams, exact argv, exits, and handoffs.
It excludes the executable and all private keys.
One synthetic pair supports only this observed scenario family.
The earlier negative evaluation and task1 native comparison remain separate historical evidence.

## Frozen-source checks and review

All four required exact-source gates passed once after the source commit:

- Focused quickstart, authored guide, recording, evidence, native limit, and read-only guard tests.
- Canonical `scripts/check`.
- Full `go test -race -count=1 ./...`.
- `golangci-lint run` with a fresh cache.

[Check events](guide-evidence/check-events.jsonl) record one exact argv, exit, and compressed log per gate.
[The runner](guide-evidence/run-guide-checks.sh) records the full source SHA and rejects source drift.
The toolchain was Go 1.26.6 on Darwin arm64; version receipts remain beside the logs.
Normal source-commit hooks passed with an isolated PREK_HOME and fresh lint cache.
The first proof commit hit a whitespace hook on raw Go build metadata.
Its original staged bytes were recovered into gzip before retry; the failed hook log remains intact.
Self-review inspected the actual diff, examples, callers, help, links, privacy boundaries, and source parity.
No material self-review finding remained.
The controller still owns independent task review and final integration review.
