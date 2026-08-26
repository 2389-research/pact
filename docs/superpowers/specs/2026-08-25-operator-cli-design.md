<!-- ABOUTME: Defines PACT's safe setup and polished operator command-line experience. -->
<!-- ABOUTME: Freezes human, machine, visual, and Omakase evaluation contracts before implementation. -->

# PACT Operator CLI Design

**Status:** Foundation and setup implemented; log, query, and show renderer
work remains pending.

## Goal

Make PACT pleasant for a person who sets up a ledger, checks its health, reads
its history, and troubleshoots it. Agents remain the main authors of events and
commits. The human path centers on `setup`, `status`, `log`, `query`, and
`show`.

The result is a polished command-style terminal interface. It does not launch
a persistent full-screen application and does not borrow card or dashboard
patterns from web interfaces.

## Success Criteria

- A person can turn a new directory into a verified, indexed PACT repository
  with one guided `pact setup` run.
- `pact status` answers whether canonical state is valid and indexed reads are
  ready, then gives one exact action when attention is needed.
- `pact log`, `pact query`, and `pact show` are easy to scan while preserving
  PACT's causal and completeness claims.
- Help works at every command level, successful help exits zero, and likely
  command typos receive a useful suggestion.
- Terminal output has clear hierarchy, restrained semantic color, good narrow
  layouts, and no color-only meaning.
- Explicit existing commands keep their JSON fields, data order, limits, and
  exit-code meanings.
- Redirected output, `--json`, and non-terminal operation stay deterministic
  and never prompt.
- Every write remains explicit. Inspection commands do not repair canonical or
  derived state.

## Product Surface

The release adds or improves these human paths:

```text
pact setup [--repo PATH] [--namespace NAMESPACE] [--actor ACTOR]
           [--key-file PATH] [--json]

pact status [--repo PATH] [--json]

pact log [existing filters and pagination flags] [--json]
pact query [existing filters and pagination flags] [--json]
pact show [--repo PATH] IDENTIFIER [--json]

pact
pact help [COMMAND [SUBCOMMAND]]
pact --help
pact COMMAND --help
```

The existing authoring and maintenance commands remain available. The work
does not add `record` or another human event-writing command.

## Command and Repository Resolution

One command registry is the source for dispatch, help, examples, command
groups, typo suggestions, and future completion metadata. A command must not
be added to a switch while absent from help, or described in help while absent
from dispatch.

Commands that open a repository discover it from the current directory by
walking through parent directories until they find `.pact`. Discovery stops at
the filesystem root. Store validation still decides whether the discovered
path is a valid PACT store.

An explicit `--repo PATH` is authoritative. PACT resolves and opens that exact
project path without walking to one of its parents. `setup --repo PATH` treats
that path as the project root to configure. Repository-independent commands,
such as key generation and hashing, do not perform discovery.

Resolution happens once before domain work. Handlers receive one normalized
repository path and do not repeat discovery or path guessing.

## Terminal and Machine Modes

Terminal detection belongs at the process boundary and is injectable below
`main` for deterministic tests. It changes presentation and prompting, never
command data, ordering, limits, or exit semantics.

- Only `setup` may prompt, and only when required input is missing.
- A fully specified command never prompts.
- `--json` never prompts and never contains ANSI escapes.
- A non-terminal invocation never prompts.
- Human output in `auto` mode sent to a non-terminal is plain, stable text.
- `--color auto|always|never` controls human color. `auto` is the default.
- `NO_COLOR` and `TERM=dumb` disable automatic color and terminal symbols.
- Color and symbols reinforce text labels; neither carries unique meaning.

Existing placements and meanings of `--json` remain valid. New presentation
flags do not alter JSON fields.

Color precedence is fixed: JSON is always plain; an explicit `--color` value
wins for human output; otherwise `NO_COLOR`, `TERM=dumb`, or a non-terminal
stream selects plain mode; otherwise `auto` selects color and symbols.

## Setup

**Implementation status:** Complete on `wip/setup-cli`. The remaining sections
of this design keep their prior status.

`pact setup` converges the requested directory on a usable local ledger:

