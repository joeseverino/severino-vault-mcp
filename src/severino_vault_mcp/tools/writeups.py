"""jseverino.com writeup tools — a Labs-domain tool group.

Read, validate, and transactionally mutate the writeup inventory and the
featured order under `05 Writeups/` plus the technology catalog. Registered onto
a server's FastMCP instance via :func:`register`, so a server that doesn't run
jseverino.com simply never composes this group. The mutating tools keep the
featured set sequential 1..N transactionally; hand-shuffling `featured_order`
across files is the exact failure mode they exist to prevent.
"""

from __future__ import annotations

from typing import Any

from vault_engine.context import ServerContext

from ..labs import writeup_service


def register(mcp, ctx: ServerContext) -> None:
    """Register the writeup tool group on ``mcp`` from a server context.

    Builds its WriteupRuntime from ``ctx.config`` / ``ctx.loader``; a server that
    omits this group never calls register, so the runtime is never built.
    """
    writeup_runtime = writeup_service.WriteupRuntime.from_config(
        ctx.config, loader=ctx.loader
    )

    @mcp.tool()
    def list_featured_writeup_order() -> dict[str, Any]:
        """FAST PATH for "what is the featured/currently published writeup order?"

        Returns only the compact home-cloud order: slot, slug, title, published,
        and featured. Prefer this over `list_writeups("featured")` when the user
        asks for the current order, currently published order, featured order,
        home order, portfolio order, or writeup order and does not need full
        frontmatter fields.
        """
        return writeup_service.list_featured_writeup_order(writeup_runtime)

    @mcp.tool()
    def list_writeups(filter: str = "all") -> dict[str, Any]:
        """USE THIS — never grep `05 Writeups/*/index.md` for writeup state.

        Single source of truth for "which writeups exist, what's published,
        what's featured, what's the featured_order." The `filter="featured"`
        view sorts by `featured_order` ascending — that's the order the home
        cloud renders, and the only correct way to reason about placement.

        If you are about to run `rg featured_order 05 Writeups/`, `cat` a
        writeup's frontmatter, or count the featured set in your head: stop
        and call this instead. Hand-counting featured_order across multiple
        files is exactly how publishes ship with the wrong order.

        Args:
            filter: One of "all", "published", "draft", "featured". The
                "featured" filter sorts by `featured_order` ascending.
        """
        return writeup_service.list_writeups(writeup_runtime, filter)

    @mcp.tool()
    def get_technology_catalog() -> dict[str, Any]:
        """USE THIS — never parse `_technology-groups.md` markdown by hand.

        Returns the catalog grouped by section with each slug's label and
        featured state. Call before featuring a tag, before adding a new
        technology to a writeup's `technologies:` list, or before answering
        any "does slug X exist?" question. The catalog file is markdown
        tables — parsing them by hand is the path to introducing slugs the
        site build warns about.
        """
        return writeup_service.get_technology_catalog(writeup_runtime)

    @mcp.tool()
    def find_writeups_using_tag(slug: str) -> dict[str, Any]:
        """USE THIS before recommending any tag be promoted to featured.

        The home cloud rule is "featured AND referenced by >=1 published
        writeup." This tool answers the second half directly — never grep
        writeup frontmatter for tag occurrences. A "this tag is earned"
        claim without a call to this tool is unverified.

        Args:
            slug: Technology slug from the catalog, e.g. "obsidian".
        """
        return writeup_service.find_writeups_using_tag(writeup_runtime, slug)

    @mcp.tool()
    def validate_writeup(slug: str, draft: bool = False) -> dict[str, Any]:
        """CALL THIS BEFORE every writeup commit. Publish-readiness report.

        Inspects:
        - Frontmatter completeness (title, description, published, published_at,
          cover_image, technologies).
        - Tech slugs vs the catalog at `_technology-groups.md` — flags slugs
          the site build will warn about.
        - Images referenced from the body vs files present in the writeup's
          `images/` folder.
        - Soft nitpicks (description length, missing optional fields).

        Returns `ok: true` only when there are no blockers, no missing tech
        slugs, and no missing images. Nits are informational.

        Args:
            slug: Writeup slug, e.g. "building-a-custom-mcp-layer".
            draft: When true, demote the published / published_at blockers to nits
                so a draft can be gate-checked mid-authoring (matches
                `site validate --draft`).
        """
        return writeup_service.validate_writeup(writeup_runtime, slug, draft=draft)

    @mcp.tool()
    def validate_all_writeups(only_published: bool = True) -> dict[str, Any]:
        """Batch validate every writeup. Returns blockers/nits aggregated.

        Surfaces the "is every writeup publishable" view in one call instead of
        requiring N `validate_writeup` invocations to find the failing ones.
        Each writeup's entry mirrors the shape of `validate_writeup`.

        Args:
            only_published: When True (default), skips drafts. Pass False to
                include `published: false` writeups too.
        """
        return writeup_service.validate_all_writeups(
            writeup_runtime,
            only_published=only_published,
        )

    @mcp.tool()
    def prepare_writeup_publish(
        slug: str,
        include_tag_usage: bool = False,
    ) -> dict[str, Any]:
        """ONE-CALL publish prep. Use this BEFORE every writeup commit.

        Composes `validate_writeup` and `list_writeups("featured")` (and
        optionally per-tag `find_writeups_using_tag`) into one response:

        - `validation`: full `validate_writeup` result (blockers, missing
          tech slugs, missing images, unresolved related_*, nits).
        - `featured_set`: current featured order, sorted ascending, plus
          this writeup's position (or `null` if unfeatured). No hand-counting.
        - `tag_usage` (only when `include_tag_usage=True`): per-technology
          "how many writeups use this tag" stats. Off by default to keep the
          response small — opt in when you actually need to decide whether a
          tag has earned its featured slot.

        `ok: true` means: frontmatter complete, all tech slugs exist in the
        catalog, all referenced images exist on disk, related_* references
        resolve. If `ok` is true, the writeup is safe to commit + push.

        Prefer this over calling `validate_writeup` + `list_writeups` +
        `find_writeups_using_tag` individually.

        Args:
            slug: Writeup slug, e.g. "building-a-custom-mcp-layer".
            include_tag_usage: If True, include per-technology usage stats
                (adds ~300-500 tokens per call). Default False.
        """
        return writeup_service.prepare_writeup_publish(
            writeup_runtime,
            slug,
            include_tag_usage=include_tag_usage,
        )

    @mcp.tool()
    def writeup_dashboard() -> dict[str, Any]:
        """Return all writeup summaries and validation results from one snapshot.

        This is the low-latency initialization path for interactive clients such
        as `site manage`. Prefer it over separate `list_writeups` and
        `validate_all_writeups` calls when both views are needed together.
        """
        return writeup_service.writeup_dashboard(writeup_runtime)

    @mcp.tool()
    def apply_writeup_plan(plan: dict[str, Any]) -> dict[str, Any]:
        """Apply scalar writeup updates and the complete featured order transactionally.

        The plan shape is:
        `{"updates": [{"slug": "...", "published": true, ...}],
        "featured_order": ["first-slug", "second-slug"]}`.
        Every changed file is staged before replacement; failures trigger rollback.
        """
        return writeup_service.apply_writeup_plan(writeup_runtime, plan)

    @mcp.tool()
    def update_writeup_frontmatter(
        slug: str,
        title: str | None = None,
        description: str | None = None,
        published: bool | None = None,
        published_at: str | None = None,
        last_reviewed: str | None = None,
        touch_last_reviewed: bool = False,
        cover_image: str | None = None,
        cover_alt: str | None = None,
        featured: bool | None = None,
        featured_order: int | None = None,
    ) -> dict[str, Any]:
        """USE THIS — never Edit YAML in `05 Writeups/<slug>/index.md` by hand.

        Mirrors `update_frontmatter` but for the writeup schema (no doc_id,
        has published/featured/featured_order). Mutates scalar fields with
        minimal disruption to surrounding formatting; lines you don't touch
        stay byte-identical.

        For cross-writeup reordering of `featured_order` (slotting a new
        writeup in at position N and shifting others), call
        `reorder_featured(slug, position)` instead — this tool only mutates
        one writeup at a time and won't keep the featured set sequential.

        Args:
            slug: Writeup slug.
            title, description, published_at, cover_image, cover_alt: scalar
                updates. None means leave unchanged.
            published: boolean update. None means leave unchanged.
            featured, featured_order: retained for backwards-compatible tool
                schema parsing but refused; use `reorder_featured` or
                `apply_writeup_plan` so ordering invariants remain transactional.
            last_reviewed: ISO date (YYYY-MM-DD). Ignored if
                `touch_last_reviewed=True`.
            touch_last_reviewed: if True, set last_reviewed to today.
        """
        if featured is not None or featured_order is not None:
            return {
                "ok": False,
                "error": (
                    "featured fields must be changed through reorder_featured "
                    "or apply_writeup_plan"
                ),
            }
        return writeup_service.update_writeup_frontmatter(
            writeup_runtime,
            slug,
            title=title,
            description=description,
            published=published,
            published_at=published_at,
            last_reviewed=last_reviewed,
            touch_last_reviewed=touch_last_reviewed,
            cover_image=cover_image,
            cover_alt=cover_alt,
        )

    @mcp.tool()
    def reorder_featured(slug: str, position: int) -> dict[str, Any]:
        """USE THIS — never hand-shuffle featured_order across multiple files.

        Transactionally reorders the featured-writeups list. The exact failure mode
        this tool prevents is documented in CHANGELOG v2.4.2: hand-editing
        featured_order across 5+ files in a row is how publishes ship with
        the wrong slot value.

        Behavior:

        - position >= 1 and slug currently UNfeatured: insert at `position`,
          shift everyone at >=position down by 1.
        - position >= 1 and slug already featured: move from current slot
          to `position`, shifting others to keep the list sequential 1..N.
        - position == 0: unfeature `slug` (set featured=false, clear
          featured_order); existing featured writeups shift up to close the
          gap.

        The resulting featured order is guaranteed sequential 1..N with no
        gaps and no duplicates.

        Args:
            slug: Writeup slug to move.
            position: Target slot (1-indexed) or 0 to unfeature.
        """
        return writeup_service.reorder_featured(writeup_runtime, slug, position)
