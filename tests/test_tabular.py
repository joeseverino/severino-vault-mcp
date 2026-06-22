"""Tests for the generic records → markdown-table renderer."""

from __future__ import annotations

from severino_vault_mcp import tabular


def test_simple_key_columns() -> None:
    out = tabular.render_table(
        [{"domain": "homelab", "answer": "192.168.1.233"}],
        [{"label": "Domain", "key": "domain"}, {"label": "Answer", "key": "answer"}],
    )
    assert "| Domain | Answer |" in out
    assert "| homelab | 192.168.1.233 |" in out


def test_template_column() -> None:
    out = tabular.render_table(
        [{"forward_scheme": "http", "forward_host": "h", "forward_port": 81}],
        [{"label": "Upstream", "template": "{forward_scheme}://{forward_host}:{forward_port}"}],
    )
    assert "| http://h:81 |" in out


def test_list_value_is_joined() -> None:
    out = tabular.render_table(
        [{"domain_names": ["a.com", "b.com"]}],
        [{"label": "Domains", "key": "domain_names"}],
    )
    assert "a.com, b.com" in out


def test_bool_renders_yes_no() -> None:
    out = tabular.render_table([{"enabled": True}], [{"label": "On", "key": "enabled"}])
    assert "| yes |" in out


def test_pipe_is_escaped() -> None:
    out = tabular.render_table([{"x": "a|b"}], [{"label": "X", "key": "x"}])
    assert "a\\|b" in out


def test_missing_template_field_is_blank() -> None:
    out = tabular.render_table([{"a": "1"}], [{"label": "Z", "template": "{nope}"}])
    assert "|  |" in out
