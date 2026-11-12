Adds `pact brief` for exact-source development handoffs and cuts the operator guide from 42,710 to 12,284 bytes.

- Returns complete state, blockers, decisions, evidence results, and the captured frontier.
- Bounds output at 16 KiB by default. Oversized results fail with empty stdout and explicit recovery guidance.
- Keeps reads separate from index repair and preserves the existing acceptance policy.
- Runs the authored record and brief examples against real Git, keys, stores, and SQLite.

Canonical checks, full race tests, fresh lint, and independent task and integration reviews passed. One synthetic reader pair retained required state facts with fewer CLI calls and returned bytes; outer calls tied. Original payload inspection still uses `pact show`. Acceptance remains local, and global completeness remains unknown.
