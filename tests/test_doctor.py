"""Doctor smoke test.

Replaces the bespoke `doctor` command that used to live in cordon.checks.json.
A check that runs *this repo's own code* is a test, not a cordon command — so it
lives here and rides the standard pytest gate (cordon's `pytest` catalog check),
instead of a hand-written commands[] entry carrying its own exec/fix.
"""
from pathlib import Path

from severino_vault_mcp.config import Config
from severino_vault_mcp.doctor import run_doctor


def test_doctor_passes_on_sample_vault(monkeypatch):
    sample = Path(__file__).resolve().parents[1] / "examples" / "sample-vault"
    monkeypatch.setenv("SVMC_VAULT_PATH", str(sample))
    assert run_doctor(Config.from_env()) == 0
