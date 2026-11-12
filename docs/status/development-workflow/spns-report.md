<!-- ABOUTME: Records the evidence resolver implementation, review, and retained verification proof. -->
<!-- ABOUTME: Identifies the frozen source and its limits without claiming ledger or workflow acceptance. -->

# pact#spns — bounded local and Git evidence

Date: 2026-11-11. Implementation and task verification passed; parent coordination
owns independent review, integration, PACT publication, and Kata closure.
No `.pact` files or Kata records were changed by this task.

## Source and scope

- Base: `89e8321f449ba2cc25b80efc34737760e25678e9`.
- Frozen implementation: `41c1ae66cf7c3e4119399ac7bdcb846b78bbe407`.
- Branch: `feat/development-evidence`.
- Worktree: `/Users/harper/.codex/worktrees/fcd0/pact`.
- Runtime: `go version go1.26.6 darwin/arm64` selected through mise;
  `git version 2.54.0 (Apple Git-157)`.
- Final gates ran after the source commit. Later changes contain only this
  report and retained evidence. The explicit source-path diff against that
  commit exited zero after all final gates.

Production files: `internal/evidence/resolve.go`, `internal/evidence/git.go`,
`cmd/pact/evidence_commands.go`, and the existing command catalog entry.
Tests: `internal/evidence/resolve_test.go`, `cmd/pact/evidence_commands_test.go`,
and `tests/e2e/development_evidence_test.go`. Package documentation and dated traps
are in `internal/evidence/README.md` and `internal/evidence/gotchas.md`.
No shared profile, validation corpus, recorder, projector, dependency, or wire
format changed.

## Implemented behavior

`Resolve(context.Context, Request) (Assessment, error)` reads one selected
repository-relative regular file or `git:<full-native-object-id>:<relative-path>`.
`ResolveAll(context.Context, []Request) ([]Assessment, error)` owns the shared
64 MiB budget and returns no partial assessments on failure. Each item is limited
to 16 MiB; callers may narrow that item bound. Exact boundary sizes pass, and
repeated selections count separately. Neither metadata checks nor streaming reads
can publish a prefix digest as a full digest.

The CLI requires one explicit `--event-ref`, selects its evidence through
`ledger.Show`, and reports assessments in order with a separate UTC `checked_at`.
Matching, changed, unavailable, unsupported, and unchecked remain distinct.
An omitted library digest yields unchecked; signed evidence already has a digest.
Exit zero reports completed assessments, including negative states. Overflow
returns exit 2, with no partial output. Git startup/execution failures remain
`ProcessError`; context cancellation retains its identity.

The command never appends events, changes trust/refs, or rebuilds the index.
Real signed-event tests compare all PACT file bytes with a real SQLite index,
and repeat the read after deleting that disposable index. Compiled CLI tests also
compare the entire Git/store/key fixture before and after reads.

Local paths reject traversal, absolute paths, all symlink components, nonregular
files, control characters, backslashes, and credential-like locator punctuation.
Malformed/unsafe refs are withheld from assessments, including early failure
paths. Git symlink blobs hash their stored link text without following targets;
working-file symlinks are unsupported. The explicit linked-worktree test passes.

Git runs through argument arrays, with no shell or pager. It ignores inherited
`GIT_*` selectors, system/global config, replacement objects, lazy fetches, external
diff/textconv, and transport protocols. `cat-file --batch-check` supplies bounded
metadata; `show` streams the exact blob. Missing blobs remain unavailable in a
real filtered clone, without fetching another pack. Full identities are checked
against the repository's native SHA-1 or SHA-256 format. Adversarial revision and
literal path tests exercise actual Git subprocesses.

Installed authoritative manuals were read before choosing arguments and controls:
`man git`, `man git-show`, `man git-cat-file`, `man git-rev-parse`, and
`man git-config`, with `MANPAGER=cat` and `col -b`. Relevant excerpts and the Git
version are retained in [spns-git-docs.txt](evidence/spns-git-docs.txt).
No technical flags were inferred from memory.

## TDD and review evidence

Every log below is under `docs/status/development-workflow/evidence/`.
The command prefix for Go commands is `env -u GOROOT mise exec --`.

| Log | Command after prefix | Exit and observed result |
|---|---|---|
| `spns-unit-red.log` | `go test ./internal/evidence` | 1; real byte/status/boundary assertions failed against empty interface stubs |
| `spns-unit-green.log` | same | 0; initial resolver cases passed |
| `spns-cli-red.log` | `go test ./cmd/pact ./tests/e2e -run 'TestEvidenceCheck\|TestCLIDevelopmentEvidence'` | 1; command missing; E2E credential fixture also refused by existing ledger safety |
| `spns-cli-red-selection.log` | `go test ./tests/e2e -run TestCLIDevelopmentEvidence` | 1; corrected public URL fixture reached missing evidence command |
| `spns-cli-green.log` | `go test ./internal/evidence ./cmd/pact ./tests/e2e -run 'TestResolve\|TestEvidenceCheck\|TestCLIDevelopmentEvidence'` | 0; real-store and compiled CLI cases passed |
| `spns-git-boundary-red.log` | `go test ./internal/evidence -run 'TestResolveGitRequiresFull\|TestResolveGitNever'` | 1; 40-hex prefix was incorrectly accepted in SHA-256 repo; no-fetch case passed |
| `spns-git-boundary-green.log` | same selected command as `spns-cli-green.log` | 0; native-format check corrected the prefix failure |
| `spns-review-red.log` | `go test ./internal/evidence -run TestResolveFailure` | 1; early limit failure reflected unsafe locator |
| `spns-review-red-cases.log` | same | 1; nonfatal assertions also exposed cancellation reflection and empty canceled selection |
| `spns-review-green.log` | `go test ./internal/evidence ./cmd/pact ./tests/e2e` | 0; all three review defects corrected |
| `spns-freeze-targeted.log` | same | 0; added linked-worktree, stored symlink bytes, empty selection, writer failure, and real bounded-stream coverage passed |

