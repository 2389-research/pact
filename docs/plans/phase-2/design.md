<!-- ABOUTME: Defines the implemented contract for PACT Phase 2. -->
<!-- ABOUTME: Freezes bounded graph, disposable SQLite, causal query, and completeness behavior. -->

# PACT Phase 2 design

**Status:** Tasks 1–7 are implemented and committed through `218536f` from
Phase 2 start `a6c6c14`. Repository dogfood is recorded by commit object
`sha256:69d67237ecf856c5b72515cd1ac43322642a847c1fc0d35955cd30e911146743`
and checkpoint object
`sha256:511f085606dc807fd3a67b2fd80afb37355d27f4b677b087cb337b32552eb485`.
The initial independent broad review returned three Important and three Minor
findings, and its re-review found three Important and two stale-doc Minor
findings. Both TDD fix waves passed the full gate. The final independent
re-review covers the Task 8 working tree based on `218536f`, from Phase 2 start
`a6c6c14`, and reports 0 Critical, 0 Important, and 0 Minor findings. Its
focused closeout commit remains pending; this record does not invent that ID.

**Dependency evidence:** Official Go module metadata on 2026-08-24 resolves
`modernc.org/sqlite` to `v1.57.0`. That module declares Go 1.25 and pins
`modernc.org/libc v1.74.4`; both are compatible with this repository's Go 1.26
toolchain. Phase 2 pins those exact versions and verifies the module graph. The
current SQLite `magic.txt` application-ID list contains no `0x50414354` or
`PACT` assignment; schema v1 uses that mnemonic ID.

## Scope

Phase 2 adds bounded canonical scanning and graph work, a disposable SQLite
index, deterministic log/query commands, cursors, and explicit local-replica
diagnostics. It reuses the working Phase 0/1 object, crypto, parent, head,
reference, `show`, verify, and checkpoint behavior.

It does not add setup, schema or payload validation, delegated authority,
projection policy, sync, raw SQL, index migration, or another canonical format.
Phase 3 stays out of scope.

## Invariants

- `.pact/objects/` is the sole ledger history. SQLite never changes object
  acceptance, IDs, signatures, heads, trust, authorization, or checkpoints.
- `.pact/index/`, `.pact/tmp/`, and `.pact/refs/` remain derived and ignored.
- `show` and strict verification work when the index is absent or unusable.
- Rebuild indexes only integrity-valid, canonical, structurally valid, authentic
  objects. Missing dependencies may yield a usable partial-replica index;
  cross-namespace parents, cycles, bad signatures, or limit failures do not.
- Read commands never rebuild. Recovery is always the explicit rebuild command.
- `observed_at` is metadata. No order, filter continuation, or cursor key uses it.
- “Locally closed” means every dependency named by present canonical objects is
  present. It never means the replica contains all objects that exist elsewhere.
- Existing Phase 0/1 JSON fields and meanings remain unchanged. New fields are
  additive.

## Exact CLI

```text
pact index status  --repo PATH [--json]
pact index rebuild --repo PATH [--json]

pact log --repo PATH
  [--namespace PREFIX]...
  [--actor KEY_ID]...
  [--limit N]
  [--cursor TOKEN]
  [--json]

pact query --repo PATH
  [--namespace PREFIX]...
  [--type EVENT_TYPE]...
  [--kind KIND]...
  [--subject SUBJECT]...
  [--actor KEY_ID]...
  [--tag TAG]...
  [--schema-ref OBJECT_OR_CORE_SCHEMA_REF]...
  [--event-ref EVENT_REF]...
  [--caused-by EVENT_REF]...
  [--supersedes EVENT_REF]...
  [--limit N]
  [--cursor TOKEN]
  [--json]
```

`index` accepts exactly one subcommand. `log` is the compact event view;
`query` is the structured event view and requires at least one filter. `show`
keeps its current CLI and reads canonical bytes directly.

Repeated values in one filter family are OR. Different families are AND. A
namespace prefix matches the exact namespace or a slash-delimited descendant,
so `org/2389` does not match `org/23890`. All other filters are exact,
case-sensitive matches after the domain's existing normalization. There are no
globs, regular expressions, comma lists, payload searches, caller-selected sort
keys, SQL fragments, or expression language.

