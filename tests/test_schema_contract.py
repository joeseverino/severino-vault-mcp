"""Legacy HQ schema and the complete engine contract coexist without drift."""

import json
import sys

import pytest
from vault_engine.schema import LABS_PROFILE

from severino_vault_mcp.__main__ import main


def run_schema(monkeypatch, capsys, *flags):
    monkeypatch.setattr(sys, "argv", ["severino-vault-mcp", "schema", *flags])
    with pytest.raises(SystemExit) as exit_info:
        main()
    assert exit_info.value.code == 0
    return capsys.readouterr().out.strip()


def test_schema_default_remains_the_legacy_hq_shape(monkeypatch, capsys, tmp_path):
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    emitted = json.loads(run_schema(monkeypatch, capsys))
    assert emitted == LABS_PROFILE.as_dict()
    assert "contract_version" not in emitted


def test_schema_complete_contract_and_fingerprint(monkeypatch, capsys, tmp_path):
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    contract = json.loads(run_schema(monkeypatch, capsys, "--contract"))
    assert contract == LABS_PROFILE.contract_dict()
    fingerprint = run_schema(monkeypatch, capsys, "--fingerprint")
    assert fingerprint == LABS_PROFILE.fingerprint()
