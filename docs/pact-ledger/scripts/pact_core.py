#!/usr/bin/env python3
"""PACT reference CLI.

PACT is a local-first, append-only, content-addressed, signed semantic ledger.
This implementation is intentionally small enough to audit while still
exercising the core design:

* deterministic JSON normalization;
* SHA-256 object identities;
* Ed25519 signatures;
* atomic content-addressed object writes;
* multi-parent commit DAGs;
* stable event references;
* signed checkpoints;
* rebuildable SQLite indexing;
* conservative authority diagnostics;
* directory-backed replica synchronization.

The reference CLI is not a hardened network service, secret store, trusted-time
service, or universal projection engine. See references/implementation-plan.md.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import json
import os
import re
import sqlite3
import stat
import sys
import tempfile
import unicodedata
import urllib.parse
from collections import defaultdict, deque
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterator, Mapping, Sequence

try:
    from cryptography.exceptions import InvalidSignature
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import (
        Ed25519PrivateKey,
        Ed25519PublicKey,
    )
except ImportError as exc:  # pragma: no cover - exercised only on missing dependency.
    raise SystemExit(
        "PACT requires the 'cryptography' package for Ed25519 signing. "
        "Install it with: python3 -m pip install cryptography"
    ) from exc


VERSION = "0.1.0"
STORE_FORMAT = "pact/store/v1"
COMMIT_FORMAT = "pact/commit/v1"
CHECKPOINT_FORMAT = "pact/checkpoint/v1"
KEY_FORMAT = "pact/key/v1"
TRUST_FORMAT = "pact/trust/v1"
CANONICALIZATION = "pact-json-v1"
PRODUCER = f"pact-reference-cli/{VERSION}"

MAX_SAFE_INTEGER = 9_007_199_254_740_991
MIN_SAFE_INTEGER = -MAX_SAFE_INTEGER

DIGEST_RE = re.compile(r"^sha256:([0-9a-f]{64})$")
KEY_ID_RE = re.compile(r"^ed25519:sha256:([0-9a-f]{64})$")
EVENT_REF_RE = re.compile(
    r"^pact:event:(sha256:[0-9a-f]{64})#([A-Za-z0-9][A-Za-z0-9._-]{0,127})$"
)
LOCAL_REF_RE = re.compile(r"^local:([A-Za-z0-9][A-Za-z0-9._-]{0,127})$")
LOCAL_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
EVENT_TYPE_RE = re.compile(r"^[a-z0-9][a-z0-9._/-]{0,255}$")
CORE_SCHEMA_RE = re.compile(r"^pact:core/[a-z0-9._/-]+/v[0-9]+$")
NAMESPACE_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]{0,511}$")
ENV_NAME_RE = re.compile(r"^[A-Z][A-Z0-9_]{2,127}$")

ALLOWED_EVENT_KINDS = {"observation", "assertion", "action", "decision", "control"}
ALLOWED_EVIDENCE_ROLES = {"primary", "supporting", "derived"}

# These names are intentionally narrow. The scanner is a safety backstop, not a
# substitute for reviewing payload schemas and minimizing immutable metadata.
SECRET_KEY_NAMES = {
    "password",
    "passwd",
    "secret",
    "client_secret",
    "api_key",
    "apikey",
    "access_token",
    "refresh_token",
    "bearer_token",
    "private_key",
    "authorization",
    "cookie",
    "session_cookie",
}

SECRET_TEXT_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    ("private key material", re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |)?PRIVATE KEY-----")),
    ("bearer credential", re.compile(r"\bBearer\s+[A-Za-z0-9._~+/=-]{16,}", re.IGNORECASE)),
    ("JWT-like credential", re.compile(r"\beyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\b")),
    ("GitHub token-like credential", re.compile(r"\bgh[pousr]_[A-Za-z0-9]{20,}\b")),
    ("AWS access-key-like credential", re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b")),
]


class PactError(Exception):
    """Expected user-facing failure with a stable CLI exit code."""

    def __init__(self, message: str, *, exit_code: int = 2, details: Any = None):
        super().__init__(message)
        self.message = message
        self.exit_code = exit_code
        self.details = details


@dataclass
class ObjectVerification:
    """Verification result for one canonical object."""

    object_id: str
    path: str
    object_type: str | None = None
    namespace: str | None = None
    integrity: str = "invalid"
    authenticity: str = "unverified"
    errors: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)
    obj: dict[str, Any] | None = None

    @property
    def structurally_valid(self) -> bool:
        return not self.errors and self.integrity == "valid" and self.authenticity == "valid"


@dataclass
class AuthorizationResult:
    """Structured authorization result kept separate from authenticity."""

    status: str
    reasons: list[str] = field(default_factory=list)
    chain: list[str] = field(default_factory=list)
    lease_status: str = "not_applicable"
    depth: int = 0


# ---------------------------------------------------------------------------
# Basic encoding, hashing, JSON parsing, and normalization
# ---------------------------------------------------------------------------


def utc_now() -> str:
    """Return a compact UTC timestamp for advisory metadata."""

    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def b64url_encode(data: bytes) -> str:
    """Encode bytes as unpadded URL-safe base64."""

    return base64.urlsafe_b64encode(data).decode("ascii").rstrip("=")


def b64url_decode(value: str) -> bytes:
    """Decode unpadded URL-safe base64 with strict character checking."""

    if not re.fullmatch(r"[A-Za-z0-9_-]+", value):
        raise PactError("invalid base64url value", exit_code=4)
    padding = "=" * ((4 - len(value) % 4) % 4)
    try:
        return base64.urlsafe_b64decode(value + padding)
    except Exception as exc:
        raise PactError("invalid base64url value", exit_code=4) from exc


def sha256_digest(data: bytes) -> str:
    """Return the canonical PACT SHA-256 identifier for exact bytes."""

    return "sha256:" + hashlib.sha256(data).hexdigest()


def key_id_for_public_key(public_key: bytes) -> str:
    """Derive the stable Ed25519 key ID from raw 32-byte public-key bytes."""

    if len(public_key) != 32:
        raise PactError("Ed25519 public keys must contain exactly 32 bytes", exit_code=4)
    return "ed25519:sha256:" + hashlib.sha256(public_key).hexdigest()


def _reject_duplicate_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    """Reject raw or NFC-colliding duplicate JSON object keys while parsing."""

    result: dict[str, Any] = {}
    normalized_seen: set[str] = set()
    for key, value in pairs:
        if key in result:
            raise PactError(f"duplicate JSON object key: {key!r}", exit_code=2)
        normalized_key = unicodedata.normalize("NFC", key)
        if normalized_key in normalized_seen:
            raise PactError(
                f"JSON object keys collide after Unicode normalization: {key!r}",
                exit_code=2,
            )
        result[key] = value
        normalized_seen.add(normalized_key)
    return result


def parse_json_bytes(data: bytes, *, source: str) -> Any:
    """Parse UTF-8 JSON while refusing BOMs, duplicate keys, and trailing data."""

    if data.startswith(b"\xef\xbb\xbf"):
        raise PactError(f"{source}: UTF-8 BOM is not allowed", exit_code=2)
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise PactError(f"{source}: JSON is not valid UTF-8", exit_code=2) from exc
    try:
        return json.loads(text, object_pairs_hook=_reject_duplicate_pairs)
    except PactError:
        raise
    except json.JSONDecodeError as exc:
        raise PactError(f"{source}: invalid JSON: {exc}", exit_code=2) from exc


def load_json(path: Path) -> Any:
    """Load a JSON file using PACT's strict parser."""

    try:
        return parse_json_bytes(path.read_bytes(), source=str(path))
    except FileNotFoundError as exc:
        raise PactError(f"file not found: {path}", exit_code=2) from exc


def normalize_value(value: Any, *, path: str = "$") -> Any:
    """Normalize a JSON-compatible value under the conservative PACT profile.

    Floats are deliberately forbidden to avoid cross-language canonicalization
    ambiguity in v1. Domain schemas should encode decimals as strings or scaled
    integers.
    """

    if value is None or isinstance(value, bool):
        return value
    if isinstance(value, int):
        if value < MIN_SAFE_INTEGER or value > MAX_SAFE_INTEGER:
            raise PactError(
                f"{path}: integer outside interoperable PACT range",
                exit_code=2,
            )
        return value
    if isinstance(value, float):
        raise PactError(
            f"{path}: floating-point values are forbidden; use a string or scaled integer",
            exit_code=2,
        )
    if isinstance(value, str):
        return unicodedata.normalize("NFC", value)
    if isinstance(value, list):
        return [normalize_value(item, path=f"{path}[{index}]") for index, item in enumerate(value)]
    if isinstance(value, dict):
        normalized: dict[str, Any] = {}
        for raw_key, raw_value in value.items():
            if not isinstance(raw_key, str):
                raise PactError(f"{path}: JSON object key is not a string", exit_code=2)
            key = unicodedata.normalize("NFC", raw_key)
            if key in normalized:
                raise PactError(
                    f"{path}: duplicate key after Unicode normalization: {key!r}",
                    exit_code=2,
                )
            normalized[key] = normalize_value(raw_value, path=f"{path}.{key}")
        return normalized
    raise PactError(f"{path}: unsupported JSON value type {type(value).__name__}", exit_code=2)


def canonical_bytes(value: Any) -> bytes:
    """Return canonical UTF-8 JSON bytes for a normalized PACT value."""

    normalized = normalize_value(value)
    text = json.dumps(
        normalized,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    )
    return text.encode("utf-8")


def pretty_json(value: Any) -> str:
    """Render normalized JSON for human inspection, never for object identity."""

    return json.dumps(normalize_value(value), ensure_ascii=False, sort_keys=True, indent=2)


def write_config_json(path: Path, value: Any, *, mode: int = 0o644) -> None:
    """Atomically write noncanonical local configuration as readable JSON."""

    path.parent.mkdir(parents=True, exist_ok=True)
    payload = (pretty_json(value) + "\n").encode("utf-8")
    _atomic_write(path, payload, mode=mode)


def _atomic_write(path: Path, data: bytes, *, mode: int = 0o644) -> None:
    """Write bytes to a sibling temporary file and atomically rename into place."""

    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temp_path = Path(temp_name)
    try:
        os.fchmod(fd, mode)
        with os.fdopen(fd, "wb", closefd=True) as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp_path, path)
        # Durability of the directory entry matters for crash recovery. Some
        # filesystems/platforms do not permit opening directories; ignore only
        # those platform limitations.
        try:
            dir_fd = os.open(path.parent, os.O_RDONLY)
            try:
                os.fsync(dir_fd)
            finally:
                os.close(dir_fd)
        except OSError:
            pass
    except Exception:
        try:
            temp_path.unlink(missing_ok=True)
        finally:
            raise


# ---------------------------------------------------------------------------
# Project-store paths and IDs
# ---------------------------------------------------------------------------


def repo_path(value: str | Path) -> Path:
    """Resolve a user-supplied repository path without requiring Git."""

    return Path(value).expanduser().resolve()


def store_path(repo: Path) -> Path:
    return repo / ".pact"


def ensure_store(repo: Path) -> Path:
    """Return an initialized store or raise a stable initialization error."""

    store = store_path(repo)
    format_path = store / "format.json"
    if not format_path.is_file():
        raise PactError(
            f"PACT store is not initialized at {store}; run 'pact init' first",
            exit_code=3,
        )
    format_obj = load_json(format_path)
    if not isinstance(format_obj, dict) or format_obj.get("format") != STORE_FORMAT:
        raise PactError(
            f"unsupported or malformed PACT store format at {format_path}",
            exit_code=3,
        )
    return store


def parse_digest(value: str, *, field_name: str = "digest") -> str:
    """Validate and normalize a PACT SHA-256 digest string."""

    if not isinstance(value, str) or not DIGEST_RE.fullmatch(value):
        raise PactError(f"invalid {field_name}: {value!r}", exit_code=2)
    return value


def validate_key_id(value: str, *, field_name: str = "key ID") -> str:
    if not isinstance(value, str) or not KEY_ID_RE.fullmatch(value):
        raise PactError(f"invalid {field_name}: {value!r}", exit_code=2)
    return value