The implementation validates namespace grammar, then uses the fixed
parameterized predicate `namespace = ? OR namespace GLOB ?` with the second
value formed as `prefix + "/*"`. Valid namespaces cannot contain SQLite GLOB
metacharacters. Subject and tag inputs use the existing NFC normalization; all
other values use their current domain grammar before SQL construction.

`--limit` defaults to 100 and accepts 1 through 1,000. Query input over that
range is a usage error. A page fetches at most `limit + 1` indexed rows and
hydrates at most `limit` canonical results. Empty results succeed with no next
cursor.

Status is an inspection command: it exits zero when it successfully identifies
any known state. Log/query accept only `current` indexes. They refuse `missing`,
`stale`, `corrupt`, `incompatible`, or `partial-build` states and direct the
operator to explicit rebuild. Rebuild never trusts or migrates the old database.
The JSON `operation` values are `index-status`, `index-rebuild`, `log`, and
`query`. Unavailable status values are JSON `null`, never empty sentinel strings.

## Output model

Index status and every indexed result contain two separate state objects:

```json
{
  "index": {
    "state": "missing|current|stale|corrupt|incompatible|partial-build",
    "coverage": "complete|partial|unavailable",
    "path": "/absolute/repo/.pact/index/pact-v1.sqlite3",
    "schema_version": 1,
    "source_fingerprint": "sha256:...",
    "logical_digest": "sha256:...",
    "rebuild_required": false
  },
  "replica": {
    "scope": "local_object_set",
    "completeness": "locally_closed|incomplete|unassessed",
    "global_completeness": "unknown",
    "blockers": []
  }
}
```

`index status` and `index rebuild` return `operation`, `index`, `replica`, a
`counts` object (`objects`, `commits`, `checkpoints`, `events`, `edges`, and
`canonical_bytes`), and `limits:{"profile":"pact/resource-limits/phase2-v1",
"status":"within_limits"}`. Rebuild also returns `created` and `replaced`
booleans. Missing or unavailable `path`, version, digest, and count values are
JSON `null`.

`coverage=partial` means the index fully covers a dependency-incomplete local
replica. `state=partial-build` means the derived database does not cover its own
declared source and is unusable. These are not the same condition.

Completeness blockers have only stable code, validated source object/event ref,
field, and missing immutable ref. V1 codes are:

- `missing_parent`;
- `missing_event_reference`;
- `missing_checkpoint_head`;
- `missing_previous_checkpoint`.

Integrity, structure, authenticity, DAG, references, authorization, and
completeness remain separate axes. Loose verify may succeed while reporting an
incomplete local replica. Strict verify fails any blocker. Official checkpoint
creation uses the same strict canonical scan and refuses before signing.

`verify --json` keeps all current fields and adds `completeness` and `limits`.
Its existing scalar `index_status` remains and reflects the current derived
state. `show --json` does not change.

Log returns compact event records: stable event ref, commit ID, namespace,
parents, actor, advisory observed time, kind, type, subject, and tags. Query
returns those fields plus local ID, schema ref, `caused_by`, and `supersedes`.
Neither command returns payload or evidence; `show EVENT_REF` remains the exact
canonical inspection path.

All result rows are resolved against and compared with their canonical commit
before output. A missing or mismatched event makes the index corrupt. Applied
filters pass the existing secret-hazard check before they can be echoed. Error
details contain codes and safe IDs, never payload excerpts, raw cursor input,
SQL, DSNs, signatures, public-key bytes, private-key paths, or matched secrets.

Log/query JSON has this exact top-level shape:

```json
{
  "operation": "log|query",
  "index": {},
  "replica": {},
  "filters": {
    "namespace": [], "type": [], "kind": [], "subject": [],
    "actor": [], "tag": [], "schema_ref": [], "event_ref": [],
    "caused_by": [], "supersedes": []
  },
  "order": {
    "kind": "known_causal_batches/v1",
    "tie_breaker": "immutable_reference",
    "tie_breaker_is_semantic": false,
    "observed_at_used": false,
    "global_completeness": "unknown"
  },
  "batches": [{"batch": 0, "complete_in_page": true, "items": []}],
  "unresolved": [],
  "page": {"limit": 100, "returned": 0, "has_more": false, "next_cursor": null}
}
```

Every filter key is present as a sorted, unique array. A page may contain part
of one matching batch; that batch uses `complete_in_page:false` on every page
that lacks some of its matching items. `unresolved` is a transport group after
ordered batches, not a claim that unresolved events occurred later. Human output
uses “causal batch” and “known local dependencies,” labels observed time
“advisory,” prints unresolved events separately, and states that batch order is
not a total order.

