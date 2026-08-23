# PACT Tool MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a usable Go `pact` CLI that can create, append, inspect, verify, and checkpoint a local signed ledger.

**Architecture:** Preserve the bundled Python implementation as the behavioral oracle. Put strict JSON, cryptographic identity, immutable storage, and ledger operations in small internal Go packages; keep `cmd/pact` as an argument and output adapter. The canonical object store remains the only source of history, while heads are computed from object bytes.

**Tech Stack:** Go 1.26, Go standard library, `golang.org/x/text/unicode/norm`, shell check script, Python reference suite through `uv`.

## Global Constraints

- Canonical signed bytes use `pact-json-v1`: UTF-8 without BOM, NFC strings and keys, lexicographically sorted keys, compact JSON, no floats, and integers in `[-9007199254740991, 9007199254740991]`.
- Object IDs are `sha256:` plus 64 lowercase hexadecimal characters.
- Key IDs are `ed25519:sha256:` plus the SHA-256 digest of the raw 32-byte Ed25519 public key.
- Ed25519 signs the raw 32-byte SHA-256 body digest, not its text form.
- Canonical objects live at `.pact/objects/sha256/<first-two-hex>/<remaining-hex>.json` and existing bytes are never overwritten.
- Private keys remain outside the project root and key files use owner-only permissions.
- Integrity, authenticity, and root authorization stay separate in results.
- The first usable cut supports `init`, `keygen`, `trust-add`, `hash`, `commit`, `heads`, `show`, `verify`, and `checkpoint` with `--json` output.
- Production code requires a focused failing test first, an observed expected failure, then the smallest passing implementation.
- Hand-written source files that support comments start with two `ABOUTME:` comment lines.

---

### Task 1: Canonical JSON and cryptographic identity

**Files:**
- Create: `go.mod`
- Create: `internal/canonical/json.go`
- Create: `internal/canonical/json_test.go`
- Create: `internal/identity/ed25519.go`
- Create: `internal/identity/ed25519_test.go`
- Create: `internal/conformance/vectors_test.go`

**Interfaces:**
- Produces: `canonical.Parse(raw []byte) (any, error)`, `canonical.Marshal(value any) ([]byte, error)`, and `canonical.Digest(raw []byte) string`.
- Produces: `identity.KeyID(public ed25519.PublicKey) (string, error)`, `identity.SignBody(body any, private ed25519.PrivateKey) (bodyDigest string, signature []byte, err error)`, and `identity.VerifyBody(body any, bodyDigest string, public ed25519.PublicKey, signature []byte) error`.

- [ ] **Step 1: Write canonicalization tests before implementation**

  Cover every valid and invalid case in `docs/pact-ledger/examples/canonicalization-vectors.json`, plus trailing JSON, invalid UTF-8, normalized nested strings, empty values, and both safe-integer boundaries. Assert the exact canonical bytes and digests.

- [ ] **Step 2: Run the focused tests and record RED**

  Run: `env -u GOROOT mise exec -- go test ./internal/canonical ./internal/conformance`

  Expected: compilation fails because `canonical.Parse`, `canonical.Marshal`, and `canonical.Digest` do not exist.

- [ ] **Step 3: Implement strict parsing and canonical encoding**

  Use `json.Decoder.Token` to build values while tracking raw and NFC-normalized keys so duplicate keys cannot be lost in a map. Decode numbers as `json.Number`, reject decimal/exponent forms, check the safe range, reject trailing tokens and BOMs, recursively normalize strings and keys, and emit compact sorted-key JSON with unescaped UTF-8.

- [ ] **Step 4: Run canonical tests and record GREEN**

  Run: `env -u GOROOT mise exec -- go test ./internal/canonical ./internal/conformance`

  Expected: PASS with no warnings.

- [ ] **Step 5: Write Ed25519 tests before implementation**

  Use the public RFC 8032 test seed `9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60` only as deterministic fixture data. Assert exact public key, PACT key ID, canonical body digest, signature over the raw digest bytes, and failures for altered bodies, digests, keys, and signatures.

- [ ] **Step 6: Run identity tests and record RED**

  Run: `env -u GOROOT mise exec -- go test ./internal/identity`

  Expected: compilation fails because the identity functions do not exist.

- [ ] **Step 7: Implement identity helpers and record GREEN**

  Validate Ed25519 lengths, call `canonical.Marshal`, hash those bytes, sign or verify the raw digest, and use unpadded base64url only at object boundaries.

  Run: `env -u GOROOT mise exec -- go test ./internal/...`

  Expected: PASS with no warnings.

