"""Doctor must pass on the bundled sample vault — a check of our own code, so a test."""
from __future__ import annotations

from pathlib import Path

from vault_engine.config import Config
from vault_engine.doctor import run_doctor

_REPO_ROOT = Path(__file__).resolve().parents[1]


def test_doctor_passes_on_sample_vault(monkeypatch) -> None:
    monkeypatch.setenv("SVMC_VAULT_PATH", str(_REPO_ROOT / "examples" / "sample-vault"))
    assert run_doctor(Config.from_env()) == 0
