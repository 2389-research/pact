# Generic PACT Walkthrough

This walkthrough uses the bundled Python v0.1 conformance oracle. It creates a
project ledger, records an observation and an assertion atomically, creates a
correction without editing history, and signs a checkpoint. For the current Go
CLI, see the repository root README.

Set the skill, project, and writable work paths:

```bash
PACT_SKILL=/path/to/pact-ledger
PROJECT=/path/to/project
KEY=~/.config/pact/keys/operator.json
WORK_DIR="$(mktemp -d)"
EVIDENCE_DIR="$WORK_DIR/evidence"
EVENT_BATCH="$WORK_DIR/event-batch.json"
CORRECTION_BATCH="$WORK_DIR/correction-batch.json"
POLICY_FILE=/path/to/current-policy.json
REPLICA=/path/to/replica-b

pact_ref() {
  python3 "$PACT_SKILL/scripts/pact.py" "$@"
}
```

## 1. Initialize and bootstrap trust

```bash
pact_ref init \
  --repo "$PROJECT" \
  --namespace org/example/project/widget

pact_ref keygen \
  --actor human/operator \
  --out "$KEY"

pact_ref trust-add \
  --repo "$PROJECT" \
  --key-file "$KEY"
```

The private key remains outside the project. `trust-add` is explicit local
bootstrap configuration, not a self-proving ledger event.

## 2. Capture and hash external evidence

```bash
mkdir -p "$EVIDENCE_DIR"
(cd "$PROJECT" && make test) > "$EVIDENCE_DIR/build-42.log" 2>&1
pact_ref hash "$EVIDENCE_DIR/build-42.log"
cp "$PACT_SKILL/examples/event-batch.json" "$EVENT_BATCH"
```

Replace `make test` with the workflow command that creates the evidence. Put its
resulting digest and locator into `$EVENT_BATCH`. The evidence bytes stay outside
the ledger.

## 3. Append an atomic commit

```bash
pact_ref commit \
  --repo "$PROJECT" \
  --key-file "$KEY" \
  --events "$EVENT_BATCH"
```

The output contains one commit ID and confirms that it contains two events. The
command observation and the verification assertion become visible together.
Copy the stable `#interpret` event reference from the `log` output in the next
step; step 5 uses it to refer to the assertion.

## 4. Inspect and verify

```bash
pact_ref verify --repo "$PROJECT" --strict
pact_ref heads --repo "$PROJECT"
pact_ref log --repo "$PROJECT"
```

The log's display order is advisory. Parent links and event references carry
causal meaning.

## 5. Correct prior knowledge

Copy the checked-in correction batch, replace its placeholder target with the
actual assertion event reference, then append it:

```bash
cp "$PACT_SKILL/examples/correction-batch.json" "$CORRECTION_BATCH"
```

Edit `$CORRECTION_BATCH` so its `target_event_ref` and `supersedes` entry name
the actual `interpret` event reference from step 3. Then append it:

```bash
pact_ref commit \
  --repo "$PROJECT" \
  --key-file "$KEY" \
  --events "$CORRECTION_BATCH"
```

The original assertion remains in immutable history. A projector whose named
policy recognizes the correction can show that the assertion was later disputed
or invalidated and why.

## 6. Sign an official frontier

Use the digest of a real external policy artifact:

```bash
POLICY=$(pact_ref hash "$POLICY_FILE" --json \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])')

pact_ref checkpoint \
  --repo "$PROJECT" \
  --key-file "$KEY" \
  --scope org/example/project/widget \
  --policy-ref "$POLICY" \
  --authority-epoch org/example/authority-epoch/1 \
  --purpose release/example
```

The checkpoint signs an exact frontier and interpretation context. It does not
make every assertion inside the frontier true.

## 7. Replicate by immutable object union

```bash
pact_ref init \
  --repo "$REPLICA" \
  --namespace org/example/project/widget

pact_ref trust-add \
  --repo "$REPLICA" \
  --key-file "$KEY"

pact_ref sync-dir \
  --repo "$REPLICA" \
  --from "$PROJECT"
```

The recipient verifies exact bytes before admitting missing objects. External
evidence is not copied automatically.