`spns-targeted-initial.log` and `spns-targeted-review.log` retain earlier successful
runs of the same full targeted command. `spns-check-initial.log` retains the first
successful canonical check. These are development evidence, not substitutes for
the frozen-source gates below.

The first lint pass (`spns-lint-initial.log`, exit 1) found eight ordinary lint
issues. The correction pass (`spns-lint-corrections.log`, exit 1) showed one
misplaced `nilerr` annotation. The deliberate missing-metadata status return now
carries its explanation on the flagged return. `spns-lint-review.log` and
`spns-freeze-lint.log` passed with zero issues. All four used:
`env -u GOROOT mise exec -- golangci-lint run ./internal/evidence/... ./cmd/pact/... ./tests/e2e/...`.

One self-review pass reread the changed process/path/CLI code, tests, and contract.
It found the two early-failure behaviors described above: locator reflection
(across limit and cancellation paths), and canceled empty operations returning
success. Both have RED/GREEN proof. The preceding Git boundary pass separately
found and fixed the SHA-256 abbreviation issue. No known blocking finding remains
within the task's agreed local boundary. This self-review is not independent
reviewer identity or organizational approval.

## Hook handling and exact log retention

Commits use the task-local cache, confirmed by `prek --help` and
`env PREK_HOME=/Users/harper/.codex/worktrees/fcd0/pact/.scratch/spns/prek prek cache dir`:

```sh
env -u GOROOT PREK_HOME=/Users/harper/.codex/worktrees/fcd0/pact/.scratch/spns/prek mise exec -- git commit -m 'feat(evidence): check bounded local and immutable Git proof'
```

The first attempt exited 1 because the trailing-whitespace hook removed trailing
spaces from two CLI RED logs; all other hooks passed. See
[spns-source-hook-attempt.log](evidence/spns-source-hook-attempt.log).
The `.log` copies are therefore hook-normalized readable copies, not exact raw
output. Before restaging, their exact originals were recovered from the still
original staged blobs with `git show :docs/status/development-workflow/evidence/spns-cli-red.log`
and its `spns-cli-red-selection.log` counterpart. `gzip -n -c` retained those bytes
with stable gzip metadata:

| Exact artifact | Decompressed SHA-256 |
|---|---|
| `spns-cli-red.log.gz` | `a19e4feed24f2f8c2896c886d0e08a7651e7ec153e7c209d486faf7b3f3b07af` |
| `spns-cli-red-selection.log.gz` | `9405aa4f56320793ea9fe2577f7b4fb07b2eb9164d7c49317ba209902312f5ce` |

Both `gzip -dc` recoveries compared equal (`cmp`, exit 0) to the exact staged
originals held in `.scratch/spns/cli-red-original.log` and
`.scratch/spns/cli-red-selection-original.log`. Nothing was reconstructed from
memory. After staging the readable hook fixes and exact compressed originals,
the same commit command exited 0; all applicable hooks, including uncached race
tests, lint, go fix, and vet passed. See
[spns-source-hooks.log](evidence/spns-source-hooks.log). No hook was bypassed.

## Frozen-source final gates

All three commands ran at source `41c1ae66cf7c3e4119399ac7bdcb846b78bbe407`.

| Exact command | Exit | Retained log |
|---|---|---|
| `env -u GOROOT mise exec -- scripts/check` | 0 | [spns-final-check.log](evidence/spns-final-check.log) |
| `env -u GOROOT mise exec -- go test -race ./...` | 0 | [spns-final-race.log](evidence/spns-final-race.log) |
| `env -u GOROOT GOLANGCI_LINT_CACHE=/Users/harper/.codex/worktrees/fcd0/pact/.scratch/spns/final-lint-cache mise exec -- golangci-lint run` | 0 | [spns-final-lint.log](evidence/spns-final-lint.log) |

The lint cache directory was newly created for this gate. Canonical verification
covers formatting, vet, the complete Go suite, compilation, a real CLI/store/index
lifecycle, and all 18 Python oracle tests. The Go suite includes unit, signed-store
integration, and compiled CLI E2E. The race suite passed; lint reported zero issues.
No required check is left failing. The source commit's hooks also ran
`go test -race -count=1 ./...` through the configured hook.

[spns-artifacts.sha256](evidence/spns-artifacts.sha256) records SHA-256 for every
retained spns log, the two exact gzip originals, and the installed-doc excerpts.
The report and that manifest are excluded from their own digest list.

## Known limits and recommendation

- Moderate, accepted boundary: trusted local Git metadata may point to common
  worktree storage or object alternates. This is not a sandbox against a process
  that can replace owned files/metadata concurrently. Protect those directories;
  static path escapes and inherited-selector redirects are covered.
- Minor: Git 2.54.0 on Darwin is the tested Git version. Older versions and other
  operating systems have not been qualified by this task. Transport denial is
  retained alongside the lazy-fetch control.
- Expected semantics: a byte match does not establish repository ownership,
  execution truth, signature authorization, independent review, or acceptance.
  Mutable files remain mutable; Git evidence provides retained exact bytes.
- Scope remains open outside this kata: parent integration and signed PACT/Kata
  readback, followed by recorder/projector work. No release or deployment was done.

Recommendation: integrate this bounded resolver after parent review. No known
blocking defect remains in this task; retain the local-metadata trust boundary
and avoid making broader sandbox or platform-support claims.
