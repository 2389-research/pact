<!-- ABOUTME: Audits current PACT user documentation against the Go CLI and Python oracle. -->
<!-- ABOUTME: Records false claims, review items, pattern expansion, and inventory gaps. -->

# Documentation Audit Report

Generated: 2026-08-26 | Audited commit: `0eda7f6`

## Executive summary

| Metric | Count |
|---|---:|
| Documents scanned | 5 |
| Verifiable claim units checked | 435 |
| Verified true | 415 (95.4%) |
| **Verified false** | **9 (2.1%)** |
| Needs review | 11 (2.5%) |
| Critical findings | 0 |
| Important false claims | 7 |
| Minor false claims | 2 |

The audit covered:

- `README.md`;
- `docs/pact-ledger/README.md`;
- `docs/pact-ledger/SKILL.md`;
- `docs/pact-ledger/TESTING.md`;
- `docs/pact-ledger/examples/walkthrough.md`.

Plans, specs, decisions, status snapshots, changelogs, prior audits, and internal
team notes were excluded. The bundled Python package was checked as the frozen
reference oracle where a document clearly scoped itself to that implementation.
Current product claims were checked against the Go command catalog, source,
tests, and compiled help at `0eda7f6`.

The main problem is not dead paths or bad flags. The current agent skill still
describes several future protocol duties as behavior the Go verifier performs
today. The root README also drifted by one command when `quickstart` shipped,
and the Python walkthrough contains a replica command that fails because it
never initializes the destination.

## False claims requiring fixes

### Current Go documentation

| Severity | Document line | Claim | Reality and evidence | Suggested fix |
|---|---|---|---|---|
| Minor | `README.md:177-179` | The listed commands are the shipped inventory. | `quickstart` also ships at `cmd/pact/command_catalog.go:56` and appears in compiled top-level help. | Add `quickstart` and a short redirect example. |
| Minor | `docs/pact-ledger/SKILL.md:379` | Canonical publication atomically renames the temporary file. | The store stages bytes, then publishes with a no-overwrite hard link at `internal/store/store.go:519-529` and `internal/store/store.go:601-614`. | Say “atomically publish with a no-overwrite hard link.” |
| Important | `docs/pact-ledger/SKILL.md:381` | A commit updates or rebuilds the disposable index. | Commit persists, verifies, and returns without touching the index at `internal/ledger/commit.go:94-110`; rebuild is explicit at `cmd/pact/index_commands.go:37-58`. | State that a canonical write may leave the index stale and require `pact index rebuild` before indexed reads. |
| Important | `docs/pact-ledger/SKILL.md:442` | Verification proves delegation references are causally prior. | The CLI rejects delegation hints at `cmd/pact/ledger_commands.go:31-41`. Verification shape-checks authority fields at `internal/ledger/verify.go:854-876` but does not evaluate delegation causality. | Limit the claim to structural authority-field checks and mark causal delegation validation unavailable. |
| Important | `docs/pact-ledger/SKILL.md:443` | Verification reports known causal revocations. | Authorization checks only local root membership and public bytes at `internal/ledger/verify.go:1199-1207`; no revocation evaluator runs. | Move revocation evaluation to the future protocol contract or mark it unavailable. |
| Important | `docs/pact-ledger/SKILL.md:444-445` | Verification separately reports delegation-based authorization. | Non-root signers remain indeterminate; only locally trusted roots become authorized at `internal/ledger/verify.go:1199-1207`. | Say root-trust authorization is separate from authenticity and delegation authorization remains indeterminate. |
| Important | `docs/pact-ledger/SKILL.md:446` | Verification proves checkpoint frontier entries are existing current heads. | Verification checks that referenced commits exist and match the named namespace at `internal/ledger/verify.go:249-263`; it does not compare them with recomputed current heads. Creation snapshots current heads at `internal/ledger/checkpoint.go:168-192`. | Separate creation from later verification and name the checks each performs. |
| Important | `docs/pact-ledger/SKILL.md:447` | Ordinary verification proves index rows can be regenerated from objects. | `pact verify` adds index status only when status succeeds at `cmd/pact/ledger_commands.go:146-152`; a missing index does not fail canonical verification. | Say verification reports index status and `pact index rebuild` performs regeneration. |

### Python reference walkthrough

| Severity | Document line | Claim | Reality and evidence | Suggested fix |
|---|---|---|---|---|
| Important | `docs/pact-ledger/examples/walkthrough.md:106-110` | The shown `sync-dir` command replicates into a fresh `/path/to/replica-b`. | `command_sync_dir` requires an initialized destination at `docs/pact-ledger/scripts/pact_core.py:2574-2577`; `ensure_store` rejects a missing store at `docs/pact-ledger/scripts/pact_core.py:355-364`. The passing sync test initializes and trusts the recipient first at `docs/pact-ledger/tests/test_pact.py:619-622`. | Initialize the destination before sync and add its trust root when imported commits should be authorized. |

## Human review queue

These claims have supporting behavior, but their wording exceeds what the code
or examples prove.

