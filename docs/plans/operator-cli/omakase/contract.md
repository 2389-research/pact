<!-- ABOUTME: Freezes the external scenario shared by all operator CLI foundation variants. -->
<!-- ABOUTME: Prevents parser-specific shortcuts from changing PACT's machine or terminal contracts. -->

# Operator CLI Omakase Contract

## Candidate Inputs

- Design: `docs/superpowers/specs/2026-08-25-operator-cli-design.md`.
- Plan: `docs/superpowers/plans/2026-08-25-operator-cli-foundation.md`.
- Shared test helper: `tests/e2e/operator_contract_test.go`.

## Required Wrapper

Each candidate adds `tests/e2e/operator_variant_test.go` with one test:

```go
func TestOperatorCLIContract(t *testing.T) {
	runOperatorCLIContract(t)
}
```

## Pass Gate

Run `go test ./tests/e2e -run TestOperatorCLIContract -count=1`,
`go test -race ./cmd/pact ./internal/status ./tests/e2e`, and
`./scripts/check`. Every command must pass.

## Candidate Evidence

Record dependency changes, production lines changed, full gate output, and
plain plus forced-color samples for healthy and stale status.

## Forbidden Shortcuts

- no changed JSON fields or exit meanings for existing explicit commands;
- no hidden index rebuild or canonical write;
- no mock mode, skipped contract, parser-error leak, or ignored writer error;
- no setup implementation in this slice;
- no edits to another candidate worktree.
