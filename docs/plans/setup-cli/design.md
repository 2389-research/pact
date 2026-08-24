<!-- ABOUTME: Defines the shared contract and evaluation rules for the PACT setup CLI. -->
<!-- ABOUTME: Keeps all Omakase variants aligned while they explore different internal designs. -->

# PACT Setup CLI Design

## Goal

Add one safe, convergent `pact setup` command that bootstraps a usable local
PACT ledger for both people and automation. Setup initializes the store,
creates or reuses one external signing key, trusts its public identity, and
runs strict verification. It does not write an event, commit, or checkpoint.

## Command Contract

```text
pact setup [--repo PATH] --namespace NAMESPACE --actor ACTOR --key-file PATH [--json]
```

- `--repo` defaults to `.`.
- `--namespace`, `--actor`, and `--key-file` have no hidden defaults.
- `--key-file` is the one explicit signing-key path. Setup creates the key when
  it is absent and validates and reuses it when it exists. Setup never replaces
  a key.
- `--json` uses the existing global JSON behavior and never prompts.
- A non-terminal invocation never prompts. It requires every non-default flag
  and exits with usage code 2 before writing anything when a value is missing.
- A terminal invocation prompts for missing values. When it had to prompt for
  any value, it prints the observed state and exact action plan to stderr, then
  asks for one confirmation before writing.
- A fully flagged terminal invocation does not prompt. The flags already state
  the operator's intent.
- Declining the one confirmation returns exit code 0, reports `cancelled`, and
  writes nothing.
- Prompt and plan text goes to stderr. Machine-readable or normal command
  results go to stdout.

Terminal detection belongs at the process adapter and must be injectable in
tests. Core setup logic never reads a terminal or process-global stream.

## Convergence and Conflict Rules

Setup first observes all relevant state without changing it. It compares that
state with the requested namespace, actor, key path, and trust root.

Before any write, setup validates the requested key location against the
resolved target repository even when the key does not exist and the store is
not initialized. It resolves every existing path ancestor and refuses lexical
or resolved containment. An in-repo key request therefore cannot leave behind
either a key or a partially initialized store.

An exact rerun succeeds and reports existing resources. A partial run resumes
from the first missing resource. Safe partial states include:

- an initialized matching store with no key;
- a valid matching key with no store;
- a matching store and key whose public identity is not trusted.

Setup stops without replacing data when it finds any conflict:

- an initialized store with a different namespace;
- an existing key with a different actor;
- a malformed key, a private key inside the project, a non-regular key, or a
  private key with group or other permission bits;
- a trusted entry whose key ID or public bytes conflict with the requested key;
- corrupt store or trust configuration.

The apply order is fixed:

1. initialize or reopen the store;
2. create or reload the external signing key;
3. add or confirm the trusted public root;
4. run strict ledger verification.

Each step rechecks current state through the existing safe primitives. If a
process fails after a completed step, setup leaves that valid state in place;
the next identical run resumes. Setup does not delete or roll back a store,
key, or trust entry. Concurrent identical invocations must converge. A
concurrent conflicting invocation must fail without overwriting the winner.
When a create operation loses an identical race, setup re-observes the new
state and reports it as existing. It never treats an arbitrary create failure
as proof that the new state matches.

## Result Contract

Successful JSON uses resolved absolute paths and this stable shape:

```json
{
  "operation": "setup",
  "ok": true,
  "status": "configured",
  "repo": "/resolved/project",
  "store": "/resolved/project/.pact",
  "namespace": "org/example/widget",
  "actor": "Alice",
  "key_path": "/resolved/keys/alice.json",
  "key_id": "ed25519:sha256:...",
  "actions": [
    {"step": "store", "status": "created"},
    {"step": "key", "status": "created"},
    {"step": "trust", "status": "created"},
    {"step": "verify", "status": "valid"}
  ]
}
```

