"""Tests for the jseverino.com writeup helpers."""

from __future__ import annotations

import importlib
import sys
from pathlib import Path

import pytest


@pytest.fixture
def fake_writeups_vault(tmp_path: Path, monkeypatch) -> Path:
    """Set up a fake vault with 05 Writeups/ and 06 Pages/_technology-groups.md."""
    (tmp_path / "01 Projects").mkdir()
    (tmp_path / "02 Infrastructure").mkdir()
    (tmp_path / "03 Runbooks").mkdir()
    (tmp_path / "05 Writeups").mkdir()
    (tmp_path / "06 Pages").mkdir()

    # Ready-to-ship writeup.
    ready = tmp_path / "05 Writeups" / "ready-piece"
    ready.mkdir()
    (ready / "images").mkdir()
    (ready / "images" / "cover.png").write_bytes(b"png-bytes")
    (ready / "images" / "diagram.png").write_bytes(b"png-bytes")
    (ready / "index.md").write_text(
        """---
title: Ready Piece
description: A short, concrete description for the ready piece.
published: true
published_at: 2026-05-29
last_reviewed: 2026-05-29
cover_image: ./images/cover.png
technologies:
  - docker
  - python
featured: true
featured_order: 2
related_projects: []
related_assets: []
---

# Ready Piece

![hero](./images/cover.png)

Body paragraph.

![diagram](./images/diagram.png)
""",
        encoding="utf-8",
    )

    # Draft writeup with several validation issues.
    draft = tmp_path / "05 Writeups" / "draft-piece"
    draft.mkdir()
    (draft / "images").mkdir()
    (draft / "images" / "cover.png").write_bytes(b"png-bytes")
    (draft / "index.md").write_text(
        """---
title: Draft Piece
description: ""
published: false
published_at:
last_reviewed: 2026-05-20
cover_image: ./images/cover.png
technologies:
  - docker
  - made-up-slug
featured: false
featured_order:
related_projects: []
related_assets: []
---

# Draft Piece

![hero](./images/cover.png)
![missing image](./images/not-here.png)
""",
        encoding="utf-8",
    )

    # Writeup with no frontmatter (should be skipped by the loader).
    bare = tmp_path / "05 Writeups" / "bare-folder"
    bare.mkdir()
    (bare / "index.md").write_text("# Just a title\n\nNo frontmatter.\n", encoding="utf-8")

    # A featured published piece at order 1 to exercise sorting.
    leader = tmp_path / "05 Writeups" / "lead-piece"
    leader.mkdir()
    (leader / "index.md").write_text(
        """---
title: Lead Piece
description: Leads the featured order.
published: true
published_at: 2026-05-01
last_reviewed: 2026-05-01
cover_image: ./cover.png
technologies:
  - docker
featured: true
featured_order: 1
related_projects: []
related_assets: []
---

# Lead Piece
""",
        encoding="utf-8",
    )

    # Technology catalog.
    (tmp_path / "06 Pages" / "_technology-groups.md").write_text(
        """# Technology Groups

Catalog header text.

## Platforms & OS

| Slug | Label | Featured |
| --- | --- | --- |
| docker | Docker | yes |
| ubuntu-server | Ubuntu Server | yes |

## Languages

| Slug | Label | Featured |
| --- | --- | --- |
| python | Python | yes |
| yaml | YAML |  |
""",
        encoding="utf-8",
    )

    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SVMC_CACHE_SECONDS", "0")
    monkeypatch.setenv(
        "SVMC_JSEVERINO_WRITEUPS_DIR", str(tmp_path / "05 Writeups")
    )
    monkeypatch.setenv(
        "SVMC_JSEVERINO_TECH_GROUPS",
        str(tmp_path / "06 Pages" / "_technology-groups.md"),
    )
    return tmp_path


def _fresh_module(name: str):
    """Re-import after env vars are set so module-level constants are fresh."""
    for mod in list(sys.modules):
        if mod.startswith("severino_vault_mcp"):
            del sys.modules[mod]
    return importlib.import_module(name)


# ----- writeup loader --------------------------------------------------------


def test_load_writeups_skips_bare_folders(fake_writeups_vault: Path) -> None:
    writeups_mod = _fresh_module("severino_vault_mcp.writeups")
    writeups = writeups_mod.load_writeups(fake_writeups_vault / "05 Writeups")
    slugs = {w.slug for w in writeups}
    assert slugs == {"ready-piece", "draft-piece", "lead-piece"}


