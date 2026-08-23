#!/usr/bin/env python3
"""Stable PACT CLI entrypoint backed by the importable reference library."""

from __future__ import annotations

import sys
from pathlib import Path

# Keep the skill self-contained when invoked directly from any working directory.
sys.path.insert(0, str(Path(__file__).resolve().parent))

from pact_core import main  # noqa: E402


if __name__ == "__main__":
    raise SystemExit(main())
