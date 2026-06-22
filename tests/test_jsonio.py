"""Tests for the single JSON serialization/parsing home."""

from __future__ import annotations

from pathlib import Path

import pytest

from severino_vault_mcp import jsonio


def test_dumps_compact_default() -> None:
    assert jsonio.dumps({"b": 1, "a": 2}) == '{"b":1,"a":2}'


def test_dumps_pretty_indents() -> None:
    out = jsonio.dumps({"a": 1}, pretty=True)
    assert "\n" in out and "  " in out


def test_canonical_is_sorted_and_indented() -> None:
    out = jsonio.canonical({"b": 1, "a": 2})
    # sorted keys → "a" before "b"; indented → newlines present.
    assert out.index('"a"') < out.index('"b"')
    assert "\n" in out


def test_loads_roundtrip() -> None:
    assert jsonio.loads('{"x": 1}') == {"x": 1}


def test_loads_labels_source_on_error() -> None:
    with pytest.raises(jsonio.JsonError) as exc:
        jsonio.loads("not json", source="mirror payload")
    assert "mirror payload is not valid JSON" in str(exc.value)


def test_load_file_missing_raises(tmp_path: Path) -> None:
    with pytest.raises(jsonio.JsonError):
        jsonio.load_file(tmp_path / "absent.json")


def test_load_file_parses(tmp_path: Path) -> None:
    path = tmp_path / "data.json"
    path.write_text('{"ok": true}', encoding="utf-8")
    assert jsonio.load_file(path) == {"ok": True}