def test_load_writeups_parses_typed_fields(fake_writeups_vault: Path) -> None:
    writeups_mod = _fresh_module("severino_vault_mcp.writeups")
    writeups = writeups_mod.load_writeups(fake_writeups_vault / "05 Writeups")
    ready = next(w for w in writeups if w.slug == "ready-piece")
    assert ready.published is True
    assert ready.featured is True
    assert ready.featured_order == 2
    assert ready.technologies == ["docker", "python"]
    assert ready.related_assets == []
    assert ready.published_at == "2026-05-29"


def test_load_writeups_handles_empty_featured_order(fake_writeups_vault: Path) -> None:
    writeups_mod = _fresh_module("severino_vault_mcp.writeups")
    writeups = writeups_mod.load_writeups(fake_writeups_vault / "05 Writeups")
    draft = next(w for w in writeups if w.slug == "draft-piece")
    assert draft.featured is False
    assert draft.featured_order is None
    assert draft.published is False


# ----- technology catalog ----------------------------------------------------


def test_load_technology_catalog_parses_tables(fake_writeups_vault: Path) -> None:
    tech_mod = _fresh_module("severino_vault_mcp.tech_groups")
    catalog = tech_mod.load_technology_catalog(
        fake_writeups_vault / "06 Pages" / "_technology-groups.md"
    )
    slugs = {(entry.slug, entry.group, entry.featured) for entry in catalog}
    assert ("docker", "Platforms & OS", True) in slugs
    assert ("yaml", "Languages", False) in slugs


def test_load_technology_catalog_missing_file_returns_empty(tmp_path: Path) -> None:
    tech_mod = _fresh_module("severino_vault_mcp.tech_groups")
    catalog = tech_mod.load_technology_catalog(tmp_path / "nope.md")
    assert catalog == []


# ----- list_writeups ---------------------------------------------------------


def test_list_featured_writeup_order_returns_compact_order(
    fake_writeups_vault: Path,
) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_featured_writeup_order()

    assert result["ok"] is True
    assert result["count"] == 2
    assert result["order"] == [
        {
            "slot": 1,
            "slug": "lead-piece",
            "title": "Lead Piece",
            "published": True,
            "featured": True,
        },
        {
            "slot": 2,
            "slug": "ready-piece",
            "title": "Ready Piece",
            "published": True,
            "featured": True,
        },
    ]