Page assembly first charges the exact empty page, including causal groups,
cursor, page fields, empty destinations, and the trailing newline. It then
charges each hydrated event and its array comma before retaining that event.
The same streaming `QueryPage` serializer checks the final result and writes
CLI JSON. Exact 16 MiB output passes; the first excess returns typed
`json_result_bytes` without allocating a full-page encoding or retaining the
excess row.

## One bounded ledger scan

Phase 2 refactors the current verifier behind one context-aware
`ledger.Scan`. `Verify`, `Heads`, commit parent selection, checkpoint preflight,
and index rebuild consume its results or smaller views backed by it. The index
package does not parse canonical JSON, verify signatures, compute heads, or own
graph meaning.

The current recursive cycle walk becomes an iterative bounded algorithm while
preserving its semantic contract. Limit checks occur before allocation where
the encoded size exposes a count, and again while streaming.

Resource exhaustion returns a typed result:

```go
type LimitError struct {
    Resource        string
    Maximum         uint64
    ObservedAtLeast uint64
    ObjectID        string
}
```

The machine error code is `resource_limit`. It carries fixed, bounded prose and
at most one validated immutable ID. It never panics, recurses past the boundary,
publishes a partial authoritative result, or keeps scanning merely to compute a
larger exact count.

## Exact resource limits

The fixed profile name is `pact/resource-limits/phase2-v1`.

| Resource | Exact maximum |
|---|---:|
| One canonical object | 4,194,304 bytes (4 MiB) |
| Canonical objects in one scan | 100,000 |
| Total canonical bytes in one scan | 1,073,741,824 bytes (1 GiB) |
| Events in one commit | 1,024 |
| Total events in one scan | 250,000 |
| Parents in one commit | 64 |
| Longest known causal path | 4,096 signed dependency edges |
| One graph frontier | 4,096 graph nodes |
| Parent/event/checkpoint plus synthetic graph edges | 1,000,000 |
| Returned results per page | default 100; hard maximum 1,000 |
| Values in one repeated filter family | 64 |
| Values across all filter families | 256 |
| Encoded cursor | 4,096 bytes |
| Decoded cursor | 3,072 bytes |
| Encoded JSON result, including the CLI newline | 16,777,216 bytes (16 MiB) |
| Stored SQLite file | 2,147,483,648 bytes (2 GiB) |
| Diagnostic samples per axis | 100 |
| One diagnostic text field | 512 UTF-8 bytes |

Depth counts parent and resolved event-reference dependencies; synthetic gate
edges do not shorten the allowed canonical history. The total edge count does
include commit parents, two synthetic gate edges per event, `caused_by`,
`supersedes`, checkpoint heads, and previous-checkpoint links. Exact-bound
inputs pass. The first value over fails with `maximum` and
`observed_at_least`. Diagnostic counts remain exact while known; sample caps set
`diagnostics_truncated:true`. Authoritative query rows never truncate silently.

Filter limits apply after normalization and before SQL construction, keeping
the parameter count and request digest bounded. The admission path applies
object, event-per-commit, and parent limits before signing. Existing over-limit
bytes remain immutable but make verification and rebuild explicitly
unassessed/refused.

## Source fingerprint

The deterministic source fingerprint binds exact canonical bytes through their
content IDs. The scanner validates every path ID against the raw-byte SHA-256,
canonical form, object structure, and signature before publication. It sorts IDs
as ASCII and streams:

```text
SHA256(
  "PACT-OBJECT-SET-FINGERPRINT-V1\x00" ||
  uint64_be(object_count) ||
  for each sorted object ID:
    uint16_be(len(object_id)) || UTF8(object_id)
)
```

IDs already bind exact bytes, so mtimes, inode numbers, paths, trust, refs, and
index files do not enter the fingerprint. The displayed form is `sha256:` plus
lowercase hex.

Status and query stream every canonical file under a shared store lock, enforce
the byte/count limits, and require each raw-byte digest to match its path ID
before comparing the object-set fingerprint. Query then re-reads and fully
verifies each distinct selected canonical commit before output. Strict
verification remains the full all-object signature and graph audit; no result
claims that a status check reverified every signature.

## SQLite schema version 1

