"""CLI argument parser construction.

Split out of `__main__` so `server.describe_commands` can introspect the
same parser that backs `--help` without importing the entry-point module
(which would form an import cycle: server -> __main__ -> server).
"""

from __future__ import annotations

import argparse

from cordon_emit import set_effect


def build_parser() -> argparse.ArgumentParser:
    """Construct the full CLI parser.

    Extracted from `main` so `describe` can introspect the same parser that
    backs `--help` — the command surface is declared exactly once.
    """
    parser = argparse.ArgumentParser(
        prog="severino-vault-mcp",
        description="Local stdio MCP server for Obsidian-style operations vaults.",
    )
    parser.add_argument(
        "--fingerprint",
        action="store_true",
        help=(
            "Print a hash of the installed package's Python sources and exit. "
            "Compared against the source repo by `site doctor` to detect a "
            "stale install."
        ),
    )
    subparsers = parser.add_subparsers(dest="command")

    doctor = subparsers.add_parser(
        "doctor",
        help="Validate configured vault frontmatter without starting the MCP server.",
    )
    doctor.add_argument(
        "--propose",
        action="store_true",
        help="Print starter frontmatter for markdown files that are missing it.",
    )

    prepare_publish = subparsers.add_parser(
        "prepare-writeup-publish",
        help=(
            "Run prepare_writeup_publish for a writeup slug and print JSON. "
            "Exits 0 if ok, 1 if blockers / missing slugs / unresolved refs. "
            "Wrapped by `site publish-writeup` in the operator's shell tooling."
        ),
    )
    prepare_publish.add_argument(
        "slug",
        help="Writeup slug, e.g. building-a-custom-mcp-layer.",
    )
    prepare_publish.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )
    prepare_publish.add_argument(
        "--include-tag-usage",
        action="store_true",
        help="Include per-technology usage stats in the response.",
    )

    validate_one = subparsers.add_parser(
        "validate-writeup",
        help=(
            "Run validate_writeup for a single slug and print JSON. Exits 0 if "
            "ok, 1 if blockers / missing slugs / missing images / unresolved "
            "refs. The CLI face of the validate_writeup MCP tool; wrapped by "
            "`site validate`."
        ),
    )
    validate_one.add_argument(
        "slug",
        help="Writeup slug, e.g. building-a-custom-mcp-layer.",
    )
    validate_one.add_argument(
        "--draft",
        action="store_true",
        help=(
            "Tolerate the published / published_at blockers so a draft can be "
            "gate-checked mid-authoring (they become nits)."
        ),
    )
    validate_one.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    list_writeups = subparsers.add_parser(
        "list-writeups",
        help=(
            "Run list_writeups for a filter and print JSON. The featured "
            "filter sorts by featured_order ascending — the order the home "
            "cloud renders. Wrapped by `site featured` in the operator's "
            "shell tooling."
        ),
    )
    list_writeups.add_argument(
        "--filter",
        default="all",
        choices=["all", "published", "draft", "featured"],
        help="Which writeups to list (default: all).",
    )
    list_writeups.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    technology_catalog = subparsers.add_parser(
        "technology-catalog",
        help=(
            "Run get_technology_catalog and print JSON: every slug, label, "
            "and featured flag grouped by section. Wrapped by `site tech` "
            "in the operator's shell tooling."
        ),
    )
    technology_catalog.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    validate_all = subparsers.add_parser(
        "validate-all-writeups",
        help=(
            "Run validate_all_writeups and print JSON. Exits 0 only when "
            "every (published, by default) writeup passes the gate. Wrapped "
            "by `site publish` as the slug-free pre-flight."
        ),
    )
    validate_all.add_argument(
        "--include-drafts",
        action="store_true",
        help="Validate published: false writeups too (default: published only).",
    )
    validate_all.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    dashboard = subparsers.add_parser(
        "writeup-dashboard",
        help=(
            "Return every writeup summary and validation result from one "
            "shared vault snapshot. Used by `site manage` for fast startup."
        ),
    )
    dashboard.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    apply_plan = subparsers.add_parser(
        "apply-writeup-plan",
        help=(
            "Read a JSON writeup mutation plan from stdin and apply all "
            "scalar updates plus the complete featured order transactionally."
        ),
    )
    apply_plan.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    reorder = subparsers.add_parser(
        "reorder-featured",
        help=(
            "Run reorder_featured: move a writeup to a 1-indexed featured "
            "slot (0 unfeatures it) and renumber the list sequential 1..N. "
            "Wrapped by `site featured <slug> <slot>`."
        ),
    )
    reorder.add_argument("slug", help="Writeup slug to move.")
    reorder.add_argument(
        "position",
        type=int,
        help="Target slot (1-indexed), or 0 to unfeature.",
    )
    reorder.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    update_writeup = subparsers.add_parser(
        "update-writeup",
        help=(
            "Update scalar writeup frontmatter fields via "
            "update_writeup_frontmatter and print JSON. Omitted flags leave "
            "fields unchanged. Wrapped by `site manage`."
        ),
    )
    update_writeup.add_argument("slug", help="Writeup slug to update.")
    update_writeup.add_argument("--title", default=None)
    update_writeup.add_argument("--description", default=None)
    update_writeup.add_argument("--published", default=None, choices=["true", "false"])
    update_writeup.add_argument("--published-at", default=None)
    update_writeup.add_argument("--last-reviewed", default=None)
    update_writeup.add_argument("--touch-last-reviewed", action="store_true")
    update_writeup.add_argument("--cover-image", default=None)
    update_writeup.add_argument("--cover-alt", default=None)
    update_writeup.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    touch_reviewed = subparsers.add_parser(
        "touch-reviewed",
        help=(
            "Set last_reviewed to today on a vault doc via update_frontmatter "
            "and print JSON. Exits 0 if ok, 1 otherwise. Wrapped by the drift "
            "guards (cf-dns / adguard / nginx / ts-acl) after a successful "
            "pull — a pull is a review, so the date moves."
        ),
    )
    touch_reviewed.add_argument(
        "relative_path",
        help=(
            "Vault-relative path, e.g. "
            "'02 Infrastructure/AdGuard/DNS Rewrites — homelab.md'."
        ),
    )
    touch_reviewed.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    backfill_aliases = subparsers.add_parser(
        "backfill-aliases",
        help=(
            "Set each folder-note's (`<folder>/index.md`) Obsidian `aliases` to "
            "its `title`, so `[[Title]]` resolves and autocompletes for notes "
            "whose filename is the non-unique `index`. Derived from `title`, so "
            "idempotent — safe to re-run to repair drift. Writeups are left alone."
        ),
    )
    backfill_aliases.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    find = subparsers.add_parser(
        "find",
        help=(
            "Run the section-scoped vault search and print the same menu JSON "
            "the MCP's find_runbook returns: ranked hits, each with its "
            "best-matching section (heading, slug, one-line summary) — never a "
            "body. The human/CLI renderer of the emit-once menu."
        ),
    )
    find.add_argument("query", help="Natural-language query, e.g. 'renew the TLS cert'.")
    find.add_argument(
        "--limit",
        type=int,
        default=5,
        help="Maximum hits to return (default 5, capped at 25).",
    )
    find.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    read = subparsers.add_parser(
        "read",
        help=(
            "Read one vault doc by doc_id and print JSON. With --section, return "
            "just that H2 span (the token-minimal path); without it, the whole "
            "body. Honors the sensitivity gate — restricted bodies are withheld "
            "(no interactive unlock on the CLI path)."
        ),
    )
    read.add_argument("doc_id", help="Stable doc_id, e.g. 'rb-add-nginx-proxy-host'.")
    read.add_argument(
        "--section",
        default=None,
        help="Section slug or heading path from a `find` hit. Omit for the whole body.",
    )
    read.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    task_list = subparsers.add_parser(
        "task-list",
        help=(
            "The backlog board: every `doc_type: task` doc, derived from the "
            "index (project tasks under 01 Projects/<project>/tasks/ + the "
            "07 Backlog/ cross-cutting bucket), filtered and ranked. Default "
            "shows live work (open + active); the thin `backlog` CLI and the "
            "Obsidian cockpit render this — the MCP is the one task brain."
        ),
    )
    task_list.add_argument("--status", default=None, help="Only this status.")
    task_list.add_argument("--project", default=None, help="Only this project (folder or related_projects).")
    task_list.add_argument("--stale-only", action="store_true", help="Only stale (open/active, untouched past the window).")
    task_list.add_argument("--all", dest="include_all", action="store_true", help="Include parked/done/wontfix.")
    task_list.add_argument("--stale-days", type=int, default=14, help="Stale window in days (default 14).")
    task_list.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    promote = subparsers.add_parser(
        "promote-note",
        help=(
            "Promote a captured note (e.g. an 00 Inbox/ capture) into a task, "
            "preserving its body and deleting the source. The capture → task "
            "half of the inbox loop; used by the Obsidian promote command."
        ),
    )
    promote.add_argument("source", help="Vault-relative path to the note.")
    promote.add_argument("--title", required=True, help="Task title.")
    promote.add_argument("--project", default=None, help="Owning project (colocates the task).")
    promote.add_argument("--effort", default="S")
    promote.add_argument("--priority", default="med")
    promote.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    update_fm = subparsers.add_parser(
        "update-frontmatter",
        help=(
            "Update fields in an existing vault doc's frontmatter (the one "
            "writer — doc_id is immutable). Enum + relation fields are validated "
            "against the schema. Used by the Obsidian relation editor so "
            "author-time edits can't dangle."
        ),
    )
    update_fm.add_argument("relative_path", help="Vault-relative path to the doc.")
    update_fm.add_argument("--title", default=None)
    update_fm.add_argument("--doc-type", dest="doc_type", default=None)
    update_fm.add_argument("--system", default=None)
    update_fm.add_argument("--environment", default=None)
    update_fm.add_argument("--status", default=None)
    update_fm.add_argument("--sensitivity", default=None)
    update_fm.add_argument("--set-related-projects", dest="set_related_projects", nargs="*", default=None, help="Replace related_projects (empty clears).")
    update_fm.add_argument("--set-related-assets", dest="set_related_assets", nargs="*", default=None, help="Replace related_assets (empty clears).")
    update_fm.add_argument("--set-tags", dest="set_tags", nargs="*", default=None, help="Replace tags (empty clears).")
    update_fm.add_argument("--touch-last-reviewed", dest="touch_last_reviewed", action="store_true", help="Set last_reviewed to today.")
    update_fm.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    task_projects = subparsers.add_parser(
        "task-projects",
        help=(
            "The task-project universe: every 01 Projects/<project>/ folder a "
            "task can be filed in, with its open-task count. The one owner of "
            "where a task can go — pickers (the Obsidian modal, the cockpit) "
            "derive from this instead of re-walking the vault layout."
        ),
    )
    task_projects.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    task_add = subparsers.add_parser(
        "task-add",
        help=(
            "Author a new task file. With --project it colocates at "
            "01 Projects/<project>/tasks/ and links related_projects; without, "
            "it files a cross-cutting task in 07 Backlog/. Schema-validated, "
            "written through the one atomic serializer."
        ),
    )
    task_add.add_argument("title", help="Imperative task title.")
    task_add.add_argument("--project", default=None, help="Owning project (an 01 Projects/<project>/ folder).")
    task_add.add_argument("--related-projects", nargs="*", default=None, help="Projects a cross-cutting task touches.")
    task_add.add_argument("--effort", default="S", help="Effort S|M|L (default S).")
    task_add.add_argument("--priority", default="med", help="Priority high|med|low (default med).")
    task_add.add_argument("--tags", nargs="*", default=None, help="Tags (default: backlog).")
    task_add.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    task_delete = subparsers.add_parser(
        "task-delete",
        help=(
            "Permanently delete a task file (for mistakes / junk only — finished "
            "or abandoned work should be task-move'd to done/wontfix, which keeps "
            "it queryable). Resolves a bare slug or the full id; refuses non-tasks."
        ),
    )
    task_delete.add_argument("doc_id", help="Task id or slug (task-foo or foo).")
    task_delete.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    task_move = subparsers.add_parser(
        "task-move",
        help=(
            "Move a task to a new status (open|active|parked|done|wontfix). "
            "Stamps closed: on done, clears it on reopen; done tasks are kept so "
            "'what shipped' stays a query. Resolves a bare slug or the full id."
        ),
    )
    task_move.add_argument("doc_id", help="Task id or slug (task-foo or foo).")
    task_move.add_argument("status", help="Target status (open|active|parked|done|wontfix).")
    task_move.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

    hq_manifest = subparsers.add_parser(
        "hq-manifest",
        help=(
            "Build the Severino HQ manifest with the package's shared "
            "frontmatter parser."
        ),
    )
    hq_manifest.add_argument("vault", help="Vault root path.")
    hq_manifest.add_argument(
        "subdirs",
        help="Colon-separated vault subdirectories to index.",
    )
    hq_manifest.add_argument(
        "--report",
        action="store_true",
        help=(
            "Print the full result (missing_frontmatter, duplicates, counts) "
            "as JSON instead of the manifest entries. Backs `hq doctor`."
        ),
    )

    brief = subparsers.add_parser(
        "brief",
        help=(
            "Doc-side vault state in one payload: recent changes, docs overdue "
            "for review, and inbox backlog. The vault leg of the `brief` shell "
            "tool, which composes it with repo and writeup state."
        ),
    )
    brief.add_argument(
        "--days", type=int, default=7,
        help="Recent-changes look-back window in days (default 7).",
    )
    brief.add_argument(
        "--review-after", type=int, default=180, dest="review_after",
        help="Flag docs whose last_reviewed is older than N days (default 180).",
    )
    brief.add_argument(
        "--limit", type=int, default=15,
        help="Max recent commits to return (default 15).",
    )
    brief.add_argument(
        "--pretty", action="store_true", help="Indent the JSON output."
    )

    schema_cmd = subparsers.add_parser(
        "schema",
        help=(
            "Emit the canonical frontmatter schema (enum sets) as JSON. "
            "Severino HQ commits this output and validates against it so the "
            "two systems share one definition."
        ),
    )
    schema_cmd.add_argument(
        "--json",
        action="store_true",
        help="Emit the schema as JSON (the default).",
    )
    schema_cmd.add_argument(
        "--check-doc",
        metavar="PATH",
        help=(
            "Instead of emitting, verify that a human schema doc's enum lines "
            "(doc_type/environment/status/sensitivity) match the canonical "
            "schema. Exit 1 and print mismatches on drift."
        ),
    )

    topology = subparsers.add_parser(
        "topology",
        help=(
            "Derive views from the topology inventory "
            "(02 Infrastructure/Topology/topology.json) — the SSOT for hosts, "
            "containers, DNS rewrites, and access rules. The hosts/DNS/diagram "
            "all derive from this one file; AI reads it via get_topology. "
            "Write/regenerate with `topology-write`."
        ),
    )
    topology.add_argument(
        "--emit",
        choices=["summary", "tables", "doc", "figure", "schema"],
        default="summary",
        help=(
            "summary: AI-grounding JSON (default). tables: the markdown body. "
            "doc: the full Topology.md build artifact. figure: a `brand figure` "
            "topology spec. schema: the declared inventory contract (canonical "
            "JSON HQ validates against)."
        ),
    )
    topology.add_argument(
        "--check-doc",
        metavar="PATH",
        help=(
            "Verify the generated region of a rendered Topology.md matches the "
            "inventory. Exit 1 and print mismatches on drift."
        ),
    )
    topology.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON output (summary / figure).",
    )

    topology_write = subparsers.add_parser(
        "topology-write",
        help=(
            "The validated write path for the authored topology inventory: "
            "validate topology.json and regenerate its derived artifacts "
            "(Topology.md + topology.figure.json) and the last_reviewed stamp. "
            "With --replace, first read a new inventory from stdin (validated) "
            "and write topology.json. Use this instead of hand-regenerating."
        ),
    )
    topology_write.add_argument(
        "--replace",
        action="store_true",
        help="Read a new topology.json from stdin (validated) before regenerating.",
    )
    topology_write.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    infra = subparsers.add_parser(
        "infra",
        help=(
            "Read structured infra datasets through the one registry "
            "(02 Infrastructure/_infra-datasets.json). With no id, list the "
            "catalog; with an id, read that dataset from its declared source "
            "(a JSON cache file, or a doc reference). The CLI face of "
            "list_infra_datasets / get_infra_dataset."
        ),
    )
    infra.add_argument(
        "dataset_id",
        nargs="?",
        default=None,
        help="Dataset id (e.g. dns_rewrites, proxy_hosts, topology). Omit to list.",
    )
    infra.add_argument(
        "--refresh",
        action="store_true",
        help=(
            "Read live via the dataset's drift guard, falling back to the cache "
            "(flagged stale) if the system is unreachable. Default: cache only."
        ),
    )
    infra.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    infra_write = subparsers.add_parser(
        "infra-write",
        help=(
            "Write a reflected dataset's live state (normalized JSON on stdin) "
            "to its cache file, regenerate the doc's generated table region, and "
            "stamp last_reviewed — one atomic-per-file write. The canonical write "
            "behind a drift guard's `pull`."
        ),
    )
    infra_write.add_argument(
        "dataset_id", help="Dataset id, e.g. dns_rewrites, proxy_hosts."
    )
    infra_write.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    describe = subparsers.add_parser(
        "describe",
        help=(
            "Emit this repo's command surface as structured JSON: every "
            "subcommand, its arguments, and help, generated from the argparse "
            "parser itself so it can't drift from --help. The 'Code/guards' leg "
            "of emit-once — AI reads it, a TUI renders a command picker."
        ),
    )
    describe.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    # Blast-radius (effect) per command, on cordon's escalating ladder
    # (schema_version 4). Every MCP CLI fast-path is a local-filesystem op: the
    # five writers mutate the vault, the rest only read — none touch the network
    # or block on a TTY, so only the effect class is recorded. set_effect()
    # annotates each subparser; cordon's emitter reads it back at describe time.
    _effects = {
        "apply-writeup-plan": "vault_write",
        "reorder-featured": "vault_write",
        "update-writeup": "vault_write",
        "touch-reviewed": "vault_write",
        "backfill-aliases": "vault_write",
        "infra-write": "vault_write",
        "topology-write": "vault_write",
    }
    for name, sub in subparsers.choices.items():
        set_effect(sub, _effects.get(name, "read"))

    return parser