- [ ] **Step 8: Commit**

  Commit: `feat: add canonical JSON and identity core`

### Task 2: Immutable store, external keys, and bootstrap CLI

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`
- Create: `internal/identity/keyfile.go`
- Create: `internal/identity/keyfile_test.go`
- Create: `internal/ledger/trust.go`
- Create: `internal/ledger/trust_test.go`
- Create: `cmd/pact/main.go`
- Create: `cmd/pact/app.go`
- Create: `cmd/pact/app_test.go`

**Interfaces:**
- Consumes: Task 1 canonical and identity functions.
- Produces: `store.Init(repo, namespace string, now time.Time) (*Store, error)`, `store.Open(repo string) (*Store, error)`, `(*Store).PutCanonical(value any) (objectID string, created bool, err error)`, and `(*Store).Get(objectID string) ([]byte, error)`.
- Produces: `identity.GenerateKeyFile(path, actor string, now time.Time) (*KeyFile, error)` and `identity.LoadKeyFile(path string, requirePrivate bool) (*KeyFile, error)`.
- Produces: `ledger.AddRoot(st *store.Store, key *identity.KeyFile, now time.Time) (created bool, err error)` and `ledger.Roots(st *store.Store) (map[string]Root, error)`.
- Produces CLI commands `init`, `keygen`, `trust-add`, and `hash` through `run(args []string, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write failing store tests**

  Cover exact layout, refusal to reinitialize, idempotent identical writes, collision/different-byte refusal, post-write hash verification, no canonical partial after an injected pre-link failure, and project-root rejection for key output.

- [ ] **Step 2: Run store tests and record RED**

  Run: `env -u GOROOT mise exec -- go test ./internal/store ./internal/identity`

  Expected: compilation fails on missing store and key-file APIs.

- [ ] **Step 3: Implement store and key files**

  Write temp files under `.pact/tmp`, sync file contents, hard-link into the canonical path with no overwrite, sync the containing directory when supported, reread and hash before success, and remove only the task's temporary file on failure. Create key files with `0600`, refuse overwrite, and verify private/public/key-ID agreement on load.

- [ ] **Step 4: Run focused tests and record GREEN**

  Run: `env -u GOROOT mise exec -- go test ./internal/store ./internal/identity`

  Expected: PASS with no warnings.

- [ ] **Step 5: Write failing trust and CLI tests**

  Exercise command argument validation and JSON fields against `references/cli-contract.md`; assert diagnostics never contain private bytes. Test root addition idempotence and conflicting public bytes as a hard failure.

- [ ] **Step 6: Implement bootstrap commands and record GREEN**

  Use `flag.FlagSet` per subcommand. Keep command handlers thin and return structured errors with the CLI contract's exit codes.

  Run: `env -u GOROOT mise exec -- go test ./cmd/pact ./internal/...`

  Expected: PASS with no warnings.

- [ ] **Step 7: Commit**

  Commit: `feat: add immutable store and bootstrap commands`

### Task 3: Signed commits, heads, inspection, and verification

**Files:**
- Create: `internal/ledger/event.go`
- Create: `internal/ledger/event_test.go`
- Create: `internal/ledger/commit.go`
- Create: `internal/ledger/commit_test.go`
- Create: `internal/ledger/verify.go`
- Create: `internal/ledger/verify_test.go`
- Modify: `cmd/pact/app.go`
- Modify: `cmd/pact/app_test.go`

**Interfaces:**
- Consumes: Tasks 1–2 packages.
- Produces normalized event-batch and signed-commit types matching `schemas/event*.json` and `schemas/commit.schema.json`.
- Produces: `ledger.Commit(st *store.Store, key *identity.KeyFile, batch EventBatch, options CommitOptions) (CommitResult, error)`.
- Produces: `ledger.Heads(st *store.Store, namespacePrefix string) (map[string][]string, error)` and `ledger.Show(st *store.Store, identifier string) (ShowResult, error)`.
- Produces: `ledger.Verify(st *store.Store, strict bool) (VerifyResult, error)`, where `VerifyResult` exposes separate integrity, authenticity, DAG/reference, and authorization counts and errors.
- Adds CLI commands `commit`, `heads`, `show`, and `verify`.

- [ ] **Step 1: Write failing event and commit tests**

  Cover required fields, exact allowed keys, NFC normalization, sorted unique tags/causal refs/supersedes, sorted events, duplicate local IDs, malformed refs and namespaces, secret-like payload refusal, explicit forks, automatic all-head parents, and immutable correction bytes.

- [ ] **Step 2: Run commit tests and record RED**

  Run: `env -u GOROOT mise exec -- go test ./internal/ledger -run 'Event|Commit|Head'`

  Expected: compilation fails on missing event and commit APIs.

- [ ] **Step 3: Implement normalized signed commits and head calculation**

  Match the Python builder's stored form. Persist `local:` caused-by references inside the signed body and expand them only in command results. Compute heads by scanning authentic commits in the exact namespace and removing every referenced parent.

- [ ] **Step 4: Run commit tests and record GREEN**

  Run: `env -u GOROOT mise exec -- go test ./internal/ledger -run 'Event|Commit|Head'`

  Expected: PASS with no warnings.

- [ ] **Step 5: Write failing verification and CLI tests**

  Cover canonical-byte mismatch, path/object digest mismatch, body digest mismatch, signature substitution, same-namespace parent enforcement, missing parents, event lookup, trusted-root authorization, visible unauthorized history, strict missing refs, and clean verification after deleting all derived refs or caches.

- [ ] **Step 6: Implement layered verification and commands**

  Never collapse results to one `valid` flag. Verification must parse and check exact stored bytes before structure, signature, DAG, references, and trust. `show` accepts object IDs and stable event refs without resolving evidence.

- [ ] **Step 7: Run ledger and CLI tests and record GREEN**

  Run: `env -u GOROOT mise exec -- go test ./internal/ledger ./cmd/pact`

  Expected: PASS with no warnings.

- [ ] **Step 8: Commit**

  Commit: `feat: append and verify signed commits`

### Task 4: Signed checkpoints and full product gate

**Files:**
- Create: `internal/ledger/checkpoint.go`
- Create: `internal/ledger/checkpoint_test.go`
- Create: `tests/e2e/cli_test.go`
- Create: `scripts/check`
- Create: `README.md`
- Modify: `cmd/pact/app.go`
- Modify: `cmd/pact/app_test.go`

**Interfaces:**
- Consumes: Tasks 1–3 packages and computed heads.
- Produces signed checkpoint objects matching `schemas/checkpoint.schema.json` and the `checkpoint` CLI command.
- Produces: `ledger.Checkpoint(st *store.Store, key *identity.KeyFile, options CheckpointOptions) (CheckpointResult, error)`.
- Produces the canonical repository check at `scripts/check`.

- [ ] **Step 1: Write failing checkpoint tests**

  Cover scope selection, sorted frontier namespaces and heads, schema refs, previous checkpoint validation, trusted-root authorization, refusal on incomplete or invalid reachable history, and stable signed inspection.

- [ ] **Step 2: Run checkpoint tests and record RED**

  Run: `env -u GOROOT mise exec -- go test ./internal/ledger -run Checkpoint`

  Expected: compilation fails because checkpoint APIs do not exist.

- [ ] **Step 3: Implement checkpoints and record GREEN**

  Select every exact namespace equal to scope or below `scope/`, compute its current heads, require at least one frontier entry, verify referenced commits before signing, sort and deduplicate schema refs, and store the signed object through the immutable store.

  Run: `env -u GOROOT mise exec -- go test ./internal/ledger -run Checkpoint`

  Expected: PASS with no warnings.

- [ ] **Step 4: Write the failing end-to-end lifecycle test**

  Build `pact`; initialize a temporary project; generate an external key; trust it; append two commits; inspect heads and an event; verify strictly; create and inspect a checkpoint; confirm no private key exists below the project; and tamper with a copied store to prove verification fails.

- [ ] **Step 5: Run end-to-end RED, finish CLI, and run GREEN**

  Run: `env -u GOROOT mise exec -- go test ./tests/e2e -v`

  Expected before CLI completion: FAIL at the first missing behavior. Expected after minimal completion: PASS with no warnings.

- [ ] **Step 6: Add canonical repository check and operator README**

  `scripts/check` must fail on unformatted Go, then run `go vet ./...`, `go test ./...`, `go build ./cmd/pact`, and the Python reference suite through `uv`. `README.md` documents install, command lifecycle, the external-key rule, current MVP boundary, and the exact check command.

- [ ] **Step 7: Run the complete gate**

  Run: `env -u GOROOT mise exec -- ./scripts/check`

  Expected: format, vet, Go tests, Go build, and all 17 Python reference tests pass with no warnings.

- [ ] **Step 8: Run real usage in a fresh temporary directory**

  Execute the README lifecycle with the built binary, read back the signed commit and checkpoint, and run strict verification. Do not point the new tool at its own repository until this throwaway lifecycle passes.

- [ ] **Step 9: Commit**

  Commit: `feat: add signed checkpoints and product gate`
