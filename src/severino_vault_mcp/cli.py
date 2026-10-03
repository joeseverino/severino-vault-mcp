"""CLI argument parser construction.

Split out of `__main__` so `describe` introspects the same parser that backs
`--help`.
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
        description=(
            "Local stdio MCP server for Joe's Obsidian vaults. With no subcommand "
            "it serves MCP; subcommands run one governed call against the labs "
            "vault (or the named dataset) and print JSON."
        ),
    )
    parser.add_argument(
        "--fingerprint",
        action="store_true",
        help=(
            "Print a hash of the package's Python sources and exit. tools "
            "compares the installed copy with the source tree to detect a "
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

    update_doc_link = subparsers.add_parser(
        "update-doc-link",
        help="Replace one exact Markdown link in an indexed document body.",
    )
    update_doc_link.add_argument("doc_id")
    update_doc_link.add_argument("label")
    update_doc_link.add_argument("expected_href")
    update_doc_link.add_argument("replacement_href")
    update_doc_link.add_argument("--pretty", action="store_true")

    touch_reviewed = subparsers.add_parser(
        "touch-reviewed",
        help=(
            "Set last_reviewed to today on a vault doc and print JSON. Exits 0 "
            "if ok, 1 otherwise."
        ),
    )
    touch_reviewed.add_argument(
        "relative_path",
        help=(
            "Vault-relative path, e.g. "
            "'03 Runbooks/Generate Internal Service Certificate.md'."
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
            "Section-scoped vault search: ranked hits, each with its "
            "best-matching section (heading, slug, one-line summary), never a "
            "body."
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

    task_reconcile = subparsers.add_parser(
        "task-reconcile",
        help=(
            "Re-home tasks into tasks/ (live) or tasks/done/ (closed) per their "
            "status — the idempotent tidy sweep for statuses edited by hand "
            "(a Base/Properties edit) that didn't move through task-move."
        ),
    )
    task_reconcile.add_argument("--pretty", action="store_true", help="Pretty-print JSON with indentation (default: compact).")

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
        nargs="?",
        default=None,
        help=(
            "Colon-separated vault subdirectories to index. Default: derived "
            "from the MCP config's indexed_dirs plus the slim content dirs "
            "(05 Writeups, 06 Pages) — one list, one owner."
        ),
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
            "tool."
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
        "--contract",
        action="store_true",
        help=(
            "Emit the complete versioned profile contract, including task and "
            "per-document rules. The legacy default remains HQ-compatible."
        ),
    )
    schema_cmd.add_argument(
        "--fingerprint",
        dest="schema_fingerprint",
        action="store_true",
        help="Emit the stable SHA-256 fingerprint of the complete profile contract.",
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

    daily_write = subparsers.add_parser(
        "daily-write",
        help=(
            "Replace the generated brief region of a daily note (markdown on "
            "stdin): the marked MIRROR:daily-brief span only, leaving the "
            "free-capture area untouched. Creates the note from the daily-note "
            "frontmatter contract if absent. The write behind tools' `vault daily`."
        ),
    )
    daily_write.add_argument(
        "--date", help="Daily note date (YYYY-MM-DD; default today)."
    )
    daily_write.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    export = subparsers.add_parser(
        "export",
        help=(
            "Emit a vault's publishable dataset as JSON. `education`: institutions "
            "and courses with their `## Site` bullets, read by jseverino.com and "
            "resume-engine."
        ),
    )
    export.add_argument("dataset", choices=["education"], help="Dataset to emit.")
    export.add_argument(
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

    # Effect per command on cordon's ladder. Every command is a local
    # filesystem op: these mutate the vault, the rest only read.
    _effects = {
        "update-doc-link": "vault_write",
        "touch-reviewed": "vault_write",
        "backfill-aliases": "vault_write",
        "daily-write": "vault_write",
    }
    for name, sub in subparsers.choices.items():
        set_effect(sub, _effects.get(name, "read"))

    return parser