1. initialize or validate the store;
2. create or validate one external signing key;
3. add or validate its trusted public root;
4. run strict canonical verification;
5. create, replace, or validate the disposable index.

Setup writes no event, commit, or checkpoint. Its index step changes only
derived state under `.pact/index`.

### Input and Prompting

`--repo` defaults to the current directory. Namespace, actor, and key path have
no hidden non-interactive defaults.

A terminal run prompts for missing namespace, actor, and key path. When
observed state already supplies a value, the prompt shows that value for
confirmation. Fresh state shows a short valid example, not an implicit
identity. An empty response remains missing. No suggested value is applied to
a non-interactive run.

After collecting missing input, setup observes the target and prints one plan
before asking for one confirmation. Declining returns success with status
`cancelled` and writes nothing. A fully flagged invocation treats its flags as
explicit intent and proceeds without a confirmation prompt.

JSON and non-terminal runs require namespace, actor, and key path. Missing
input exits with usage code 2 before any write.

### Convergence and Conflicts

Setup observes all relevant state before mutation. An identical rerun succeeds
without changing store, key, trust, canonical object, or current index bytes.
A valid partial run resumes from its first missing or stale resource.

Setup never replaces a conflicting store, private key, or trusted root. It
stops on a namespace mismatch, actor mismatch, malformed or unsafe key, trust
conflict, corrupt store, or unsafe filesystem shape. The diagnostic names the
conflict and gives a safe next action when PACT can know one.

The private key stays outside `.pact` and uses restricted permissions. Setup
validates the requested path against the resolved project before either the
store or key exists. No result, prompt, log, or error prints private key bytes
or public key bytes.

Concurrent identical runs converge. Concurrent conflicting runs do not
overwrite the winner. A create conflict must reopen and validate the published
state; an arbitrary filesystem failure never counts as an identical race.

### Result

Setup's actions always appear in store, key, trust, verify, index order.
Resource actions report `created` or `existing`, verification reports `valid`,
and index reports `created`, `rebuilt`, or `current`.

```json
{
  "operation": "setup",
  "ok": true,
  "status": "ready",
  "repo": "/resolved/project",
  "store": "/resolved/project/.pact",
  "namespace": "org/example/widget",
  "actor": "Alice",
  "key_file": "/resolved/keys/alice.json",
  "key_id": "ed25519:sha256:...",
  "actions": [
    {"name": "store", "status": "created"},
    {"name": "key", "status": "created"},
    {"name": "trust", "status": "created"},
    {"name": "verify", "status": "valid"},
    {"name": "index", "status": "created"}
  ]
}
```

A cancelled result uses `ok:true`, `status:"cancelled"`, and an empty action
list. Failures retain the current JSON error envelope. If setup fails after a
successful step, error details include completed actions so the caller can
report the durable partial state and rerun safely.

All result writers propagate write and flush errors. A renderer failure returns
unexpected-error exit code 10 even when the operation already published valid
state. PACT does not claim it can print recovery text through a failed stream.

## Status

`pact status` is the daily health command. It performs strict ledger
verification and index inspection, then renders one combined typed result. It
uses the existing verification and index domains; the command layer does not
parse canonical files or invent a second integrity model.

Status reports:

- resolved repository, store, and default namespace;
- overall health: `healthy`, `attention`, or `broken`;
- strict verification outcome and separate verification axes;
- local heads and canonical object, commit, checkpoint, and event counts;
- local replica completeness and the explicit unknown global completeness;
- derived index state, coverage, and rebuild requirement;
- zero or one primary next action.

`healthy` means strict verification passed and the index is current.
`attention` means canonical state passed but indexed reads need operator work.
`broken` means strict verification or safe store inspection failed. A directory
with no PACT store receives the existing store exit class and the action
`pact setup`; PACT does not pretend it inspected a ledger that is not there.

Status returns zero only when strict verification succeeds and the index is
current and able to serve indexed reads. Verification failures retain their
existing integrity, authorization, or missing-dependency exit class. A missing,
stale, corrupt, incompatible, or partial-build index returns missing-dependency
code 9 after rendering the observed status. Human mode writes that failed
summary and its action to stderr. JSON mode uses the existing error envelope
and places the same typed status summary in `details`; it does not emit a
second JSON document to stdout.

