# Generic PACT Walkthrough

This walkthrough creates a project ledger, records an observation and an
assertion atomically, creates a correction without editing history, and signs a
checkpoint.

Set the skill path:

```bash
PACT_SKILL=/path/to/pact-ledger
PACT="python3 $PACT_SKILL/scripts/pact.py"
PROJECT=/path/to/project
KEY=~/.config/pact/keys/operator.json
```

## 1. Initialize and bootstrap trust

```bash
$PACT init \
  --repo "$PROJECT" \
  --namespace org/example/project/widget

$PACT keygen \
  --actor human/operator \
  --out "$KEY"

$PACT trust-add \
  --repo "$PROJECT" \
  --key-file "$KEY"
```

The private key remains outside the project. `trust-add` is explicit local
bootstrap configuration, not a self-proving ledger event.

## 2. Capture and hash external evidence

```bash
mkdir -p /external/evidence
make test > /external/evidence/build-42.log 2>&1
$PACT hash /external/evidence/build-42.log
```

Put the resulting digest and locator into a copy of `event-batch.json`. The bytes
stay outside the ledger.

## 3. Append an atomic commit

```bash
$PACT commit \
  --repo "$PROJECT" \
  --key-file "$KEY" \
  --events event-batch.json
```

The output contains one commit ID and two stable event references. The command
observation and the verification assertion become visible together.

## 4. Inspect and verify

```bash
$PACT verify --repo "$PROJECT" --strict
$PACT heads --repo "$PROJECT"
$PACT log --repo "$PROJECT"
```

The log's display order is advisory. Parent links and event references carry
causal meaning.

## 5. Correct prior knowledge

Copy `correction-batch.json`, replacing its placeholder target with the actual
assertion event reference, then append it:

```bash
$PACT commit \
  --repo "$PROJECT" \
  --key-file "$KEY" \
  --events correction-batch.json
```

The original assertion remains in immutable history. A projection can now show
that it was later invalidated and why.

## 6. Sign an official frontier

Use the digest of a real external policy artifact:

```bash
POLICY=$($PACT hash /external/policies/current-policy.json --json \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])')

$PACT checkpoint \
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
$PACT sync-dir \
  --repo /path/to/replica-b \
  --from "$PROJECT"
```

The recipient verifies exact bytes before admitting missing objects. External
evidence is not copied automatically.
