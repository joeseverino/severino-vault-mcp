"""The schema emitter is the contract HQ consumes — keep it honest."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

from severino_vault_mcp import schema

_REPO_ROOT = Path(__file__).resolve().parents[1]


def test_as_dict_matches_the_sets() -> None:
    data = schema.as_dict()
    assert set(data["doc_types"]) == schema.DOC_TYPES
    assert set(data["environments"]) == schema.ENVIRONMENTS
    assert set(data["statuses"]) == schema.STATUSES
    assert set(data["sensitivities"]) == schema.SENSITIVITIES
    assert tuple(data["doc_id_prefixes"]) == schema.DOC_ID_PREFIXES
    assert tuple(data["required_fields"]) == schema.REQUIRED_FIELDS


def test_as_dict_is_sorted_and_stable() -> None:
    data = schema.as_dict()
    for key in ("doc_types", "environments", "statuses", "sensitivities"):
        assert data[key] == sorted(data[key]), f"{key} must be sorted for a stable diff"


def test_schema_is_canonicalized() -> None:
    # The drift we removed: `lab` and the sensitivity aliases must be gone, so
    # the MCP can no longer accept a value HQ would reject.
    assert "lab" not in schema.ENVIRONMENTS
    assert {"public", "internal", "sensitive", "restricted"} == schema.SENSITIVITIES


def _doc(env_line: str = None, sens_line: str = None) -> str:
    return "\n".join(
        [
            "## Schema",
            "```yaml",
            "doc_type:      "
            + " | ".join(sorted(schema.DOC_TYPES)),
            env_line or ("environment:   " + " | ".join(sorted(schema.ENVIRONMENTS))),
            "status:        " + " | ".join(sorted(schema.STATUSES)),
            sens_line or ("sensitivity:   " + " | ".join(sorted(schema.SENSITIVITIES))),
            "```",
        ]
    )


def test_check_doc_enums_passes_when_current() -> None:
    assert schema.check_doc_enums(_doc()) == []


def test_check_doc_enums_flags_unknown_value() -> None:
    bad = _doc(env_line="environment:   homelab | lab | other")
    mismatches = schema.check_doc_enums(bad)
    assert any("environment" in m and "lab" in m for m in mismatches)


def test_check_doc_enums_flags_missing_value() -> None:
    bad = _doc(sens_line="sensitivity:   public | internal")
    mismatches = schema.check_doc_enums(bad)
    assert any("sensitivity" in m and "missing" in m for m in mismatches)


def test_check_doc_enums_flags_absent_field() -> None:
    text = "doc_type: runbook | architecture_note | decision_record"
    mismatches = schema.check_doc_enums(text)
    assert any(m.startswith("environment:") for m in mismatches)


def test_cli_emits_as_dict() -> None:
    proc = subprocess.run(
        [sys.executable, "-m", "severino_vault_mcp", "schema", "--json"],
        capture_output=True,
        text=True,
        check=True,
        cwd=_REPO_ROOT,
        env={**os.environ, "PYTHONPATH": str(_REPO_ROOT / "src")},
    )
    assert json.loads(proc.stdout) == schema.as_dict()
