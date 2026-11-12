<!-- ABOUTME: Records the deterministic development projection implementation and exact-source proof. -->
<!-- ABOUTME: Separates local policy support, signed claims, review fixes, and remaining limits. -->

# Development projection — pact#pzm4

Source: `939c6223777fb60da804f551378b70424e99e8a8`.
Base: `2d688b5e75d58de421808c57f70106d196e4599e`.
Branch: `feat/development-projection`.
Worktree: `/Users/harper/.codex/worktrees/a7b6/pact`.
This report records library implementation and verification.
It does not record Kata closure or an official PACT acceptance.
The parent owns independent review, integration, provenance recording, and publication.

## API and output

`development.Project(context.Context, *store.Store, ProjectRequest)` returns
`WorkState` and an error.
The request requires an exact namespace, subject, and full Git source revision.
A nil frontier captures current namespace heads once.
An explicit empty frontier selects no history.
An explicit nonempty frontier requires unique commits from the requested namespace.

The output includes the exact frontier, requested work identity, profile digest,
policy digest, and projector version/digest.
It includes checks, findings, recorded decisions, declared next actions,
evidence assessments, local completeness, and supported acceptance.
Collections encode as arrays.
Valid claim records carry their signed payloads and event references.
Issues identify unknown or invalid relevant events by exact event reference.
No proof contents appear in output.

Acceptance support has four states:

- `not_recorded`: no valid decision exists for the requested revision.
- `supported`: the named local policy supports the current accepted decision set.
- `unsupported`: the recorded decision set lacks required local support.
- `unresolved`: causally maximal decisions conflict between accepted and rejected.

The output preserves earlier recorded decisions.
It never converts a signed rejection into a missing decision.
It never converts unavailable evidence into a missing recorded acceptance.
The default policy artifact remains byte-identical to the prerequisite profile.

## Semantic choices

One store read lock encloses a bounded `ledger.Scan` and bounded canonical reads.
Each selected payload read must hash to its scanned object ID.
The projector includes parent and `caused_by` closure.
It retains every event in each selected commit.
It refuses missing or invalid selected causal dependencies.
Default head capture refuses any invalid canonical object.
An explicit valid cut can exclude unrelated malformed objects.
Newer events never enter an explicit older cut.

Checks and decisions select the requested revision.
Findings and next actions retain the selected work's whole reachable history.
Only declared next actions appear.
Unknown schemas, invalid payloads, unknown authority, unresolved relevant refs,
and unmatched selected evidence prevent positive support.
All relevant payload checks reuse `ValidatePayload`.
All selected evidence reads share one `evidence.ResolveAll` operation budget.

Findings retain their original review/finding identity.
Passing checks, changed source, cause links, and supersedes links do not close them.
Resolution requires an explicit approved review.
That review must observe the original finding.
It must name its own exact source as the correction source.
It must have known root authorization and durable matching selected proof.
An acceptance must observe the resolving review before using its resolution.
Open Critical or Important findings block support.
Minor findings remain visible.
Later blocking findings invalidate support at the newer cut.

Causal maxima determine the current decision set.
Concurrent incompatible decisions remain unresolved.
Hash order only stabilizes output ordering.
Timestamps and array positions never choose a winner.
Selected check and review proofs must match namespace, subject, repository label,
and revision.
They must be causally observed by the decision.
Their evidence selections must be nonempty, durable, and matching.
The decision itself must select durable matching proof.
Unselected checks remain visible without replacing the declared proof selection.

## Tests and review

The initial RED run used a minimal empty projector.
The rubric failed on missing decisions, supported acceptance, blocking findings,
explicit resolution, frozen cuts, concurrent decisions, wrong-source proof,
unobserved proof, mutable proof, unknown schemas, invalid payloads, missing refs,
and unknown authority.
The corresponding implementation made the rubric pass.

The expanded tests use real signed stores, trusted and untrusted keys, Git commits,
retained blobs, divergent branches, and explicit frontiers.
They cover changed working files with stable Git proof and unavailable evidence.
They cover correction-source mismatch, missing finding IDs, source changes,
same-commit causal links, empty selections, and cross-work proof.
They cover cancellation, duplicate frontiers, missing parents, commit atomicity,
and supersedes links that do not establish causality.
Small unit tests cover exact local references and evidence-selection predicates.
No test mocks a store, Git, signature, or evidence resolver.
Fixture Git processes strip every inherited `GIT_*` variable.

