<!-- ABOUTME: Records exact-source proof for the bounded public development brief. -->
<!-- ABOUTME: Separates correctness, CLI calls, returned bytes, and enclosing shell calls. -->

# Bounded brief command evidence

Doctor Biz approved the command and revised gate in the shared implementation plan.
Source commit: `7ad8add4788f8d1f797ac5dd654cef8dc47619cf`.
Source tree: `6af00e92d141b6b81f98dbdfa8c250c436e15896`.
Base: `8e6851818f892f1e6a3b1a85944786519c41e8eb`.
The command implementation and exact-source gates pass.
The controller owns independent review, guide work, publication, and task closure.

The adapter uses `development.Project` for all interpretation.
It checks index readiness without rebuilding or nesting owner locks.
That check precedes a separate projector capture.
The returned frontier identifies the captured cut; it does not claim atomic index readiness.
JSON adds `operation: brief` beside every WorkState field.
Human output adds a title to indented, quoted state and escapes terminal control characters.
Both formats are encoded before stdout writes, including the final newline.
Required output never loses a blocker to fit the budget.

The controller authorized one narrow owner follow-up during implementation.
`ProjectRequestError` now identifies the three existing plain request failures.
Their messages and projection semantics remain unchanged.
The adapter maps those errors and evidence limits to usage errors.
Existing typed owners retain their exit classifications.
Cancellation and unknown read failures default to exit 10.
The embedded projector digest changed with its source bytes.
No profile, policy, canonical wire, trust, or cursor behavior changed.

## Native comparison

Both paths read identical real signed stores from the existing public corpus.
The baseline uses one query, one show per object, strict verify, and selected evidence checks.
The candidate executes the compiled public `pact brief --json` command with its default budget.
The internal driver is not the measured candidate.

| Public history | Baseline CLI calls | Brief CLI calls | Baseline stdout bytes | Brief stdout bytes | Baseline argv bytes | Brief argv bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Open Important finding | 5 | 1 | 8,932 | 3,103 | 887 | 249 |
| Explicit correction and acceptance | 9 | 1 | 17,111 | 5,173 | 1,758 | 253 |
| Concurrent incompatible decisions | 13 | 1 | 25,256 | 7,791 | 2,609 | 253 |

Every scenario passed the revised gate: fewer CLI calls and nonincreasing stdout bytes.
Measured paths had zero failed CLI calls, truncations, changed required facts, or unsupported acceptance.
Argv bytes count argument UTF-8 bytes plus one separator per argument, excluding the executable path.
Both paths share one enclosing shell invocation, so that separate count ties at 1:1.
Setup, compilation, owner-parity checks, and guard tests sit outside the measured paths.
Internal subprocess work remains uninstrumented.
These results do not establish total process, elapsed-time, token, or general efficiency gains.
The earlier 3:3 shell-call rejection remains valid historical evidence under its original rule.

## Required facts and references

The tests compare every returned WorkState field with the owner result.
They also apply the public corpus's independent expected acceptance rubric.
Baseline queries expose every event ref, and the selected shows expose every complete event.
Strict verification succeeds, and selected baseline evidence checks match actual bytes.

| Required fact | Open | Corrected | Conflict |
| --- | --- | --- | --- |
| Exact source revision | `62ba249ef5cafdee4860900220f826dfad0ba869` | `fd0a35409b7f10ea96d6f2af192632bb79b5d810` | Same corrected revision |
| Captured frontier commits | 1 | 1 | 2 |
| Exact-source passed checks | 1 | 1 | 2 |
| Important finding `finding-1` | Open | Resolved | Resolved |
| Explicit resolving review refs | 0 | 1 | 2 |
| Recorded decisions | Absent | Accepted | Accepted and rejected |
| Supported acceptance | `not_recorded` | `supported` | `unresolved` |
| Support reason | None | None | `concurrent_incompatible_decisions` |
| Declared next actions | 1 | 1 | 1 |
| Actual selected evidence assessments | 2 matching | 5 matching | 8 matching |