The only live file is `.pact/index/pact-v1.sqlite3`. The application ID is
`0x50414354` (ASCII `PACT`, decimal `1346454356`), `PRAGMA user_version` is 1,
and metadata format is `pact/sqlite-index/v1`.

All tables are `STRICT`. Relationship tables use `WITHOUT ROWID`. Foreign keys
are enabled. The builder uses one connection, one transaction, and parameterized
inserts in primary-key order. Published readers use read-only/query-only
connections and fixed prepared statements.

```sql
CREATE TABLE index_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT, WITHOUT ROWID;

CREATE TABLE objects (
  object_id TEXT PRIMARY KEY,
  object_type TEXT NOT NULL CHECK (object_type IN ('commit','checkpoint')),
  namespace TEXT NOT NULL,
  body_digest TEXT NOT NULL,
  actor_key_id TEXT NOT NULL,
  actor_label TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  integrity_state TEXT NOT NULL CHECK (integrity_state = 'valid'),
  structure_state TEXT NOT NULL CHECK (structure_state = 'valid'),
  authenticity_state TEXT NOT NULL CHECK (authenticity_state = 'valid'),
  completeness_state TEXT NOT NULL CHECK (completeness_state IN ('complete','partial'))
) STRICT, WITHOUT ROWID;

CREATE TABLE commits (
  commit_id TEXT PRIMARY KEY REFERENCES objects(object_id),
  event_count INTEGER NOT NULL CHECK (event_count BETWEEN 1 AND 1024)
) STRICT, WITHOUT ROWID;

CREATE TABLE parent_edges (
  child_id TEXT NOT NULL REFERENCES commits(commit_id),
  parent_id TEXT NOT NULL,
  resolved INTEGER NOT NULL CHECK (resolved IN (0,1)),
  PRIMARY KEY (child_id, parent_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE events (
  event_ref TEXT PRIMARY KEY,
  commit_id TEXT NOT NULL REFERENCES commits(commit_id),
  local_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('observation','assertion','action','decision','control')),
  event_type TEXT NOT NULL,
  subject TEXT NOT NULL,
  schema_ref TEXT NOT NULL,
  causal_batch INTEGER,
  causal_status TEXT NOT NULL CHECK (causal_status IN ('ordered','unresolved')),
  UNIQUE (commit_id, local_id),
  CHECK ((causal_status = 'ordered' AND causal_batch IS NOT NULL AND causal_batch >= 0) OR
         (causal_status = 'unresolved' AND causal_batch IS NULL))
) STRICT, WITHOUT ROWID;

CREATE TABLE event_tags (
  event_ref TEXT NOT NULL REFERENCES events(event_ref),
  tag TEXT NOT NULL,
  PRIMARY KEY (event_ref, tag)
) STRICT, WITHOUT ROWID;

CREATE TABLE event_links (
  source_ref TEXT NOT NULL REFERENCES events(event_ref),
  relation TEXT NOT NULL CHECK (relation IN ('caused_by','supersedes')),
  target_ref TEXT NOT NULL,
  resolved INTEGER NOT NULL CHECK (resolved IN (0,1)),
  PRIMARY KEY (source_ref, relation, target_ref)
) STRICT, WITHOUT ROWID;

CREATE TABLE checkpoints (
  checkpoint_id TEXT PRIMARY KEY REFERENCES objects(object_id),
  scope TEXT NOT NULL,
  policy_ref TEXT NOT NULL,
  authority_epoch TEXT NOT NULL,
  previous_checkpoint TEXT
) STRICT, WITHOUT ROWID;

CREATE TABLE checkpoint_schema_refs (
  checkpoint_id TEXT NOT NULL REFERENCES checkpoints(checkpoint_id),
  schema_ref TEXT NOT NULL,
  PRIMARY KEY (checkpoint_id, schema_ref)
) STRICT, WITHOUT ROWID;

CREATE TABLE checkpoint_frontier (
  checkpoint_id TEXT NOT NULL REFERENCES checkpoints(checkpoint_id),
  namespace TEXT NOT NULL,
  head_id TEXT NOT NULL,
  resolved INTEGER NOT NULL CHECK (resolved IN (0,1)),
  PRIMARY KEY (checkpoint_id, namespace, head_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE heads (
  namespace TEXT NOT NULL,
  commit_id TEXT NOT NULL REFERENCES commits(commit_id),
  PRIMARY KEY (namespace, commit_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE completeness_blockers (
  source_id TEXT NOT NULL,
  code TEXT NOT NULL CHECK (code IN (
    'missing_parent','missing_event_reference',
    'missing_checkpoint_head','missing_previous_checkpoint'
  )),
  field TEXT NOT NULL,
  missing_ref TEXT NOT NULL,
  PRIMARY KEY (source_id, code, field, missing_ref)
) STRICT, WITHOUT ROWID;

CREATE INDEX objects_namespace_idx ON objects(namespace, object_type, object_id);
CREATE INDEX objects_actor_idx ON objects(actor_key_id, object_id);
CREATE INDEX events_type_idx ON events(event_type, causal_batch, event_ref);
CREATE INDEX events_kind_idx ON events(kind, causal_batch, event_ref);
CREATE INDEX events_subject_idx ON events(subject, causal_batch, event_ref);
CREATE INDEX events_schema_idx ON events(schema_ref, causal_batch, event_ref);
CREATE INDEX events_order_idx ON events(causal_status, causal_batch, event_ref);
CREATE INDEX events_commit_idx ON events(commit_id, local_id);
CREATE INDEX event_tags_tag_idx ON event_tags(tag, event_ref);
CREATE INDEX event_links_target_idx ON event_links(target_ref, relation, source_ref);
CREATE INDEX parent_edges_parent_idx ON parent_edges(parent_id, child_id);
```

