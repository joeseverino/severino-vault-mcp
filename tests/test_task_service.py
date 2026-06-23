"""Tasks — the vault's one task brain (index-derived board, atomic writes)."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from severino_vault_mcp import schema
from severino_vault_mcp.config import Config
from severino_vault_mcp.task_service import (
    add_task,
    delete_task,
    list_projects,
    list_tasks,
    promote_note,
    reconcile_tasks,
    set_task_status,
)
from severino_vault_mcp.vault import VaultLoader


@pytest.fixture
def task_vault(tmp_path: Path, monkeypatch) -> Path:
    """A vault with one project folder and the cross-cutting bucket."""
    (tmp_path / "01 Projects" / "cordon").mkdir(parents=True)
    (tmp_path / "07 Backlog").mkdir()
    (tmp_path / "01 Projects" / "cordon" / "index.md").write_text(
        "---\ndoc_id: project-cordon\ntitle: Cordon\n"
        "doc_type: architecture_note\n---\n# Cordon\n",
        encoding="utf-8",
    )
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SVMC_INDEXED_DIRS", "01 Projects:07 Backlog")
    monkeypatch.setenv("SVMC_CACHE_SECONDS", "0")
    return tmp_path


def _loader() -> VaultLoader:
    return VaultLoader(Config.from_env())


def test_schema_models_the_task_profile() -> None:
    assert "task" in schema.DOC_TYPES
    assert "task-" in schema.DOC_ID_PREFIXES
    assert schema.TASK_STATUSES == {"open", "active", "parked", "done", "wontfix"}
    # The profiles stay distinct where it matters: a runbook can never be
    # "parked"/"open"/"done" (only "active" is shared between the lifecycles).
    assert {"open", "parked", "done", "wontfix"}.isdisjoint(schema.STATUSES)
    data = schema.as_dict()
    assert data["task_statuses"] == sorted(schema.TASK_STATUSES)


def test_add_to_project_colocates_and_links(task_vault: Path) -> None:
    result = add_task(_loader(), title="Tighten v4 semantics", project="cordon", effort="M")
    assert result["ok"] is True
    assert result["relative_path"] == "01 Projects/cordon/tasks/task-tighten-v4-semantics.md"
    fm = (task_vault / result["relative_path"]).read_text()
    assert "doc_type: task" in fm
    assert "status: open" in fm
    assert "- cordon" in fm  # related_projects set from the folder
    assert "effort: M" in fm


def test_add_cross_cutting_goes_to_the_bucket(task_vault: Path) -> None:
    result = add_task(_loader(), title="Add CI parity gate", related_projects=["cordon", "tools"])
    assert result["relative_path"] == "07 Backlog/task-add-ci-parity-gate.md"
    assert result["project"] == "cross"


def test_add_rejects_unknown_project(task_vault: Path) -> None:
    result = add_task(_loader(), title="x", project="nope")
    assert result["ok"] is False
    assert "no such project" in result["error"]


def test_add_rejects_bad_effort(task_vault: Path) -> None:
    result = add_task(_loader(), title="x", project="cordon", effort="XL")
    assert result["ok"] is False
    assert "effort" in result["error"]


def test_add_refuses_duplicate(task_vault: Path) -> None:
    add_task(_loader(), title="Dup", project="cordon")
    again = add_task(_loader(), title="Dup", project="cordon")
    assert again["ok"] is False
    assert "already exists" in again["error"]


def test_list_groups_by_project_over_both_sources(task_vault: Path) -> None:
    add_task(_loader(), title="Tighten v4", project="cordon")
    add_task(_loader(), title="Cross thing", related_projects=["cordon", "tools"])
    board = list_tasks(_loader())
    assert board["total"] == 2
    assert board["counts"]["project"] == {"cordon": 1, "cross": 1}
    # Project derives from location: the bucket task is "cross".
    by_slug = {t["slug"]: t for t in board["tasks"]}
    assert by_slug["tighten-v4"]["project"] == "cordon"
    assert by_slug["cross-thing"]["project"] == "cross"


def test_list_default_hides_parked_and_done(task_vault: Path) -> None:
    add_task(_loader(), title="Live one", project="cordon")
    add_task(_loader(), title="Shelved", project="cordon")
    set_task_status(_loader(), "task-shelved", "parked")
    live = list_tasks(_loader())
    assert [t["slug"] for t in live["tasks"]] == ["live-one"]
    everything = list_tasks(_loader(), include_all=True)
    assert {t["slug"] for t in everything["tasks"]} == {"live-one", "shelved"}


def test_closing_files_into_done_subfolder_and_reopen_moves_back(task_vault: Path) -> None:
    add_task(_loader(), title="Filing", project="cordon")
    done = set_task_status(_loader(), "filing", "done")
    assert done["relative_path"] == "01 Projects/cordon/tasks/done/task-filing.md"
    assert (task_vault / done["relative_path"]).exists()
    assert not (task_vault / "01 Projects/cordon/tasks/task-filing.md").exists()
    reopened = set_task_status(_loader(), "filing", "open")
    assert reopened["relative_path"] == "01 Projects/cordon/tasks/task-filing.md"
    assert not (task_vault / "01 Projects/cordon/tasks/done/task-filing.md").exists()


def test_reconcile_rehomes_hand_edited_statuses(task_vault: Path) -> None:
    add_task(_loader(), title="Hand done", project="cordon")
    p = task_vault / "01 Projects/cordon/tasks/task-hand-done.md"
    p.write_text(p.read_text().replace("status: open", "status: done"))  # hand edit, no move
    res = reconcile_tasks(_loader())
    assert res["moved"] == 1
    assert (task_vault / "01 Projects/cordon/tasks/done/task-hand-done.md").exists()
    # idempotent
    assert reconcile_tasks(_loader())["moved"] == 0


def test_shipped_lists_recently_done_kept_in_place(task_vault: Path) -> None:
    add_task(_loader(), title="Shipped it", project="cordon")
    set_task_status(_loader(), "shipped-it", "done")  # stamps closed: today
    board = list_tasks(_loader())
    # Kept in place (filed under done/, not archived away), hidden from the open
    # board, surfaced as shipped.
    assert (task_vault / "01 Projects/cordon/tasks/done/task-shipped-it.md").exists()
    assert all(t["slug"] != "shipped-it" for t in board["tasks"])
    assert [t["slug"] for t in board["shipped"]] == ["shipped-it"]


def test_move_to_done_stamps_closed_then_reopen_clears(task_vault: Path) -> None:
    add_task(_loader(), title="Ship it", project="cordon")
    done = set_task_status(_loader(), "ship-it", "done")  # bare slug resolves
    assert done["ok"] is True and done["status"] == "done"
    fm = (task_vault / done["relative_path"]).read_text()
    assert "status: done" in fm
    assert "closed: " in fm
    reopened = set_task_status(_loader(), "task-ship-it", "open")  # full id resolves
    fm = (task_vault / reopened["relative_path"]).read_text()
    assert "status: open" in fm
    assert "closed:" not in fm  # cleared on reopen


def test_move_rejects_bad_status_and_missing_task(task_vault: Path) -> None:
    add_task(_loader(), title="Real", project="cordon")
    assert set_task_status(_loader(), "real", "sideways")["ok"] is False
    assert set_task_status(_loader(), "ghost", "done")["ok"] is False


def test_list_projects_is_the_colocation_universe_with_open_counts(task_vault: Path) -> None:
    (task_vault / "01 Projects" / "tools").mkdir()
    add_task(_loader(), title="A cordon thing", project="cordon")
    add_task(_loader(), title="Another cordon thing", project="cordon")
    add_task(_loader(), title="Cross thing")  # 07 Backlog, not a project
    result = list_projects(_loader())
    by_slug = {p["slug"]: p["open"] for p in result["projects"]}
    # Every 01 Projects/ folder appears (cordon + the empty tools); cross-cutting
    # work is not a project and is excluded; counts are live tasks only.
    assert set(by_slug) == {"cordon", "tools"}
    assert by_slug["cordon"] == 2
    assert by_slug["tools"] == 0


def test_delete_removes_a_task_and_refuses_non_tasks(task_vault: Path) -> None:
    add_task(_loader(), title="Junk", project="cordon")
    rel = "01 Projects/cordon/tasks/task-junk.md"
    assert (task_vault / rel).exists()
    result = delete_task(_loader(), "junk")  # bare slug resolves
    assert result["ok"] is True and result["deleted"] is True
    assert not (task_vault / rel).exists()
    # gone from the board, and a non-task is refused
    assert all(t["slug"] != "junk" for t in list_tasks(_loader())["tasks"])
    assert delete_task(_loader(), "project-cordon")["ok"] is False
    assert delete_task(_loader(), "ghost")["ok"] is False


def test_promote_note_creates_a_task_preserving_body_and_removes_source(task_vault: Path) -> None:
    inbox = task_vault / "00 Inbox"
    inbox.mkdir()
    note = inbox / "2026-06-23 idea.md"
    note.write_text("---\ndoc_id: inbox-x\ncreated: 2026-06-23\n---\n\nWire the retry backoff.\n", encoding="utf-8")
    result = promote_note(_loader(), "00 Inbox/2026-06-23 idea.md", title="Wire the retry backoff", project="cordon")
    assert result["ok"] is True
    assert result["relative_path"] == "01 Projects/cordon/tasks/task-wire-the-retry-backoff.md"
    assert result["promoted_from"] == "00 Inbox/2026-06-23 idea.md"
    body = (task_vault / result["relative_path"]).read_text()
    assert "doc_type: task" in body
    assert "Wire the retry backoff" in body  # the captured body survived
    assert not note.exists()  # source removed


def test_move_refuses_non_task_docs(task_vault: Path) -> None:
    # project-cordon is an architecture_note, not a task.
    result = set_task_status(_loader(), "project-cordon", "done")
    assert result["ok"] is False
    assert "not a task" in result["error"]
