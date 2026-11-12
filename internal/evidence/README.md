<!-- ABOUTME: Documents the bounded evidence resolver and its CLI observation contract. -->
<!-- ABOUTME: Defines local scope, immutable byte semantics, statuses, and limits for callers. -->

# Local evidence checks

`pact evidence check --repo PATH --event-ref REF --json` selects all evidence on
one explicit event through `ledger.Show`. It neither appends events nor rebuilds
the index. A missing or stale index does not prevent this canonical read.

The result contains `event_ref`, a fresh UTC `checked_at`, and an `assessments`
array in evidence order. Each assessment contains `ref`, the author's `digest`,
`actual_digest`, `bytes`, and `status`. This is a current byte observation.
The author's timestamp and claim, signature verification, and supported acceptance
remain separate. Exit zero means the selected assessments were produced, including
changed/unavailable/unsupported states; it does not mean every item matched.
Overflow refuses the operation with exit 2 and no partial assessments. Process or
read failures use the existing unexpected-error exit.

## Library interface

```go
Resolve(context.Context, Request) (Assessment, error)
ResolveAll(context.Context, []Request) ([]Assessment, error)
```

`Request` has `Repo`, `Ref`, `Digest` strings and `MaxBytes int64`.
Zero `MaxBytes` selects `MaxItemBytes` (16 MiB). Positive values may narrow that
limit; negative values or values above it fail with `ErrLimit`. `ResolveAll`
owns the 64 MiB aggregate limit and returns no partial assessments on error.
Every selection consumes its bytes, including repeated references. An empty
selection returns an empty non-nil slice. No prefix digest is reported as a
complete digest.

- `matching`: exact bytes hash to the claimed SHA-256 digest.
- `changed`: exact bytes have a different digest.
- `unavailable`: the local file or Git object cannot be obtained.
- `unsupported`: the locator or selected object falls outside this resolver.
- `unchecked`: bytes were hashed but the library caller supplied no digest.
  Canonical signed evidence always supplies a digest.

`errors.Is(err, ErrLimit)` identifies a byte refusal. Context cancellation keeps
its standard error identity. `ProcessError` identifies failed Git startup or
execution; it is distinct from Git's successful `missing` object response.
Raw subprocess diagnostics and evidence contents never become error messages.

## Supported locators and scope

Working files use repository-relative slash paths. Git evidence uses
`git:<full-native-object-id>:<repository-relative-path>`: 40 lowercase hex
characters in SHA-1 repositories, 64 in SHA-256 repositories. Mutable revision
names and abbreviations are unsupported. Git locators may select a commit or
root tree containing the path. Only the resulting blob is read.

Absolute paths (including historical absolute locators), traversal, backslashes,
control characters, and locator syntax containing `@`, `?`, or `#` are refused.
Colons in ordinary file locators are refused as possible schemes; colons in the
path portion of Git locators are literal. Malformed/unsafe locator strings are
withheld from the returned `ref`, so URL credentials cannot be reflected.

Local file reads use `os.Root` confinement and reject all symlink components and
nonregular files. Git reads require `.git` in the selected scope rather than
searching ancestors. Normal linked worktrees are supported. Git symlink blobs
contain the link text: Git never follows them to a filesystem target here.
The repository's Git metadata and locally installed Git are trusted, including
normal worktree metadata and configured object alternates. This is not a sandbox
against another process that can replace owned files or repository metadata.

Git runs directly with argument arrays, no shell or pager. All inherited `GIT_*`
variables are removed; system/global config is disabled for these subprocesses.
Lazy fetches, replacement objects, external diff/text conversion, and transport
protocols are disabled. `cat-file --batch-check` supplies blob identity/type/size;
`show` streams the exact blob to the bounded digest writer. The real partial-clone
test exercises the no-fetch condition. Only synthetic public evidence is used in tests; no real credentials, private
operator keys, or external remote repositories are used.

The implementation uses documented Git `--show-object-format` and lazy-fetch
controls. Verification on this machine uses Git 2.54.0 (Apple Git-157); older Git
versions have not been qualified. `protocol.allow=never` also denies transport
if Git does not honor the lazy-fetch environment control.