`objects.namespace` is a commit namespace or checkpoint scope. Missing targets
remain text with `resolved=0`; they deliberately have no target foreign key.
The index stores no payload, evidence body, signature, public key, private data,
trust root, or filesystem path.

`index_meta` has exactly these keys:

- `format=pact/sqlite-index/v1`;
- `schema_version=1`;
- `schema_digest`;
- `source_fingerprint`;
- `logical_digest`;
- `limits_contract=pact/resource-limits/phase2-v1`;
- all source and logical row counts;
- local completeness status.

The schema digest is SHA-256 of the checked-in exact DDL constant. The logical
digest streams each table in the DDL order, each row in primary-key order, and
each row as a canonical JSON array in DDL column order. Length framing and the
domain prefix `PACT-INDEX-LOGICAL-ROWS-V1\x00` prevent ambiguity. Metadata's
`logical_digest` value is empty during its own hash. Rebuild parity compares the
logical digest and normalized rows/results, not physical SQLite bytes.

On open, the package checks file size, application ID, user version, metadata,
exact required tables/indexes and no extra trigger/view, schema digest,
`PRAGMA quick_check`, `PRAGMA foreign_key_check`, counts, logical digest, and
source fingerprint. It classifies unsupported format/schema as incompatible,
physical or logical damage as corrupt, coverage mismatch as partial-build, and
source mismatch as stale.

The SQLite size gate reads the approved profile directly. An exact 2 GiB sparse
file continues into the real read-only SQLite and header-validation path; the
first excess is classified corrupt before the reader opens.

## Deterministic causal batches

The scheduler creates `start(commit)`, `event(ref)`, and `finish(commit)` nodes.
It adds:

1. `start(C) -> event(E)` and `event(E) -> finish(C)` for every event in C;
2. `finish(P) -> start(C)` for every present parent P of C;
3. `event(T) -> event(S)` when S has a resolved `caused_by` target T.

`supersedes` remains indexed and filterable, but does not constrain causal
order. It is an assertion whose meaning belongs to projection policy after
Phase 2. Missing targets add blockers and no invented edge. Present
cross-namespace parents and any remaining parent/`caused_by` cycle are invalid.

Iterative Kahn removal takes the whole current zero-indegree frontier, sorts it
by node kind and immutable ID for deterministic processing, enforces the width
and edge limits, and records the removal batch on event nodes. Synthetic-only
batches may create numeric gaps. Filtering never recomputes batches.

A batch is an antichain of the known graph. A lower batch number alone does not
claim a path to every higher-batch event. Output includes direct predecessors
and this exact order description:

```json
{
  "kind": "known_causal_batches/v1",
  "tie_breaker": "immutable_reference",
  "tie_breaker_is_semantic": false,
  "observed_at_used": false,
  "global_completeness": "unknown"
}
```

Events with a missing parent or `caused_by` dependency appear in an `unresolved`
group sorted by event ref. A missing `supersedes` target makes the replica
incomplete but does not move an otherwise ordered event into that group.
Log/query may show all such events only with the replica blockers. Strict verify
and checkpoints still fail.

