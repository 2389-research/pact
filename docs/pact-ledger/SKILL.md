---
name: pact-ledger
description: >-
  Operate PACT, a signed, append-only semantic ledger for durable provenance,
  resumable state, scoped authority, concurrent writers, projections, and checkpoints.
metadata:
  version: "0.2.4"
---

<!-- ABOUTME: Guides operators through supported PACT recording and local handoff commands. -->
<!-- ABOUTME: Separates signed history, checked proof, local authority, and interpreted acceptance. -->

# PACT Ledger

PACT records signed assertions, observations, decisions, and actions in a causal DAG.
Evidence stays external, addressed by locator and digest.
Indexes, refs, and reports are rebuildable; signed objects are canonical.
Correct history by appending events, never editing objects.
Never record secrets or private personal data.

## Select the ledger and signer

In worktrees, select the established ledger with `--repo PATH`; do not initialize another.
Discovery walks parents for `.pact`; explicit `--repo PATH` selects only that root.
Replace examples' `--repo .` with the canonical root when needed.

`pact setup` creates the store, external key, local trust, verification, and index in order:

```sh
pact setup --repo /path/to/project --namespace org/example/development \
  --actor operator/alice --key-file /path/outside/project/operator.key.json --json
```

Keep keys outside the project under OS access controls; never commit or export them.
Exact setup reruns resume; conflicting namespace, actor, key, or trust refuses overwrite.
Trust bootstrap is out-of-band; a configured root cannot prove its own authority.
Check visibility and contents before publishing.

Protect repository and key directories.

## Shipped commands

Run `pact help` or `pact COMMAND --help` for current flags and examples.
Use `--json` for machines and `--color never` for plain text.

| Command | Purpose |
| --- | --- |
| `pact quickstart` | Print this guide. |
| `pact setup` | Bootstrap or resume setup. |
| `pact init` | Create the store. |
| `pact keygen` | Create an external key. |
| `pact trust-add` | Add a local root. |
| `pact status` | Verify and inspect index health. |
| `pact heads` | Show namespace heads. |
| `pact brief` | Read bounded development state. |
| `pact show` | Read original object/event payloads. |
| `pact evidence check` | Check one event's evidence. |
| `pact verify` | Verify history; `--strict` requires dependencies. |
| `pact log` | Read causal metadata. |
| `pact query` | Filter and page metadata. |
| `pact record` | Record validated development facts. |
| `pact commit` | Sign a raw batch. |
| `pact checkpoint` | Sign a checkpoint. |
| `pact index status` | Inspect the index. |
| `pact index rebuild` | Rebuild explicitly. |
| `pact hash` | Hash evidence. |

The CLI implements Ed25519 objects, DAG validation, root trust, checkpoints, indexes, and `development/v1`.
It lacks arbitrary schemas/policies, replica sync, trusted time, advanced delegation/revocation,
and hardened multi-tenant key service. Non-root authorization remains indeterminate.
References describe broader protocol operations, not additional shipped commands.

## Read a handoff

Run `pact status --repo . --json` to inspect local health.
Healthy status exits 0; index problems exit 9. Nonhealthy JSON goes to stderr.
Investigate integrity or authorization failures before acting.
For index problems, run `pact index rebuild --repo . --json`, then retry.
Reads never repair a missing, stale, corrupt, or incompatible index.
`show` and canonical `verify` remain available without the index.

Select exact namespace, subject, and full source revision.
Repeat `--frontier ID` for an explicit cut; omission captures current namespace heads once.
Index readiness precedes that capture separately; it is not an atomic guarantee.
Retain the returned frontier, profile/policy digests, and projector identity.

After recording the example below, read its exact source with the same bindings:

<!-- development-brief-example -->

