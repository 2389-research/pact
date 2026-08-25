#!/usr/bin/env python3
"""Fast conformance tests for PACT schemas and canonical helpers."""

from __future__ import annotations

import importlib.util
import json
import sys
import unittest
import unicodedata
from pathlib import Path


SKILL_ROOT = Path(__file__).resolve().parents[1]
REPOSITORY_ROOT = SKILL_ROOT.parents[1]
PACT_PATH = SKILL_ROOT / "scripts" / "pact_core.py"

spec = importlib.util.spec_from_file_location("pact_reference", PACT_PATH)
assert spec is not None and spec.loader is not None
pact = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = pact
spec.loader.exec_module(pact)

try:
    from jsonschema import Draft202012Validator
    from referencing import Registry, Resource
except ImportError:  # pragma: no cover - dev dependency is optional for users.
    Draft202012Validator = None  # type: ignore[assignment]
    Registry = None  # type: ignore[assignment]
    Resource = None  # type: ignore[assignment]


class PactContractTest(unittest.TestCase):
    def test_canonical_json_normalizes_unicode_and_rejects_floats(self) -> None:
        decomposed = "e\u0301"
        composed = unicodedata.normalize("NFC", decomposed)
        first = pact.canonical_bytes({"name": decomposed, "value": 1})
        second = pact.canonical_bytes({"value": 1, "name": composed})
        self.assertEqual(first, second)
        self.assertEqual(first, ('{"name":"%s","value":1}' % composed).encode("utf-8"))

        with self.assertRaises(pact.PactError):
            pact.canonical_bytes({"value": 1.25})
        with self.assertRaises(pact.PactError):
            pact.canonical_bytes({"value": pact.MAX_SAFE_INTEGER + 1})

    def test_namespace_and_event_pattern_semantics(self) -> None:
        self.assertTrue(
            pact.match_namespace_pattern(
                "org/example/project/widget/**",
                "org/example/project/widget",
            )
        )
        self.assertTrue(
            pact.match_namespace_pattern(
                "org/example/project/widget/**",
                "org/example/project/widget/audit/worker",
            )
        )
        self.assertTrue(
            pact.match_namespace_pattern(
                "org/example/project/widget/*",
                "org/example/project/widget/audit",
            )
        )
        self.assertFalse(
            pact.match_namespace_pattern(
                "org/example/project/widget/*",
                "org/example/project/widget/audit/worker",
            )
        )
        self.assertTrue(pact.match_event_type_pattern("audit.*", "audit.finding.proposed"))
        self.assertFalse(pact.match_event_type_pattern("audit.*", "build.test.executed"))

    def test_secret_scanner_allows_indirection_and_rejects_raw_values(self) -> None:
        self.assertEqual(
            pact.scan_secret_hazards({"required_env": "OPENAI_API_KEY"}),
            [],
        )
        hazards = pact.scan_secret_hazards(
            {
                "password": "correct-horse-battery-staple",
                "endpoint": "https://user:pass@example.test/data",
            }
        )
        self.assertTrue(any("secret-like field" in item for item in hazards))
        self.assertTrue(any("URL userinfo" in item for item in hazards))

    @unittest.skipIf(Draft202012Validator is None, "jsonschema dev dependency not installed")
    def test_all_schemas_are_well_formed_and_examples_validate(self) -> None:
        schema_dir = SKILL_ROOT / "schemas"
        schemas: dict[str, dict] = {}
        for path in sorted(schema_dir.glob("*.schema.json")):
            schema = json.loads(path.read_text(encoding="utf-8"))
            Draft202012Validator.check_schema(schema)
            schemas[schema["$id"]] = schema

        registry = Registry()
        for identifier, schema in schemas.items():
            registry = registry.with_resource(identifier, Resource.from_contents(schema))

        event_batch_schema = schemas["https://pact.local/schemas/event-batch.schema.json"]
        event_batch_validator = Draft202012Validator(event_batch_schema, registry=registry)
        for name in ("event-batch.json", "correction-batch.json", "delegation-batch.json"):
            instance = json.loads((SKILL_ROOT / "examples" / name).read_text(encoding="utf-8"))
            event_batch_validator.validate(instance)

        delegation_batch = json.loads(
            (SKILL_ROOT / "examples" / "delegation-batch.json").read_text(encoding="utf-8")
        )
        delegation_schema = schemas[
            "https://pact.local/schemas/delegation-payload.schema.json"
        ]
        Draft202012Validator(delegation_schema, registry=registry).validate(
            delegation_batch["events"][0]["payload"]
        )

        projection_schema = schemas[
            "https://pact.local/schemas/projection-manifest.schema.json"
        ]
        projection = json.loads(
            (SKILL_ROOT / "examples" / "projection-manifest.json").read_text(
                encoding="utf-8"
            )
        )
        Draft202012Validator(projection_schema, registry=registry).validate(projection)

    def test_full_module_go_hooks_do_not_accept_filenames(self) -> None:
        config = (REPOSITORY_ROOT / ".pre-commit-config.yaml").read_text(
            encoding="utf-8"
        )
        for hook_id in ("go-mod-tidy", "go-unit-tests"):
            marker = f"- id: {hook_id}"
            self.assertIn(marker, config)
            block = config.split(marker, 1)[1].split("\n      - id:", 1)[0]
            block = block.split("\n\n", 1)[0]
            self.assertIn("pass_filenames: false", block)


if __name__ == "__main__":
    unittest.main(verbosity=2)
