#!/usr/bin/env python3
"""Frozen PACT canonicalization conformance vectors."""

from __future__ import annotations

import importlib.util
import json
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "pact_conformance_core", ROOT / "scripts" / "pact_core.py"
)
assert SPEC is not None and SPEC.loader is not None
PACT = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = PACT
SPEC.loader.exec_module(PACT)


class CanonicalizationVectorTest(unittest.TestCase):
    def test_frozen_vectors(self) -> None:
        vectors = json.loads((ROOT / "examples" / "canonicalization-vectors.json").read_text())
        self.assertEqual(vectors["profile"], "pact-json-v1")
        for vector in vectors["valid"]:
            with self.subTest(vector=vector["name"]):
                value = PACT.parse_json_bytes(
                    vector["input_json"].encode("utf-8"), source=vector["name"]
                )
                canonical = PACT.canonical_bytes(value)
                self.assertEqual(canonical.decode("utf-8"), vector["canonical_utf8"])
                self.assertEqual(PACT.sha256_digest(canonical), vector["sha256"])

        for vector in vectors["invalid"]:
            with self.subTest(vector=vector["name"]):
                if "input_hex" in vector:
                    raw = bytes.fromhex(vector["input_hex"])
                else:
                    raw = vector["input_json"].encode("utf-8")
                with self.assertRaises(PACT.PactError) as captured:
                    value = PACT.parse_json_bytes(raw, source=vector["name"])
                    PACT.canonical_bytes(value)
                self.assertIn(vector["error_contains"], captured.exception.message)


if __name__ == "__main__":
    unittest.main(verbosity=2)
