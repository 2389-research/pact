<!-- ABOUTME: States PACT's local filesystem boundary for store and external-key operations. -->
<!-- ABOUTME: Records the residual local-process risk and required operator controls. -->

# Local filesystem boundary

PACT protects against accidental or static symlink placement in an initialized
store and when choosing an external key path. It resolves the requested
repository or existing key-directory symlinks, then refuses symlinks inside the
store layout.

PACT does not defend against a concurrent local process that already has
permission to replace repository or key-directory path components during an
operation. That process can already alter local trust and key storage, so this
implementation does not claim race-free no-follow filesystem security.

Required operator control: owned directories, restrictive permissions.