Status never creates or rebuilds an index. It does not store a cached health
claim or report integrity that it did not check.

When canonical verification fails before safe index inspection, `index` is
JSON `null` and human output says `not inspected`. PACT does not label an index
missing merely because invalid source bytes prevented inspection.

The new JSON result has one compact summary rather than embedding every object
from `verify --json`:

```json
{
  "operation": "status",
  "ok": true,
  "health": "healthy",
  "repo": "/resolved/project",
  "store": "/resolved/project/.pact",
  "default_namespace": "org/example/widget",
  "verification": {
    "strict": true,
    "ok": true,
    "integrity": {"errors": [], "warnings": []},
    "structure": {"errors": [], "warnings": []},
    "authenticity": {"errors": [], "warnings": []},
    "dag": {"errors": [], "warnings": []},
    "references": {"errors": [], "warnings": []}
  },
  "index": {},
  "replica": {},
  "counts": {},
  "heads": {},
  "next_action": null
}
```

`index` and `replica` reuse the current `index status` shapes. Counts reuse
existing verification field names. When action is needed, `next_action` is an
object with stable `reason` and `command` strings.

## Log, Query, and Show

`log` keeps its current filters, causal result, cursor rules, and default page
limit of 100 in both human and JSON modes. A terminal does not receive a
smaller semantic page merely because it is a terminal.

Human log output becomes a compact history:

- index and completeness context appears once, before results;
- causal batches remain visibly separate;
- unresolved events remain in a separate labeled group;
- observed time stays labeled advisory;
- immutable event references remain copyable;
- narrow output moves long references to their own line rather than hiding
  required text;
- a next page prints an exact continuation command using the returned cursor.

The layout never implies that causal batch number is a global or wall-clock
order. `query` uses the same event and batch primitives while retaining its
larger field set and existing filter display.

`show` places identifier, kind, integrity, and authenticity first. It renders
event metadata and content, or object metadata and body, as labeled sections.
Nested values use a deterministic indentation and key order suitable for
terminal reading. Its JSON result does not change.

## Help and Diagnostics

`pact`, `pact help`, and `pact --help` print top-level help to stdout and exit
zero. Command and nested-command help do the same. Help contains a one-line
purpose, usage, command groups, meaningful flag descriptions, and at most two
examples for the requested level.

Unknown commands exit with usage code 2. PACT suggests one command only when a
deterministic similarity threshold finds a clear match. It does not guess when
several commands are equally plausible.

Human diagnostics use this structure:

```text
× index is stale

  The ledger changed after the last index build.
  Run: pact index rebuild
```

Every diagnostic states what failed. When the cause and safe response are
known, it also states why and supplies one exact action. It omits an action
when guessing could damage or hide state. Machine errors preserve the current
`ok`, `error`, `exit_code`, and optional `details` envelope.

Raw framework errors, Go flag usage dumps, stack traces, SQL, cursor internals,
and secret material never reach normal diagnostics.

## Visual Language

The interface uses terminal typography: spacing, indentation, alignment,
weight, and restrained color. It avoids large ASCII branding, nested boxes,
animation, and web-style cards.

- Green means verified or ready.
- Amber means operator action is needed.
- Red means the command or data failed.
- Cyan distinguishes references and commands.
- Dim text carries secondary context but never required information.
- `✓`, `!`, and `×` are the automatic-terminal symbols; plain mode uses words.
- Headings and labels remain readable with every color removed.

Layouts target 60, 80, and 120-column terminals. They may wrap and reflow, but
must not omit data, change result order, or truncate a value needed for a
follow-up command. JSON and redirected text do not depend on terminal width.

The first Omakase slice will include real status and help fixtures so visual
quality is judged from output, not from a renderer API in isolation.

## Internal Boundaries

The process adapter owns OS streams, terminal capability, width, environment,
and process exit. A testable application adapter owns command resolution,
repository discovery, rendering selection, and error mapping. Domain handlers
return typed results and errors; they do not print.

Existing packages remain the sources of truth:

- `internal/store` owns store layout, store validation, safe filesystem access,
  and a read-only default-namespace accessor;