def validate_namespace(value: str) -> str:
    """Validate a canonical logical namespace with no empty path segments."""

    if not isinstance(value, str):
        raise PactError("namespace must be a string", exit_code=2)
    value = unicodedata.normalize("NFC", value)
    if not NAMESPACE_RE.fullmatch(value):
        raise PactError(f"invalid namespace: {value!r}", exit_code=2)
    if value.startswith("/") or value.endswith("/") or "//" in value:
        raise PactError(f"namespace must not contain empty path segments: {value!r}", exit_code=2)
    if any(segment in {".", ".."} for segment in value.split("/")):
        raise PactError(f"namespace must not contain '.' or '..' segments: {value!r}", exit_code=2)
    return value


def object_path_for_id(store: Path, object_id: str) -> Path:
    """Map a digest to its immutable content-addressed filesystem path."""

    digest = parse_digest(object_id, field_name="object ID")
    hex_digest = digest.split(":", 1)[1]
    return store / "objects" / "sha256" / hex_digest[:2] / f"{hex_digest[2:]}.json"


def object_id_from_path(store: Path, path: Path) -> str:
    """Recover the expected object ID from a canonical object-store path."""

    try:
        relative = path.relative_to(store / "objects" / "sha256")
    except ValueError as exc:
        raise PactError(f"object path is outside canonical store: {path}", exit_code=4) from exc
    parts = relative.parts
    if len(parts) != 2 or len(parts[0]) != 2 or not parts[1].endswith(".json"):
        raise PactError(f"malformed object-store path: {path}", exit_code=4)
    remainder = parts[1][:-5]
    hex_digest = parts[0] + remainder
    object_id = f"sha256:{hex_digest}"
    parse_digest(object_id, field_name="object path digest")
    return object_id


def iter_object_paths(store: Path) -> Iterator[Path]:
    """Yield canonical object paths in stable lexical order."""

    root = store / "objects" / "sha256"
    if not root.exists():
        return
    for path in sorted(root.glob("*/*.json")):
        if path.is_file():
            yield path


def read_object(store: Path, object_id: str) -> dict[str, Any]:
    """Read one object by digest and verify the path-level content hash."""

    path = object_path_for_id(store, object_id)
    if not path.is_file():
        raise PactError(f"object not found: {object_id}", exit_code=9)
    raw = path.read_bytes()
    actual_id = sha256_digest(raw)
    if actual_id != object_id:
        raise PactError(
            f"object digest mismatch at {path}: expected {object_id}, got {actual_id}",
            exit_code=4,
        )
    obj = parse_json_bytes(raw, source=str(path))
    if not isinstance(obj, dict):
        raise PactError(f"object is not a JSON object: {object_id}", exit_code=4)
    return obj


def write_canonical_object(store: Path, obj: Mapping[str, Any]) -> tuple[str, bool]:
    """Persist a signed object atomically without ever replacing different bytes.

    Returns `(object_id, created)`; an identical existing object is idempotent.
    """

    raw = canonical_bytes(dict(obj))
    object_id = sha256_digest(raw)
    path = object_path_for_id(store, object_id)
    if path.exists():
        existing = path.read_bytes()
        if existing != raw:
            raise PactError(
                f"content-address collision or corruption at {path}",
                exit_code=4,
            )
        return object_id, False

    path.parent.mkdir(parents=True, exist_ok=True)
    temp_dir = store / "tmp"
    temp_dir.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix="object.", suffix=".tmp", dir=temp_dir)
    temp_path = Path(temp_name)
    try:
        os.fchmod(fd, 0o644)
        with os.fdopen(fd, "wb", closefd=True) as handle:
            handle.write(raw)
            handle.flush()
            os.fsync(handle.fileno())
        # Never overwrite. A concurrent identical writer may win the race; in
        # that case compare exact bytes and discard our temp file.
        try:
            os.link(temp_path, path)
            created = True
        except FileExistsError:
            if path.read_bytes() != raw:
                raise PactError(
                    f"content-address collision or concurrent corruption at {path}",
                    exit_code=4,
                )
            created = False
        temp_path.unlink(missing_ok=True)
    except Exception:
        temp_path.unlink(missing_ok=True)
        raise

    # Verify post-write bytes before reporting success.
    persisted = path.read_bytes()
    if persisted != raw or sha256_digest(persisted) != object_id:
        raise PactError(f"post-write verification failed for {object_id}", exit_code=4)
    return object_id, created


# ---------------------------------------------------------------------------
# Key files, signing, and local trust bootstrap
# ---------------------------------------------------------------------------


def load_key_file(path: Path, *, require_private: bool = True) -> dict[str, Any]:
    """Load and internally verify a PACT key file."""

    obj = load_json(path)
    if not isinstance(obj, dict) or obj.get("format") != KEY_FORMAT:
        raise PactError(f"unsupported or malformed PACT key file: {path}", exit_code=2)
    if obj.get("algorithm") != "ed25519":
        raise PactError(f"unsupported key algorithm in {path}", exit_code=2)
    actor = obj.get("actor")
    if not isinstance(actor, str) or not actor.strip():
        raise PactError(f"key file has invalid actor label: {path}", exit_code=2)
    public_raw = b64url_decode(str(obj.get("public_key", "")))
    if len(public_raw) != 32:
        raise PactError(f"key file has invalid Ed25519 public key: {path}", exit_code=2)
    expected_key_id = key_id_for_public_key(public_raw)
    if obj.get("key_id") != expected_key_id:
        raise PactError(f"key ID does not match public key in {path}", exit_code=4)

    private_value = obj.get("private_key")
    if require_private and not isinstance(private_value, str):
        raise PactError(f"private key is required in {path}", exit_code=2)
    if isinstance(private_value, str):
        private_raw = b64url_decode(private_value)
        if len(private_raw) != 32:
            raise PactError(f"key file has invalid Ed25519 private key: {path}", exit_code=2)
        private_key = Ed25519PrivateKey.from_private_bytes(private_raw)
        derived_public = private_key.public_key().public_bytes(
            encoding=serialization.Encoding.Raw,
            format=serialization.PublicFormat.Raw,
        )
        if derived_public != public_raw:
            raise PactError(f"private/public key mismatch in {path}", exit_code=4)
    return obj


def sign_body(body: Mapping[str, Any], key_obj: Mapping[str, Any]) -> tuple[str, dict[str, str]]:
    """Hash a canonical body and sign the 32-byte digest with Ed25519."""

    body_raw = canonical_bytes(dict(body))
    body_digest = sha256_digest(body_raw)
    private_raw = b64url_decode(str(key_obj["private_key"]))
    private_key = Ed25519PrivateKey.from_private_bytes(private_raw)
    signature_raw = private_key.sign(bytes.fromhex(body_digest.split(":", 1)[1]))
    signature = {
        "algorithm": "ed25519",
        "key_id": str(key_obj["key_id"]),
        "public_key": str(key_obj["public_key"]),
        "value": b64url_encode(signature_raw),
    }
    return body_digest, signature


def verify_signature_envelope(obj: Mapping[str, Any]) -> list[str]:
    """Return signature/body-digest errors for a signed commit/checkpoint."""

    errors: list[str] = []
    body = obj.get("body")
    signature = obj.get("signature")
    if not isinstance(body, dict):
        return ["signed object body is missing or not an object"]
    if not isinstance(signature, dict):
        return ["signed object signature is missing or not an object"]

    expected_body_digest = sha256_digest(canonical_bytes(body))
    if obj.get("body_digest") != expected_body_digest:
        errors.append(
            f"body digest mismatch: expected {expected_body_digest}, got {obj.get('body_digest')!r}"
        )

    if signature.get("algorithm") != "ed25519":
        errors.append("unsupported or missing signature algorithm")
        return errors
    try:
        public_raw = b64url_decode(str(signature.get("public_key", "")))
        signature_raw = b64url_decode(str(signature.get("value", "")))
    except PactError as exc:
        errors.append(exc.message)
        return errors
    if len(public_raw) != 32:
        errors.append("Ed25519 public key is not 32 bytes")
        return errors
    if len(signature_raw) != 64:
        errors.append("Ed25519 signature is not 64 bytes")
        return errors

    expected_key_id = key_id_for_public_key(public_raw)
    if signature.get("key_id") != expected_key_id:
        errors.append("signature key ID does not match embedded public key")

    actor = body.get("actor")
    if not isinstance(actor, dict) or actor.get("key_id") != expected_key_id:
        errors.append("body actor key ID does not match signature key ID")

    body_digest = obj.get("body_digest")
    if isinstance(body_digest, str) and DIGEST_RE.fullmatch(body_digest):
        try:
            Ed25519PublicKey.from_public_bytes(public_raw).verify(
                signature_raw,
                bytes.fromhex(body_digest.split(":", 1)[1]),
            )
        except InvalidSignature:
            errors.append("Ed25519 signature verification failed")
        except Exception as exc:
            errors.append(f"signature verification error: {exc}")
    else:
        errors.append("body digest is malformed; signature cannot be checked")
    return errors


def trust_file(store: Path) -> Path:
    return store / "trust.json"


def load_trust(store: Path) -> dict[str, Any]:
    """Load local out-of-band trust-root configuration."""

    path = trust_file(store)
    if not path.exists():
        return {"format": TRUST_FORMAT, "roots": []}
    obj = load_json(path)
    if not isinstance(obj, dict) or obj.get("format") != TRUST_FORMAT:
        raise PactError(f"malformed local trust file: {path}", exit_code=3)
    roots = obj.get("roots")
    if not isinstance(roots, list):
        raise PactError(f"malformed local trust roots: {path}", exit_code=3)
    return obj


def trust_root_map(store: Path) -> dict[str, dict[str, Any]]:
    """Return trusted roots keyed by stable key ID, validating collisions."""

    result: dict[str, dict[str, Any]] = {}
    for root in load_trust(store).get("roots", []):
        if not isinstance(root, dict):
            raise PactError("malformed trust-root entry", exit_code=3)
        key_id = validate_key_id(str(root.get("key_id", "")), field_name="trusted root key ID")
        public_raw = b64url_decode(str(root.get("public_key", "")))
        if key_id_for_public_key(public_raw) != key_id:
            raise PactError(f"trusted root public key mismatch for {key_id}", exit_code=4)
        if key_id in result and result[key_id].get("public_key") != root.get("public_key"):
            raise PactError(f"conflicting trusted-root bytes for {key_id}", exit_code=4)
        result[key_id] = root
    return result


# ---------------------------------------------------------------------------
# Secret-hazard scanning
# ---------------------------------------------------------------------------


def _looks_redacted_or_indirect(value: str) -> bool:
    """Recognize common safe placeholders and environment-variable names."""

    stripped = value.strip()
    lowered = stripped.lower()
    if lowered in {"", "redacted", "[redacted]", "<redacted>", "***", "none", "null"}:
        return True
    if ENV_NAME_RE.fullmatch(stripped):
        return True
    if stripped.startswith("${") and stripped.endswith("}") and ENV_NAME_RE.fullmatch(stripped[2:-1]):
        return True
    if stripped.startswith("$") and ENV_NAME_RE.fullmatch(stripped[1:]):
        return True
    return False


def _url_secret_hazards(value: str) -> list[str]:
    """Find credentials or obvious secret query parameters in a URI-like string."""

    hazards: list[str] = []
    try:
        parsed = urllib.parse.urlsplit(value)
    except ValueError:
        return hazards
    if not parsed.scheme:
        return hazards
    if parsed.username is not None or parsed.password is not None:
        hazards.append("credential-bearing URL userinfo")
    query = urllib.parse.parse_qs(parsed.query, keep_blank_values=True)
    for key, values in query.items():
        if key.lower() in SECRET_KEY_NAMES:
            for item in values:
                if not _looks_redacted_or_indirect(item):
                    hazards.append(f"secret-like URL query parameter {key!r}")
                    break
    return hazards


def scan_secret_hazards(value: Any, *, path: str = "$") -> list[str]:
    """Recursively identify obvious immutable-secret hazards.

    The scanner intentionally reports paths and classes, never suspected secret
    values themselves.
    """

    hazards: list[str] = []
    if isinstance(value, dict):
        for raw_key, item in value.items():
            key = str(raw_key)
            child_path = f"{path}.{key}"
            if key.lower() in SECRET_KEY_NAMES and isinstance(item, str):
                if not _looks_redacted_or_indirect(item):
                    hazards.append(f"{child_path}: secret-like field value")
            hazards.extend(scan_secret_hazards(item, path=child_path))
    elif isinstance(value, list):
        for index, item in enumerate(value):
            hazards.extend(scan_secret_hazards(item, path=f"{path}[{index}]"))
    elif isinstance(value, str):
        for label, pattern in SECRET_TEXT_PATTERNS:
            if pattern.search(value):
                hazards.append(f"{path}: {label}")
        for label in _url_secret_hazards(value):
            hazards.append(f"{path}: {label}")
    return sorted(set(hazards))