Resource status is `created` or `existing`; verification status is `valid`.
The action order never changes. A cancelled interactive run uses the same
operation, requested fields, an empty action list, `ok: true`, and
`status: "cancelled"`.

Errors retain the existing JSON envelope and exit-code classes. When a failure
occurs after one or more apply steps, `details.actions` lists completed actions
in the same shape so automation can report partial progress and rerun safely.
No output or error includes private key bytes.

Normal human output identifies the operation and status, then lists the
resolved store, key ID, key path, and action statuses. It never prints the
private key or the public-key bytes.

## Internal Boundaries Shared by Every Variant

All variants must reuse these production primitives rather than reproduce
their file formats or safety checks:

- `store.Init` and `store.Open` for the local store;
- `identity.GenerateKeyFile` and `identity.LoadSigningKey` for the private key;
- `ledger.AddRoot` and `ledger.Roots` for trust;
- `ledger.Verify` with strict mode for the final check.

The store package needs one read-only way to return the validated default
namespace. It remains the only code that parses `format.json`. The CLI must not
duplicate that parser.

The identity package needs one read-only path validator for planned signing
keys. It must apply project-containment checks before a key or store exists and
remain the sole source of key-location safety rules. The CLI and setup workflow
must not duplicate those rules.

The command layer owns flags, terminal prompting, confirmation, JSON and human
rendering, and exit-code mapping. The selected setup architecture owns state
inspection and apply sequencing. Time and terminal behavior must be injected
so tests remain deterministic.

## Omakase Variants

The variants differ only in where setup state and sequencing live. Their CLI,
wire output, safety rules, and scenarios are identical.

### Variant A: CLI Orchestrator

Keep setup inspection and sequencing in focused files under `cmd/pact`. Call
the existing packages directly and use small command-local types for observed
state and actions. This is the fewest new abstractions, but it couples the
workflow to the CLI.

### Variant B: Setup Service

Add an `internal/setup` service with typed request, plan, action, and result
values. The service exposes pure inspection plus an apply operation; the CLI
adapts prompts and output. This creates one reusable boundary without building
a general reconciliation framework.

### Variant C: Desired-State Reconciler

Add an `internal/setup` reconciler that derives an ordered action list from
observed and desired state, then executes that list. This makes convergence
rules explicit and makes future setup resources easy to add, at the cost of
more types and machinery now.

The judge should favor the smallest design that makes conflicts, partial
progress, and concurrent reruns easy to prove. Extensibility that has no use in
this release earns no credit by itself.

## Test Contract

Every variant must add unit, integration, and end-to-end coverage. Tests use
real files, real Ed25519 keys, the compiled CLI where applicable, and no mock
mode in production code.

The same external scenario suite runs against every variant:

1. A fresh, fully flagged non-terminal run creates the store, external key,
   trusted root, and passes strict verification.
2. An exact rerun reports all resources as existing and leaves store and key
   bytes unchanged.
3. Store-only, key-only, and untrusted-key partial states each converge.
4. Store namespace and existing-key actor conflicts fail without writes.
5. Malformed, in-project, wrong-mode, and non-regular private keys fail with
   the correct exit class and leak no key material.
6. Missing values in non-terminal and JSON runs exit 2, emit one diagnostic,
   and write nothing.
7. A terminal run with missing values prompts once for each missing value and
   confirms once; acceptance configures the ledger and refusal writes nothing.
8. Concurrent identical setup runs converge. Concurrent conflicting runs
   never overwrite store, key, or trust state.
9. Existing trusted state succeeds. Corrupt store and trust files return typed
   failures with completed-action details when relevant.
10. Output uses resolved paths, deterministic action order, and no private or
    public key bytes.

Each survivor must also pass the repository's canonical `scripts/check`, Go
race tests, vet, lint, and a real compiled-binary setup run.

## Out of Scope

This release does not add setup manifests, global key registries, key
rotation, trust removal, event templates, commits, checkpoints, rollback,
`--dry-run`, or a long-running setup service. Those need concrete use cases
before they add weight to this command.
