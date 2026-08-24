<!-- ABOUTME: Records the bounded Phase 2 storage and query architecture comparison. -->
<!-- ABOUTME: Explains why one atomically replaced SQLite snapshot won the design gate. -->

# Phase 2 Omakase result

## Gate

This was a design-only Omakase. The Phase 2 goal forbids production code before
Doctor Biz approves the exact contract, so the normal implementation judge could
not honestly score code or passing scenarios. The three agents produced complete
designs against one scenario contract; the controller added the smallest viable
fourth design and ran a design-feasibility gate instead.

All variants preserve these hard rules:

- canonical object bytes are the sole ledger history;
- the index is derived, disposable, and never used for object admission;
- queries never rebuild as a side effect;
- ordering uses signed dependency edges, never timestamps;
- local dependency closure never claims global completeness;
- one bounded ledger scan supplies verification, heads, checkpoint admission,
  and indexing;
- Phase 2 adds no setup or Phase 3 behavior.

## Variants

| Variant | Publication | Recovery rule | Main cost | Result |
|---|---|---|---|---|
| Fixed snapshot | Build, validate, sync, then replace `pact-v1.sqlite3` with one rename | Prior file remains until publication; explicit rebuild repairs any unusable state | Full rebuild | Selected |
| Numbered snapshots | Publish generation-named files and select the highest sequence | Refuse a damaged newest generation; retain and clean old files | Selection and cleanup states | Rejected |
| Database plus manifest | Publish a digest-named database, then atomically replace `manifest.json` | Manifest alone selects the active database | Two-file validation and full hashing | Rejected |
| Generation catalog | Publish immutable generations, then replace an active/previous catalog | Previous may serve only when it matches the current source fingerprint | Catalog, fallback, cleanup, and disk amplification | Rejected |

## Feasibility scorecard

Scores are integers from 1 to 5. They assess the written designs, not code that
does not yet exist.

| Criterion | Fixed snapshot | Numbered snapshots | DB + manifest | Generation catalog |
|---|---:|---:|---:|---:|
| Fitness for Phase 2 | 5 | 4 | 4 | 4 |
| Justified complexity | 5 | 3 | 3 | 2 |
| Readability | 5 | 4 | 4 | 3 |
| Robustness and bounds | 4 | 5 | 5 | 5 |
| Maintainability | 5 | 4 | 3 | 3 |
| **Total** | **24/25** | **20/25** | **19/25** | **17/25** |

No design has a critical feasibility flaw. The fixed snapshot wins because the
extra selector and fallback states do not buy a Phase 2 requirement. Atomic
replacement already guarantees that a failed build leaves the prior valid file
or no usable index. A stale prior generation can never be served, so retaining
it as an automatic fallback has little value.

The selected design keeps two good ideas from the other proposals:

1. It records a deterministic logical-row digest, so rebuild parity does not
   depend on SQLite page layout.
2. It resolves and verifies every selected result against its canonical commit
   before output, so a derived row cannot override immutable bytes.

## Shared scenario contract

Every implementation task must preserve this matrix. Unit, real-SQLite
integration, and compiled end-to-end tests divide the work; no E2E mock mode is
allowed.

| Scenario | Required proof |
|---|---|
| Fork and explicit merge | Both branches and the merge remain visible; heads and parent rows agree with canonical verification. |
| Timestamp reversal | Reversing or equalizing `observed_at` leaves causal batches and cursors unchanged. |
| Parent namespace | A present cross-namespace parent is invalid and prevents index publication. |
| Cross-namespace event reference | A stable `caused_by` reference supplies a known causal edge without changing commit parent rules; `supersedes` stays non-causal policy input. |
| Missing dependencies | Stable blocker codes distinguish parent, event, checkpoint head, and previous-checkpoint gaps. |
| Partial checkpoint | Any completeness blocker prevents official checkpoint creation before bytes are signed or stored. |
| First build and status | A real SQLite database validates as current and reports local coverage. |
| Delete and rebuild | Moving the index aside and rebuilding yields identical logical rows and normalized query results. |
| Stale/corrupt/incompatible index | Log/query refuse; explicit rebuild recovers solely from canonical bytes. |
| Interrupted rebuild | Failures before publication leave the prior file; publication exposes only a fully validated replacement. |
| Repeated rebuild and restart | Logical digest, causal batches, filters, pages, and same-source cursors remain stable. |
| Filters and empty results | OR within one repeated field, AND across fields, namespace segment boundaries, and empty success are exact. |
| Pagination and cursor misuse | No duplicates or omissions; split causal batches retain the same batch number; malformed, changed-query, and stale cursors fail by code. |
| Exact limits | Every exact boundary passes; one over returns a typed bounded result and publishes nothing. |
| Hostile graph shapes | Deep, wide, and high-edge fixtures stop iteratively without panic, recursion, or unbounded diagnostics. |
| Source immutability | Object hashes, heads, trust bytes, refs, and prior checkpoint bytes match before and after every read/rebuild path. |
| Output safety | Database, cursors, stdout, stderr, and diagnostics contain no private key or unsafe matched value. |
| Rebuild/query race | Shared readers and the exclusive rebuild observe one source/index state under the race detector and real processes. |
| Version mismatch | Application ID, user version, schema digest, and required schema changes each produce explicit incompatibility. |

## Outcome

The exact selected contract is in `docs/plans/phase-2/design.md`. Production
implementation remains blocked until Doctor Biz approves it.