# ---------------------------------------------------------------------------
# Event and signed-object structural validation
# ---------------------------------------------------------------------------


def _require_exact_keys(obj: Mapping[str, Any], *, required: set[str], optional: set[str], path: str) -> None:
    missing = sorted(required - set(obj))
    extra = sorted(set(obj) - required - optional)
    if missing:
        raise PactError(f"{path}: missing required fields: {', '.join(missing)}", exit_code=2)
    if extra:
        raise PactError(f"{path}: unsupported fields: {', '.join(extra)}", exit_code=2)


def validate_event_ref(value: str, *, allow_local: bool = True, field_name: str = "event reference") -> str:
    if not isinstance(value, str):
        raise PactError(f"{field_name} must be a string", exit_code=2)
    if allow_local and LOCAL_REF_RE.fullmatch(value):
        return value
    if EVENT_REF_RE.fullmatch(value):
        return value
    raise PactError(f"invalid {field_name}: {value!r}", exit_code=2)


def event_ref(commit_id: str, local_id: str) -> str:
    parse_digest(commit_id, field_name="commit ID")
    if not LOCAL_ID_RE.fullmatch(local_id):
        raise PactError(f"invalid local event ID: {local_id!r}", exit_code=2)
    return f"pact:event:{commit_id}#{local_id}"


def parse_event_ref(value: str) -> tuple[str, str]:
    match = EVENT_REF_RE.fullmatch(value)
    if not match:
        raise PactError(f"invalid event reference: {value!r}", exit_code=2)
    return match.group(1), match.group(2)


def validate_evidence(item: Any, *, path: str) -> dict[str, Any]:
    if not isinstance(item, dict):
        raise PactError(f"{path}: evidence entry must be an object", exit_code=2)
    _require_exact_keys(
        item,
        required={"ref", "digest", "media_type", "role"},
        optional={"redacted", "description"},
        path=path,
    )
    ref = item["ref"]
    if not isinstance(ref, str) or not ref or len(ref) > 2048:
        raise PactError(f"{path}.ref: invalid evidence locator", exit_code=2)
    digest = parse_digest(str(item["digest"]), field_name=f"{path}.digest")
    media_type = item["media_type"]
    if not isinstance(media_type, str) or not media_type or len(media_type) > 255:
        raise PactError(f"{path}.media_type: invalid media type", exit_code=2)
    role = item["role"]
    if role not in ALLOWED_EVIDENCE_ROLES:
        raise PactError(f"{path}.role: invalid evidence role {role!r}", exit_code=2)
    result: dict[str, Any] = {
        "ref": unicodedata.normalize("NFC", ref),
        "digest": digest,
        "media_type": unicodedata.normalize("NFC", media_type),
        "role": role,
    }
    if "redacted" in item:
        if not isinstance(item["redacted"], bool):
            raise PactError(f"{path}.redacted: must be boolean", exit_code=2)
        result["redacted"] = item["redacted"]
    if "description" in item:
        description = item["description"]
        if not isinstance(description, str) or len(description) > 512:
            raise PactError(f"{path}.description: invalid description", exit_code=2)
        result["description"] = unicodedata.normalize("NFC", description)
    return result


def normalize_event(item: Any, *, path: str, local_ids: set[str]) -> dict[str, Any]:
    """Validate and normalize one semantic event envelope."""

    if not isinstance(item, dict):
        raise PactError(f"{path}: event must be an object", exit_code=2)
    _require_exact_keys(
        item,
        required={
            "local_id",
            "kind",
            "type",
            "subject",
            "schema_ref",
            "payload",
            "evidence",
            "caused_by",
            "supersedes",
            "tags",
        },
        optional=set(),
        path=path,
    )
    local_id = item["local_id"]
    if not isinstance(local_id, str) or not LOCAL_ID_RE.fullmatch(local_id):
        raise PactError(f"{path}.local_id: invalid local event ID", exit_code=2)
    if local_id in local_ids:
        raise PactError(f"{path}.local_id: duplicate local event ID {local_id!r}", exit_code=2)
    local_ids.add(local_id)

    kind = item["kind"]
    if kind not in ALLOWED_EVENT_KINDS:
        raise PactError(f"{path}.kind: invalid event kind {kind!r}", exit_code=2)
    event_type = item["type"]
    if not isinstance(event_type, str) or not EVENT_TYPE_RE.fullmatch(event_type):
        raise PactError(f"{path}.type: invalid event type {event_type!r}", exit_code=2)
    subject = item["subject"]
    if not isinstance(subject, str) or not subject or len(subject) > 512:
        raise PactError(f"{path}.subject: invalid subject", exit_code=2)
    schema_ref = item["schema_ref"]
    if not isinstance(schema_ref, str) or not (
        DIGEST_RE.fullmatch(schema_ref) or CORE_SCHEMA_RE.fullmatch(schema_ref)
    ):
        raise PactError(f"{path}.schema_ref: invalid schema reference", exit_code=2)
    payload = item["payload"]
    if not isinstance(payload, dict):
        raise PactError(f"{path}.payload: payload must be an object", exit_code=2)

    evidence_raw = item["evidence"]
    if not isinstance(evidence_raw, list):
        raise PactError(f"{path}.evidence: must be an array", exit_code=2)
    evidence = [
        validate_evidence(evidence_item, path=f"{path}.evidence[{index}]")
        for index, evidence_item in enumerate(evidence_raw)
    ]

    caused_raw = item["caused_by"]
    if not isinstance(caused_raw, list):
        raise PactError(f"{path}.caused_by: must be an array", exit_code=2)
    caused_by = sorted(
        {validate_event_ref(str(ref), allow_local=True, field_name=f"{path}.caused_by") for ref in caused_raw}
    )

    supersedes_raw = item["supersedes"]
    if not isinstance(supersedes_raw, list):
        raise PactError(f"{path}.supersedes: must be an array", exit_code=2)
    supersedes = sorted(
        {
            validate_event_ref(str(ref), allow_local=False, field_name=f"{path}.supersedes")
            for ref in supersedes_raw
        }
    )

    tags_raw = item["tags"]
    if not isinstance(tags_raw, list):
        raise PactError(f"{path}.tags: must be an array", exit_code=2)
    tags: list[str] = []
    for tag in tags_raw:
        if not isinstance(tag, str) or not tag or len(tag) > 128:
            raise PactError(f"{path}.tags: invalid tag", exit_code=2)
        tags.append(unicodedata.normalize("NFC", tag))
    tags = sorted(set(tags))

    return {
        "local_id": local_id,
        "kind": kind,
        "type": event_type,
        "subject": unicodedata.normalize("NFC", subject),
        "schema_ref": schema_ref,
        "payload": normalize_value(payload, path=f"{path}.payload"),
        "evidence": evidence,
        "caused_by": caused_by,
        "supersedes": supersedes,
        "tags": tags,
    }


def normalize_event_batch(batch: Any) -> dict[str, Any]:
    """Validate a commit input batch and normalize all semantic events."""

    if not isinstance(batch, dict):
        raise PactError("event batch must be a JSON object", exit_code=2)
    _require_exact_keys(
        batch,
        required={"events"},
        optional={"namespace", "observed_at", "correlation_id", "metadata"},
        path="$",
    )
    events_raw = batch["events"]
    if not isinstance(events_raw, list) or not events_raw:
        raise PactError("event batch must contain at least one event", exit_code=2)
    local_ids: set[str] = set()
    events = [
        normalize_event(item, path=f"$.events[{index}]", local_ids=local_ids)
        for index, item in enumerate(events_raw)
    ]
    events.sort(key=lambda event: event["local_id"])

    # Resolve same-commit references only after all local IDs are known.
    for event in events:
        for reference in event["caused_by"]:
            local_match = LOCAL_REF_RE.fullmatch(reference)
            if local_match and local_match.group(1) not in local_ids:
                raise PactError(
                    f"event {event['local_id']!r} references missing same-commit event {reference!r}",
                    exit_code=2,
                )
            if local_match and local_match.group(1) == event["local_id"]:
                raise PactError(
                    f"event {event['local_id']!r} cannot cause itself",
                    exit_code=2,
                )

    result: dict[str, Any] = {"events": events}
    if "namespace" in batch:
        result["namespace"] = validate_namespace(str(batch["namespace"]))
    if "observed_at" in batch:
        observed_at = batch["observed_at"]
        if not isinstance(observed_at, str) or not observed_at or len(observed_at) > 64:
            raise PactError("observed_at must be a short timestamp string", exit_code=2)
        result["observed_at"] = unicodedata.normalize("NFC", observed_at)
    if "correlation_id" in batch:
        correlation_id = batch["correlation_id"]
        if not isinstance(correlation_id, str) or len(correlation_id) > 255:
            raise PactError("correlation_id must be a string no longer than 255 characters", exit_code=2)
        result["correlation_id"] = unicodedata.normalize("NFC", correlation_id)
    metadata = batch.get("metadata", {})
    if not isinstance(metadata, dict):
        raise PactError("metadata must be an object", exit_code=2)
    result["metadata"] = normalize_value(metadata, path="$.metadata")

    hazards = scan_secret_hazards(result)
    if hazards:
        raise PactError(
            "refusing to sign immutable secret-like material",
            exit_code=7,
            details={"hazards": hazards},
        )
    return result


def normalize_authority(
    *, delegation_ref_value: str | None, epoch: str | None, lease_ref_value: str | None
) -> dict[str, Any]:
    """Build the fixed authority-hint envelope used by commit bodies."""

    delegation_ref = (
        validate_event_ref(delegation_ref_value, allow_local=False, field_name="delegation reference")
        if delegation_ref_value
        else None
    )
    lease_ref = (
        validate_event_ref(lease_ref_value, allow_local=False, field_name="lease reference")
        if lease_ref_value
        else None
    )
    if epoch is not None:
        if not isinstance(epoch, str) or not epoch or len(epoch) > 255:
            raise PactError("authority epoch must be a nonempty short string", exit_code=2)
        epoch = unicodedata.normalize("NFC", epoch)
    return {
        "delegation_ref": delegation_ref,
        "epoch": epoch,
        "lease_ref": lease_ref,
    }


def validate_commit_body(body: Any) -> None:
    """Validate a parsed commit body without mutating it."""

    if not isinstance(body, dict):
        raise PactError("commit body must be an object", exit_code=4)
    _require_exact_keys(
        body,
        required={"namespace", "parents", "actor", "authority", "observed_at", "metadata", "events"},
        optional={"correlation_id"},
        path="$.body",
    )
    namespace = validate_namespace(str(body["namespace"]))
    if namespace != body["namespace"]:
        raise PactError("commit namespace is not canonical", exit_code=4)
    parents = body["parents"]
    if not isinstance(parents, list):
        raise PactError("commit parents must be an array", exit_code=4)
    normalized_parents = sorted({parse_digest(str(parent), field_name="parent ID") for parent in parents})
    if parents != normalized_parents:
        raise PactError("commit parents are not sorted unique canonical IDs", exit_code=4)
    actor = body["actor"]
    if not isinstance(actor, dict):
        raise PactError("commit actor must be an object", exit_code=4)
    _require_exact_keys(actor, required={"key_id", "label"}, optional=set(), path="$.body.actor")
    validate_key_id(str(actor["key_id"]))
    if not isinstance(actor["label"], str) or not actor["label"]:
        raise PactError("commit actor label is invalid", exit_code=4)
    authority = body["authority"]
    if not isinstance(authority, dict):
        raise PactError("commit authority must be an object", exit_code=4)
    _require_exact_keys(
        authority,
        required={"delegation_ref", "epoch", "lease_ref"},
        optional=set(),
        path="$.body.authority",
    )
    if authority["delegation_ref"] is not None:
        validate_event_ref(str(authority["delegation_ref"]), allow_local=False)
    if authority["lease_ref"] is not None:
        validate_event_ref(str(authority["lease_ref"]), allow_local=False)
    if authority["epoch"] is not None and not isinstance(authority["epoch"], str):
        raise PactError("authority epoch must be string or null", exit_code=4)
    if not isinstance(body["observed_at"], str) or not body["observed_at"]:
        raise PactError("commit observed_at is invalid", exit_code=4)
    if not isinstance(body["metadata"], dict):
        raise PactError("commit metadata must be an object", exit_code=4)
    if "correlation_id" in body and not isinstance(body["correlation_id"], str):
        raise PactError("commit correlation_id must be a string", exit_code=4)

    batch = {
        "events": body["events"],
        "metadata": body["metadata"],
    }
    normalized = normalize_event_batch(batch)
    if normalized["events"] != body["events"]:
        raise PactError("commit events are not in canonical normalized form", exit_code=4)


