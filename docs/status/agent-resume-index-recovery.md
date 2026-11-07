# Agent resume and index recovery — 2026-11-06

## Scope and decision

Consumer session logs showed repeated writes and verification, but no `log` or
`query` use. They also showed a rejected bare-array event batch and an index
rebuild failure after Git omitted empty ignored directories. This change fixes
the directory recovery and documents existing read/write commands. It adds no
query API, evidence store, or approval policy.

## Fresh-context handoff experiment

Two agents independently received the same read-only snapshot of a consumer
ledger: 51 signed commits, 60 events, one namespace head, current index. One read
skill 0.2.1; the other read 0.2.2. Neither received prior session history or access
to broad project reports. Each could resolve individual evidence references.

Task: identify unfinished work, blocking findings, exact source revision, proof,
and limits. Both correctly identified the same unfinished slice, all five
blocking findings, and the same source revision. Both distinguished a passing
canonical check from a failed review and withheld acceptance. Both checked all
seven evidence files attached to the decisive events; their digests matched.
Neither invented CLI flags or needed to read raw canonical-object files.

| Observation | Existing skill | Updated skill |
|---|---:|---:|
| Investigative shell invocations, excluding notes write | 16 | 26 |
| Command failures | 0 | 0 |
| Correct handoff | Yes | Yes |

The baseline encountered two truncated outputs and uninformative human-mode
`heads`/`verify` output, then recovered with JSON/selective reads. The updated
run paged all 60 metadata events in three pages, read exact events, and checked
reverse correction links. Its initial combined skill/context read also truncated
and required a selective reread.

This is a single application test per guide, not a performance benchmark.
Output accounting differed between agents, so byte/token savings are not
claimed. More shell calls in the updated run do not establish greater efficiency.
The baseline already succeeded: there is no demonstrated need for a new resume
command. The guide makes the working procedure, pagination limits, and evidence
checks explicit. Test notes remain local because they include consumer evidence
paths and findings; the consumer snapshot is not bundled in this repository.

## Regression and validation

- Before the fix, unit/path, real SQLite integration, and compiled CLI scenarios
  failed on the missing index directory. CLI reproduced exit 10 with
  `index operation failed`.
- Rebuild now creates only the missing index directory under the existing store
  mutation lock. Existing files and symlinks still cause refusal. No canonical
  objects, trust, or refs are rewritten. Missing `tmp` and `refs` are irrelevant
  to rebuilding the index.
- The same scenarios pass after the fix. The compiled lifecycle compares query
  results, logical digest, heads, and protected file bytes before/after recovery.
- The skill's JSON batch was extracted and executed in a throwaway ledger:
  setup, commit, rebuild, subject query, and exact event show all succeeded.
- `make check`, `go test -race ./...`, and `golangci-lint run` passed. The canonical
  gate includes Go vet, the Go suite, compiled CLI usage, and 18 Python reference
  tests. Existing quickstart tests confirm the embedded skill matches its source.

## Remaining limits

Evidence stays external; a mutable locator can still drift. The guide now records
Git revision/path recovery information, but Pact does not retrieve or hash that
evidence. Cursors remain snapshot pagination tokens, not incremental change feeds.
Generic errors for other index failures and the observed human-mode output need
separate diagnosis. Neither prevents the JSON workflow exercised here.
