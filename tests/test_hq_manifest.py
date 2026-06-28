from pathlib import Path

from severino_vault_mcp.labs.hq_manifest import build_hq_manifest


def _write_doc(path: Path, doc_id: str, notes: str = "") -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        f"""---
doc_id: {doc_id}
title: Example
doc_type: runbook
system: Example
environment: other
status: active
sensitivity: internal
{notes}---

# Example
""",
        encoding="utf-8",
    )


def test_manifest_uses_shared_multiline_frontmatter_parser(
    tmp_path: Path,
) -> None:
    _write_doc(
        tmp_path / "03 Runbooks" / "Example.md",
        "rb-example",
        "notes: >-\n  First line.\n  Second line.\n",
    )

    result = build_hq_manifest(tmp_path, ["03 Runbooks"])

    assert result["ok"] is True
    assert result["entries"][0]["notes"] == "First line. Second line."


def test_manifest_fails_closed_on_duplicate_doc_ids(tmp_path: Path) -> None:
    _write_doc(
        tmp_path / "02 Infrastructure" / "Example.md",
        "infra-example",
    )
    _write_doc(
        tmp_path / "03 Runbooks" / "Duplicate.md",
        "infra-example",
    )

    result = build_hq_manifest(
        tmp_path,
        ["02 Infrastructure", "03 Runbooks"],
    )

    assert result["ok"] is False
    assert result["duplicates"] == [
        {
            "doc_id": "infra-example",
            "first": "02 Infrastructure/Example.md",
            "second": "03 Runbooks/Duplicate.md",
        }
    ]
