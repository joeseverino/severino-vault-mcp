# AGENTS.md — house rules for severino-vault-mcp

Canonical agent guide for this repo. `CLAUDE.md` is a symlink to this file, so
Claude Code and any AGENTS.md-aware tool read the same source. Read this before
editing; it answers what agents otherwise re-derive every session.

Local stdio MCP server (FastMCP) that turns an Obsidian-style ops vault into
structured AI context, plus an operator workflow pack for jseverino.com.

## Repo conventions

- **Solo-authored, but never commit to `main` — branch → PR.** Branch from a
  freshly fetched `origin/main` (`git fetch origin && git checkout -b <name>
  origin/main`), never from a stale local tree (multiple sessions touch these
  repos, so local `main` lags). Push, open a PR, and hand back only on green CI
  with no unresolved review comments; Joe approves or comments, then it merges.
  No `Co-Authored-By` / "Claude" trailers, no AI attribution in commit messages.
  Commit/push/PR only when asked.
- Consumed by the `tools` repo (`~/Documents/Code/Assets/tools/`): `bin/site`,
  `lib/site/manage-tui.mjs`, and the drift guards call the installed console
  script. Changes to tool output or CLI subcommands are a **cross-repo
  contract** change — see below.

## Architecture (the spine)

`server.py` only *registers* FastMCP tools/resources and delegates; the logic
lives in FastMCP-free service modules so the same code backs both the MCP and
the `site` CLI without importing FastMCP for short-lived shell calls:

- `frontmatter.py` — constrained-YAML parse + serialize. `yaml_escape` is the
  **only** scalar escaper; generic *and* writeup writes both go through it.
- `atomic_write.py` — durable replacement. `atomic_write_text` (one file) and
  `transactional_replace` (many files, locked, rollback) share one primitive.
- `paths.py` — `validate_indexed_path` / `path_within_root` (single trust
  boundary; mutations stay inside the vault root).
- `vault.py` — indexing, alias resolution, duplicate-`doc_id` exclusion. Each
  `Doc` carries `sections` (see below) parsed at index time.
- `sections.py` — H2-scoped section chunking for token-minimal retrieval (P1 of
  `docs/federated-retrieval.md`). `parse_sections` (H3-folded, token-cap
  sub-split, doc-unique heading slugs), `resolve_section` (slug or heading-path),
  `section_summary`. FastMCP-free; `search.py` scores spans, `read_doc(section=)`
  returns one. Pure data — no presentation, so the CLI can render the same spans.