The compiled `testdata/projectdriver` runs the real library against signed fixtures.
It emits canonical JSON.
The test compares every output byte against an independently authored expected state.
It covers both supported acceptance and concurrent conflicting decisions.
It repeats the old cut after newer blocking history appears.
The expected-state constructor does not invoke the projector.

Local review found two Important issues before source freeze:

1. Traversal could keep doing work after its causal budget failed.
   A RED regression proved continued work.
   The fix stops later traversals and decision comparisons.
   Issue deduplication also uses a set.
2. Default head capture could silently omit an invalid canonical object.
   A RED regression proved that omission.
   Default capture now refuses invalid objects.
   Explicit cuts still exclude unrelated malformed history.

The first commit attempt failed `go fix -diff`.
The fix uses `strings.CutPrefix` for local references.
The original failure log is retained losslessly as gzip.
A later attempt failed when the whitespace hook changed that text log.
The gzip artifact preserves the original bytes without changing hook policy.
The final source commit passed every applicable hook.
No hook was bypassed.
The task used `PREK_HOME=/tmp/pzm4-prek-20261111`.

An early expanded fixture failed because it assumed normalized event order.
The corrected fixture locates the acceptance by local ID.
Its failure log remains retained.
That fixture failure was not treated as product RED evidence.

## Exact-source verification

The source commit completed with a clean worktree.
Final gates ran against that frozen source.
The later proof commit changes only documentation and evidence files.
Go reported `go1.26.6 darwin/arm64` under `mise`.
Golangci-lint reported `2.13.2`, built with Go `1.27.0`.

| Gate | Exact command | Exit | Evidence |
| --- | --- | --- | --- |
| Focused tests and compiled driver | `env -u GOROOT mise exec -- go test -count=1 -v ./internal/development ./internal/ledger` | 0 | [pzm4-frozen-focused.log](evidence/pzm4-frozen-focused.log) |
| Canonical check | `env -u GOROOT mise exec -- scripts/check` | 0 | [pzm4-frozen-check.log](evidence/pzm4-frozen-check.log) |
| Race suite | `env -u GOROOT mise exec -- go test -race -count=1 ./...` | 0 | [pzm4-frozen-race.log](evidence/pzm4-frozen-race.log) |
| Cold-cache lint | `env -u GOROOT GOCACHE=/tmp/pzm4-939c622-go-cache GOLANGCI_LINT_CACHE=/tmp/pzm4-939c622-lint-cache mise exec -- golangci-lint run` | 0 | [pzm4-frozen-lint.log](evidence/pzm4-frozen-lint.log) |

Initial and intermediate logs remain under `evidence/pzm4-*`.
The initial RED and both review RED commands exited 1.
Their paired GREEN runs exited 0.
The preliminary canonical, race, and lint commands exited 0.
Those preliminary results do not replace the exact-source gates.

## Identity and limits

- profile_digest: `sha256:b89dd93eb851ca4db63e9e29fbd1d060310f2268c8d37ed00c46004f2eafeab9`.
- policy_digest: `sha256:e9d050d6b7170e608677c0c56b338a888c2ec001499b0119adca52cf0c8fd437`.
- projector_version: `development/projector/v1`.
- projector_digest: `sha256:2bbf4768d020ecc4d3009aa89f297b519577d67d25f312cd0f9651b2bf8e4d2e`.

The profile digest covers the canonical map from artifact filenames to byte digests.
The projector digest covers the canonical map for the embedded `project.go`,
`project_types.go`, and `policy.go` bytes.
This identity is distinct from the inspected work revision.
It identifies the projection algorithm rather than the full binary or dependencies.
There is no unknown-identity fallback.

Remaining limits:

- Moderate: the core bounded inventory scans the local store before cut selection.
  Unrelated store growth can exhaust a scan limit for an old cut.
  The result refuses rather than returning partial support.
- Moderate: causal queries share a one-million-step work budget.
  Large decision sets can require a narrower frontier.
- Minor: the library has no encoded output-size cap.
  The later brief adapter owns that cap.
- The local root cannot establish reviewer independence or organizational approval.
  Global completeness remains unknown.
- Source identity and evidence hashes do not prove execution or repository ownership.
  The projector does not check source commit existence.
- Evidence observations happen after the canonical snapshot lock is released.
  The deterministic result has no wall-clock field.
  Observation time belongs in separate caller metadata.
- The library reads canonical storage directly.
  It does not create, repair, or require the derived index.