```sh
source_revision="$(jq -r '.source.revision' "$work_dir/record.json")"
"$pact" brief --repo "$repo" --profile development/v1 \
  --namespace org/example/development --subject widget/task-1 \
  --source-revision "$source_revision" --json > "$work_dir/brief-result.json"
cat "$work_dir/brief-result.json"
```

JSON contains every WorkState field plus `operation=brief`.
This is derived engine state; use exact `show` for original review/parent payloads and proof.
Human output preserves the same state and escapes control characters.
Exit 0 means a successful observation, not acceptance.

The default encoded limit is 16,384 bytes; `--max-bytes N` accepts 1,024 through 65,536.
The limit includes encoding and final newline.
Oversized required facts exit 2 with empty stdout; JSON stderr reports
`details.code=brief_output_limit`, requested bytes, and required bytes.
Narrow the task/cut or raise the limit. Never truncate blockers; report unread scope.

For discovery or original payloads:

```sh
pact log --repo . --namespace org/example/development --limit 20 --json
pact query --repo . --namespace org/example/development --subject widget/task-1 --limit 100 --json
pact show --repo . 'pact:event:sha256:<commit-digest>#<local-id>' --json
pact evidence check --repo . --event-ref 'pact:event:sha256:<commit-digest>#<local-id>' --json
pact verify --repo . --strict --json
```

`log`/`query` omit payloads and evidence; show each containing object once.
Resolve same-object `local:ID` as `pact:event:COMMIT_ID#ID`.
Read `unresolved`; follow `page.has_more` with unchanged filters/limit and `--cursor page.next_cursor`.
Pages are causal, not newest-first; cursors continue a snapshot, not changes since a session.
If a cursor fails after changes, restart and compare refs. Recheck heads before acting.

Follow targets with `show` and direct successors with `query --caused-by REF` or `query --supersedes REF`.
Repeat relevant successors; filters do not compute transitive closure.
Supersession adds no causal edge or erasure. Time, hash order, and batch number cannot pick winners.

## Record development facts

`pact record --profile development/v1` validates development facts and selected evidence.
It does not run reported commands. Use raw `pact commit` for other or unchecked domains.

Supply one object with `operation_id`, `namespace`, `subject`, `source`, and `events`.
`source` contains repository identity and full Git revision.
Events contain `local_id`, `type`, `payload`, `evidence`, `caused_by`, and `supersedes`.
The recorder supplies kind, subject, schema digest, and profile-owned payload fields.
Do not repeat `operation_id`/`source` in payloads. Unknown fields fail before signing.

Types: `dev.check.executed`, `dev.review.completed`, `dev.acceptance.recorded`, and `dev.next_action.recorded`.
Check outcomes: `passed`, `failed`, `interrupted`, `not_run`, or `unknown`; use `coverage` for incomplete coverage.
Review verdicts: `approved`, `changes_required`, or `inconclusive`.
Acceptance decisions: `accepted` or `rejected`.

This example checks a committed `proof.txt` blob and records the result.
Bind `pact` to the binary, `repo` to the Git/PACT root, `key_file` to its external signer,
and `work_dir` to temporary output storage. Use namespace `org/example/development`.
It requires `git` and `jq`; inspect inputs before signing.

<!-- development-record-example -->