| Document line | Review item | Evidence-backed rewrite |
|---|---|---|
| `README.md:61-62` | “Safe for automation” is broader than non-interactive, convergent behavior. | Say a fully flagged setup never prompts, and `--json` emits machine-readable output. |
| `README.md:288-291` | “Costly” has no benchmark or threshold. | Say query work grows with the canonical object set because each query scans and hashes it. |
| `docs/pact-ledger/SKILL.md:200-205` | The startup procedure says competing ledgers are found and reported, while CLI discovery stops at the nearest ancestor. | Give agents an explicit filesystem search recipe or require `--repo` when scope is uncertain. |
| `docs/pact-ledger/SKILL.md:701` | “Schema-valid” implies domain payload schema execution. | Say “core-envelope-valid”; domain schema validation is unavailable or indeterminate. |
| `docs/pact-ledger/README.md:37` | “Transport-neutral object sync” blurs protocol design, Python directory sync, and the Go CLI. | Say the contract is transport-neutral, the Python oracle implements directory union, and the Go CLI has no sync command. |
| `docs/pact-ledger/README.md:58,66` | The stable CLI and quick start are not labeled as Python-oracle-specific. | Add a Python v0.1 oracle banner and point current users to the root README or `pact quickstart`. |
| `docs/pact-ledger/examples/walkthrough.md:11` | A scalar `$PACT` command works through Bash word splitting but fails in zsh and with spaces in the skill path. | Use a shell function that quotes the script path and forwards `"$@"`. |
| `docs/pact-ledger/examples/walkthrough.md:38-40` | The evidence step assumes root-directory write access and runs `make test` outside `$PROJECT`. | Define a writable evidence directory and run the replaceable workflow command from the project. |
| `docs/pact-ledger/examples/walkthrough.md:43-52` | `event-batch.json` has no defined source or working directory. | Copy the checked-in example to an explicit work directory and pass its full path. |
| `docs/pact-ledger/examples/walkthrough.md:71-78` | `correction-batch.json` has no defined source or working directory. | Copy the checked-in example to an explicit work directory and pass its full path. |
| `docs/pact-ledger/examples/walkthrough.md:81-82` | Any projection can appear to understand invalidation semantics. | Say a projector whose named policy recognizes the correction can show the later dispute or invalidation. |

## Pass 2A: pattern expansion

False-claim patterns were expanded across all five documents.

| Pattern | False claims | Root cause |
|---|---:|---|
| Future verifier behavior stated as current | 4 | The Go skill retained delegation, revocation, and checkpoint claims from the broader protocol and Python oracle. |
| Persistence and derived-index lifecycle drift | 3 | The skill still described rename publication, automatic index work, and index regeneration during verification. |
| Hand-written current command inventory | 1 | `quickstart` shipped after the last README inventory update. |
| Tutorial prerequisite omission | 1 | The sync walkthrough copied the command without the initialized-recipient setup used by its test. |

All references to `python3`, `scripts/pact.py`, `sync-dir`, and `reindex` were
classified by scope. The commands are valid for the bundled Python oracle; the
remaining issue is unclear labeling in its README and walkthrough. No legacy
Python command remains in the emitted `SKILL.md`.

Searches for `atomic rename`, `update or rebuild`, `delegation`, `revocation`,
`schema-valid`, `existing heads`, `index rows`, and `competing ledgers` found no
additional false implementation claim outside the entries above. Related
protocol rules remain conceptual and should stay clearly separated from current
CLI capability.

## Pass 2B: inventory and gap detection

- The Go catalog contains 16 leaf command shapes. The root README names 15 and
  omits only `quickstart`.
- The Python oracle exposes 12 subcommands. Its documented command names and
  flags are valid, including `reindex` and `sync-dir`; they are not Go commands.
- Every concrete repository file referenced by the audited docs exists.
- `scripts/check` is the only root automation script and is documented.
  `scripts/pact.py` and `scripts/pact_core.py` are the two meaningful Python
  oracle scripts and are documented. The remaining `scripts/__init__.py` is a
  package marker, not an operator command.
- The numeric Phase 2 limits, query filter families, index states, exit codes,
  cursor errors, `NO_COLOR`, and `TERM=dumb` behavior in the root README match
  source and tests.
- The repository has no services, timers, or HTTP API endpoints, so those
  inventory categories have no documentation gap.
- `go.mod` requires Go 1.26, but the root install section does not state that
  version. It invokes `mise` without a checked-in mise configuration. Add an
  explicit Go 1.26 prerequisite or check in the toolchain configuration.

## Verification evidence

- Compiled `pact help` lists the same 16 command shapes as
  `cmd/pact/command_catalog.go`.
- Targeted Go documentation-contract tests passed during claim verification.
- The bundled reference command and its documented flags were checked through
  `python scripts/pact.py --help`.
- The Python oracle suite passed all 18 tests:

  ```text
  Ran 18 tests in 0.384s
  OK
  ```

## Recommended fix order

1. Correct the current `SKILL.md` verifier, authorization, and index claims.
2. Fix the Python sync walkthrough so its documented command succeeds.
3. Add `quickstart` and its redirect example to the root README.
4. Label the Python README and walkthrough as oracle-specific.
5. Tighten the eleven review phrases and state the Go toolchain requirement.