Pagination may split a batch. Every item retains the same `causal_batch`; each
page batch sets `complete_in_page:false` when the transport boundary splits it.
This avoids raising the result cap merely to fit a hostile wide batch and does
not invent order between pages.

## Cursor contract

The cursor is unpadded base64url of canonical JSON with exact keys:

```json
{
  "after_group": "ordered|unresolved",
  "after_batch": 12,
  "after_ref": "pact:event:...",
  "checksum": "sha256:...",
  "format": "pact/query-cursor/v1",
  "logical_digest": "sha256:...",
  "query_digest": "sha256:...",
  "schema_version": 1,
  "source_fingerprint": "sha256:..."
}
```

`after_batch` is null for the unresolved group. The continuation tuple is group
rank, batch, immutable ref; it is a transport key only. The query digest hashes
canonical JSON containing command/view, all sorted-unique filter arrays, limit,
and ordering contract. Cursor input is excluded.

`checksum` is SHA-256 of `PACT-QUERY-CURSOR-V1\x00` plus the canonical cursor
object with `checksum` omitted. It detects accidental damage; it is not a
signature or authorization check.

Decode checks encoded and decoded size before allocation, base64 grammar,
canonical JSON, exact keys, field grammar, supported version, current source and
logical digests, query digest, and existence of the continuation row. A cursor
is not an authorization token and needs no secret MAC. It survives process
restart and deterministic same-source rebuild; a source, schema, query, view, or
limit change fails explicitly.

Cursor codes are `cursor_invalid`, `cursor_query_mismatch`, and `cursor_stale`.
Malformed and query-mismatch cursors use exit 2; stale cursors use exit 9. Raw
cursor text never appears in diagnostics.

## Rebuild and publication

Canonical publication keeps using the existing exclusive store lock. Rebuild
holds that exclusive lock for its full scan, build, and publication. Status,
log, and query add a shared lock on the same lock file and hold it from source
validation through SQL selection and canonical result resolution. A commit or
checkpoint may finish preflight while a read/rebuild runs, but its canonical
publication waits for the exclusive lock; a post-rebuild commit therefore makes
the index explicitly stale rather than racing the published snapshot.

Rebuild performs:

1. Validate real, non-symlink `.pact/index` and canonical paths.
2. Run the shared bounded canonical scan. Refuse hard validation or limit
   failures; keep safe sorted blockers for missing dependencies.
3. Create `.pact/index/.build-<random>.sqlite3` mode `0600` on the same
   filesystem. Use `database/sql`, `foreign_keys=ON`, rollback journal,
   `synchronous=FULL`, one writer connection, and one transaction. With
   `modernc.org/sqlite v1.57.0`, the fixed writer DSN uses
   `_foreign_keys=1&_journal_mode=DELETE&_synchronous=FULL&_busy_timeout=5000`.
4. Insert rows with parameterized prepared statements in stable order. Compute
   and store schema/source/logical digests and counts.
5. Commit, close every handle, reopen with the fixed
   `mode=ro&_query_only=1&_defensive=1&_foreign_keys=1&_busy_timeout=5000`
   settings, and run the complete normal validation path plus fixed
   representative queries.
6. Close, sync the database file, enforce the 2 GiB bound, and validate that no
   journal or WAL sidecar remains.
7. Atomically rename the closed temporary file over `pact-v1.sqlite3`, then
   sync `.pact/index/`. This rename is the only publication point.
8. Reopen and validate the published file, then run a second bounded canonical
   scan and compare its verified source fingerprint. The exclusive store lock
   spans both scans and publication; byte-level failure and interruption tests
   prove canonical, trust, ref, and checkpoint bytes stay unchanged. Return
   current/partial coverage only after the second scan passes.

A crash or error before rename leaves the prior destination untouched. A first
build leaves no usable index. A crash after a fully synced temp file is renamed
exposes the validated new file or, if the directory entry was not durable, the
prior file or none. A platform that cannot atomically replace an existing file
returns a publication error and leaves the old file; Phase 2 does not delete it
first or add a compatibility path.

Temporary `.build-*` files are never selected. Explicit rebuild may remove only
exact, regular, non-symlink temp names after it holds the exclusive lock. Log,
query, and status never clean them.

## Narrow package boundary

`internal/index` is the only package that imports the SQLite driver or contains
SQL. Its surface is:

```go
type Manager struct { /* store plus fixed contracts */ }

func New(st *store.Store) *Manager
func (m *Manager) Status(ctx context.Context) (Status, error)
func (m *Manager) Rebuild(ctx context.Context) (RebuildResult, error)
func (m *Manager) Log(ctx context.Context, q LogRequest) (QueryPage, error)
func (m *Manager) Query(ctx context.Context, q QueryRequest) (QueryPage, error)

func Project(ctx context.Context, scan ledger.ScanResult) (Snapshot, error)
func LogicalDigest(ctx context.Context, snapshot Snapshot) (string, error)
```

No database handle or SQL leaves the package. Dynamic WHERE clauses come only
from a fixed field-to-clause table; caller values are parameters. Context flows
through scans, projection, deterministic chunk/merge sorting, logical-row
encoding, transactions, and queries. Cancellation returns unchanged through
the locked rebuild, status, log, and query operations.

`internal/index` consumes the immutable projection returned by `ledger.Scan`
and calls ledger canonical result resolution. Ledger never imports index. The
CLI maps these domain results to JSON; it never formats raw SQL rows.

## Error and exit contract

Stable detail codes are:

- `index_missing`, `index_stale`, `index_corrupt`, `index_incompatible`,
  `index_partial_build`;
- `source_invalid`, `source_changed`, `resource_limit`;
- `cursor_invalid`, `cursor_query_mismatch`, `cursor_stale`;
- `index_publication_failed`.

Usage/filter/cursor input failures use exit 2. Canonical integrity, structure,
authenticity, or DAG failure keeps exit 4. Missing/stale/unusable derived state,
resource limits, missing dependencies, and stale cursors use exit 9. Unexpected
I/O or driver failures use exit 10. Index corruption is described by its detail
code but does not relabel canonical history as corrupt.

## Test and product gate

The shared scenario matrix is recorded in
`docs/plans/phase-2/omakase/result.md`. Implementation uses strict focused RED,
the smallest GREEN change, and refactor for each behavior. Required layers are:

- unit tests for fingerprints, exact DDL, graph batches, limits, filters,
  cursors, states, and errors;
- integration tests with real files, SQLite, and Ed25519 objects for rebuild,
  corruption, failure boundaries, partial replicas, concurrency, and parity;
- compiled E2E tests for every CLI shape, restart, deletion/rebuild, output
  safety, exact concurrent success contracts, interrupted recovery with strict
  verification, and unchanged canonical/trust/ref state;
- sparse-file integration tests proving exact 2 GiB reaches SQLite validation
  while 2 GiB plus one byte never opens;
- cold `scripts/check`, full race, vet, scratch build, zero-issue linter, module
  verification, and the full repository dogfood flow.

The dogfood event must match the intersection of namespace `org/2389/pact`,
type `build.capability.verified`, subject `pact/mvp/phase-0-1`, actor
`ed25519:sha256:bdf05ec0cd70191a1343dd4db0cd0c7b094794238a73e64bdcca06081053dd3a`,
and tag `dogfood` before and after recoverable index removal. The existing
commit, checkpoint, trust bytes, heads, and hashes must remain unchanged until
all gates pass and the one signed Phase 2 event is intentionally appended.

The implemented gate passed before and after the intentional append. The
existing dogfood intersection returned only `#phase-0-1-mvp` with source
fingerprint
`sha256:c057555e08261de323603c053d2b3813b7444b644acccfad530ddd5d6294e16a`
and logical digest
`sha256:b673e993507589aa01c2de6a8094fce0a56521c1350ce05d422e87b02666e4e5`
across index removal and rebuild. The new `#phase-2-mvp` event proved stale
refusal, direct canonical inspection, explicit rebuild, exact query, and strict
verification. The chained final checkpoint left a current complete index after
rebuild. Exact commands, IDs, hashes, and counts are in
`docs/status/phase-2-dogfood.md`.

## Known weakness and recommendation

Every indexed query hashes the bounded canonical object set and verifies
selected canonical commits. That adds O(canonical bytes) I/O, but it keeps stale
detection and row authority clear without a second mutable epoch. Rebuild takes
an exclusive lock for a full scan, so large stores pause writers and queries.
Severity is Medium for the local Phase 2 scope.

Ship this fixed-snapshot design. Do not add incremental updates, a generation
catalog, automatic fallback, or a freshness cache until real use proves that the
simple full rebuild is inadequate and a new contract is approved.