def validate_checkpoint_body(body: Any) -> None:
    """Validate a parsed checkpoint body without mutating it."""

    if not isinstance(body, dict):
        raise PactError("checkpoint body must be an object", exit_code=4)
    _require_exact_keys(
        body,
        required={
            "scope",
            "frontier",
            "policy_ref",
            "schema_refs",
            "authority_epoch",
            "previous_checkpoint",
            "actor",
            "observed_at",
            "metadata",
        },
        optional=set(),
        path="$.body",
    )
    scope = validate_namespace(str(body["scope"]))
    if scope != body["scope"]:
        raise PactError("checkpoint scope is not canonical", exit_code=4)
    frontier = body["frontier"]
    if not isinstance(frontier, list) or not frontier:
        raise PactError("checkpoint frontier must contain at least one namespace", exit_code=4)
    seen_namespaces: set[str] = set()
    normalized_frontier: list[dict[str, Any]] = []
    for index, entry in enumerate(frontier):
        if not isinstance(entry, dict):
            raise PactError(f"checkpoint frontier[{index}] must be an object", exit_code=4)
        _require_exact_keys(
            entry,
            required={"namespace", "heads"},
            optional=set(),
            path=f"$.body.frontier[{index}]",
        )
        namespace = validate_namespace(str(entry["namespace"]))
        if namespace in seen_namespaces:
            raise PactError(f"duplicate checkpoint namespace: {namespace}", exit_code=4)
        seen_namespaces.add(namespace)
        heads = entry["heads"]
        if not isinstance(heads, list) or not heads:
            raise PactError(f"checkpoint namespace {namespace} has no heads", exit_code=4)
        normalized_heads = sorted({parse_digest(str(head), field_name="checkpoint head") for head in heads})
        if heads != normalized_heads:
            raise PactError(f"checkpoint heads for {namespace} are not canonical", exit_code=4)
        normalized_frontier.append({"namespace": namespace, "heads": normalized_heads})
    normalized_frontier.sort(key=lambda entry: entry["namespace"])
    if frontier != normalized_frontier:
        raise PactError("checkpoint frontier is not sorted by namespace", exit_code=4)
    parse_digest(str(body["policy_ref"]), field_name="policy reference")
    schema_refs = body["schema_refs"]
    if not isinstance(schema_refs, list):
        raise PactError("checkpoint schema_refs must be an array", exit_code=4)
    normalized_schemas = sorted({parse_digest(str(item), field_name="schema reference") for item in schema_refs})
    if schema_refs != normalized_schemas:
        raise PactError("checkpoint schema_refs are not canonical", exit_code=4)
    if not isinstance(body["authority_epoch"], str) or not body["authority_epoch"]:
        raise PactError("checkpoint authority_epoch is invalid", exit_code=4)
    if body["previous_checkpoint"] is not None:
        parse_digest(str(body["previous_checkpoint"]), field_name="previous checkpoint")
    actor = body["actor"]
    if not isinstance(actor, dict):
        raise PactError("checkpoint actor must be an object", exit_code=4)
    _require_exact_keys(actor, required={"key_id", "label"}, optional=set(), path="$.body.actor")
    validate_key_id(str(actor["key_id"]))
    if not isinstance(actor["label"], str) or not actor["label"]:
        raise PactError("checkpoint actor label is invalid", exit_code=4)
    if not isinstance(body["observed_at"], str) or not body["observed_at"]:
        raise PactError("checkpoint observed_at is invalid", exit_code=4)
    if not isinstance(body["metadata"], dict):
        raise PactError("checkpoint metadata must be an object", exit_code=4)


def validate_signed_object(obj: Any) -> tuple[str, str | None]:
    """Validate a commit/checkpoint envelope and return `(type, namespace/scope)`."""

    if not isinstance(obj, dict):
        raise PactError("canonical object must be a JSON object", exit_code=4)
    _require_exact_keys(
        obj,
        required={"format", "body", "body_digest", "signature"},
        optional=set(),
        path="$",
    )
    object_format = obj.get("format")
    if object_format == COMMIT_FORMAT:
        validate_commit_body(obj.get("body"))
        return "commit", str(obj["body"]["namespace"])
    if object_format == CHECKPOINT_FORMAT:
        validate_checkpoint_body(obj.get("body"))
        return "checkpoint", str(obj["body"]["scope"])
    raise PactError(f"unsupported signed object format: {object_format!r}", exit_code=4)


# ---------------------------------------------------------------------------
# Object verification, DAG helpers, and event lookup
# ---------------------------------------------------------------------------


def verify_object_file(store: Path, path: Path) -> ObjectVerification:
    """Verify exact bytes, canonical form, structure, body digest, and signature."""

    try:
        expected_id = object_id_from_path(store, path)
    except PactError as exc:
        return ObjectVerification(
            object_id="unknown",
            path=str(path),
            errors=[exc.message],
        )

    result = ObjectVerification(object_id=expected_id, path=str(path))
    try:
        raw = path.read_bytes()
    except OSError as exc:
        result.errors.append(f"cannot read object: {exc}")
        return result

    actual_id = sha256_digest(raw)
    if actual_id != expected_id:
        result.errors.append(f"object digest mismatch: path says {expected_id}, bytes say {actual_id}")
        return result

    try:
        obj = parse_json_bytes(raw, source=str(path))
    except PactError as exc:
        result.errors.append(exc.message)
        return result

    try:
        if canonical_bytes(obj) != raw:
            result.errors.append("object bytes are not canonical pact-json-v1")
            return result
    except PactError as exc:
        result.errors.append(exc.message)
        return result

    result.integrity = "valid"
    result.obj = obj if isinstance(obj, dict) else None

    try:
        object_type, namespace = validate_signed_object(obj)
        result.object_type = object_type
        result.namespace = namespace
    except PactError as exc:
        result.errors.append(exc.message)
        return result

    signature_errors = verify_signature_envelope(obj)
    if signature_errors:
        result.errors.extend(signature_errors)
        result.authenticity = "invalid"
    else:
        result.authenticity = "valid"
    return result


def scan_verified_objects(store: Path) -> dict[str, ObjectVerification]:
    """Verify every canonical path and return results keyed by expected object ID."""

    results: dict[str, ObjectVerification] = {}
    for path in iter_object_paths(store):
        verification = verify_object_file(store, path)
        if verification.object_id in results:
            # This should be impossible under the canonical layout, but report it
            # rather than allowing one path to hide another.
            verification.errors.append("duplicate canonical object ID path")
        results[verification.object_id] = verification
    return results


def commit_objects(
    verifications: Mapping[str, ObjectVerification], *, require_valid: bool = True
) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for object_id, verification in verifications.items():
        if verification.object_type != "commit" or verification.obj is None:
            continue
        if require_valid and not verification.structurally_valid:
            continue
        result[object_id] = verification.obj
    return result


def checkpoint_objects(
    verifications: Mapping[str, ObjectVerification], *, require_valid: bool = True
) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for object_id, verification in verifications.items():
        if verification.object_type != "checkpoint" or verification.obj is None:
            continue
        if require_valid and not verification.structurally_valid:
            continue
        result[object_id] = verification.obj
    return result


def build_event_map(commits: Mapping[str, Mapping[str, Any]]) -> dict[str, dict[str, Any]]:
    """Index stable event references to their commit and event envelopes."""

    events: dict[str, dict[str, Any]] = {}
    for commit_id, commit_obj in commits.items():
        body = commit_obj["body"]
        for event in body["events"]:
            reference = event_ref(commit_id, event["local_id"])
            events[reference] = {
                "commit_id": commit_id,
                "namespace": body["namespace"],
                "actor": body["actor"],
                "authority": body["authority"],
                "event": event,
            }
    return events


def parent_map(commits: Mapping[str, Mapping[str, Any]]) -> dict[str, list[str]]:
    return {commit_id: list(obj["body"]["parents"]) for commit_id, obj in commits.items()}


def detect_cycles(parents: Mapping[str, Sequence[str]]) -> list[list[str]]:
    """Return any cycles found by depth-first search over known commits."""

    WHITE, GRAY, BLACK = 0, 1, 2
    color: dict[str, int] = defaultdict(int)
    stack: list[str] = []
    cycles: list[list[str]] = []

    def visit(node: str) -> None:
        color[node] = GRAY
        stack.append(node)
        for parent in parents.get(node, []):
            if parent not in parents:
                continue
            if color[parent] == WHITE:
                visit(parent)
            elif color[parent] == GRAY:
                try:
                    index = stack.index(parent)
                    cycles.append(stack[index:] + [parent])
                except ValueError:
                    cycles.append([node, parent, node])
        stack.pop()
        color[node] = BLACK

    for node in sorted(parents):
        if color[node] == WHITE:
            visit(node)
    return cycles


def is_ancestor(ancestor: str, descendant: str, parents: Mapping[str, Sequence[str]]) -> bool:
    """Return whether `ancestor` is reachable through descendant's parent links."""

    if ancestor == descendant:
        return True
    queue: deque[str] = deque(parents.get(descendant, []))
    seen: set[str] = set()
    while queue:
        current = queue.popleft()
        if current == ancestor:
            return True
        if current in seen:
            continue
        seen.add(current)
        queue.extend(parents.get(current, []))
    return False


def ancestors_of(commit_id: str, parents: Mapping[str, Sequence[str]]) -> set[str]:
    """Return all known causal ancestors of a commit, excluding the commit itself."""

    result: set[str] = set()
    queue: deque[str] = deque(parents.get(commit_id, []))
    while queue:
        current = queue.popleft()
        if current in result:
            continue
        result.add(current)
        queue.extend(parents.get(current, []))
    return result


def compute_heads(commits: Mapping[str, Mapping[str, Any]], namespace: str | None = None) -> dict[str, list[str]]:
    """Compute current local heads per exact namespace from canonical commits."""

    by_namespace: dict[str, set[str]] = defaultdict(set)
    referenced_as_parent: dict[str, set[str]] = defaultdict(set)
    for commit_id, obj in commits.items():
        ns = obj["body"]["namespace"]
        if namespace is not None and ns != namespace:
            continue
        by_namespace[ns].add(commit_id)
        for parent in obj["body"]["parents"]:
            referenced_as_parent[ns].add(parent)
    return {
        ns: sorted(ids - referenced_as_parent.get(ns, set()))
        for ns, ids in sorted(by_namespace.items())
    }


def namespace_in_scope(namespace: str, scope: str) -> bool:
    return namespace == scope or namespace.startswith(scope + "/")


# ---------------------------------------------------------------------------
# Conservative authority evaluation
# ---------------------------------------------------------------------------


def match_namespace_pattern(pattern: str, namespace: str) -> bool:
    """Evaluate the deterministic v1 namespace wildcard syntax."""

    if pattern.endswith("/**"):
        base = pattern[:-3]
        return namespace == base or namespace.startswith(base + "/")
    if pattern.endswith("/*"):
        base = pattern[:-2]
        if not namespace.startswith(base + "/"):
            return False
        remainder = namespace[len(base) + 1 :]
        return bool(remainder) and "/" not in remainder
    return namespace == pattern


def match_event_type_pattern(pattern: str, event_type: str) -> bool:
    """Evaluate exact, `prefix.*`, or global `*` event-type capabilities."""

    if pattern == "*":
        return True
    if pattern.endswith(".*"):
        base = pattern[:-2]
        return event_type == base or event_type.startswith(base + ".")
    return event_type == pattern


def required_control_capabilities(events: Sequence[Mapping[str, Any]]) -> set[str]:
    """Map reserved control events to additional delegation capabilities."""

    required: set[str] = {"commit"}
    for event in events:
        event_type = event.get("type")
        if event_type == "pact.authority.delegated":
            required.add("delegate")
        elif event_type == "pact.authority.revoked":
            required.add("revoke")
        elif event_type == "pact.authority.epoch_advanced":
            required.add("advance_epoch")
        elif event_type == "pact.schema.registered":
            required.add("register_schema")
        elif event_type == "pact.policy.registered":
            required.add("register_policy")
        elif event_type == "pact.policy.activated":
            required.add("activate_policy")
    return required


