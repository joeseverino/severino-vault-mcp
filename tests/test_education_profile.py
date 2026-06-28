"""The second schema profile — proof the engine governs a non-ops vault.

EDUCATION_PROFILE is a SchemaProfile with its own doc-types, statuses, and id
prefixes. The same write path validates against whichever profile it's handed,
so a Labs server and an Education server differ only by the profile their context
carries — the whole point of the engine extraction.
"""

from __future__ import annotations

from severino_vault_mcp import schema
from severino_vault_mcp.config import Config
from severino_vault_mcp.schema import EDUCATION_PROFILE, LABS_PROFILE
from severino_vault_mcp.vault import VaultLoader
from severino_vault_mcp.vault_write_service import add_frontmatter


def test_education_profile_is_a_distinct_coherent_contract() -> None:
    edu = EDUCATION_PROFILE.as_dict()
    assert EDUCATION_PROFILE.name == "education"
    assert edu["doc_types"] == [
        "assignment", "course", "course_note", "resource", "task",
    ]
    assert edu["doc_id_prefixes"] == ["course-", "cnote-", "asg-", "res-", "task-"]
    # A different contract from Labs — different doc-types and prefixes.
    labs = LABS_PROFILE.as_dict()
    assert edu["doc_types"] != labs["doc_types"]
    assert set(edu["doc_id_prefixes"]) != set(labs["doc_id_prefixes"])
    # The universal task lifecycle is shared verbatim — a task is a task.
    assert edu["task_statuses"] == labs["task_statuses"]


def test_hq_schema_emit_still_the_labs_profile() -> None:
    # The contract HQ commits and validates against is unchanged by a 2nd profile.
    assert schema.as_dict() == LABS_PROFILE.as_dict()


def test_write_path_validates_against_the_handed_profile(tmp_path, monkeypatch) -> None:
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    runbooks = tmp_path / "03 Runbooks"
    runbooks.mkdir()
    (runbooks / "cs6250.md").write_text("# CS6250\n", encoding="utf-8")
    loader = VaultLoader(Config.from_env())

    common = dict(
        relative_path="03 Runbooks/cs6250.md",
        doc_id="course-cs6250",
        title="CS6250",
        doc_type="course",
        system="gt",
        environment="gatech",
        status="active",
        sensitivity="internal",
    )

    # Default (Labs) profile rejects an education doc_type / prefix / environment.
    rejected = add_frontmatter(loader, **common)
    assert rejected["ok"] is False
    assert "doc_type" in rejected["error"]

    # The Education profile accepts the same doc through the same code path.
    accepted = add_frontmatter(loader, profile=EDUCATION_PROFILE, **common)
    assert accepted["ok"] is True