- `internal/identity` owns key creation, loading, and private-key path safety;
- `internal/ledger` owns trust, strict verification, heads, show, and canonical
  meaning;
- `internal/index` owns disposable-index status, rebuild, log, and query.

A small typed setup service owns observe, plan, and apply sequencing. It calls
the domain packages and does not reproduce their file formats. The CLI owns
prompts and confirmation. Setup workflow types must remain specific to setup;
this release does not add a general desired-state engine.

Status may use a small typed application service to compose existing ledger
and index results. It may not duplicate their parsers, status rules, or data.

Human and JSON renderers consume the same typed command result. Renderer-only
maps are allowed at the serialization edge; command handlers must not build
one human result and a separate machine result.

## Prior Setup Review Requirements

The deferred setup branches are evidence, not merge sources. The selected
implementation must resolve the four Important findings from their final plan
review:

1. Any domain API signature change updates every production and test caller in
   the same compiling step, including setup.
2. Lock and mutation outcomes are typed. A conflict reopens and validates
   published state, cleanup never unwraps a nil error, and arbitrary failures
   do not masquerade as convergence.
3. Alias-path concurrency tests cover both initialization and later mutation,
   and prove final bytes as well as returned status.
4. Every normal and JSON output path propagates writer failure. Tests prove the
   exit code and the in-memory completed-action result after publication; they
   do not demand a diagnostic from the failed stream itself.

## Omakase Selection

Before the full build, three isolated variants implement the same vertical
slice: command registry, help, repository discovery, and status rendering.

1. a focused standard-library command registry;
2. Cobra behind the same application boundary;
3. Kong behind the same application boundary.

The variants receive this spec and one black-box scenario contract. They may
not change JSON, exit, discovery, terminal, or visual requirements to suit a
framework. The evaluation favors human usability, contract correctness,
maintainability, test quality, and small scope. Dependency count alone does not
win, and unused framework features earn no credit.

After the shared scenarios and fresh-eyes review, the judge selects one
variant. The losing worktrees and branches are removed. Setup, log, query,
show, and global diagnostic polish proceed only on the winning branch.

## Test and Verification Contract

Development follows TDD. The first test in each behavior slice must fail for
the missing behavior before production code changes.

Unit tests cover command metadata, typo thresholds, repository resolution,
status classification, setup planning, renderer wrapping, ANSI policy, and
writer failures.

Integration tests use real stores, real Ed25519 keys, canonical objects, and
real SQLite indexes. They cover setup convergence, partial states, conflicts,
strict status, stale and broken indexes, log pagination, query detail, and show
rendering.

End-to-end tests execute the compiled `pact` binary. Interactive coverage uses
a real pseudoterminal; it does not add a production mock mode. Scenarios cover:

1. fresh guided setup, one confirmation, strict verification, and a current
   initial index;
2. setup rerun, partial-state recovery, conflicts, concurrent aliases, and no
   secret leakage;
3. discovery from a nested directory and authoritative `--repo` behavior;
4. healthy, incomplete, invalid, missing-index, and stale-index status with
   exact exit classes and next actions;
5. readable log, query, and show output over real signed events;
6. top-level and command help, no-argument help, typo suggestions, JSON, pipes,
   `NO_COLOR`, and `TERM=dumb`;
7. stdout and stderr failures before and after durable setup actions.

Golden files cover plain and colored output at 60, 80, and 120 columns. Tests
assert that plain output contains no escape bytes and that every status has a
text label independent of color or symbol.

Before completion, the winner must pass unit, integration, end-to-end, race,
canonical `scripts/check`, compiled-binary smoke, and repository dogfood runs.
No existing test, lint, or hook failure may be hidden or bypassed.

## Out of Scope

- a full-screen TUI, web UI, daemon, or live watcher;
- event authoring, commit authoring, or a `record` command for people;
- automatic repair from `status`, `log`, `query`, or `show`;
- canonical writes hidden behind inspection commands;
- global key registries, key rotation, trust removal, or setup manifests;
- shell completion scripts in this release, though command metadata must not
  block them later;
- Phase 3 schema, projection, delegation, sync, or authority features.
