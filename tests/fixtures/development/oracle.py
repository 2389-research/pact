# ABOUTME: Checks public profile payload fixtures with the reference test environment's JSON Schema library.
# ABOUTME: Keeps schema validity separate from the semantic and projection expectations for later tasks.
import json
import pathlib
import sys

import jsonschema


def main():
    root = pathlib.Path(sys.argv[1])
    fixtures = root / "tests/fixtures/development"
    schemas = root / "internal/development/schema"
    inputs = json.loads((fixtures / "inputs.json").read_text())
    expected = json.loads((fixtures / "expected.json").read_text())
    names = {
        "dev.check.executed": "check.schema.json",
        "dev.review.completed": "review.schema.json",
        "dev.acceptance.recorded": "acceptance.schema.json",
        "dev.next_action.recorded": "next-action.schema.json",
    }
    validators = {}
    for event_type, filename in names.items():
        schema = json.loads((schemas / filename).read_text())
        jsonschema.Draft202012Validator.check_schema(schema)
        validators[event_type] = jsonschema.Draft202012Validator(schema)
    assert set(inputs) == set(expected)
    count = 0
    for name, record in inputs.items():
        errors = []
        for event in record["events"]:
            payload = dict(event["payload"], operation_id=record["operation_id"], source=record["source"])
            errors.extend(validators[event["type"]].iter_errors(payload))
            count += 1
        assert (not errors) == expected[name]["schema_valid"], (name, [e.message for e in errors])
    # Every schema is closed, including nested records. Enums reject a catch-all outcome.
    check = inputs["open-finding"]["events"][0]["payload"]
    payload = dict(check, operation_id="test", source=inputs["open-finding"]["source"])
    validator = validators["dev.check.executed"]
    assert not validator.is_valid(dict(payload, extra="unknown"))
    assert not validator.is_valid(dict(payload, source=dict(payload["source"], extra="unknown")))
    assert not validator.is_valid(dict(payload, outcome="partial"))
    assert not validator.is_valid(dict(payload, source=dict(payload["source"], revision=payload["source"]["revision"] + "\n")))
    assert validator.is_valid(dict(payload, outcome="interrupted", exit_code=-1, signal="SIGTERM"))
    print(f"Validated {count} payloads in {len(inputs)} public cases; semantic/projection expectations remain declarative.")


if __name__ == "__main__":
    main()
