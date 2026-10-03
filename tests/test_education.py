"""The education dataset export, hermetic against a tmp vault."""

from __future__ import annotations

from pathlib import Path

import pytest
from vault_engine.config import Config
from vault_engine.schema import EDUCATION_PROFILE
from vault_engine.vault import VaultLoader

from severino_vault_mcp.cli import build_parser
from severino_vault_mcp.education import education_dataset

INSTITUTION = """---
doc_id: res-gt
title: Georgia Institute of Technology
doc_type: resource
status: active
institution: Georgia Institute of Technology
slug: georgia-tech
description: Coursework, course by course.
---

# Georgia Institute of Technology
"""


def _course(doc_id: str, code: str, term: str, status: str, site: str = "") -> str:
    section = f"\n## Site\n\n{site}\n\n## Scratch\n\nprivate notes\n" if site else ""
    return f"""---
doc_id: {doc_id}
title: Course {code}
doc_type: course
status: {status}
code: {code}
term: {term}
---

# Course {code}

## Notes
{section}"""


def _write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def _loader(tmp_path: Path) -> VaultLoader:
    config_toml = tmp_path / "config.toml"
    config_toml.write_text(
        f'[vault]\npath = "{tmp_path / "vault"}"\nindexed_dirs = ["Georgia Tech"]\n',
        encoding="utf-8",
    )
    return VaultLoader(Config.load(config_toml, env={}))


@pytest.fixture()
def vault(tmp_path: Path) -> Path:
    root = tmp_path / "vault" / "Georgia Tech"
    _write(root / "index.md", INSTITUTION)
    _write(
        root / "Completed" / "CS1" / "index.md",
        _course("course-cs1", "CS1000", "Fall 2025", "completed", "- Learned things."),
    )
    _write(
        root / "CS2" / "index.md",
        _course("course-cs2", "CS2000", "Spring 2026", "active", "- Doing things."),
    )
    _write(root / "Planned" / "CS3" / "index.md", _course("course-cs3", "CS3000", "Fall 2026", "upcoming"))
    return tmp_path


def test_dataset_groups_sorts_and_extracts_site_bullets(vault: Path) -> None:
    result = education_dataset(_loader(vault), EDUCATION_PROFILE.statuses)

    assert result["ok"] is True
    [gt] = result["institutions"]
    assert gt["institution"] == "Georgia Institute of Technology"
    assert gt["slug"] == "georgia-tech"
    assert [course["code"] for course in gt["courses"]] == ["CS1000", "CS2000", "CS3000"]
    assert gt["courses"][0]["site_bullets"] == "- Learned things."
    # The `## Scratch` section after `## Site` never leaks into the export.
    assert "private" not in gt["courses"][1]["site_bullets"]
    # Upcoming courses export with empty bullets; consumers apply surface policy.
    assert gt["courses"][2]["status"] == "upcoming"
    assert gt["courses"][2]["site_bullets"] == ""


def test_out_of_profile_status_fails_closed(vault: Path) -> None:
    _write(
        vault / "vault" / "Georgia Tech" / "CS4" / "index.md",
        _course("course-cs4", "CS4000", "Spring 2027", "planned"),
    )

    result = education_dataset(_loader(vault), EDUCATION_PROFILE.statuses)

    assert result["ok"] is False
    assert any("planned" in error for error in result["errors"])


def test_institution_note_without_slug_fails_closed(vault: Path) -> None:
    _write(
        vault / "vault" / "Georgia Tech" / "index.md",
        INSTITUTION.replace("slug: georgia-tech\n", ""),
    )

    result = education_dataset(_loader(vault), EDUCATION_PROFILE.statuses)

    assert result["ok"] is False
    assert any("slug" in error for error in result["errors"])


def test_course_missing_term_fails_closed(vault: Path) -> None:
    _write(
        vault / "vault" / "Georgia Tech" / "CS5" / "index.md",
        _course("course-cs5", "CS5000", "Fall 2026", "upcoming").replace("term: Fall 2026\n", ""),
    )

    result = education_dataset(_loader(vault), EDUCATION_PROFILE.statuses)

    assert result["ok"] is False
    assert any("CS5" in error and "term" in error for error in result["errors"])


def test_cli_parses_export() -> None:
    args = build_parser().parse_args(["export", "education", "--pretty"])
    assert args.command == "export"
    assert args.pretty is True
