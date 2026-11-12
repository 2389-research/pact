<!-- ABOUTME: Records combined verification of the validated profile and evidence resolver. -->
<!-- ABOUTME: Binds prerequisite acceptance to frozen source and retained real test results. -->

# Validation and evidence integration

Frozen combined revision: 2d688b5e75d58de421808c57f70106d196e4599e.
Validation source736a1be/proofc0e3624 and evidence source41c1ae6/proof748b117
each passed independent task review without remaining findings. Evidence changes
were integrated via ordinary cherry-picks508925f and1a15797; no conflict, hook
bypass, main merge, deployment or release occurred.

The parent ran these complete commands concurrently on the unchanged combined
source and inspected full failure markers and package results:

| Command after env -u GOROOT mise exec -- | Exit | Evidence |
|---|---|---|
| scripts/check | 0 | evidence/integration-2d688b5-check.log |
| go test -race ./... | 0 | evidence/integration-2d688b5-race.log |
| golangci-lint run (fresh isolated cache) | 0, zero issues | evidence/integration-2d688b5-lint.log |

Canonical covers all Go packages, vet/build, compiled CLI and 18 reference tests.
No tracked source diff remained after the runs. The main checkout still resolves
to its own directory. The evidence tests and their isolated Git fixtures do not
change enclosing Git configuration, refs or index.

Both artifact manifests verify. The two compressed CLI RED originals decompress
to the exact SHA256 values in spns-report.md; readable hook-normalized versions
remain labeled separately. No raw output was reconstructed.

M1 from khqq-review.md is explicitly resolved by validation's regression at
736a1be and independently confirmed in 3ezv-review.md. The earlier record remains
available at its named Git/Pact cut.

This accepts only the prerequisite validation/resolver interfaces. Recorder,
work-state projection, brief promotion, lifecycle and fresh-context adoption
remain open. Trusted local metadata and unqualified older Git/platform limits
remain as documented in spns-report.md.