def _parse_advisory_time(value: Any) -> dt.datetime | None:
    if not isinstance(value, str):
        return None
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=dt.timezone.utc)
    return parsed.astimezone(dt.timezone.utc)


def evaluate_authorization_all(
    *,
    store: Path,
    commits: Mapping[str, Mapping[str, Any]],
    events: Mapping[str, Mapping[str, Any]],
    parents: Mapping[str, Sequence[str]],
) -> dict[str, AuthorizationResult]:
    """Evaluate available root/delegation proofs conservatively.

    This evaluator deliberately labels missing or policy-dependent evidence as
    indeterminate. It verifies causal scope and known revocations but does not
    claim trusted wall-clock lease enforcement.
    """

    roots = trust_root_map(store)
    memo: dict[str, AuthorizationResult] = {}
    evaluating: set[str] = set()

    def evaluate(commit_id: str) -> AuthorizationResult:
        if commit_id in memo:
            return memo[commit_id]
        if commit_id in evaluating:
            result = AuthorizationResult("indeterminate", ["authorization recursion cycle"])
            memo[commit_id] = result
            return result
        evaluating.add(commit_id)
        obj = commits.get(commit_id)
        if obj is None:
            result = AuthorizationResult("indeterminate", ["commit not available"])
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result

        body = obj["body"]
        actor_key_id = body["actor"]["key_id"]
        signature_public = obj["signature"]["public_key"]
        trusted_root = roots.get(actor_key_id)
        if trusted_root is not None:
            if trusted_root.get("public_key") == signature_public:
                result = AuthorizationResult(
                    "authorized",
                    ["signer is a locally bootstrapped trusted root"],
                    [actor_key_id],
                    depth=0,
                )
            else:
                result = AuthorizationResult(
                    "unauthorized",
                    ["trusted-root key ID has conflicting public bytes"],
                    [actor_key_id],
                )
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result

        authority = body.get("authority", {})
        delegation_ref = authority.get("delegation_ref")
        if not delegation_ref:
            result = AuthorizationResult(
                "indeterminate",
                ["signer is not a trusted root and no delegation reference was supplied"],
                [actor_key_id],
            )
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result

        delegation_entry = events.get(delegation_ref)
        if delegation_entry is None:
            result = AuthorizationResult(
                "indeterminate",
                [f"delegation event is unavailable: {delegation_ref}"],
                [actor_key_id],
            )
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result
        delegation_event = delegation_entry["event"]
        delegation_commit_id = delegation_entry["commit_id"]
        if delegation_event.get("type") != "pact.authority.delegated":
            result = AuthorizationResult(
                "unauthorized",
                ["referenced delegation event has the wrong event type"],
                [actor_key_id, delegation_ref],
            )
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result
        if not any(is_ancestor(delegation_commit_id, parent, parents) for parent in body["parents"]):
            result = AuthorizationResult(
                "unauthorized",
                ["delegation is not causally prior to the delegated commit"],
                [actor_key_id, delegation_ref],
            )
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result

        payload = delegation_event.get("payload")
        if not isinstance(payload, dict):
            result = AuthorizationResult(
                "unauthorized",
                ["delegation payload is not an object"],
                [actor_key_id, delegation_ref],
            )
            memo[commit_id] = result
            evaluating.discard(commit_id)
            return result

        reasons: list[str] = []
        if payload.get("delegate_key_id") != actor_key_id:
            reasons.append("delegation target key does not match signer")

        namespace_patterns = payload.get("namespace_patterns")
        if not isinstance(namespace_patterns, list) or not any(
            isinstance(pattern, str) and match_namespace_pattern(pattern, body["namespace"])
            for pattern in namespace_patterns
        ):
            reasons.append("delegation does not cover commit namespace")

        event_type_patterns = payload.get("event_type_patterns")
        event_types = [event["type"] for event in body["events"]]
        if not isinstance(event_type_patterns, list):
            reasons.append("delegation event-type patterns are missing")
        else:
            for event_type in event_types:
                if not any(
                    isinstance(pattern, str) and match_event_type_pattern(pattern, event_type)
                    for pattern in event_type_patterns
                ):
                    reasons.append(f"delegation does not cover event type {event_type}")

        delegated_epoch = payload.get("epoch")
        commit_epoch = authority.get("epoch")
        if delegated_epoch is not None and commit_epoch != delegated_epoch:
            reasons.append("commit authority epoch does not match delegation epoch")

        capabilities = payload.get("capabilities")
        required_capabilities = required_control_capabilities(body["events"])
        if not isinstance(capabilities, list):
            reasons.append("delegation capabilities are missing")
        else:
            missing_capabilities = sorted(required_capabilities - set(capabilities))
            if missing_capabilities:
                reasons.append(
                    "delegation lacks control capabilities: " + ", ".join(missing_capabilities)
                )

        # Delegating authority requires both the explicit `delegate` capability
        # and remaining subdelegation depth. A child may never grant more depth
        # than its parent delegation retained.
        child_delegations = [
            event
            for event in body["events"]
            if event.get("type") == "pact.authority.delegated"
        ]
        if child_delegations:
            allow_subdelegation = payload.get("allow_subdelegation")
            max_depth = payload.get("max_subdelegation_depth")
            if allow_subdelegation is not True:
                reasons.append("delegation forbids subdelegation")
            if not isinstance(max_depth, int) or isinstance(max_depth, bool) or max_depth < 1:
                reasons.append("delegation has no remaining subdelegation depth")
            else:
                for child_event in child_delegations:
                    child_payload = child_event.get("payload")
                    if not isinstance(child_payload, dict):
                        reasons.append("child delegation payload is not an object")
                        continue
                    child_depth = child_payload.get("max_subdelegation_depth")
                    if (
                        not isinstance(child_depth, int)
                        or isinstance(child_depth, bool)
                        or child_depth < 0
                    ):
                        reasons.append("child delegation has invalid subdelegation depth")
                    elif child_depth > max_depth - 1:
                        reasons.append(
                            "child delegation exceeds remaining subdelegation depth"
                        )
                    if child_payload.get("allow_subdelegation") is True and child_depth == 0:
                        reasons.append(
                            "child delegation permits subdelegation with zero remaining depth"
                        )

        # A revocation in the commit's known ancestry is causally prior and can
        # therefore be enforced without relying on wall-clock time.
        for ancestor_id in ancestors_of(commit_id, parents):
            ancestor_obj = commits.get(ancestor_id)
            if ancestor_obj is None:
                continue
            for ancestor_event in ancestor_obj["body"]["events"]:
                if ancestor_event.get("type") != "pact.authority.revoked":
                    continue
                revoke_payload = ancestor_event.get("payload", {})
                if not isinstance(revoke_payload, dict):
                    continue
                if (
                    revoke_payload.get("target_delegation_ref") == delegation_ref
                    or revoke_payload.get("target_key_id") == actor_key_id
                ):
                    reasons.append("delegation/key was causally revoked before this commit")

        issuer_result = evaluate(delegation_commit_id)
        if issuer_result.status != "authorized":
            reasons.append(
                f"delegation issuer is not authorized: {issuer_result.status}"
            )

        # Lease comparison is explicitly advisory because actor timestamps are
        # not trusted-time proofs. Report it separately instead of silently
        # converting it into hard authorization.
        lease_status = "not_applicable"
        lease = payload.get("lease")
        if isinstance(lease, dict):
            commit_time = _parse_advisory_time(body.get("observed_at"))
            not_before = _parse_advisory_time(lease.get("not_before"))
            not_after = _parse_advisory_time(lease.get("not_after"))
            if commit_time is None:
                lease_status = "advisory_unparseable"
            elif not_before and commit_time < not_before:
                lease_status = "advisory_before_lease"
            elif not_after and commit_time > not_after:
                lease_status = "advisory_after_lease"
            else:
                lease_status = "advisory_within_lease"

        if reasons:
            status = "unauthorized" if any(
                reason
                for reason in reasons
                if not reason.startswith("delegation issuer is not authorized: indeterminate")
            ) else "indeterminate"
        else:
            status = "authorized"
        result = AuthorizationResult(
            status,
            reasons or ["delegation chain and scope are structurally valid"],
            issuer_result.chain + [delegation_ref, actor_key_id],
            lease_status=lease_status,
            depth=issuer_result.depth + 1,
        )
        memo[commit_id] = result
        evaluating.discard(commit_id)
        return result

    for commit_id in sorted(commits):
        evaluate(commit_id)
    return memo


# ---------------------------------------------------------------------------
# Full-store verification
# ---------------------------------------------------------------------------


def verify_store(repo: Path, *, strict: bool = False) -> dict[str, Any]:
    """Verify canonical objects, DAG structure, refs, checkpoints, and authority."""

    store = ensure_store(repo)
    verifications = scan_verified_objects(store)
    commits = commit_objects(verifications, require_valid=True)
    checkpoints = checkpoint_objects(verifications, require_valid=True)
    events = build_event_map(commits)
    parents = parent_map(commits)

    errors: list[str] = []
    warnings: list[str] = []
    for object_id, verification in sorted(verifications.items()):
        errors.extend(f"{object_id}: {error}" for error in verification.errors)
        warnings.extend(f"{object_id}: {warning}" for warning in verification.warnings)

    # Parent existence and namespace compatibility are structural DAG checks.
    for commit_id, obj in sorted(commits.items()):
        namespace = obj["body"]["namespace"]
        for parent_id in obj["body"]["parents"]:
            parent_obj = commits.get(parent_id)
            if parent_obj is None:
                message = f"{commit_id}: missing or invalid parent {parent_id}"
                (errors if strict else warnings).append(message)
                continue
            if parent_obj["body"]["namespace"] != namespace:
                errors.append(
                    f"{commit_id}: parent {parent_id} belongs to different namespace "
                    f"{parent_obj['body']['namespace']!r}"
                )

    for cycle in detect_cycles(parents):
        errors.append("commit DAG cycle: " + " -> ".join(cycle))

    # Validate external event references. Same-commit local references were
    # already checked during structural event validation.
    for commit_id, obj in sorted(commits.items()):
        for event in obj["body"]["events"]:
            source_ref = event_ref(commit_id, event["local_id"])
            for field_name in ("caused_by", "supersedes"):
                for reference in event[field_name]:
                    if LOCAL_REF_RE.fullmatch(reference):
                        continue
                    if reference not in events:
                        message = f"{source_ref}: unresolved {field_name} reference {reference}"
                        (errors if strict else warnings).append(message)

    # Checkpoint frontiers must refer to valid commits in matching namespaces.
    for checkpoint_id, checkpoint_obj in sorted(checkpoints.items()):
        body = checkpoint_obj["body"]
        for frontier_entry in body["frontier"]:
            namespace = frontier_entry["namespace"]
            for head_id in frontier_entry["heads"]:
                head_obj = commits.get(head_id)
                if head_obj is None:
                    errors.append(f"{checkpoint_id}: missing checkpoint head {head_id}")
                elif head_obj["body"]["namespace"] != namespace:
                    errors.append(
                        f"{checkpoint_id}: head {head_id} namespace mismatch "
                        f"({head_obj['body']['namespace']!r} != {namespace!r})"
                    )
        previous = body.get("previous_checkpoint")
        if previous is not None and previous not in checkpoints:
            message = f"{checkpoint_id}: previous checkpoint is unavailable: {previous}"
            (errors if strict else warnings).append(message)

    authorization: dict[str, AuthorizationResult] = {}
    if commits:
        try:
            authorization = evaluate_authorization_all(
                store=store,
                commits=commits,
                events=events,
                parents=parents,
            )
        except PactError as exc:
            errors.append(f"authority evaluation failed: {exc.message}")

    auth_counts: dict[str, int] = defaultdict(int)
    for result in authorization.values():
        auth_counts[result.status] += 1

    # The index is disposable, but report whether it exists. Deep index
    # equivalence is established by `reindex`, which is safe to run separately.
    index_path = store / "index" / "pact.sqlite3"
    index_status = "present" if index_path.is_file() else "missing"

    return {
        "operation": "verify",
        "repo": str(repo),
        "store": str(store),
        "strict": strict,
        "ok": not errors,
        "counts": {
            "objects": len(verifications),
            "commits": len(commits),
            "checkpoints": len(checkpoints),
            "events": len(events),
            "authorized": auth_counts.get("authorized", 0),
            "unauthorized": auth_counts.get("unauthorized", 0),
            "indeterminate": auth_counts.get("indeterminate", 0),
        },
        "heads": compute_heads(commits),
        "index_status": index_status,
        "errors": errors,
        "warnings": warnings,
        "authorization": {
            commit_id: {
                "status": result.status,
                "reasons": result.reasons,
                "chain": result.chain,
                "lease_status": result.lease_status,
                "depth": result.depth,
            }
            for commit_id, result in sorted(authorization.items())
        },
        "objects": {
            object_id: {
                "type": verification.object_type,
                "namespace": verification.namespace,
                "integrity": verification.integrity,
                "authenticity": verification.authenticity,
                "errors": verification.errors,
                "warnings": verification.warnings,
                "path": verification.path,
            }
            for object_id, verification in sorted(verifications.items())
        },
    }


