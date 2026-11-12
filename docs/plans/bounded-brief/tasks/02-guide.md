<!-- ABOUTME: Defines the short operator-guide and native brief adoption kata. -->
<!-- ABOUTME: Preserves one embedded guide and separates measured input cost from correctness. -->

# Shorten the operator guide and verify native handoffs

Read ../implementation-plan.md and task1's report after its verified closure.
Use the completed public brief command; never substitute the internal test driver.

## Files and interfaces

- Edit the original docs/pact-ledger/SKILL.md and README.md.
- Extend cmd/pact/quickstart_command_test.go and real guide examples as needed.
- Reuse tests/e2e/development_guide_test.go and add a real native brief example.
- Write docs/status/bounded-brief/guide-report.md and the retained paired exercise.
- Update only the relevant existing gotchas entry after checking for duplicates.

Keep the original guide as the sole embedded source.
Keep raw quickstart output and that source at or below12,288 bytes.
Retain its metadata, actual command inventory, canonical store selection, and the executable record example.
Retain retry/index recovery, layered outcomes, exact proof, conflict, and authority limits.
Replace long background and unsupported future-operation prose with links to existing references/*.md.
Do not rewrite the frozen Python protocol/reference package or introduce a second operator guide.
Add a bounded brief recipe that shows exact source, frontier, limits, and read-only recovery behavior.
Keep unknown global completeness and shared-key independence limits visible.

## Test-first steps

- [x] Add a failing quickstart size/byte-parity contract and native brief example assertions.
- [x] Condense the original guide while preserving actual supported instructions.
- [x] Execute the authored record and brief examples with real Git/store/key/index bindings.
- [x] Verify guide links and native help rather than testing only copied source strings.
- [x] Prepare identical open, corrected, and conflict public stores once.
- [x] Use two clean reader contexts with the same configured model and explicit inner/outer output budgets.
- [x] Give the baseline its actual public read recipe and the candidate its native brief recipe.
- [x] Compare exact source/ref/blocker/decision/evidence/action/authority facts against an authored rubric.
- [x] Count guide bytes, output bytes, tool calls, top-level CLI work, errors, missing facts, and unsupported claims.
- [x] Retain ties and regressions; do not rerun pairs seeking a favorable result.
- [x] Run focused guide/example tests, canonical check, race, fresh lint, and normal hooks on frozen source.

The previous guide was42,710 bytes and exceeded one outer context budget.
This task must prove the new bounded guide is actually delivered without truncation.
Choose explicit16,000-token outer budgets and sufficient inner budgets for both readers.
Record configured model identifiers separately from unexposed deployment/build metadata.
One synthetic pair supports only its local observation.
The controller owns independent review, final integration review, PACT, and Kata closure.
