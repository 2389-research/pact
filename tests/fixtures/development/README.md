<!-- ABOUTME: Explains the public synthetic development scenarios and their expected limits. -->
<!-- ABOUTME: Keeps fixture facts separate from semantic and projection answer rubrics. -->

# Public development corpus

These inputs are synthetic. They contain no consumer transcript, consumer source,
application output, or signing key. `source.fast-import` creates two fixed Git
commits with public synthetic source and proof text. Import it with Git's
`fast-import` in a fresh temporary repository; compiled CLI tests do so and
compare both committed IDs with input source identities. The fixture author is
synthetic and is not a signing identity. Tests generate fresh temporary keys.

`inputs.json` contains only record inputs. `expected.json` holds independent
schema/domain expectations and the work-state rubric. `histories.json` gives
later projection tests the sequence or fork to construct and the expected
unsupported/incomplete results. A `record-name#event-id` target in that file is
a fixture binding instruction, never a new PACT wire reference.

The positive loop starts with a passing check and an unresolved Important
finding. Its next action names that finding. On a second source, an approved
review explicitly resolves the original finding with correction source and
proof, then acceptance selects the new check and review. Before admission,
replace the correction's placeholder immutable review reference using the
binding instructions with the actual signed original review ref. Positive
supported acceptance applies to that complete bound history, not to the
standalone correction template with its unresolved placeholder.

Concurrent decisions start from the same frontier after the base record.
The unresolved-dependency history adds a valid-shaped missing caused-by ref.
The unknown-schema history replaces the selected review's schema identity.
These are declarative projection scenarios; Task 1 does not implement or claim
those decisions. The changed schema is an envelope override because callers
cannot override the fixed profile's schema through `InputEvent`.

The invalid `partial` outcome, missing revision, exit/outcome mismatch, and bad
ref fail JSON Schema. Duplicate finding IDs with different summaries pass
schema shape and must fail domain validation. All five are invalid caller inputs
for the later validator. Partial coverage is a separate, valid structured fact.

Run the oracle in the existing reference test environment:

```sh
uv run --with-requirements docs/pact-ledger/requirements-dev.txt python tests/fixtures/development/oracle.py .
```

Real-store and compiled CLI tests adapt positive fixtures to ordinary ledger
batches, inject operation/source, and select bundled exact schema digests. That
adapter is test code, not the forthcoming validation/recording API. The compiled
CLI also transports the invalid `partial` payload through raw `commit` and strict
verification unchanged: healthy ledger verification does not claim domain
validity. Evidence checks here compare exact synthetic Git blob bytes; they do
not implement the planned resolver or assess external evidence.

The correction and rejection acceptance events declare local causal links to
both selected proof events. Payload references alone do not establish observation.
The compiled read evaluation verifies the public corpus through the real projector.
Absent acceptance produces `not_recorded`; incompatible concurrent decisions
produce `unresolved`. Complete local closure is `locally_closed`, never a claim
of global completeness. The original profile-task report remains historical proof
of its narrower schema and transport checks.
