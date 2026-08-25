<!-- ABOUTME: Records the verified Phase 0 and Phase 1 MVP state and dogfood evidence. -->
<!-- ABOUTME: Separates the shipped core from deferred setup work and later milestones. -->

# Phase 0 and Phase 1 MVP Status

## Status

The Phase 0 contract and Phase 1 single-replica core passed their product gate
and were dogfooded on this repository on 2026-08-24. The verified code revision
was `9428ad1` on `wip/phase-0-1-core`.

This historical snapshot is not “phase 0.1.” Phase 0 froze the v1 contract, and
Phase 1 implemented the local immutable core. At revision `9428ad1`, the MVP did
not yet claim the later Phase 2 index, query, or graph resource limits.

## Gate evidence

The following commands passed from a cold cache without warnings:

```sh
env -u GOROOT mise exec -- ./scripts/check
env -u GOROOT mise exec -- go test -race ./...
env -u GOROOT mise exec -- go vet ./...
env -u GOROOT mise exec -- go build -o .scratch/pact-build-check ./cmd/pact
env -u GOROOT mise exec -- golangci-lint run
```

The canonical check passed all Go unit and end-to-end packages plus all 17
Python reference tests. The linter reported zero issues. This satisfies the
Phase 0 gate through the independent Go implementation and Python verifier.
The real lifecycle below satisfies the Phase 1 exit gate.

## Dogfood lifecycle

The real binary initialized this repository with explicit Phase 1 commands.
The private key stayed outside the project root.

```sh
pact=.scratch/pact-phase-0-1
repo=/Users/harper/Public/src/2389/pact
key_file="$HOME/.config/pact/keys/org-2389-pact-root.json"

"$pact" init --repo "$repo" --namespace org/2389/pact --json
"$pact" keygen --actor 'Harper Reed' --out "$key_file" --json
"$pact" trust-add --repo "$repo" --key-file "$key_file" --json
"$pact" commit --repo "$repo" --key-file "$key_file" \
  --events .scratch/phase-0-1-dogfood-event.json \
  --namespace org/2389/pact --json
"$pact" heads --repo "$repo" --namespace org/2389/pact --json
"$pact" show --repo "$repo" \
  sha256:5e7cf3a370a15ba838200ecef9fc8927a3855f3d27216466f1a5182598d804ce \
  --json
"$pact" verify --repo "$repo" --strict --json
"$pact" checkpoint --repo "$repo" --key-file "$key_file" \
  --scope org/2389/pact \
  --policy-ref sha256:27ef52b038652366afcb48308326eca92da80c5598291ee0317454e3ab77fac0 \
  --authority-epoch phase-0-1-root-v1 \
  --schema-ref sha256:14b5fb39a48736dafb104e6232b230799e1f2d2ce021b4c0f6385cf77d36cded \
  --purpose 'Phase 0 and Phase 1 MVP dogfood checkpoint' --json
"$pact" show --repo "$repo" \
  sha256:2e97c1e773c4ff3c272ff736e2218c7c753946552d3e61b9d8f96fc1035d7e42 \
  --json
"$pact" verify --repo "$repo" --strict --json
```

The checkpoint policy reference is the PACT decisions document digest. Its
schema reference is the checked-in event schema digest. Neither is a
placeholder object ID.

## Durable results

- Namespace: `org/2389/pact`
- Signed commit:
  `sha256:5e7cf3a370a15ba838200ecef9fc8927a3855f3d27216466f1a5182598d804ce`
- Stable event reference:
  `pact:event:sha256:5e7cf3a370a15ba838200ecef9fc8927a3855f3d27216466f1a5182598d804ce#phase-0-1-mvp`
- Signed checkpoint:
  `sha256:2e97c1e773c4ff3c272ff736e2218c7c753946552d3e61b9d8f96fc1035d7e42`
- Strict verification: two valid objects, one commit, one checkpoint, one
  event, one authorized commit, and zero integrity, structure, authenticity,
  DAG, reference, unauthorized, or indeterminate failures.
- Head: the signed commit above under `org/2389/pact`.

Both object and event inspection returned valid integrity and authenticity.
The checkpoint frontier contains the expected head. Two consecutive read-only
strict verifications produced byte-identical JSON.

The original dogfood run recorded a real-store recovery check that moved
`.pact/index` and `.pact/refs` aside, verified strictly twice, compared every
canonical object hash, and restored the empty directories. Verification stayed
valid and canonical hashes did not change.

The external key file is mode `0600`; the PACT-owned `pact` and `keys`
directories above it are mode `0700`. An exact private-key-byte scan found no
match under `.pact`.

## Deferred setup CLI

The setup CLI is not part of this shipped core. The Omakase ended without an
accepted variant, and setup work stopped. The proposed architecture-repair
plan records a rejected verdict with four Important findings.

No setup production code was merged into `wip/phase-0-1-core`. The rejected
design, plan, and review are preserved on branch
`setup-cli/omakase/setup-service` at:

- `docs/plans/setup-cli/architecture-repair.md`
- `docs/plans/setup-cli/omakase/variant-setup-service/architecture-repair-plan.md`
- `docs/plans/setup-cli/omakase/variant-setup-service/architecture-repair-review.md`

Use `init`, `keygen`, and `trust-add` until setup receives a new bounded goal
and an approved plan.

## Later milestone

At this snapshot, Phase 2 was the next bounded goal. Its planned scope was the
remaining DAG, SQLite index/rebuild, query, partial-replica diagnostics, and
resource-limit contract in
`docs/pact-ledger/references/implementation-plan.md`. Phase 2 work began after
this recorded Phase 0/1 gate; the repository README describes the current
operator surface.
