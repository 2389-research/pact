<!-- ABOUTME: Records the missing release-test prerequisite and its bounded workflow correction. -->
<!-- ABOUTME: Preserves the failed tag and names the replacement release without changing product behavior. -->

# Release v1.1.1

The v1.1.0 tag push succeeded at18378c3.
Release workflow43224759554 failed before creating a GitHub release.
Its Go suite could not find uv in PATH.
TestProfileCorpusAgainstReferenceJSONSchema invokes the existing Python JSON Schema oracle through uv.
The standalone conformance job installed uv, but the Go test and release jobs did not.

The controller reproduced the exact missing-executable failure with a real compiled test and restricted PATH.
Restoring the installed uv executable made that same oracle pass.
The fix adds the existing pinned astral-sh/setup-uv@v10.0.1 action to both affected jobs.
It removes no test, changes no timeout, and changes no permission.
The product Go, schema, wire, and guide bytes remain unchanged.
The canonical check and GoReleaser configuration check passed after the correction.
The controller directly reviewed the six workflow lines against the existing conformance setup.
Normal commit hooks validate both workflow files without bypasses.
Retained failure and RED/GREEN/canonical logs live under evidence/release-*.log.gz.

Preserve v1.1.0 as the failed release tag.
The v1.1.1 tag and release are published at8337d3e74e42bd2ae9a13af8aeb7431a1e9d16bd.
The annotated tag object is1ea2ef28fc63ade3c5c301b227ea54043f311230.
Release: https://github.com/2389-research/pact/releases/tag/v1.1.1.
Release ID:442976706; workflow run:43226090904.
The real Linux release job passed, including uv setup and the full Go suite.
Four macOS/Linux archives and checksums.txt are published.
All four downloaded archive hashes match the published manifest.
The downloaded macOS arm64 binary passed help and exact quickstart-byte checks.
Its build metadata names the tagged commit and vcs.modified=false.
The other three platform binaries were not executed locally.

The release notes include the development features since v1.0.2.
Their exact body was freshly read back after the update.
Retained manifest: evidence/release-v1.1.1-checksums.txt.
Manifest SHA-256:c6aec251d1e2f53d7c09e1df11c7dda17bfa856781e35c32346689016ce2b0b0.

PACT preserves the failed attempt in2d075455#release-failed.
PACT records verified publication ina176c12d#release-published, caused by that failed attempt.
Both records passed fresh readback, integrity, authenticity, and local-root authorization.
Strict verification passed with a current index.
The GitHub checksum reference was downloaded and checked explicitly.
The fixed evidence resolver does not claim to resolve arbitrary HTTPS references.

The original push/release authorization covers this necessary prerequisite correction.
No main merge or service deployment is included.