All states preserve namespace `org/example/development` and subject `widget/task-1`.
The original signed next action remains visible, including its blocker references.
The adapter does not infer another action or reinterpret that historical declaration.
Evidence is durable Git proof with 77 matching bytes per selection.
Its observed digest is `sha256:c3f58fa85abd4403a63dc44eb205a4be351054f86f0f748865d4094160d3f446`.
Completeness stays `locally_closed` for the selected local frontier, with global completeness `unknown`.
Authority is `locally_configured_root`, and reviewer independence is `not_established`.
Each full claim, payload, source, frontier, finding, decision, support reason, and evidence ref remains in the retained stdout.

Profile digest: `sha256:b89dd93eb851ca4db63e9e29fbd1d060310f2268c8d37ed00c46004f2eafeab9`.
Policy digest: `sha256:e9d050d6b7170e608677c0c56b338a888c2ec001499b0119adca52cf0c8fd437`.
Projector: `development/projector/v1`, digest `sha256:9d7c69c89cad7a81333d6b71a9da3c0ad8211ceb4621afa69fbc8361d157c267`.

## Bounds, uncertainty, and read-only checks

The signed boundary fixture includes quotes, backslashes, controls, Unicode, and newlines.
Human output requires 4,101 encoded bytes; budgets 4,100/4,101/4,102 exercise the exact boundary.
JSON requires 3,133 bytes; budgets 3,132/3,133/3,134 exercise its separate boundary.
Both formats reject a valid 1,024-byte budget with exit 2, empty stdout, and a diagnostic below 1,024 bytes.
JSON limit errors retain `brief_output_limit` and exact requested/required byte counts.
An Important finding above 65,536 bytes produces that error instead of a partial positive view.

Compiled tests cover missing, stale, and corrupt indexes with actionable recovery errors.
The existing index-owner suite covers incompatible and partial-build classification.
No brief read repairs an index.
Explicit frontier output stays byte-identical after later history and an explicit index rebuild.
Duplicate and cross-namespace frontier requests retain usage classification.
Unknown schemas preserve open findings, recorded decisions, and inconclusive unsupported state.
Unavailable Git evidence preserves recorded decisions and reports unavailable assessments and unsupported acceptance.
Writer failures name brief, while the quickstart diagnostic remains unchanged.
Protected-file snapshots compare canonical objects, trust, index, Git, evidence, and external keys before and after reads.
The archive retains project-file hashes; external private keys never enter the export.

## Verification and retained proof

The initial behavioral RED proved that help and real-store reads failed because brief was absent.
The error-classification RED proved unknown read failures and cancellation incorrectly used exit 2.
Both meaningful REDs and their GREENs are retained losslessly.
One early parity test failed on Go integer types versus decoded JSON numbers.
The corrected assertion compares the complete JSON values without weakening field coverage.
An initial classification test compile error is also retained.

The first normal commit attempt failed lint.
Its auto-fixer damaged two compound exit assertions and omitted an import.
Explicit `errors.AsType` bindings now preserve all original exit, stdout, and diagnostic bounds.
The second normal commit attempt passed every applicable hook, including the full race suite.
No hook was bypassed.
Self-review found no remaining source issue after the owner error correction.

At the frozen source, all four gates exited 0:

1. Focused CLI, development-owner, and E2E packages with `-count=1 -v`.
2. `scripts/check`, including Go vet/test/build, live CLI use, and 18 reference tests.
3. `go test -race -count=1 ./...`.
4. `golangci-lint run` with a fresh cache; result: `0 issues`.

The toolchain was Go 1.26.6 on Darwin arm64 through `env -u GOROOT mise exec --`.
The lint tool was 2.13.2.
Source bytes stayed unchanged throughout these gates.

The [manifest](evidence/manifest.json) identifies source, checks, metrics, exclusions, and pending independent review.
The [check events](evidence/check-events.jsonl) retain each exact argv, source SHA, exit, and compressed log path.
The [native archive](evidence/7ad8add-native-captures.tar.gz) retains actual command argv/stdout/stderr and public signed fixtures.
It excludes the built executable and all signing keys.
The [binary hash](evidence/native-binary.sha256) identifies the measured executable.
The [source hashes](evidence/source-files.sha256) identify every changed source file.
The [artifact hashes](evidence/SHA256SUMS) cover the retained evidence files.
Raw logs are gzip-compressed so whitespace hooks cannot alter their original bytes.

No product defect remains known from this implementation and verification pass.
The controller's independent review and the separate guide exercise still need their own receipts.
