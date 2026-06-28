"""Tests for the daily-note brief-region writer (the mirror mechanic).

The load-bearing guarantee: a re-run rewrites only the generated region and
leaves the human's free-capture area below it byte-for-byte.
"""

from __future__ import annotations

from pathlib import Path

import pytest
from vault_engine.config import Config
from vault_engine.daily_write import write_daily_block


@pytest.fixture
def config(tmp_path: Path, monkeypatch) -> Config:
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    return Config.from_env()


def _note(config: Config, day: str) -> Path:
    return config.vault_path / config.daily_notes_dir / f"{day}.md"


def test_first_run_creates_the_note_with_the_region(config: Config) -> None:
    result = write_daily_block(config, "> [!info] hello", note_date="2026-06-25")
    assert result["ok"] is True
    assert result["created"] is True
    assert result["inserted"] is True

    text = _note(config, "2026-06-25").read_text(encoding="utf-8")
    assert "doc_id: daily-20260625" in text
    assert "MIRROR:BEGIN daily-brief" in text
    assert "MIRROR:END daily-brief" in text
    assert "> [!info] hello" in text


def test_rerun_replaces_region_and_preserves_capture_area(config: Config) -> None:
    write_daily_block(config, "first content", note_date="2026-06-25")
    path = _note(config, "2026-06-25")
    # the human appends free notes BELOW the generated region
    path.write_text(path.read_text(encoding="utf-8") + "\n## My notes\n- did a thing\n",
                    encoding="utf-8")

    result = write_daily_block(config, "second content (updated)", note_date="2026-06-25")
    assert result["ok"] is True
    assert result["inserted"] is False

    after = path.read_text(encoding="utf-8")
    assert "second content (updated)" in after
    assert "first content" not in after            # region rewritten in place, not appended
    assert "## My notes\n- did a thing" in after    # capture area untouched


def test_idempotent_same_content_is_a_noop(config: Config) -> None:
    write_daily_block(config, "same", note_date="2026-06-25")
    result = write_daily_block(config, "same", note_date="2026-06-25")
    assert result["ok"] is True
    assert result["changed"] is False


def test_invalid_date_is_an_error(config: Config) -> None:
    result = write_daily_block(config, "x", note_date="not-a-date")
    assert result["ok"] is False
    assert "invalid date" in result["error"]