# ---------------------------------------------------------------------------
# SQLite index
# ---------------------------------------------------------------------------


def index_path(store: Path) -> Path:
    return store / "index" / "pact.sqlite3"


def create_index_schema(connection: sqlite3.Connection) -> None:
    """Create the disposable query index schema."""

    connection.executescript(
        """
        PRAGMA foreign_keys = ON;

        CREATE TABLE objects (
            object_id TEXT PRIMARY KEY,
            object_type TEXT NOT NULL,
            format TEXT NOT NULL,
            namespace TEXT NOT NULL,
            actor_key_id TEXT NOT NULL,
            actor_label TEXT NOT NULL,
            observed_at TEXT NOT NULL,
            body_digest TEXT NOT NULL,
            path TEXT NOT NULL
        );

        CREATE TABLE commits (
            commit_id TEXT PRIMARY KEY REFERENCES objects(object_id) ON DELETE CASCADE,
            correlation_id TEXT
        );

        CREATE TABLE parents (
            commit_id TEXT NOT NULL REFERENCES commits(commit_id) ON DELETE CASCADE,
            parent_id TEXT NOT NULL,
            PRIMARY KEY (commit_id, parent_id)
        );

        CREATE TABLE events (
            event_ref TEXT PRIMARY KEY,
            commit_id TEXT NOT NULL REFERENCES commits(commit_id) ON DELETE CASCADE,
            local_id TEXT NOT NULL,
            kind TEXT NOT NULL,
            event_type TEXT NOT NULL,
            subject TEXT NOT NULL,
            schema_ref TEXT NOT NULL,
            payload_json TEXT NOT NULL,
            caused_by_json TEXT NOT NULL,
            supersedes_json TEXT NOT NULL,
            tags_json TEXT NOT NULL,
            UNIQUE (commit_id, local_id)
        );

        CREATE TABLE evidence (
            event_ref TEXT NOT NULL REFERENCES events(event_ref) ON DELETE CASCADE,
            ordinal INTEGER NOT NULL,
            ref TEXT NOT NULL,
            digest TEXT NOT NULL,
            media_type TEXT NOT NULL,
            role TEXT NOT NULL,
            redacted INTEGER,
            description TEXT,
            PRIMARY KEY (event_ref, ordinal)
        );

        CREATE TABLE checkpoints (
            checkpoint_id TEXT PRIMARY KEY REFERENCES objects(object_id) ON DELETE CASCADE,
            scope TEXT NOT NULL,
            policy_ref TEXT NOT NULL,
            authority_epoch TEXT NOT NULL,
            previous_checkpoint TEXT,
            schema_refs_json TEXT NOT NULL,
            metadata_json TEXT NOT NULL
        );

        CREATE TABLE checkpoint_heads (
            checkpoint_id TEXT NOT NULL REFERENCES checkpoints(checkpoint_id) ON DELETE CASCADE,
            namespace TEXT NOT NULL,
            head_id TEXT NOT NULL,
            PRIMARY KEY (checkpoint_id, namespace, head_id)
        );

        CREATE INDEX events_type_idx ON events(event_type);
        CREATE INDEX events_subject_idx ON events(subject);
        CREATE INDEX events_commit_idx ON events(commit_id);
        CREATE INDEX objects_namespace_idx ON objects(namespace);
        CREATE INDEX objects_actor_idx ON objects(actor_key_id);
        CREATE INDEX parents_parent_idx ON parents(parent_id);
        """
    )


