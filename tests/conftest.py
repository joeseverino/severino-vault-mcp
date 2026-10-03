"""Keep every test off this machine's real edu and life vault configs."""

from __future__ import annotations

from pathlib import Path

import pytest


@pytest.fixture(autouse=True)
def _hermetic_vaults(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("SVMC_EDU_CONFIG", str(tmp_path / "no-edu.toml"))
    monkeypatch.setenv("SVMC_LIFE_CONFIG", str(tmp_path / "no-life.toml"))
