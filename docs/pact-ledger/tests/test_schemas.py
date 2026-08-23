#!/usr/bin/env python3
"""Validate PACT JSON Schemas and bundled examples."""

from __future__ import annotations

import json
import unittest
from pathlib import Path

from jsonschema import Draft202012Validator
from referencing import Registry, Resource


ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIR = ROOT / "schemas"


class SchemaTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.schemas = {
            path.name: json.loads(path.read_text())
            for path in sorted(SCHEMA_DIR.glob("*.json"))
        }
        registry = Registry()
        for schema in cls.schemas.values():
            Draft202012Validator.check_schema(schema)
            registry = registry.with_resource(schema["$id"], Resource.from_contents(schema))
        cls.registry = registry

    def test_event_batch_examples(self) -> None:
        validator = Draft202012Validator(
            self.schemas["event-batch.schema.json"], registry=self.registry
        )
        for name in ("event-batch.json", "delegation-batch.json", "correction-batch.json"):
            with self.subTest(example=name):
                validator.validate(json.loads((ROOT / "examples" / name).read_text()))

    def test_projection_manifest_example(self) -> None:
        validator = Draft202012Validator(
            self.schemas["projection-manifest.schema.json"], registry=self.registry
        )
        validator.validate(json.loads((ROOT / "examples" / "projection-manifest.json").read_text()))


if __name__ == "__main__":
    unittest.main(verbosity=2)
