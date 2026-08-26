<!-- ABOUTME: Records the tested operator CLI adapter decision and cleanup evidence. -->
<!-- ABOUTME: Makes the winning architecture and rejected tradeoffs durable for later CLI work. -->

# Operator CLI Omakase Result

## Contract

- Pinned base: `a2d88749363224db1a497fa538d46094458c4898`.
- Scenario command: `env -u GOROOT mise exec -- go test ./tests/e2e -run TestOperatorCLIContract -count=1`.

## Candidates

### stdlib

- Final commit: `76e4a6b`.
- Dependencies: `golang.org/x/term v0.45.0` only.
- Changed production lines: 978.
- Final root gate: contract, race, and canonical gates passed.
- Fresh-eyes: PASS and eligible; no Critical, Important, or Minor findings.

Healthy plain status sample:

```text
Healthy

Ledger
  Namespace: org/example/widget
  Strict verification: true

Replica
  Local completeness: locally closed
  Global completeness: unknown

Index
  State: current
  Coverage: complete
```

### Cobra

- Final commit: `100db61`.
- Dependencies: `github.com/spf13/cobra v1.10.2`, its `pflag` and `mousetrap`
  transitives, and `golang.org/x/term v0.45.0`.
- Changed production lines: 1,091.
- Final root gate: contract, race, and canonical gates passed.
- Fresh-eyes: PASS and eligible; no Critical, Important, or Minor findings.

Healthy plain status sample:

```text
Healthy

Ledger
  Default namespace: org/example/widget
  Strict verification: valid

Replica
  Local completeness: locally closed
  Global completeness: unknown

Index
  State: current
  Coverage: complete
  Rebuild required: false
```

### Kong

- Final commit: `87129e5`.
- Dependencies: `github.com/alecthomas/kong v1.16.1` and
  `golang.org/x/term v0.45.0`.
- Changed production lines: 1,172.
- Final root gate: contract, race, and canonical gates passed.
- Fresh-eyes: FAIL and ineligible. Important: `pact help status extra` and
  `pact help index status extra` normalize to valid help paths and exit 0
  instead of usage 2 at `cmd/pact/kong_adapter.go:118`. Minor: static
  `Commands: status` on detail help and unused width.

Healthy plain status sample:

```text
Healthy

Ledger
  Repository: /private/var/folders/rc/cyjg3p3x0cb4w4xlb8yqm_1h0000gn/T/tmp.W4G6WscEbf/repo
  Store: /private/var/folders/rc/cyjg3p3x0cb4w4xlb8yqm_1h0000gn/T/tmp.W4G6WscEbf/repo/.pact
  Default namespace: org/example/widget
  Strict verification: true

Replica
  Local completeness: locally closed
  Global completeness: unknown

Index
  State: current
  Coverage: complete
```

## Scores

| Criterion | stdlib | Cobra | Kong |
|---|---:|---:|---:|
| Fitness for Purpose | 5 | 5 | 2 |
| Justified Complexity | 5 | 3 | 3 |
| Readability | 4 | 5 | 4 |
| Robustness & Scale | 4 | 4 | 2 |
| Maintainability | 5 | 4 | 3 |
| **Total** | **23/25** | **21/25** | **14/25** |

Hard gates: the Fitness Gate (delta at least 2) triggered; the Critical Flaw
gate did not trigger. Kong remains ineligible because of the Important
fresh-eyes defect.

## Decision

Select stdlib. It is the smallest eligible implementation at 978 changed
production lines, adds only `golang.org/x/term`, and keeps one bounded command
catalog with typed handlers. Its healthy output gives the operator the needed
ledger, replica, and index state without framework output.

Cobra has the clearest local control flow and the only 5/5 readability score,
but its command tree and three added modules add scope without a required
benefit. Kong's valid-prefix, trailing-garbage help defect makes it ineligible
despite passing the broad gates.

## Known Weaknesses

- Medium — stdlib captures terminal width but the renderer does not consume it;
  no 60-, 80-, or 120-column golden output exists. Task 6 owns responsive
  rendering and those goldens.
- Low — command help lacks rich per-flag descriptions. Task 6 owns the help
  and diagnostic contract hardening.

## Cleanup

Worktrees, removed:

- `operator-cli-stdlib`
- `operator-cli-cobra`
- `operator-cli-kong`

Branches, removed:

- `operator-cli/omakase/stdlib`
- `operator-cli/omakase/cobra`
- `operator-cli/omakase/kong`

Older setup worktrees are out of scope and untouched.
