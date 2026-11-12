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
Publish v1.1.1 after the workflow fix passes local verification and hooks.
Verify the real Linux release job, archives, checksums, and native binary afterward.
The original push/release authorization covers this necessary prerequisite correction.
No main merge or service deployment is included.
