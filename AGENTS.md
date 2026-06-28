# AGENTS.md — house rules for severino-vault-mcp

Canonical agent guide for this repo. `CLAUDE.md` is a symlink to this file, so
Claude Code and any AGENTS.md-aware tool read the same source. Read this before
editing; it answers what agents otherwise re-derive every session.

Local stdio MCP server (FastMCP) that turns an Obsidian-style ops vault into
structured AI context, plus an operator workflow pack for jseverino.com.

**This repo is a thin domain server on [`severino-vault-engine`](https://github.com/joeseverino/vault-engine).**
The generic vault-governance core (index, search, sensitivity gate, atomic
writes, task ledger, schema-profile framework, `register_core`) lives in the
engine, distributed on PyPI as `severino-vault-engine` (import name
`vault_engine`) and pinned in `pyproject.toml`. What's *here* is the Labs profile
binding plus the jseverino.com domain tools. When you reach for a generic
behavior, it's in the engine — change it there, release, bump the pin; don't
re-implement it locally. See the engine's own `AGENTS.md` for the core's rules.

## Architecture (the spine)

The spine moved to the engine. This repo composes it. `server.py` is a thin
composition root — it builds one `ServerContext`, calls `register_core` for the
engine generics, then registers the Labs tool groups:

```python
_CTX = ServerContext(Config.from_env())          # engine; defaults to LABS_PROFILE
register_core(mcp, _CTX, build_parser=build_parser)  # engine: the 18 generic tools
site_ops_tools.register(mcp, _CTX)               # Labs domain groups (this repo)
writeups_tools.register(mcp, _CTX)
topology_tools.register(mcp, _CTX)
infra_datasets_tools.register(mcp, _CTX)
```

**In the engine (`vault_engine`), not here** — don't edit these in this repo:
`config`, `context` (`ServerContext`), `core_tools` (`register_core` + the 18
generic tools), `frontmatter` (the one serializer/`yaml_escape`), `atomic_write`
(`atomic_write_text` / `transactional_replace`), `paths`, `vault`, `sections`,
`search`, `sensitivity`, `jsonio`, `mirror`, `tabular`, `daily_notes`,
`daily_write`, `task_service`, `brief_service`, `secret_unlock`, `doctor`,
`cli_introspect` (the cordon `describe` binding), `vault_write_service`,
`vault_search_service`, `vault_query_service`, and `schema` — which carries
`SchemaProfile` **and** `LABS_PROFILE`, the canonical Labs enum contract. Edit
the Labs doc-types/statuses/prefixes in the **engine's** `schema.py`, not here.

**In this repo** — the Labs domain layer + the composition/CLI surface:

- `server.py` — composition root (above). No `@mcp.tool()` wall anymore; it wires
  `register_core` + the four Labs groups onto one `ServerContext`.
- `cli.py` — `build_parser()`: the argparse CLI surface (incl. `schema`, the
  domain writers `topology-write` / `infra-write` / `daily-write`, and the
  `find` / `read` console subcommands). Per-command blast radius is declared with
  `cordon_emit.set_effect` on each subparser; the engine's `cli_introspect`
  projects this parser to the Cordon contract for `tools describe --repos`.
- `__main__.py` — CLI dispatch over `build_parser`.
- `tools/` — the FastMCP **registration groups**, one `register(mcp, ctx)` per
  domain, thin wiring over the `labs/` services: `tools/site_ops.py`,
  `tools/writeups.py`, `tools/topology.py`, `tools/infra_datasets.py`.
- `labs/` — the Labs **domain logic** (FastMCP-free, so the same code backs both
  the MCP and the `site` CLI):
  - `labs/writeup_service.py`, `labs/writeups.py` — writeup reads/validation/transactions.
  - `labs/site_ops_service.py` — jseverino.com D1 readers, schema apply, header check.
  - `labs/topology.py` — authored inventory + the CLI-only `topology-write`
    (validate `topology.json`, regenerate `Topology.md` + the figure, stamp
    `last_reviewed`; `--replace` reads a new inventory on stdin).
  - `labs/infra_datasets.py` — the infra-dataset registry (`_infra-datasets.json`):
    the sensitivity-gated read model (`get_infra_dataset`, cache + `--refresh`
    read-through with fallback) and the CLI-only `infra-write` (JSON cache +
    generated doc table + `last_reviewed`). `topology-write` / `infra-write` are
    CLI-only **by design** — never MCP tools, so AI sessions can't write arbitrary
    JSON into the vault.
  - `labs/hq_manifest.py` — HQ manifest synthesis on the shared parser.
  - `labs/tech_groups.py` — the technology-taxonomy checks.

The schema contract still flows out through this repo's CLI: `severino-vault-mcp
schema --json` emits `LABS_PROFILE.as_dict()` (defined in the engine); Severino
HQ commits that JSON (`docs_index/schema.json`) and validates its manifest
importer against it, so the two systems can't drift on what `hq sync` accepts.
After changing the Labs profile (in the **engine**): release the engine + bump
the pin here, `site reinstall-mcp`, then `hq schema` (regenerates HQ's copy),
then commit + deploy HQ. Guarded by `docs_index/tests.py` in HQ.

Depth lives in `docs/architecture.md`, `docs/ai-safety-security.md`,
`docs/ai-tool-contract.md`. Update those + `CHANGELOG.md` when you change behavior.
Forward direction (proposal, not yet built): `docs/federated-retrieval.md` —
section-scoped, federated, token-minimal retrieval over the vault *and* sibling
repos' own docs, to kill the code→repo-doc→vault copy step.

## Cross-repo contract (don't break the tools repo)

- Every service returns one dict. Failures use a single
  `{"ok": false, "error": "<message>"}` envelope (singular `error`).
  `manage-tui.mjs` reads `json.error`; CLI subcommands exit 0/1 on `.ok`.
- To expose a tool to the shell: add a subparser in `cli.build_parser` (with a
  `cordon_emit.set_effect`) and a handler in `__main__.py` (mirror an existing
  block), then in the tools repo `site reinstall-mcp`.
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
uv run pytest -q                 # 163 tests, ~3s
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
- New behavior gets a regression test in the matching `tests/test_*.py`:
  `test_search.py` = vault/write, `test_writeups.py` = writeups,
  `test_site_ops.py` = D1/PII, `test_hq_manifest.py` = manifest,
  `test_topology.py` / `test_infra_datasets.py` = the authored/pulled infra
  writers, `test_cli_dispatch.py` = CLI wiring, `test_daily_write.py` /
  `test_doctor.py` = the daily-note + doctor surfaces. Generic-core behavior is
  tested in the **engine** repo, not here.

## Write model

Schema-specific by design: no tool takes an arbitrary path + arbitrary text.
Validate enum fields against the active `SchemaProfile` before touching disk,
keep `doc_id` immutable, render through the engine's `serialize_frontmatter`
(`vault_engine.frontmatter`), and replace atomically via the engine's
`atomic_write_text` / `transactional_replace` — never hand-roll YAML or
`open(path, "w")`. If you can't name the file shape, validate the fields, and
report exactly what changed, don't expose it as a write tool.
