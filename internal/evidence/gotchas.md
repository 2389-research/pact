# Evidence resolver gotchas

- 2026-11-11: A 40-hex revision can be an abbreviated SHA-256 Git object ID.
  Validate length against the selected repository's native object format;
  syntax alone does not prove an immutable full identity. The real SHA-256
  repository test covers this trap.
- 2026-11-11: Git subprocess working directories do not override inherited
  `GIT_DIR`, object-directory, or config selectors. Both resolver and fixture
  processes strip inherited `GIT_*`; the resolver then sets its fixed local
  controls. Never remove this filtering when adding Git reads.