def test_list_writeups_all_returns_every_indexed(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_writeups()
    assert result["ok"] is True
    assert result["count"] == 3
    slugs = {w["slug"] for w in result["writeups"]}
    assert slugs == {"ready-piece", "draft-piece", "lead-piece"}


def test_list_writeups_filters_published(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_writeups("published")
    slugs = {w["slug"] for w in result["writeups"]}
    assert slugs == {"ready-piece", "lead-piece"}


def test_list_writeups_filters_draft(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_writeups("draft")
    slugs = {w["slug"] for w in result["writeups"]}
    assert slugs == {"draft-piece"}


def test_list_writeups_featured_is_sorted_by_order(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_writeups("featured")
    ordered = [w["slug"] for w in result["writeups"]]
    assert ordered == ["lead-piece", "ready-piece"]
    assert [w["slug"] for w in result["order"]] == ["lead-piece", "ready-piece"]
    assert result["order"][0]["slot"] == 1
    assert [w["slug"] for w in result["featured_order"]] == ["lead-piece", "ready-piece"]


def test_list_writeups_published_includes_compact_featured_order(
    fake_writeups_vault: Path,
) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_writeups("published")

    assert result["count"] == 2
    assert [w["slug"] for w in result["featured_order"]] == ["lead-piece", "ready-piece"]
    assert result["featured_order"] == [
        {
            "slot": 1,
            "slug": "lead-piece",
            "title": "Lead Piece",
            "published": True,
            "featured": True,
        },
        {
            "slot": 2,
            "slug": "ready-piece",
            "title": "Ready Piece",
            "published": True,
            "featured": True,
        },
    ]


def test_list_writeups_rejects_unknown_filter(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.list_writeups("bogus")
    assert result["ok"] is False
    assert "unknown filter" in result["error"]


def test_list_writeups_rejects_path_outside_vault(
    fake_writeups_vault: Path,
    tmp_path: Path,
    monkeypatch,
) -> None:
    outside = tmp_path.parent / f"{tmp_path.name}-outside-writeups"
    outside.mkdir()
    monkeypatch.setenv("SVMC_JSEVERINO_WRITEUPS_DIR", str(outside))
    server = _fresh_module("severino_vault_mcp.server")

    result = server.list_writeups()

    assert result["ok"] is False
    assert "inside configured vault root" in result["error"]


# ----- get_technology_catalog ------------------------------------------------


def test_get_technology_catalog_returns_grouped(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_technology_catalog()
    assert result["ok"] is True
    assert result["total_slugs"] == 4
    assert result["featured_count"] == 3
    assert {"slug": "yaml", "label": "YAML", "featured": False} in result["by_group"]["Languages"]


def test_get_technology_catalog_rejects_path_outside_vault(
    fake_writeups_vault: Path,
    tmp_path: Path,
    monkeypatch,
) -> None:
    outside = tmp_path.parent / f"{tmp_path.name}-catalog.md"
    outside.write_text("# Catalog\n", encoding="utf-8")
    monkeypatch.setenv("SVMC_JSEVERINO_TECH_GROUPS", str(outside))
    server = _fresh_module("severino_vault_mcp.server")

    result = server.get_technology_catalog()

    assert result["ok"] is False
    assert "inside configured vault root" in result["error"]


# ----- find_writeups_using_tag ----------------------------------------------


def test_find_writeups_using_tag_returns_matches(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_writeups_using_tag("docker")
    assert result["ok"] is True
    assert result["total_matches"] == 3
    assert result["published_matches"] == 2


def test_find_writeups_using_tag_no_match(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_writeups_using_tag("does-not-exist")
    assert result["ok"] is True
    assert result["total_matches"] == 0
    assert result["writeups"] == []


def test_find_writeups_using_tag_requires_slug(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_writeups_using_tag("   ")
    assert result["ok"] is False


# ----- validate_writeup ------------------------------------------------------


def test_validate_writeup_passes_for_ready_piece(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.validate_writeup("ready-piece")
    assert result["ok"] is True, result
    assert result["blockers"] == []
    assert result["missing_tech_slugs"] == []
    assert result["missing_images"] == []


def test_validate_writeup_reports_blockers_and_misses(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.validate_writeup("draft-piece")
    assert result["ok"] is False
    assert any("published is false" in b for b in result["blockers"])
    assert any("published_at empty" in b for b in result["blockers"])
    assert any("description missing" in b for b in result["blockers"])
    assert result["missing_tech_slugs"] == ["made-up-slug"]
    assert result["missing_images"] == ["images/not-here.png"]


def test_validate_writeup_missing_folder(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.validate_writeup("never-existed")
    assert result["ok"] is False
    assert "writeup folder not found" in result["error"]


# ----- prepare_writeup_publish ----------------------------------------------


def test_prepare_writeup_publish_composes_validate_and_featured(
    fake_writeups_vault: Path,
) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.prepare_writeup_publish("ready-piece")

    assert result["ok"] is True
    assert result["slug"] == "ready-piece"
    # validation passes through full report
    assert result["validation"]["ok"] is True
    assert result["validation"]["blockers"] == []
    # featured set is sorted ascending
    order_slots = [item["slot"] for item in result["featured_set"]["order"]]
    assert order_slots == sorted(order_slots)
    # this writeup's position is surfaced
    assert result["featured_set"]["this_writeup_position"] == 2


def test_prepare_writeup_publish_reports_failed_validation(
    fake_writeups_vault: Path,
) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.prepare_writeup_publish("draft-piece")
    assert result["ok"] is False
    assert any("published is false" in b for b in result["validation"]["blockers"])
    # unfeatured writeups have null position
    assert result["featured_set"]["this_writeup_position"] is None


def test_prepare_writeup_publish_omits_tag_usage_by_default(
    fake_writeups_vault: Path,
) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.prepare_writeup_publish("ready-piece")
    # Default off — saves ~300-500 tokens per call.
    assert "tag_usage" not in result


def test_prepare_writeup_publish_includes_tag_usage_when_requested(
    fake_writeups_vault: Path,
) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.prepare_writeup_publish("ready-piece", include_tag_usage=True)
    # ready-piece has technologies: [docker, python] — both should show usage stats
    assert "docker" in result["tag_usage"]
    assert "python" in result["tag_usage"]
    assert result["tag_usage"]["docker"]["total_writeups"] >= 1


# ----- validate_writeup unresolved refs -------------------------------------


def test_validate_writeup_flags_unresolved_related_assets(fake_writeups_vault: Path) -> None:
    # Inject a dangling related_assets entry, then validate.
    index = fake_writeups_vault / "05 Writeups" / "ready-piece" / "index.md"
    text = index.read_text(encoding="utf-8")
    text = text.replace(
        "related_assets: []",
        "related_assets:\n  - never-existed-thing",
    )
    index.write_text(text, encoding="utf-8")
    server = _fresh_module("severino_vault_mcp.server")
    result = server.validate_writeup("ready-piece")
    assert any("never-existed-thing" in ref for ref in result["unresolved_refs"])
    # ok should now be false because of the dangling ref
    assert result["ok"] is False


# ----- update_writeup_frontmatter -------------------------------------------


def test_update_writeup_frontmatter_touches_last_reviewed(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.update_writeup_frontmatter("draft-piece", touch_last_reviewed=True)
    assert result["ok"] is True
    assert "last_reviewed" in result["changed_fields"]
    body = (fake_writeups_vault / "05 Writeups" / "draft-piece" / "index.md").read_text(
        encoding="utf-8"
    )
    # New last_reviewed value should be in the file
    from datetime import date as _date
    assert f"last_reviewed: {_date.today().isoformat()}" in body


def test_update_writeup_frontmatter_flips_published(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.update_writeup_frontmatter("draft-piece", published=True, published_at="2026-05-30")
    assert result["ok"] is True
    assert set(result["changed_fields"]) == {"published", "published_at"}
    body = (fake_writeups_vault / "05 Writeups" / "draft-piece" / "index.md").read_text(
        encoding="utf-8"
    )
    assert "published: true" in body
    assert "published_at: 2026-05-30" in body


def test_update_writeup_frontmatter_no_op_when_unchanged(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    # ready-piece already has published=True, so passing the same value is a no-op
    result = server.update_writeup_frontmatter("ready-piece", published=True)
    assert result["ok"] is True
    assert result.get("no_op") is True


def test_update_writeup_frontmatter_preserves_other_lines(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    path = fake_writeups_vault / "05 Writeups" / "draft-piece" / "index.md"
    before = path.read_text(encoding="utf-8")
    server.update_writeup_frontmatter("draft-piece", cover_image="./images/new.png")
    after = path.read_text(encoding="utf-8")
    # Only the cover_image line should differ
    before_lines = before.splitlines()
    after_lines = after.splitlines()
    assert len(before_lines) == len(after_lines)
    diff = [(i, b, a) for i, (b, a) in enumerate(zip(before_lines, after_lines, strict=True)) if b != a]
    assert len(diff) == 1
    assert "cover_image:" in diff[0][1]


# ----- reorder_featured ------------------------------------------------------


def test_reorder_featured_inserts_unfeatured_writeup(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    # draft-piece is unfeatured; insert at position 2 (between lead and ready)
    result = server.reorder_featured("draft-piece", position=2)
    assert result["ok"] is True
    assert result["new_position"] == 2
    order = result["featured_order_after"]
    slugs = [item["slug"] for item in order]
    slots = [item["slot"] for item in order]
    assert slugs == ["lead-piece", "draft-piece", "ready-piece"]
    assert slots == [1, 2, 3]


def test_reorder_featured_moves_existing_writeup(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    # lead-piece is at 1, ready-piece at 2. Move lead to position 2.
    result = server.reorder_featured("lead-piece", position=2)
    assert result["ok"] is True
    slugs = [item["slug"] for item in result["featured_order_after"]]
    assert slugs == ["ready-piece", "lead-piece"]


def test_reorder_featured_unfeatures_with_position_zero(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.reorder_featured("lead-piece", position=0)
    assert result["ok"] is True
    assert result["new_position"] is None
    slugs = [item["slug"] for item in result["featured_order_after"]]
    # Only ready-piece remains featured, now at slot 1
    assert slugs == ["ready-piece"]
    assert result["featured_order_after"][0]["slot"] == 1


def test_reorder_featured_rejects_out_of_range(fake_writeups_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    # Only 2 featured writeups + 1 unfeatured target = max insert position is 3.
    result = server.reorder_featured("draft-piece", position=99)
    assert result["ok"] is False
    assert "out of range" in result["error"]