def rebuild_index(repo: Path) -> dict[str, Any]:
    """Destroy and reconstruct SQLite solely from valid canonical objects."""

    store = ensure_store(repo)
    verifications = scan_verified_objects(store)
    invalid = [
        f"{object_id}: {', '.join(result.errors)}"
        for object_id, result in verifications.items()
        if not result.structurally_valid
    ]
    if invalid:
        raise PactError(
            "cannot rebuild index while canonical objects fail verification",
            exit_code=4,
            details={"objects": invalid},
        )

    db_path = index_path(store)
    db_path.parent.mkdir(parents=True, exist_ok=True)
    temp_path = db_path.with_suffix(".sqlite3.tmp")
    temp_path.unlink(missing_ok=True)
    connection = sqlite3.connect(temp_path)
    try:
        create_index_schema(connection)
        with connection:
            for object_id, verification in sorted(verifications.items()):
                assert verification.obj is not None
                obj = verification.obj
                body = obj["body"]
                connection.execute(
                    """
                    INSERT INTO objects (
                        object_id, object_type, format, namespace, actor_key_id,
                        actor_label, observed_at, body_digest, path
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        object_id,
                        verification.object_type,
                        obj["format"],
                        verification.namespace,
                        body["actor"]["key_id"],
                        body["actor"]["label"],
                        body["observed_at"],
                        obj["body_digest"],
                        verification.path,
                    ),
                )
                if verification.object_type == "commit":
                    connection.execute(
                        "INSERT INTO commits (commit_id, correlation_id) VALUES (?, ?)",
                        (object_id, body.get("correlation_id")),
                    )
                    for parent in body["parents"]:
                        connection.execute(
                            "INSERT INTO parents (commit_id, parent_id) VALUES (?, ?)",
                            (object_id, parent),
                        )
                    for event in body["events"]:
                        reference = event_ref(object_id, event["local_id"])
                        connection.execute(
                            """
                            INSERT INTO events (
                                event_ref, commit_id, local_id, kind, event_type,
                                subject, schema_ref, payload_json, caused_by_json,
                                supersedes_json, tags_json
                            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                            """,
                            (
                                reference,
                                object_id,
                                event["local_id"],
                                event["kind"],
                                event["type"],
                                event["subject"],
                                event["schema_ref"],
                                canonical_bytes(event["payload"]).decode("utf-8"),
                                canonical_bytes(event["caused_by"]).decode("utf-8"),
                                canonical_bytes(event["supersedes"]).decode("utf-8"),
                                canonical_bytes(event["tags"]).decode("utf-8"),
                            ),
                        )
                        for ordinal, evidence in enumerate(event["evidence"]):
                            connection.execute(
                                """
                                INSERT INTO evidence (
                                    event_ref, ordinal, ref, digest, media_type,
                                    role, redacted, description
                                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                                """,
                                (
                                    reference,
                                    ordinal,
                                    evidence["ref"],
                                    evidence["digest"],
                                    evidence["media_type"],
                                    evidence["role"],
                                    None if "redacted" not in evidence else int(evidence["redacted"]),
                                    evidence.get("description"),
                                ),
                            )
                elif verification.object_type == "checkpoint":
                    connection.execute(
                        """
                        INSERT INTO checkpoints (
                            checkpoint_id, scope, policy_ref, authority_epoch,
                            previous_checkpoint, schema_refs_json, metadata_json
                        ) VALUES (?, ?, ?, ?, ?, ?, ?)
                        """,
                        (
                            object_id,
                            body["scope"],
                            body["policy_ref"],
                            body["authority_epoch"],
                            body["previous_checkpoint"],
                            canonical_bytes(body["schema_refs"]).decode("utf-8"),
                            canonical_bytes(body["metadata"]).decode("utf-8"),
                        ),
                    )
                    for entry in body["frontier"]:
                        for head in entry["heads"]:
                            connection.execute(
                                """
                                INSERT INTO checkpoint_heads (checkpoint_id, namespace, head_id)
                                VALUES (?, ?, ?)
                                """,
                                (object_id, entry["namespace"], head),
                            )
        connection.execute("PRAGMA optimize")
    finally:
        connection.close()

    os.replace(temp_path, db_path)
    return {
        "operation": "reindex",
        "repo": str(repo),
        "index": str(db_path),
        "objects": len(verifications),
        "commits": sum(1 for result in verifications.values() if result.object_type == "commit"),
        "checkpoints": sum(
            1 for result in verifications.values() if result.object_type == "checkpoint"
        ),
    }


def ensure_index(repo: Path) -> Path:
    store = ensure_store(repo)
    db_path = index_path(store)
    if not db_path.is_file():
        rebuild_index(repo)
    return db_path


# ---------------------------------------------------------------------------
# Command implementations
# ---------------------------------------------------------------------------


def command_init(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    repo.mkdir(parents=True, exist_ok=True)
    store = store_path(repo)
    if store.exists() and any(store.iterdir()):
        raise PactError(f"refusing to overwrite existing PACT store: {store}", exit_code=3)
    namespace = validate_namespace(args.namespace)
    (store / "objects" / "sha256").mkdir(parents=True, exist_ok=True)
    (store / "index").mkdir(parents=True, exist_ok=True)
    (store / "refs").mkdir(parents=True, exist_ok=True)
    (store / "tmp").mkdir(parents=True, exist_ok=True)
    write_config_json(
        store / "format.json",
        {
            "format": STORE_FORMAT,
            "default_namespace": namespace,
            "created_at": utc_now(),
            "canonicalization": CANONICALIZATION,
            "hash_algorithm": "sha256",
            "signature_algorithm": "ed25519",
        },
    )
    write_config_json(store / "trust.json", {"format": TRUST_FORMAT, "roots": []})
    # `.gitignore` is plain text, not JSON. Keep immutable objects visible to
    # project policy while ignoring only disposable local state.
    _atomic_write(store / ".gitignore", b"index/\ntmp/\nrefs/\n", mode=0o644)
    index_result = rebuild_index(repo)
    return {
        "operation": "init",
        "store": str(store),
        "default_namespace": namespace,
        "format": STORE_FORMAT,
        "index": index_result["index"],
    }


def command_keygen(args: argparse.Namespace) -> dict[str, Any]:
    output = Path(args.out).expanduser().resolve()
    if output.exists():
        raise PactError(f"refusing to overwrite existing key file: {output}", exit_code=2)
    actor = unicodedata.normalize("NFC", args.actor.strip())
    if not actor or len(actor) > 255:
        raise PactError("actor label must be 1–255 characters", exit_code=2)
    private_key = Ed25519PrivateKey.generate()
    private_raw = private_key.private_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PrivateFormat.Raw,
        encryption_algorithm=serialization.NoEncryption(),
    )
    public_raw = private_key.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )
    key_id = key_id_for_public_key(public_raw)
    key_obj = {
        "format": KEY_FORMAT,
        "algorithm": "ed25519",
        "actor": actor,
        "key_id": key_id,
        "public_key": b64url_encode(public_raw),
        "private_key": b64url_encode(private_raw),
        "created_at": utc_now(),
    }
    write_config_json(output, key_obj, mode=0o600)
    try:
        current_mode = stat.S_IMODE(output.stat().st_mode)
        if current_mode & 0o077:
            os.chmod(output, 0o600)
    except OSError:
        pass
    return {
        "operation": "keygen",
        "actor": actor,
        "key_id": key_id,
        "public_key": key_obj["public_key"],
        "path": str(output),
    }


def command_trust_add(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    store = ensure_store(repo)
    key_path = Path(args.key_file).expanduser().resolve()
    key_obj = load_key_file(key_path, require_private=False)
    trust = load_trust(store)
    roots = list(trust.get("roots", []))
    existing = next((root for root in roots if root.get("key_id") == key_obj["key_id"]), None)
    if existing is not None:
        if existing.get("public_key") != key_obj["public_key"]:
            raise PactError(
                f"trusted-root collision for {key_obj['key_id']}",
                exit_code=4,
            )
        created = False
    else:
        roots.append(
            {
                "key_id": key_obj["key_id"],
                "actor": key_obj["actor"],
                "public_key": key_obj["public_key"],
                "added_at": utc_now(),
            }
        )
        roots.sort(key=lambda root: root["key_id"])
        write_config_json(trust_file(store), {"format": TRUST_FORMAT, "roots": roots})
        created = True
    return {
        "operation": "trust-add",
        "key_id": key_obj["key_id"],
        "actor": key_obj["actor"],
        "created": created,
        "trust_file": str(trust_file(store)),
        "note": "local trust bootstrap is out-of-band configuration, not ledger history",
    }


def load_default_namespace(store: Path) -> str:
    format_obj = load_json(store / "format.json")
    return validate_namespace(str(format_obj.get("default_namespace", "")))


def load_valid_commits_for_write(store: Path) -> tuple[dict[str, dict[str, Any]], dict[str, ObjectVerification]]:
    verifications = scan_verified_objects(store)
    invalid = {
        object_id: result.errors
        for object_id, result in verifications.items()
        if not result.structurally_valid
    }
    if invalid:
        raise PactError(
            "refusing to mutate a store with invalid canonical objects",
            exit_code=4,
            details=invalid,
        )
    return commit_objects(verifications, require_valid=True), verifications


def command_commit(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    store = ensure_store(repo)
    commits, _ = load_valid_commits_for_write(store)
    batch_path = Path(args.events).expanduser().resolve()
    batch = normalize_event_batch(load_json(batch_path))
    namespace = validate_namespace(
        args.namespace or batch.get("namespace") or load_default_namespace(store)
    )
    key_path = Path(args.key_file).expanduser().resolve()
    key_obj = load_key_file(key_path, require_private=True)

    if args.parent:
        parents = sorted({parse_digest(parent, field_name="parent ID") for parent in args.parent})
    else:
        parents = compute_heads(commits, namespace=namespace).get(namespace, [])

    for parent in parents:
        parent_obj = commits.get(parent)
        if parent_obj is None:
            raise PactError(f"parent commit is unavailable or invalid: {parent}", exit_code=9)
        if parent_obj["body"]["namespace"] != namespace:
            raise PactError(
                f"parent {parent} belongs to namespace {parent_obj['body']['namespace']!r}, "
                f"not {namespace!r}",
                exit_code=2,
            )

    metadata = dict(batch.get("metadata", {}))
    metadata.setdefault("producer", PRODUCER)
    body: dict[str, Any] = {
        "namespace": namespace,
        "parents": parents,
        "actor": {
            "key_id": key_obj["key_id"],
            "label": key_obj["actor"],
        },
        "authority": normalize_authority(
            delegation_ref_value=args.delegation_ref,
            epoch=args.epoch,
            lease_ref_value=args.lease_ref,
        ),
        "observed_at": batch.get("observed_at") or utc_now(),
        "metadata": metadata,
        "events": batch["events"],
    }
    correlation_id = args.correlation_id or batch.get("correlation_id")
    if correlation_id:
        if len(correlation_id) > 255:
            raise PactError("correlation ID is too long", exit_code=2)
        body["correlation_id"] = unicodedata.normalize("NFC", correlation_id)

    body_hazards = scan_secret_hazards(body)
    if body_hazards:
        raise PactError(
            "refusing to sign immutable secret-like material",
            exit_code=7,
            details={"hazards": body_hazards},
        )

    body_digest, signature = sign_body(body, key_obj)
    obj = {
        "format": COMMIT_FORMAT,
        "body": body,
        "body_digest": body_digest,
        "signature": signature,
    }
    object_id, created = write_canonical_object(store, obj)

    # Verify the exact persisted object before updating the disposable index.
    verification = verify_object_file(store, object_path_for_id(store, object_id))
    if not verification.structurally_valid:
        raise PactError(
            f"new commit failed post-write verification: {object_id}",
            exit_code=4,
            details=verification.errors,
        )
    rebuild_index(repo)

    # Re-evaluate authorization with the newly admitted commit included.
    all_verifications = scan_verified_objects(store)
    all_commits = commit_objects(all_verifications, require_valid=True)
    all_events = build_event_map(all_commits)
    auth = evaluate_authorization_all(
        store=store,
        commits=all_commits,
        events=all_events,
        parents=parent_map(all_commits),
    ).get(object_id, AuthorizationResult("indeterminate", ["authorization unavailable"]))

    refs = [event_ref(object_id, event["local_id"]) for event in body["events"]]
    return {
        "operation": "commit",
        "object_id": object_id,
        "created": created,
        "namespace": namespace,
        "parents": parents,
        "event_refs": refs,
        "integrity": "valid",
        "authenticity": "valid",
        "authorization": auth.status,
        "authorization_reasons": auth.reasons,
        "lease_status": auth.lease_status,
        "path": str(object_path_for_id(store, object_id)),
    }


def command_verify(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    result = verify_store(repo, strict=args.strict)
    if not result["ok"]:
        raise PactError(
            "PACT verification failed",
            exit_code=4,
            details=result,
        )
    return result


def command_reindex(args: argparse.Namespace) -> dict[str, Any]:
    return rebuild_index(repo_path(args.repo))


def command_heads(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    store = ensure_store(repo)
    commits, _ = load_valid_commits_for_write(store)
    heads = compute_heads(commits)
    if args.namespace:
        scope = validate_namespace(args.namespace)
        heads = {ns: ids for ns, ids in heads.items() if namespace_in_scope(ns, scope)}
    return {
        "operation": "heads",
        "repo": str(repo),
        "scope": args.namespace,
        "heads": heads,
        "note": "heads describe this local replica; they are not a global completeness claim",
    }


def command_hash(args: argparse.Namespace) -> dict[str, Any]:
    path = Path(args.file).expanduser().resolve()
    try:
        data = path.read_bytes()
    except FileNotFoundError as exc:
        raise PactError(f"file not found: {path}", exit_code=2) from exc
    return {
        "operation": "hash",
        "path": str(path),
        "digest": sha256_digest(data),
        "size": len(data),
    }


def command_log(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    db_path = ensure_index(repo)
    query = """
        SELECT
            e.event_ref, e.commit_id, e.local_id, e.kind, e.event_type,
            e.subject, e.schema_ref, e.payload_json, e.caused_by_json,
            e.supersedes_json, e.tags_json,
            o.namespace, o.actor_key_id, o.actor_label, o.observed_at
        FROM events e
        JOIN objects o ON o.object_id = e.commit_id
        WHERE 1 = 1
    """
    params: list[Any] = []
    if args.namespace:
        scope = validate_namespace(args.namespace)
        query += " AND (o.namespace = ? OR o.namespace LIKE ?)"
        params.extend([scope, scope + "/%"])
    if args.type:
        query += " AND e.event_type = ?"
        params.append(args.type)
    if args.subject:
        query += " AND e.subject = ?"
        params.append(args.subject)
    if args.actor:
        validate_key_id(args.actor)
        query += " AND o.actor_key_id = ?"
        params.append(args.actor)
    query += " ORDER BY o.observed_at DESC, e.commit_id DESC, e.local_id ASC LIMIT ?"
    params.append(args.limit)

    connection = sqlite3.connect(db_path)
    connection.row_factory = sqlite3.Row
    try:
        rows = connection.execute(query, params).fetchall()
    finally:
        connection.close()
    events = []
    for row in rows:
        events.append(
            {
                "event_ref": row["event_ref"],
                "commit_id": row["commit_id"],
                "local_id": row["local_id"],
                "kind": row["kind"],
                "type": row["event_type"],
                "subject": row["subject"],
                "schema_ref": row["schema_ref"],
                "payload": json.loads(row["payload_json"]),
                "caused_by": json.loads(row["caused_by_json"]),
                "supersedes": json.loads(row["supersedes_json"]),
                "tags": json.loads(row["tags_json"]),
                "namespace": row["namespace"],
                "actor": {
                    "key_id": row["actor_key_id"],
                    "label": row["actor_label"],
                },
                "observed_at": row["observed_at"],
            }
        )
    return {
        "operation": "log",
        "repo": str(repo),
        "count": len(events),
        "events": events,
        "ordering_note": (
            "human-readable order uses advisory observed_at and object ID; "
            "only DAG reachability proves causal order"
        ),
    }


def command_show(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    store = ensure_store(repo)
    identifier = args.identifier
    if EVENT_REF_RE.fullmatch(identifier):
        commit_id, local_id = parse_event_ref(identifier)
        obj = read_object(store, commit_id)
        if obj.get("format") != COMMIT_FORMAT:
            raise PactError(f"event reference points to non-commit object: {commit_id}", exit_code=4)
        event = next((event for event in obj["body"]["events"] if event["local_id"] == local_id), None)
        if event is None:
            raise PactError(f"event not found: {identifier}", exit_code=9)
        verification = verify_object_file(store, object_path_for_id(store, commit_id))
        return {
            "operation": "show",
            "identifier": identifier,
            "kind": "event",
            "commit_id": commit_id,
            "namespace": obj["body"]["namespace"],
            "actor": obj["body"]["actor"],
            "observed_at": obj["body"]["observed_at"],
            "event": event,
            "integrity": verification.integrity,
            "authenticity": verification.authenticity,
            "errors": verification.errors,
        }
    object_id = parse_digest(identifier, field_name="object ID")
    obj = read_object(store, object_id)
    verification = verify_object_file(store, object_path_for_id(store, object_id))
    return {
        "operation": "show",
        "identifier": object_id,
        "kind": verification.object_type,
        "integrity": verification.integrity,
        "authenticity": verification.authenticity,
        "errors": verification.errors,
        "object": obj,
    }


def command_checkpoint(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    store = ensure_store(repo)
    verification_result = verify_store(repo, strict=True)
    if not verification_result["ok"]:
        raise PactError(
            "cannot checkpoint an invalid or incomplete strict frontier",
            exit_code=4,
            details=verification_result,
        )
    verifications = scan_verified_objects(store)
    commits = commit_objects(verifications, require_valid=True)
    checkpoints = checkpoint_objects(verifications, require_valid=True)
    scope = validate_namespace(args.scope)
    all_heads = compute_heads(commits)
    frontier = [
        {"namespace": namespace, "heads": heads}
        for namespace, heads in all_heads.items()
        if namespace_in_scope(namespace, scope)
    ]
    frontier.sort(key=lambda entry: entry["namespace"])
    if not frontier:
        raise PactError(f"no commit heads found under checkpoint scope {scope!r}", exit_code=9)

    policy_ref = parse_digest(args.policy_ref, field_name="policy reference")
    schema_refs = sorted(
        {parse_digest(value, field_name="schema reference") for value in (args.schema_ref or [])}
    )
    previous = None
    if args.previous:
        previous = parse_digest(args.previous, field_name="previous checkpoint")
        if previous not in checkpoints:
            raise PactError(f"previous checkpoint is unavailable or invalid: {previous}", exit_code=9)
    key_obj = load_key_file(Path(args.key_file).expanduser().resolve(), require_private=True)
    metadata: dict[str, Any] = {"producer": PRODUCER}
    if args.purpose:
        metadata["purpose"] = unicodedata.normalize("NFC", args.purpose)
    body = {
        "scope": scope,
        "frontier": frontier,
        "policy_ref": policy_ref,
        "schema_refs": schema_refs,
        "authority_epoch": unicodedata.normalize("NFC", args.authority_epoch),
        "previous_checkpoint": previous,
        "actor": {
            "key_id": key_obj["key_id"],
            "label": key_obj["actor"],
        },
        "observed_at": utc_now(),
        "metadata": metadata,
    }
    body_hazards = scan_secret_hazards(body)
    if body_hazards:
        raise PactError(
            "refusing to sign immutable secret-like material",
            exit_code=7,
            details={"hazards": body_hazards},
        )

    body_digest, signature = sign_body(body, key_obj)
    obj = {
        "format": CHECKPOINT_FORMAT,
        "body": body,
        "body_digest": body_digest,
        "signature": signature,
    }
    object_id, created = write_canonical_object(store, obj)
    persisted = verify_object_file(store, object_path_for_id(store, object_id))
    if not persisted.structurally_valid:
        raise PactError(
            f"new checkpoint failed post-write verification: {object_id}",
            exit_code=4,
            details=persisted.errors,
        )
    rebuild_index(repo)

    root = trust_root_map(store).get(key_obj["key_id"])
    checkpoint_authorization = "authorized" if root else "indeterminate"
    reasons = (
        ["checkpoint signer is a locally trusted root"]
        if root
        else ["reference CLI does not evaluate delegated checkpoint capability"]
    )
    return {
        "operation": "checkpoint",
        "object_id": object_id,
        "created": created,
        "scope": scope,
        "frontier": frontier,
        "policy_ref": policy_ref,
        "schema_refs": schema_refs,
        "authority_epoch": args.authority_epoch,
        "previous_checkpoint": previous,
        "integrity": "valid",
        "authenticity": "valid",
        "authorization": checkpoint_authorization,
        "authorization_reasons": reasons,
        "path": str(object_path_for_id(store, object_id)),
    }


def _resolve_source_store(value: str) -> Path:
    source = Path(value).expanduser().resolve()
    if source.name == ".pact" and (source / "format.json").is_file():
        return source
    candidate = source / ".pact"
    if (candidate / "format.json").is_file():
        return candidate
    raise PactError(f"sync source is not an initialized PACT project/store: {source}", exit_code=8)


def command_sync_dir(args: argparse.Namespace) -> dict[str, Any]:
    repo = repo_path(args.repo)
    store = ensure_store(repo)
    source_store = _resolve_source_store(args.source)
    if source_store == store:
        raise PactError("sync source and destination are the same store", exit_code=8)

    # Validate all candidate objects before copying any of them. Authorization is
    # intentionally not an admission requirement: authentic unauthorized history
    # may still be retained and excluded by projection policy.
    candidates: list[tuple[str, Path, bytes]] = []
    rejected: list[dict[str, Any]] = []
    for source_path in iter_object_paths(source_store):
        verification = verify_object_file(source_store, source_path)
        if not verification.structurally_valid:
            rejected.append(
                {
                    "path": str(source_path),
                    "object_id": verification.object_id,
                    "errors": verification.errors,
                }
            )
            continue
        candidates.append((verification.object_id, source_path, source_path.read_bytes()))
    if rejected:
        raise PactError(
            "sync source contains invalid canonical objects; nothing was imported",
            exit_code=8,
            details={"rejected": rejected},
        )

    imported_paths: list[Path] = []
    already_present = 0
    try:
        for object_id, source_path, raw in candidates:
            destination = object_path_for_id(store, object_id)
            if destination.exists():
                if destination.read_bytes() != raw:
                    raise PactError(
                        f"destination has conflicting bytes for {object_id}",
                        exit_code=4,
                    )
                already_present += 1
                continue
            destination.parent.mkdir(parents=True, exist_ok=True)
            _atomic_write(destination, raw, mode=0o644)
            # Verify recipient-side bytes rather than trusting source verification.
            persisted = verify_object_file(store, destination)
            if not persisted.structurally_valid:
                raise PactError(
                    f"recipient-side admission verification failed for {object_id}",
                    exit_code=8,
                    details=persisted.errors,
                )
            imported_paths.append(destination)

        # Admission is complete only when the union is structurally valid. A
        # deliberately partial replica may retain missing-parent warnings, but
        # hard graph/checkpoint errors roll back only this not-yet-admitted batch.
        result = verify_store(repo, strict=False)
        if not result["ok"]:
            raise PactError(
                "synced objects would make the destination ledger invalid",
                exit_code=8,
                details={"verification": result},
            )
        rebuild_index(repo)
    except Exception:
        for imported_path in reversed(imported_paths):
            imported_path.unlink(missing_ok=True)
        # Restore the query index to the last admitted canonical object set.
        try:
            rebuild_index(repo)
        except Exception:
            pass
        raise

    imported = len(imported_paths)
    return {
        "operation": "sync-dir",
        "source": str(source_store),
        "destination": str(store),
        "examined": len(candidates),
        "imported": imported,
        "already_present": already_present,
        "rejected": 0,
        "verification_ok": result["ok"],
        "verification_errors": result["errors"],
        "verification_warnings": result["warnings"],
        "heads_after": result["heads"],
        "note": "sync unions ledger objects only; external evidence is not transferred",
    }


# ---------------------------------------------------------------------------
# Output formatting and CLI parser
# ---------------------------------------------------------------------------


def emit_result(result: Mapping[str, Any], *, as_json: bool) -> None:
    """Emit stable machine JSON or compact human-readable output."""

    if as_json:
        print(json.dumps(normalize_value(dict(result)), ensure_ascii=False, sort_keys=True))
        return

    operation = result.get("operation", "result")
    print(f"PACT {operation}")
    if operation == "init":
        print(f"  store: {result['store']}")
        print(f"  namespace: {result['default_namespace']}")
    elif operation == "keygen":
        print(f"  actor: {result['actor']}")
        print(f"  key ID: {result['key_id']}")
        print(f"  key file: {result['path']}")
    elif operation == "trust-add":
        print(f"  actor: {result['actor']}")
        print(f"  key ID: {result['key_id']}")
        print(f"  added: {result['created']}")
    elif operation == "commit":
        print(f"  commit: {result['object_id']}")
        print(f"  namespace: {result['namespace']}")
        print(f"  events: {len(result['event_refs'])}")
        print(f"  integrity/authenticity: {result['integrity']}/{result['authenticity']}")
        print(f"  authorization: {result['authorization']}")
        for reason in result.get("authorization_reasons", []):
            print(f"    - {reason}")
    elif operation == "verify":
        print(f"  ok: {result['ok']}")
        counts = result["counts"]
        print(
            "  objects/commits/events/checkpoints: "
            f"{counts['objects']}/{counts['commits']}/{counts['events']}/{counts['checkpoints']}"
        )
        print(
            "  authorization: "
            f"{counts['authorized']} authorized, {counts['unauthorized']} unauthorized, "
            f"{counts['indeterminate']} indeterminate"
        )
        for warning in result.get("warnings", []):
            print(f"  warning: {warning}")
    elif operation == "heads":
        for namespace, heads in result["heads"].items():
            print(f"  {namespace}")
            for head in heads:
                print(f"    {head}")
    elif operation == "hash":
        print(f"  {result['digest']}  {result['path']} ({result['size']} bytes)")
    elif operation == "log":
        print(f"  events: {result['count']}")
        for event in result["events"]:
            print(
                f"  {event['observed_at']} {event['event_ref']} "
                f"{event['type']} {event['subject']}"
            )
        print(f"  note: {result['ordering_note']}")
    elif operation == "show":
        print(pretty_json(result))
    elif operation == "checkpoint":
        print(f"  checkpoint: {result['object_id']}")
        print(f"  scope: {result['scope']}")
        print(f"  namespaces: {len(result['frontier'])}")
        print(f"  authorization: {result['authorization']}")
    elif operation == "sync-dir":
        print(
            f"  imported/already present: {result['imported']}/{result['already_present']}"
        )
        print(f"  verification ok: {result['verification_ok']}")
    elif operation == "reindex":
        print(f"  index: {result['index']}")
        print(f"  objects: {result['objects']}")
    else:
        print(pretty_json(result))


def emit_error(error: PactError, *, as_json: bool) -> None:
    """Emit expected failures without leaking secret values."""

    if as_json:
        payload: dict[str, Any] = {
            "ok": False,
            "error": error.message,
            "exit_code": error.exit_code,
        }
        if error.details is not None:
            payload["details"] = error.details
        print(json.dumps(normalize_value(payload), ensure_ascii=False, sort_keys=True), file=sys.stderr)
    else:
        print(f"PACT error: {error.message}", file=sys.stderr)
        if error.details is not None:
            print(pretty_json(error.details), file=sys.stderr)


def add_json_flag(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--json", action="store_true", help="emit one machine-readable JSON object")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="pact",
        description="PACT local-first immutable signed semantic ledger reference CLI",
    )
    parser.add_argument("--version", action="version", version=f"PACT {VERSION}")
    subparsers = parser.add_subparsers(dest="command", required=True)

    init_parser = subparsers.add_parser("init", help="initialize a project-local PACT store")
    init_parser.add_argument("--repo", default=".")
    init_parser.add_argument("--namespace", required=True)
    add_json_flag(init_parser)
    init_parser.set_defaults(handler=command_init)

    keygen_parser = subparsers.add_parser("keygen", help="generate an external Ed25519 actor key")
    keygen_parser.add_argument("--actor", required=True)
    keygen_parser.add_argument("--out", required=True)
    add_json_flag(keygen_parser)
    keygen_parser.set_defaults(handler=command_keygen)

    trust_parser = subparsers.add_parser("trust-add", help="add a local out-of-band trust root")
    trust_parser.add_argument("--repo", default=".")
    trust_parser.add_argument("--key-file", required=True)
    add_json_flag(trust_parser)
    trust_parser.set_defaults(handler=command_trust_add)

    hash_parser = subparsers.add_parser("hash", help="hash exact external evidence bytes")
    hash_parser.add_argument("file")
    add_json_flag(hash_parser)
    hash_parser.set_defaults(handler=command_hash)

    commit_parser = subparsers.add_parser("commit", help="append one atomic signed semantic commit")
    commit_parser.add_argument("--repo", default=".")
    commit_parser.add_argument("--key-file", required=True)
    commit_parser.add_argument("--events", required=True)
    commit_parser.add_argument("--namespace")
    commit_parser.add_argument("--parent", action="append", default=[])
    commit_parser.add_argument("--delegation-ref")
    commit_parser.add_argument("--epoch")
    commit_parser.add_argument("--lease-ref")
    commit_parser.add_argument("--correlation-id")
    add_json_flag(commit_parser)
    commit_parser.set_defaults(handler=command_commit)

    verify_parser = subparsers.add_parser("verify", help="verify objects, DAG, refs, and authority")
    verify_parser.add_argument("--repo", default=".")
    verify_parser.add_argument("--strict", action="store_true")
    add_json_flag(verify_parser)
    verify_parser.set_defaults(handler=command_verify)

    reindex_parser = subparsers.add_parser("reindex", help="rebuild disposable SQLite from objects")
    reindex_parser.add_argument("--repo", default=".")
    add_json_flag(reindex_parser)
    reindex_parser.set_defaults(handler=command_reindex)

    heads_parser = subparsers.add_parser("heads", help="show current local heads per namespace")
    heads_parser.add_argument("--repo", default=".")
    heads_parser.add_argument("--namespace")
    add_json_flag(heads_parser)
    heads_parser.set_defaults(handler=command_heads)

    log_parser = subparsers.add_parser("log", help="query semantic events from the rebuildable index")
    log_parser.add_argument("--repo", default=".")
    log_parser.add_argument("--namespace")
    log_parser.add_argument("--type")
    log_parser.add_argument("--subject")
    log_parser.add_argument("--actor")
    log_parser.add_argument("--limit", type=int, default=100)
    add_json_flag(log_parser)
    log_parser.set_defaults(handler=command_log)

    show_parser = subparsers.add_parser("show", help="show one object or stable event reference")
    show_parser.add_argument("--repo", default=".")
    show_parser.add_argument("identifier")
    add_json_flag(show_parser)
    show_parser.set_defaults(handler=command_show)

    checkpoint_parser = subparsers.add_parser("checkpoint", help="create a signed official frontier")
    checkpoint_parser.add_argument("--repo", default=".")
    checkpoint_parser.add_argument("--key-file", required=True)
    checkpoint_parser.add_argument("--scope", required=True)
    checkpoint_parser.add_argument("--policy-ref", required=True)
    checkpoint_parser.add_argument("--authority-epoch", required=True)
    checkpoint_parser.add_argument("--schema-ref", action="append", default=[])
    checkpoint_parser.add_argument("--previous")
    checkpoint_parser.add_argument("--purpose")
    add_json_flag(checkpoint_parser)
    checkpoint_parser.set_defaults(handler=command_checkpoint)

    sync_parser = subparsers.add_parser("sync-dir", help="union valid objects from another directory replica")
    sync_parser.add_argument("--repo", default=".")
    sync_parser.add_argument("--from", dest="source", required=True)
    add_json_flag(sync_parser)
    sync_parser.set_defaults(handler=command_sync_dir)

    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    as_json = bool(getattr(args, "json", False))
    try:
        if getattr(args, "limit", 1) <= 0:
            raise PactError("--limit must be greater than zero", exit_code=2)
        result = args.handler(args)
        emit_result(result, as_json=as_json)
        return 0
    except PactError as error:
        emit_error(error, as_json=as_json)
        return error.exit_code
    except KeyboardInterrupt:
        error = PactError("operation interrupted", exit_code=9)
        emit_error(error, as_json=as_json)
        return error.exit_code
    except Exception as exc:  # pragma: no cover - last-resort safety boundary.
        error = PactError(f"unexpected internal error: {exc}", exit_code=10)
        emit_error(error, as_json=as_json)
        return error.exit_code


if __name__ == "__main__":
    raise SystemExit(main())