```sh
revision="$(git -C "$repo" rev-parse HEAD)"
git -C "$repo" cat-file -e "$revision:proof.txt"
git -C "$repo" show "$revision:proof.txt" > "$work_dir/proof.txt"
digest="$("$pact" hash "$work_dir/proof.txt" --json | jq -r .digest)"

jq -n --arg revision "$revision" --arg digest "$digest" '{
  operation_id: "proof-blob-check-1",
  namespace: "org/example/development",
  subject: "widget/task-1",
  source: {repository: "synthetic/widget", revision: $revision},
  events: [{
    local_id: "check", type: "dev.check.executed",
    payload: {
      name: "proof blob exists",
      command: ["git", "cat-file", "-e", ($revision + ":proof.txt")],
      outcome: "passed", exit_code: 0, evidence_indexes: [0]
    },
    evidence: [{ref: ("git:" + $revision + ":proof.txt"), digest: $digest,
      media_type: "text/plain", role: "primary"}],
    caused_by: [], supersedes: []
  }]
}' > "$work_dir/record.json"

"$pact" record --repo "$repo" --profile development/v1 \
  --input "$work_dir/record.json" --key-file "$key_file" --json \
  > "$work_dir/record-result.json"
cat "$work_dir/record-result.json"

"$pact" record --repo "$repo" --profile development/v1 \
  --input "$work_dir/record.json" --key-file "$key_file" --json \
  > "$work_dir/retry-result.json"
cat "$work_dir/retry-result.json"

event_ref="$(jq -r '.event_refs[0]' "$work_dir/record-result.json")"
"$pact" show --repo "$repo" "$event_ref" --json
"$pact" evidence check --repo "$repo" --event-ref "$event_ref" --json
"$pact" query --repo "$repo" --namespace org/example/development \
  --subject widget/task-1 --limit 100 --json
"$pact" verify --repo "$repo" --strict --json
```

`git:FULL_COMMIT:relative/path` selects immutable blob bytes from `--repo`; never abbreviate the commit.
Unprefixed relative paths select mutable bytes, not durable source proof. Other URI schemes are unsupported.
Retain Git objects and paths; digests cannot restore bytes.

### Retry and index recovery

Retry uncertain writes with the same facts, operation ID, namespace, signer, and store.
Identity is scoped by namespace and signer. Exact retries return `admission=existing` and original refs.
Changed facts conflict; inspect every object. Different facts need a new operation ID.
Changing signer does not reconcile an old operation.

Read admission, validation, evidence, and index readiness separately, including error details.
Index-refresh errors do not undo admission. Retain object/event refs; rebuild or retry exact input.
Existing retries retain admission even when evidence changes or disappears.
Raw `commit` and `checkpoint` writes require an explicit index rebuild before indexed reads.
There is no hidden read repair.

### Interpret decisions and blockers

Keep a passed check separate from open Important findings and explicit acceptance.
Absent acceptance stays `not_recorded`; green checks never close findings by themselves.
A resolving review names the original `review_event_ref`, finding ID, correction source,
and selected evidence indexes in `resolves`.
Acceptance separately names exact `check_refs`, `review_refs`, and the policy digest.
Same-batch refs use `local:ID` with explicit `caused_by` links.
External refs use full event refs and must be observed through causal history.
Keep concurrent incompatible acceptance and rejection as `unresolved`.

Local policy requires exact-source passing checks, approved reviews, explicit resolution,
durable matching evidence, and local authority. `supported` is local interpretation, not organizational approval.
WorkState retains declared actions with their own Source and event refs.
Old actions remain historical; check their source and blockers before choosing current work.

Keep integrity, authenticity, authorization, schema validation, evidence availability,
recorded decisions, and supported acceptance separate.
Inspect each evidence assessment; successful execution alone does not mean matching bytes.
Raw verification does not validate arbitrary domain schemas.
A shared key does not establish reviewer independence.
Local closure never establishes global completeness.
Signatures and matching proof cannot prove that a reported check or review actually occurred.

Report subject, source/frontier, checked evidence, blockers/refs, decisions, supported acceptance,
actions with their source, and unread/unresolved scope before acting.

## Protocol detail

- [Architecture](references/architecture.md).
- [Objects, evidence, checkpoints](references/object-model.md).
- [Trust, delegation, revocation, epochs](references/authority-model.md).
- [Projections, conflicts, policy, manifests](references/projection-contract.md).
- [Events, corrections, schemas](references/event-catalog.md).
- [Reference/future CLI](references/cli-contract.md); native help defines shipped commands.
- [Threats and secrets](references/threat-model.md).
- [Workflow integration](references/adoption-guide.md).