- `vault_write_service.py` — generic frontmatter writes + the drift-guard
  fast paths `touch_reviewed` and `update_mirror_block` (section-scoped
  ```json mirror replacement; both skip the `index(force=True)` rebuild the
  guards don't need). `update-mirror-block` is CLI-only by design — never an
  MCP tool, so AI sessions can't write arbitrary JSON into doc bodies.
- `vault_query_service.py` — `recent_changes` (git log), `search_body`
  (ripgrep), shared `doc_to_hit` projection.
- `vault_search_service.py` — the section-menu **single source** for emit-once,
  render-many. `find_sections` (ranked menu, the shape `find_runbook` renders)
  and `read_section` (one span or whole body, gated; restricted withheld with no
  CLI unlock). `server.py` and the `find`/`read` console subcommands both render
  this — never restate the menu shape.
- `cli_introspect.py` — a **thin binding over cordon's Python reference emitter**
  (`cordon_emit`, the `cordon-emit` dependency). `describe_parser` injects this
  repo's inventory coordinates (`group`/`order`) and delegates; cordon owns the
  algorithm and the schema. It projects the argparse parser (built by
  `cli.build_parser`) to a conformant **Cordon v4** contract
  (`https://github.com/joeseverino/cordon`). The "Code/guards" leg of emit-once:
  `--help` made machine-readable and drift-proof. We *introspect* the parser
  where the `tools` repo *declares* via a DSL; both converge on the one schema,
  so `tools describe --repos` folds this CLI in. Per-command blast radius is
  declared with `cordon_emit.set_effect` on each subparser in `cli.build_parser`.
  The contract revision lives in `cordon_emit.SCHEMA_VERSION` (not here); to
  follow a new `cordon-vN.json`, bump the `cordon-emit` pin in `pyproject.toml`.
  Conformance is gated against cordon's own `conformance/validate.mjs`
  (`tests/test_search.py`, skips when cordon isn't a sibling).
- `writeup_service.py` — writeup reads/validation/transactions.
- `site_ops_service.py` — jseverino.com D1 readers, schema apply, header check.
- `hq_manifest.py` — HQ manifest synthesis on the shared parser.
- `schema.py` — the **canonical** frontmatter enum contract (doc types,
  environments, statuses, sensitivities, doc_id prefixes). Edit the sets here
  and nowhere else. `severino-vault-mcp schema --json` emits it; Severino HQ
  commits that JSON (`docs_index/schema.json`) and validates its manifest
  importer against it, so the two systems can't drift on what `hq sync` accepts.
  After changing it: `site reinstall-mcp`, then `hq schema` (regenerates HQ's
  copy), then commit + deploy HQ. Guarded by `tests/test_schema.py` here and
  `docs_index/tests.py` in HQ.

Depth lives in `docs/architecture.md`, `docs/ai-safety-security.md`,
`docs/ai-tool-contract.md`. Update those + `CHANGELOG.md` when you change behavior.
Forward direction (proposal, not yet built): `docs/federated-retrieval.md` —
section-scoped, federated, token-minimal retrieval over the vault *and* sibling
repos' own docs, to kill the code→repo-doc→vault copy step.

## Cross-repo contract (don't break the tools repo)

- Every service returns one dict. Failures use a single
  `{"ok": false, "error": "<message>"}` envelope (singular `error`).
  `manage-tui.mjs` reads `json.error`; CLI subcommands exit 0/1 on `.ok`.
- To expose a tool to the shell: add a subparser + handler in `__main__.py`
  (mirror an existing block), then in the tools repo `site reinstall-mcp`.
  `site` runs the *installed* console script — a stale `uv tool` install is
  real drift, caught by `--fingerprint` (`site doctor`).

## Safety model (keep it symmetric)

- Sensitivity gate: `public`/`internal`/`sensitive` release bodies;
  `restricted` (a.k.a. secret_adjacent) withheld unless local unlock.
- Operator D1 PII mirrors that: `list_contact_submissions` redacts by default
  (abbrev name, masked email, message preview); `include_pii=True` releases
  full rows and writes a body-free audit line via `audit_event`. Same for
  `list_csp_reports` client fields. **Never put body/PII content in the audit
  log** — only the action + a row count.

## Verify (before claiming a change works)

```bash
uv run pytest -q                 # 105 tests, ~3s
uv run ruff check src/ tests/    # lint (CI gate)
scripts/check.sh                 # everything CI runs
```

## Test idioms (save a round-trip)

- Tests are hermetic: a fixture builds a tiny fake vault on disk; no running
  MCP host needed. Each tool is called directly (`server.find_runbook(...)`).
- `_fresh_module(name)` deletes cached `severino_vault_mcp.*` modules and
  re-imports, so module-level `Config.from_env()` picks up `monkeypatch.setenv`.
  Set env **before** calling it.
- `monkeypatch.setattr(server.<module>, "<fn>", ...)` works because services do
  `from .x import y` (binds the name in the module namespace) — patch the
  *attribute on the importing module*, not the source. This is how the
  atomic-write-failure and D1-stub tests inject behavior without real I/O.
- New behavior gets a regression test in the matching `tests/test_*.py`
  (`test_search.py` = vault/write, `test_writeups.py` = writeups,
  `test_site_ops.py` = D1/PII, `test_hq_manifest.py` = manifest).

## Write model

Schema-specific by design: no tool takes an arbitrary path + arbitrary text.
Validate enum fields before touching disk, keep `doc_id` immutable, render
through `serialize_frontmatter`, replace atomically. If you can't name the file
shape, validate the fields, and report exactly what changed, don't expose it as
a write tool.
