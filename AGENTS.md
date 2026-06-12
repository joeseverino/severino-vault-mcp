# AGENTS.md — house rules for severino-vault-mcp

Canonical agent guide for this repo. `CLAUDE.md` is a symlink to this file, so
Claude Code and any AGENTS.md-aware tool read the same source. Read this before
editing; it answers what agents otherwise re-derive every session.

Local stdio MCP server (FastMCP) that turns an Obsidian-style ops vault into
structured AI context, plus an operator workflow pack for jseverino.com.

## Repo conventions

- **Solo-authored. Work on `main`.** No `Co-Authored-By` / "Claude" trailers,
  no AI attribution in commit messages. Commit/push only when asked.
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
- `vault.py` — indexing, alias resolution, duplicate-`doc_id` exclusion.
- `vault_write_service.py` — generic frontmatter writes + `touch_reviewed`
  (fast path: skips the `index(force=True)` rebuild the drift guards don't need).
- `vault_query_service.py` — `recent_changes` (git log), `search_body`
  (ripgrep), shared `doc_to_hit` projection.
- `writeup_service.py` — writeup reads/validation/transactions.
- `site_ops_service.py` — jseverino.com D1 readers, schema apply, header check.
- `hq_manifest.py` — HQ manifest synthesis on the shared parser.

Depth lives in `docs/architecture.md`, `docs/ai-safety-security.md`,
`docs/ai-tool-contract.md`. Update those + `CHANGELOG.md` when you change behavior.

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
uv run pytest -q                 # 91 tests, ~3s
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
