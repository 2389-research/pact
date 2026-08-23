#!/usr/bin/env python3
"""End-to-end tests for the PACT reference CLI."""

from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any


SKILL_ROOT = Path(__file__).resolve().parents[1]
PACT = SKILL_ROOT / "scripts" / "pact.py"
_SPEC = importlib.util.spec_from_file_location("pact_reference_cli", PACT)
assert _SPEC is not None and _SPEC.loader is not None
PACT_MODULE = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = PACT_MODULE
_SPEC.loader.exec_module(PACT_MODULE)
POLICY_DIGEST = "sha256:" + "a" * 64


class PactCliTest(unittest.TestCase):
    """Exercise public CLI behavior rather than private helper functions."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="pact-test-")
        self.root = Path(self.temp.name)
        self.repo = self.root / "project"
        self.key = self.root / "root-key.json"
        self._run("init", "--repo", str(self.repo), "--namespace", "org/example/project/widget")
        self.root_key = self._run("keygen", "--actor", "human/root", "--out", str(self.key))
        self._run("trust-add", "--repo", str(self.repo), "--key-file", str(self.key))

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _run(self, *args: str, expect: int = 0) -> dict[str, Any]:
        command = [*args, "--json"]
        stdout = io.StringIO()
        stderr = io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            return_code = PACT_MODULE.main(command)
        self.assertEqual(
            return_code,
            expect,
            msg=(
                f"command failed: pact {' '.join(command)}\n"
                f"stdout:\n{stdout.getvalue()}\n"
                f"stderr:\n{stderr.getvalue()}"
            ),
        )
        stream = stdout.getvalue() if return_code == 0 else stderr.getvalue()
        self.assertTrue(stream.strip(), msg=f"no JSON output from pact {' '.join(command)}")
        return json.loads(stream)

    def _write_json(self, name: str, value: Any) -> Path:
        path = self.root / name
        path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")
        return path

    def _evidence(self, name: str = "evidence.txt", content: bytes = b"evidence\n") -> tuple[Path, str]:
        path = self.root / name
        path.write_bytes(content)
        digest = "sha256:" + hashlib.sha256(content).hexdigest()
        return path, digest

    def _event_batch(
        self,
        *,
        local_id: str,
        event_type: str,
        subject: str,
        payload: dict[str, Any],
        namespace: str = "org/example/project/widget",
        evidence: list[dict[str, Any]] | None = None,
        caused_by: list[str] | None = None,
        supersedes: list[str] | None = None,
    ) -> dict[str, Any]:
        return {
            "namespace": namespace,
            "events": [
                {
                    "local_id": local_id,
                    "kind": "observation",
                    "type": event_type,
                    "subject": subject,
                    "schema_ref": "pact:core/generic-object/v1",
                    "payload": payload,
                    "evidence": evidence or [],
                    "caused_by": caused_by or [],
                    "supersedes": supersedes or [],
                    "tags": ["test"],
                }
            ],
        }

    def _commit(self, batch: dict[str, Any], *, key: Path | None = None, extra: list[str] | None = None) -> dict[str, Any]:
        batch_path = self._write_json(f"batch-{len(list(self.root.glob('batch-*.json')))}.json", batch)
        arguments = [
            "commit",
            "--repo",
            str(self.repo),
            "--key-file",
            str(key or self.key),
            "--events",
            str(batch_path),
        ]
        if extra:
            arguments.extend(extra)
        return self._run(*arguments)

    def test_full_lifecycle_and_checkpoint(self) -> None:
        evidence_path, digest = self._evidence()
        batch = self._event_batch(
            local_id="e1",
            event_type="build.test.executed",
            subject="build/linux/1",
            payload={"command": "make test", "exit_code": 0, "result": "pass"},
            evidence=[
                {
                    "ref": evidence_path.as_uri(),
                    "digest": digest,
                    "media_type": "text/plain",
                    "role": "primary",
                }
            ],
        )
        commit = self._commit(batch)
        self.assertEqual(commit["integrity"], "valid")
        self.assertEqual(commit["authenticity"], "valid")
        self.assertEqual(commit["authorization"], "authorized")
        self.assertEqual(len(commit["event_refs"]), 1)

        verification = self._run("verify", "--repo", str(self.repo), "--strict")
        self.assertTrue(verification["ok"])
        self.assertEqual(verification["counts"]["objects"], 1)
        self.assertEqual(verification["counts"]["events"], 1)

        heads = self._run("heads", "--repo", str(self.repo))
        self.assertEqual(heads["heads"]["org/example/project/widget"], [commit["object_id"]])

        log = self._run("log", "--repo", str(self.repo), "--type", "build.test.executed")
        self.assertEqual(log["count"], 1)
        self.assertEqual(log["events"][0]["event_ref"], commit["event_refs"][0])

        checkpoint = self._run(
            "checkpoint",
            "--repo",
            str(self.repo),
            "--key-file",
            str(self.key),
            "--scope",
            "org/example/project/widget",
            "--policy-ref",
            POLICY_DIGEST,
            "--authority-epoch",
            "org/example/epoch/1",
        )
        self.assertEqual(checkpoint["integrity"], "valid")
        self.assertEqual(checkpoint["authorization"], "authorized")

        reindex = self._run("reindex", "--repo", str(self.repo))
        self.assertEqual(reindex["objects"], 2)
        final_verification = self._run("verify", "--repo", str(self.repo), "--strict")
        self.assertTrue(final_verification["ok"])
        self.assertEqual(final_verification["counts"]["checkpoints"], 1)

    def test_append_only_correction_keeps_original_bytes(self) -> None:
        first = self._commit(
            self._event_batch(
                local_id="claim",
                event_type="capability.state.asserted",
                subject="capability/example",
                payload={"state": "pass"},
            )
        )
        original_path = Path(first["path"])
        original_bytes = original_path.read_bytes()

        correction = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "invalidate",
                        "kind": "decision",
                        "type": "pact.assertion.invalidated",
                        "subject": "capability/example",
                        "schema_ref": "pact:core/generic-object/v1",
                        "payload": {
                            "target_event_ref": first["event_refs"][0],
                            "reason": "the wrong fixture was used",
                        },
                        "evidence": [],
                        "caused_by": [first["event_refs"][0]],
                        "supersedes": [first["event_refs"][0]],
                        "tags": ["correction"],
                    }
                ],
            }
        )
        self.assertNotEqual(first["object_id"], correction["object_id"])
        self.assertEqual(original_path.read_bytes(), original_bytes)
        self.assertEqual(correction["parents"], [first["object_id"]])
        verification = self._run("verify", "--repo", str(self.repo), "--strict")
        self.assertEqual(verification["counts"]["commits"], 2)
        self.assertEqual(verification["counts"]["events"], 2)

    def test_native_fork_and_explicit_merge(self) -> None:
        genesis = self._commit(
            self._event_batch(
                local_id="genesis",
                event_type="work.item.discovered",
                subject="work/1",
                payload={"state": "open"},
            )
        )
        branch_a = self._commit(
            self._event_batch(
                local_id="a",
                event_type="work.note.observed",
                subject="work/1",
                payload={"note": "branch a"},
            ),
            extra=["--parent", genesis["object_id"]],
        )
        branch_b = self._commit(
            self._event_batch(
                local_id="b",
                event_type="work.note.observed",
                subject="work/1",
                payload={"note": "branch b"},
            ),
            extra=["--parent", genesis["object_id"]],
        )
        heads = self._run("heads", "--repo", str(self.repo))["heads"]["org/example/project/widget"]
        self.assertEqual(set(heads), {branch_a["object_id"], branch_b["object_id"]})

        merge = self._commit(
            self._event_batch(
                local_id="merge",
                event_type="work.branches.observed",
                subject="work/1",
                payload={"branches": 2},
            ),
            extra=[
                "--parent",
                branch_a["object_id"],
                "--parent",
                branch_b["object_id"],
            ],
        )
        merged_heads = self._run("heads", "--repo", str(self.repo))["heads"][
            "org/example/project/widget"
        ]
        self.assertEqual(merged_heads, [merge["object_id"]])

    def test_scoped_delegation_authorizes_agent_commit(self) -> None:
        agent_key = self.root / "agent-key.json"
        agent = self._run("keygen", "--actor", "agent/reviewer", "--out", str(agent_key))
        delegation = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "delegate",
                        "kind": "control",
                        "type": "pact.authority.delegated",
                        "subject": "authority/agent/reviewer",
                        "schema_ref": "pact:core/authority-delegation/v1",
                        "payload": {
                            "delegate_key_id": agent["key_id"],
                            "delegate_label": "agent/reviewer",
                            "namespace_patterns": ["org/example/project/widget/**"],
                            "event_type_patterns": ["audit.*"],
                            "epoch": "org/example/epoch/1",
                            "lease": {"not_before": None, "not_after": None},
                            "allow_subdelegation": False,
                            "max_subdelegation_depth": 0,
                            "capabilities": ["commit"],
                        },
                        "evidence": [],
                        "caused_by": [],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            }
        )
        agent_commit = self._commit(
            self._event_batch(
                local_id="observe",
                event_type="audit.finding.proposed",
                subject="finding/1",
                payload={"title": "example"},
            ),
            key=agent_key,
            extra=[
                "--delegation-ref",
                delegation["event_refs"][0],
                "--epoch",
                "org/example/epoch/1",
            ],
        )
        self.assertEqual(agent_commit["authorization"], "authorized")
        verification = self._run("verify", "--repo", str(self.repo), "--strict")
        self.assertEqual(
            verification["authorization"][agent_commit["object_id"]]["status"],
            "authorized",
        )

    def test_causal_revocation_preserves_but_unauthorizes_later_commit(self) -> None:
        agent_key = self.root / "revoked-agent-key.json"
        agent = self._run("keygen", "--actor", "agent/revoked", "--out", str(agent_key))
        delegation = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "delegate",
                        "kind": "control",
                        "type": "pact.authority.delegated",
                        "subject": "authority/agent/revoked",
                        "schema_ref": "pact:core/authority-delegation/v1",
                        "payload": {
                            "delegate_key_id": agent["key_id"],
                            "delegate_label": "agent/revoked",
                            "namespace_patterns": ["org/example/project/widget/**"],
                            "event_type_patterns": ["audit.*"],
                            "epoch": "org/example/epoch/1",
                            "lease": {"not_before": None, "not_after": None},
                            "allow_subdelegation": False,
                            "max_subdelegation_depth": 0,
                            "capabilities": ["commit"],
                        },
                        "evidence": [],
                        "caused_by": [],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            }
        )
        first_agent_commit = self._commit(
            self._event_batch(
                local_id="before-revocation",
                event_type="audit.finding.proposed",
                subject="finding/before",
                payload={"title": "before"},
            ),
            key=agent_key,
            extra=[
                "--delegation-ref",
                delegation["event_refs"][0],
                "--epoch",
                "org/example/epoch/1",
            ],
        )
        self.assertEqual(first_agent_commit["authorization"], "authorized")

        self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "revoke",
                        "kind": "control",
                        "type": "pact.authority.revoked",
                        "subject": "authority/agent/revoked",
                        "schema_ref": "pact:core/generic-object/v1",
                        "payload": {
                            "target_delegation_ref": delegation["event_refs"][0],
                            "target_key_id": agent["key_id"],
                            "reason": "test revocation",
                        },
                        "evidence": [],
                        "caused_by": [first_agent_commit["event_refs"][0]],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            }
        )
        after_revocation = self._commit(
            self._event_batch(
                local_id="after-revocation",
                event_type="audit.finding.proposed",
                subject="finding/after",
                payload={"title": "after"},
            ),
            key=agent_key,
            extra=[
                "--delegation-ref",
                delegation["event_refs"][0],
                "--epoch",
                "org/example/epoch/1",
            ],
        )
        self.assertEqual(after_revocation["authenticity"], "valid")
        self.assertEqual(after_revocation["authorization"], "unauthorized")
        self.assertTrue(
            any("revoked" in reason for reason in after_revocation["authorization_reasons"])
        )
        verification = self._run("verify", "--repo", str(self.repo), "--strict")
        self.assertTrue(verification["ok"])
        self.assertEqual(
            verification["authorization"][after_revocation["object_id"]]["status"],
            "unauthorized",
        )

    def test_forbidden_subdelegation_is_not_authorized(self) -> None:
        parent_key = self.root / "parent-agent-key.json"
        child_key = self.root / "child-agent-key.json"
        parent = self._run("keygen", "--actor", "agent/parent", "--out", str(parent_key))
        child = self._run("keygen", "--actor", "agent/child", "--out", str(child_key))
        parent_delegation = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "delegate-parent",
                        "kind": "control",
                        "type": "pact.authority.delegated",
                        "subject": "authority/agent/parent",
                        "schema_ref": "pact:core/authority-delegation/v1",
                        "payload": {
                            "delegate_key_id": parent["key_id"],
                            "delegate_label": "agent/parent",
                            "namespace_patterns": ["org/example/project/widget/**"],
                            "event_type_patterns": ["pact.authority.*", "audit.*"],
                            "epoch": "org/example/epoch/1",
                            "lease": {"not_before": None, "not_after": None},
                            "allow_subdelegation": False,
                            "max_subdelegation_depth": 0,
                            "capabilities": ["commit", "delegate"],
                        },
                        "evidence": [],
                        "caused_by": [],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            }
        )
        child_delegation = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "delegate-child",
                        "kind": "control",
                        "type": "pact.authority.delegated",
                        "subject": "authority/agent/child",
                        "schema_ref": "pact:core/authority-delegation/v1",
                        "payload": {
                            "delegate_key_id": child["key_id"],
                            "delegate_label": "agent/child",
                            "namespace_patterns": ["org/example/project/widget/**"],
                            "event_type_patterns": ["audit.*"],
                            "epoch": "org/example/epoch/1",
                            "lease": {"not_before": None, "not_after": None},
                            "allow_subdelegation": False,
                            "max_subdelegation_depth": 0,
                            "capabilities": ["commit"],
                        },
                        "evidence": [],
                        "caused_by": [],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            },
            key=parent_key,
            extra=[
                "--delegation-ref",
                parent_delegation["event_refs"][0],
                "--epoch",
                "org/example/epoch/1",
            ],
        )
        self.assertEqual(child_delegation["authenticity"], "valid")
        self.assertEqual(child_delegation["authorization"], "unauthorized")
        self.assertTrue(
            any(
                "subdelegation" in reason
                for reason in child_delegation["authorization_reasons"]
            )
        )

    def test_bounded_subdelegation_authorizes_one_child_level(self) -> None:
        parent_key = self.root / "allowed-parent-key.json"
        child_key = self.root / "allowed-child-key.json"
        parent = self._run("keygen", "--actor", "agent/parent-ok", "--out", str(parent_key))
        child = self._run("keygen", "--actor", "agent/child-ok", "--out", str(child_key))
        parent_delegation = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "delegate-parent",
                        "kind": "control",
                        "type": "pact.authority.delegated",
                        "subject": "authority/agent/parent-ok",
                        "schema_ref": "pact:core/authority-delegation/v1",
                        "payload": {
                            "delegate_key_id": parent["key_id"],
                            "delegate_label": "agent/parent-ok",
                            "namespace_patterns": ["org/example/project/widget/**"],
                            "event_type_patterns": ["pact.authority.*", "audit.*"],
                            "epoch": "org/example/epoch/1",
                            "lease": {"not_before": None, "not_after": None},
                            "allow_subdelegation": True,
                            "max_subdelegation_depth": 1,
                            "capabilities": ["commit", "delegate"],
                        },
                        "evidence": [],
                        "caused_by": [],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            }
        )
        child_delegation = self._commit(
            {
                "namespace": "org/example/project/widget",
                "events": [
                    {
                        "local_id": "delegate-child",
                        "kind": "control",
                        "type": "pact.authority.delegated",
                        "subject": "authority/agent/child-ok",
                        "schema_ref": "pact:core/authority-delegation/v1",
                        "payload": {
                            "delegate_key_id": child["key_id"],
                            "delegate_label": "agent/child-ok",
                            "namespace_patterns": ["org/example/project/widget/**"],
                            "event_type_patterns": ["audit.*"],
                            "epoch": "org/example/epoch/1",
                            "lease": {"not_before": None, "not_after": None},
                            "allow_subdelegation": False,
                            "max_subdelegation_depth": 0,
                            "capabilities": ["commit"],
                        },
                        "evidence": [],
                        "caused_by": [],
                        "supersedes": [],
                        "tags": ["authority"],
                    }
                ],
            },
            key=parent_key,
            extra=[
                "--delegation-ref",
                parent_delegation["event_refs"][0],
                "--epoch",
                "org/example/epoch/1",
            ],
        )
        self.assertEqual(child_delegation["authorization"], "authorized")
        child_commit = self._commit(
            self._event_batch(
                local_id="child-observation",
                event_type="audit.finding.proposed",
                subject="finding/child-ok",
                payload={"title": "allowed child"},
            ),
            key=child_key,
            extra=[
                "--delegation-ref",
                child_delegation["event_refs"][0],
                "--epoch",
                "org/example/epoch/1",
            ],
        )
        self.assertEqual(child_commit["authorization"], "authorized")
        self.assertEqual(child_commit["lease_status"], "advisory_within_lease")

    def test_secret_like_payload_is_refused(self) -> None:
        batch_path = self._write_json(
            "secret.json",
            self._event_batch(
                local_id="secret",
                event_type="operation.config.observed",
                subject="config/1",
                payload={"password": "this-is-an-actual-secret-value"},
            ),
        )
        error = self._run(
            "commit",
            "--repo",
            str(self.repo),
            "--key-file",
            str(self.key),
            "--events",
            str(batch_path),
            expect=7,
        )
        self.assertIn("secret-like", error["error"])
        self.assertTrue(error["details"]["hazards"])
        verification = self._run("verify", "--repo", str(self.repo), "--strict")
        self.assertEqual(verification["counts"]["objects"], 0)

    def test_directory_sync_unions_objects_without_overwrite(self) -> None:
        first = self._commit(
            self._event_batch(
                local_id="source",
                event_type="source.item.observed",
                subject="source/1",
                payload={"value": "one"},
            )
        )
        replica = self.root / "replica"
        self._run("init", "--repo", str(replica), "--namespace", "org/example/project/widget")
        self._run("trust-add", "--repo", str(replica), "--key-file", str(self.key))
        sync = self._run("sync-dir", "--repo", str(replica), "--from", str(self.repo))
        self.assertEqual(sync["imported"], 1)
        self.assertEqual(
            sync["heads_after"]["org/example/project/widget"],
            [first["object_id"]],
        )
        second_sync = self._run("sync-dir", "--repo", str(replica), "--from", str(self.repo))
        self.assertEqual(second_sync["imported"], 0)
        self.assertEqual(second_sync["already_present"], 1)

    def test_tampering_is_detected(self) -> None:
        commit = self._commit(
            self._event_batch(
                local_id="e1",
                event_type="build.test.executed",
                subject="build/1",
                payload={"exit_code": 0},
            )
        )
        object_path = Path(commit["path"])
        raw = object_path.read_bytes()
        object_path.write_bytes(raw[:-1] + (b"0" if raw[-1:] != b"0" else b"1"))
        error = self._run("verify", "--repo", str(self.repo), "--strict", expect=4)
        self.assertIn("verification failed", error["error"].lower())
        details = error["details"]
        self.assertFalse(details["ok"])
        self.assertTrue(details["errors"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
