# Animated Help Final Fix Report

Base: `d7048f7`

## Implementation

- Added `terminalAnimationConfig`, which retains 80x30 fallback dimensions but
  enables frames only for a TTY with positive observed width and height.
- Reserved `AnimationFrames == 0` for a TTY without safe geometry; the bare
  welcome then skips the decorative seal and prints normal help.
- Preserved redirected static output and independent color-terminal detection.
- Tested exact repaint ownership for 40-column and 12-column renderers.
- Scoped README motion wording to capable terminals.

Files changed:

- `cmd/pact/main.go`
- `cmd/pact/seal_animation.go`
- `cmd/pact/seal_animation_test.go`
- `README.md`

## TDD evidence

### RED

Command:

```sh
env -u GOROOT mise exec -- go test ./cmd/pact -run 'Test(BarePactFallbacksAndExplicitHelp|TerminalAnimationConfig|ProductionSealAnimationIsFinite)$' -count=1
```

Output:

```text
# pact/cmd/pact [pact/cmd/pact.test]
cmd/pact/seal_animation_test.go:81:29: undefined: terminalAnimationConfig
FAIL	pact/cmd/pact [build failed]
FAIL
```

### GREEN

Command:

```sh
env -u GOROOT mise exec -- go test ./cmd/pact -run 'Test(BarePactFallbacksAndExplicitHelp|TerminalAnimationConfig|ProductionSealAnimationIsFinite)$' -count=1
```

Output:

```text
ok  	pact/cmd/pact	0.225s
```

The first scaled repaint assertion exposed a stale hard-coded `\x1b[29A`
expectation in the test. The test now derives the rewind from the width; no
production change was needed for that test strengthening.

## Broad verification

### Focused package

```text
$ env -u GOROOT mise exec -- go test ./cmd/pact -count=1
ok  	pact/cmd/pact	3.303s
```

### Compiled operator E2E

```text
$ env -u GOROOT mise exec -- go test ./tests/e2e -run '^TestOperatorCLIContract$' -count=1
ok  	pact/tests/e2e	1.093s
```

### Vet

```text
$ env -u GOROOT mise exec -- go vet ./...
```

Exit status: 0.

### Race

```text
$ env -u GOROOT mise exec -- go test -race ./...
ok  	pact/cmd/pact	12.177s
ok  	pact/internal/canonical	1.207s
ok  	pact/internal/conformance	1.741s
ok  	pact/internal/identity	1.917s
ok  	pact/internal/index	(cached)
ok  	pact/internal/ledger	(cached)
ok  	pact/internal/status	(cached)
ok  	pact/internal/store	(cached)
ok  	pact/tests/e2e	16.295s
```

### Canonical check

```text
$ env -u GOROOT mise exec -- ./scripts/check
ok  	pact/cmd/pact	5.066s
ok  	pact/internal/canonical	(cached)
ok  	pact/internal/conformance	(cached)
ok  	pact/internal/identity	(cached)
ok  	pact/internal/index	(cached)
ok  	pact/internal/ledger	(cached)
ok  	pact/internal/status	(cached)
ok  	pact/internal/store	(cached)
ok  	pact/tests/e2e	14.646s
test_frozen_vectors (test_conformance.CanonicalizationVectorTest.test_frozen_vectors) ... ok
test_all_schemas_are_well_formed_and_examples_validate (test_contract.PactContractTest.test_all_schemas_are_well_formed_and_examples_validate) ... ok
test_canonical_json_normalizes_unicode_and_rejects_floats (test_contract.PactContractTest.test_canonical_json_normalizes_unicode_and_rejects_floats) ... ok
test_full_module_go_hooks_do_not_accept_filenames (test_contract.PactContractTest.test_full_module_go_hooks_do_not_accept_filenames) ... ok
test_namespace_and_event_pattern_semantics (test_contract.PactContractTest.test_namespace_and_event_pattern_semantics) ... ok
test_secret_scanner_allows_indirection_and_rejects_raw_values (test_contract.PactContractTest.test_secret_scanner_allows_indirection_and_rejects_raw_values) ... ok
test_append_only_correction_keeps_original_bytes (test_pact.PactCliTest.test_append_only_correction_keeps_original_bytes) ... ok
test_bounded_subdelegation_authorizes_one_child_level (test_pact.PactCliTest.test_bounded_subdelegation_authorizes_one_child_level) ... ok
test_causal_revocation_preserves_but_unauthorizes_later_commit (test_pact.PactCliTest.test_causal_revocation_preserves_but_unauthorizes_later_commit) ... ok
test_directory_sync_unions_objects_without_overwrite (test_pact.PactCliTest.test_directory_sync_unions_objects_without_overwrite) ... ok
test_forbidden_subdelegation_is_not_authorized (test_pact.PactCliTest.test_forbidden_subdelegation_is_not_authorized) ... ok
test_full_lifecycle_and_checkpoint (test_pact.PactCliTest.test_full_lifecycle_and_checkpoint) ... ok
test_native_fork_and_explicit_merge (test_pact.PactCliTest.test_native_fork_and_explicit_merge) ... ok
test_scoped_delegation_authorizes_agent_commit (test_pact.PactCliTest.test_scoped_delegation_authorizes_agent_commit) ... ok
test_secret_like_payload_is_refused (test_pact.PactCliTest.test_secret_like_payload_is_refused) ... ok
test_tampering_is_detected (test_pact.PactCliTest.test_tampering_is_detected) ... ok
test_event_batch_examples (test_schemas.SchemaTest.test_event_batch_examples) ... ok
test_projection_manifest_example (test_schemas.SchemaTest.test_projection_manifest_example) ... ok

----------------------------------------------------------------------
Ran 18 tests in 0.519s

OK
```

### Whitespace

```text
$ git diff --check
```

Exit status: 0.

## Fresh-eyes review

Reviewed `README.md`, `cmd/pact/main.go`, `cmd/pact/seal_animation.go`, and
`cmd/pact/seal_animation_test.go` after broad gates. No findings: the fix takes
no external input, does not alter color-terminal detection, preserves the
static non-TTY path, and derives repaint counts from renderer geometry.

## Commit and hooks

Commit SHA: pending normal-hook commit; this report is updated with the final
SHA before handoff.

Hook output: pending normal-hook commit.

## Concerns

None. A TTY whose dimensions cannot be observed intentionally receives normal
help without the seal, avoiding unsafe cursor rewrites.