No unresolved Critical or Important implementation finding remains from local review.
Independent parent review remains required before integration.
No recorder, CLI, catalog, wire format, trust model, runtime dependency, or general
policy engine changed.
No `.pact` object, Kata state, or parent progress file changed.

## Retained evidence hashes

| Artifact | SHA-256 of retained bytes |
| --- | --- |
| [pzm4-check-pre-freeze.log](evidence/pzm4-check-pre-freeze.log) | `sha256:3b3c203df43ba033f0669eaf883918725f1bb06bcd7e1a5a03d3cdc1332a9e61` |
| [pzm4-fixture-failure.log](evidence/pzm4-fixture-failure.log) | `sha256:17de125f39bb4f79231f09cb62fe4dfc33a09d07453a619dc093b81798315019` |
| [pzm4-frozen-check.log](evidence/pzm4-frozen-check.log) | `sha256:39a80998ba8cdc48542bbc380e2f503092ab43c526341acfe824241e1dce4a9f` |
| [pzm4-frozen-focused.log](evidence/pzm4-frozen-focused.log) | `sha256:a5a434b05f9f01e1fbfd087506d992179d07af2353e47b036c54808d81cf99f1` |
| [pzm4-frozen-lint.log](evidence/pzm4-frozen-lint.log) | `sha256:e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| [pzm4-frozen-race.log](evidence/pzm4-frozen-race.log) | `sha256:cd1a9925a4cdabf2b00bfd481960812c9e9c5d3930b4a4e04370a0466d79ac08` |
| [pzm4-frozen-source.json](evidence/pzm4-frozen-source.json) | `sha256:3aad54e644ceb94145ffae2d83a44ef6ac8cfb78c9fee5b5b4e824921b9e0020` |
| [pzm4-green-driver.log](evidence/pzm4-green-driver.log) | `sha256:160a82e993fdd5a826eb24f6a5b8e612dfc1b16d2eefce25acda20e217fc0e40` |
| [pzm4-green-expanded.log](evidence/pzm4-green-expanded.log) | `sha256:b98bddd20bbeb643e828d1dd9edafa0d556a772f450c1da7771f37170edf854a` |
| [pzm4-green-final-review.log](evidence/pzm4-green-final-review.log) | `sha256:dd6fd687408d592d431fda49a8d40b13231d2b2c071169c8b4e49bfe0c6e303b` |
| [pzm4-green-initial.log](evidence/pzm4-green-initial.log) | `sha256:6e3dd708dd991d9ed04b695be6519bfe059e36a7a91d4323a676e4fa2be3bd67` |
| [pzm4-green-review.log](evidence/pzm4-green-review.log) | `sha256:2dcaf198591f8ccda6dcdc4ad4567eede12ea398ecab42127222eaa268d5d9ae` |
| [pzm4-hooks-go-fix-failure.log.gz](evidence/pzm4-hooks-go-fix-failure.log.gz) | `sha256:75e1b5a5c2d8f5460f3addbc40cc5b65bb8dd07b10a013db3ac1fe8207841dc9` |
| [pzm4-lint-pre-freeze.log](evidence/pzm4-lint-pre-freeze.log) | `sha256:e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| [pzm4-race-pre-freeze.log](evidence/pzm4-race-pre-freeze.log) | `sha256:b349f9aaed5f4b5d709c79639e4d7bea3ae3db2e297a7b1ea2c5922bb8c63205` |
| [pzm4-red-causal-budget.log](evidence/pzm4-red-causal-budget.log) | `sha256:6f107f6b4ee342d83b321ef5bf636c69f52d3c7f4cbbbc6e7898d0349d66dad8` |
| [pzm4-red-default-frontier.log](evidence/pzm4-red-default-frontier.log) | `sha256:9f5ae7539597807404e74ed5e5847f2ad6f82f03e1ea8596b5ebb53adaf6c831` |
| [pzm4-red.log](evidence/pzm4-red.log) | `sha256:7fbdd3264782f55eae16fbd6111977fdeb3b8b2dbc15215cce284ca4496d10ac` |
| [pzm4-source-hooks.log](evidence/pzm4-source-hooks.log) | `sha256:40e828b677f09506822f9e011d9020b91c80c3ae9a70d1a8e1e6bb891463572e` |

The decompressed original hook-failure log has digest
`sha256:8837f49e16fc7018863361261a59cbda390a10e4832e7d79c8ea705a46b0c6dc`.
