# Changelog

## [2.4.0](https://github.com/joeseverino/severino-vault-mcp/compare/v2.3.0...v2.4.0) (2026-06-16)


### Features

* add section-scoped retrieval ([0f6a888](https://github.com/joeseverino/severino-vault-mcp/commit/0f6a8881d3f955e16d10ef65d3a95dfc80cd5eca))
* describe carries schema_version 3 + per-command effect ([#11](https://github.com/joeseverino/severino-vault-mcp/issues/11)) ([80f7e65](https://github.com/joeseverino/severino-vault-mcp/commit/80f7e65fa0b292ee5fe76a588dfebcb314c7a200))
* emit the shared v4 describe contract (federation parity) ([#14](https://github.com/joeseverino/severino-vault-mcp/issues/14)) ([a51ac09](https://github.com/joeseverino/severino-vault-mcp/commit/a51ac091e4ec8dc1980f0f476b41270fb6d59d0a))
* emit-once render-many — CLI section retrieval + describe ([#10](https://github.com/joeseverino/severino-vault-mcp/issues/10)) ([97a4247](https://github.com/joeseverino/severino-vault-mcp/commit/97a4247c98495eaaa760b6dbd9f0758ea90a0b67))
* gate describe against cordon's validator, name the Cordon standard ([#15](https://github.com/joeseverino/severino-vault-mcp/issues/15)) ([d13a1f4](https://github.com/joeseverino/severino-vault-mcp/commit/d13a1f457b1db831f066611b2aca9edbf18178af))
* redact contact/CSP PII by default behind an include_pii gate ([60b6bfc](https://github.com/joeseverino/severino-vault-mcp/commit/60b6bfc016651ab2962da66416e0d65cd9ee27e4))
* schema --check-doc and hq-manifest --report for downstream guards ([1e3fab9](https://github.com/joeseverino/severino-vault-mcp/commit/1e3fab911aa1f8295370717131eace54c0225f43))
* single-source the frontmatter schema; emit it for HQ ([b623c9f](https://github.com/joeseverino/severino-vault-mcp/commit/b623c9f7375ee535d1abca197359a4c128561c98))
* stopword-aware ranking + section-scoped update-mirror-block writer ([dc88d6e](https://github.com/joeseverino/severino-vault-mcp/commit/dc88d6e165537215631041861f03cbc4b6e504c9))


### Bug Fixes

* **ci:** re-pin gate to [@main](https://github.com/main); make check.sh the one independently-runnable gate ([#20](https://github.com/joeseverino/severino-vault-mcp/issues/20)) ([2d8a578](https://github.com/joeseverino/severino-vault-mcp/commit/2d8a5780df4c7cb3c10ce566f0acdba4eafed157))


### Documentation

* add AGENTS.md as the canonical agent guide; symlink CLAUDE.md ([2a66c48](https://github.com/joeseverino/severino-vault-mcp/commit/2a66c4833e2a89ec3a5f37841770af3b1eb90a0a))
* build out the sample vault to mirror the real structure + add a README ([#12](https://github.com/joeseverino/severino-vault-mcp/issues/12)) ([be8d373](https://github.com/joeseverino/severino-vault-mcp/commit/be8d373abe072df6b168a6c8186c00a3841d3351))
* correct AGENTS workflow — branch from fresh origin/main, then PR ([#16](https://github.com/joeseverino/severino-vault-mcp/issues/16)) ([1f8a63b](https://github.com/joeseverino/severino-vault-mcp/commit/1f8a63b463a1a0b32e82480037f00de9baa50a0f))
* document the touch-reviewed subcommand ([5b0cb41](https://github.com/joeseverino/severino-vault-mcp/commit/5b0cb412c615629b2399983616a988f8ee3683a5))
* note the sample vault is exercised in CI ([#13](https://github.com/joeseverino/severino-vault-mcp/issues/13)) ([da5eb10](https://github.com/joeseverino/severino-vault-mcp/commit/da5eb10b0bf6303beb10cbe1a39b7d70db767232))
* resolve the 6 open decisions in the federated-retrieval proposal ([3a83ea2](https://github.com/joeseverino/severino-vault-mcp/commit/3a83ea2c8e45a8b3ef1b5acbb1f923df9ee42407))

## [Unreleased]

### Changed

- **`describe` now emits a conformant [Cordon v4](https://github.com/joeseverino/cordon)
  contract** (was a v3 subset), so `tools describe --repos` folds this repo in as
  a *homogeneous* sibling: same `schema_version`, `group`/`order` inventory
  metadata, and `paras`/`examples` fields (empty here — the MCP CLI has no
  per-command prose). `cli_introspect.py` drops the argparse-only `type`/`default`
  keys that the schema forbids (`additionalProperties: false`) and always emits
  `takes_value`. Cordon (canonical schema `cordon-v4.json`) is the single source
  of the contract; `tools describe --repos` validates this output against it, and
  a regression test here also pipes `describe` through cordon's own
  `conformance/validate.mjs` (plus an always-on structural test) — one schema,
  validated from both repos, no second contract to drift. The per-command
  `effect` blast-radius signal is unchanged (the five vault writers stay
  `vault_write`); it now rides in the full v4 shape. Tests in
  `tests/test_search.py`.

- `find_runbook` / `get_runbook` ranking now (a) strips filler stopwords from
  the query so natural-language intents ("edit a `.age` file in place") don't
  manufacture matches, and (b) adds a small, capped signal for query terms that
  appear only in a doc's body — enough to surface a runbook that documents a
  command in prose ("test resolver latency", "encrypt a file") without letting
  body length outrank a direct title/tag hit. Measured against the vault's
  Quick Index intent→doc tables as ground truth (`scripts/eval_ranking.py`):
  top-1 50→57, top-3 69→74, top-5 80→88 of 122 real queries. Regression tests
  in `tests/test_search.py` lock both behaviors against the fixture vault.

### Added

- **Emit-once CLI rendering of section retrieval (vault decision record
  `report-emit-once-render-many`).** A new FastMCP-free `vault_search_service.py`
  single-sources the section *menu* computation; `server.py`'s `find_runbook`
  now renders from it instead of its own copy, so the MCP and the shell can't
  drift. Two console subcommands render the *same* payload for the human/TUI
  path: `find <query> [--limit N]` prints the ranked section menu
  (`{ok, query, indexed_doc_count, hits:[{…, heading, section, section_summary}]}`,
  never a body), and `read <doc_id> [--section <slug-or-heading-path>]` prints
  one section span (or the whole body), honoring the sensitivity gate —
  restricted bodies are withheld with no interactive unlock, matching
  `search_body` (the one-shot local unlock stays a `read_doc`/MCP affordance).
- **`describe` console subcommand — the repo's command surface as structured
  JSON.** Generated by walking the argparse parser itself
  (`cli_introspect.describe_parser`), so it can't drift from `--help`. Emits
  `{ok, name, description, global_options, commands:[{name, summary,
  args:[{name, positional, required, help, flags?, choices?, default?,
  takes_value?}]}]}`. The "Code/guards" leg of emit-once, render-many: an AI
  session reads it token-minimally instead of parsing `AGENTS.md`, a TUI renders
  it as a command picker, and a guard can diff it. The parser is now built in
  `build_parser()` so `describe` introspects the exact parser that backs
  `--help`. Also exposed as the `describe_commands` MCP tool (same JSON), so an
  AI session can learn the CLI surface in one structured call instead of reading
  the scripts or always-loaded prose. Regression tests in `tests/test_search.py`.

- **Section-scoped retrieval (P1 of `docs/federated-retrieval.md`).** `sections.py`
  chunks every doc body into addressable H2 spans (H3+ folded in; an over-cap
  section sub-splits at its H3 boundaries, then hard-wraps), each with a
  doc-unique heading slug. `find_runbook` / `get_runbook` hits now carry a
  section menu line (heading, slug, one-line summary — never a body), and
  `read_doc(doc_id, section="<slug-or-heading-path>")` returns just that span
  instead of the whole body. `get_runbook` returns the matched section's body
  when a section actually scores the query, and falls back to the full body on a
  metadata-only match so the answer is never dropped. Fully additive: the
  no-`section` `read_doc` path is byte-identical to before, and the existing
  response keys are preserved (cross-repo contract intact). Vault-only — no
  federation yet. Regression tests in `tests/test_search.py`.

- `scripts/eval_ranking.py` — rank-quality eval that scores `find_runbook`
  against the Quick Index's hand-maintained intent→doc map. A script (needs the
  live vault), not a CI test; it labels nothing, but its misses cluster into
  ranker bugs vs. doc-metadata gaps.

- `update_mirror_block()` and the matching stdin-driven
  `update-mirror-block <relative-path> --heading <h> [--touch-reviewed]`
  console subcommand replace the fenced ```json mirror block under one
  heading in a vault doc. It is the canonical writer behind the drift guards'
  `pull` (cf-dns / adguard / ts-acl): the block search is scoped to the named
  section (code fences skipped), so a second mirror in the same doc can never
  be matched or clobbered — the section-bleed the guards' old awk had — and
  the block plus the optional `last_reviewed` stamp land in **one** atomic
  write through the shared path validation and frontmatter serializer. The
  payload is stored verbatim (it is the guard's own `normalize` output) and
  only validated to parse as JSON. CLI-only by design — not registered as an
  MCP tool, so an AI session cannot write arbitrary JSON into doc bodies.
- `schema` console subcommand (`severino-vault-mcp schema --json`) emits the
  canonical frontmatter enum contract as stable, sorted JSON. Severino HQ
  commits this output and validates its manifest importer against it, so the MCP
  and HQ share one definition of what `hq sync` accepts (the tools repo's
  `hq schema` regenerates HQ's copy; both sides have drift-guard tests).
- `schema --check-doc <path>` verifies a human schema doc's enum lines
  (doc_type/environment/status/sensitivity) against the canonical sets and exits
  1 on drift — backs `hq schema`'s guard for the vault's Frontmatter Schema doc.
- `hq-manifest --report` prints the full structured result (missing_frontmatter,
  duplicates, counts) instead of the entries, so `hq doctor` reports the
  vault↔HQ gap through the one manifest contract instead of re-walking the vault.

- `writeup_dashboard()` and the matching `writeup-dashboard` console command
  return all writeup summaries, featured order, and validation results from
  one shared snapshot.
- `apply_writeup_plan(plan)` and the matching stdin-driven
  `apply-writeup-plan` console command apply multiple scalar updates plus the
  complete featured order in one locked transaction.
- Transaction tests simulate a mid-replacement failure and verify rollback.
- Shared `frontmatter.py` parsing now serves vault indexing, generic writes,
  doctor validation, writeups, and HQ manifest generation.
- `hq-manifest <vault> <dir-a:dir-b>` replaces the tools repository's separate
  manifest parser.
- Console-script subcommand `touch-reviewed <relative-path> [--pretty]` stamps
  a vault doc's `last_reviewed` to today. It shares the path validation and
  frontmatter serializer used by the write tools but skips the vault-cache
  rebuild, since the drift guards only need the file on disk updated. Prints
  JSON and exits 0/1 on `ok`. It backs the tools-repo drift guards
  (`cf-dns` / `adguard` / `nginx` / `ts-acl`): a successful `pull` calls it
  so the vault mirror's review date moves with the pull — a pull is a review.

- Console-script subcommands `list-writeups [--filter]`,
  `technology-catalog`, `validate-all-writeups [--include-drafts]`,
  `reorder-featured <slug> <position>`, and
  `update-writeup <slug> [--title|--description|--published|...]`, printing
  the same JSON as the matching MCP tools. They back `site featured`,
  `site tech`, `site manage`, and the slug-free gate inside `site publish`,
  keeping all writeup-frontmatter and catalog parsing in this package.
  (A redundant `set-writeup-published` subcommand existed briefly pre-release;
  publish flips go through `update-writeup --published`.)
- Writeup summaries (`list_writeups`, `validate_writeup.frontmatter`) now
  include `description`, `cover_image`, and `cover_alt`, so the `site manage`
  detail view can render and edit the full scalar frontmatter surface.
- `update_writeup_frontmatter` now accepts `cover_alt`, a one-sentence
  description of what the cover image shows. The site's listing card and
  article hero use it as the `<img alt>` (falling back to the writeup title),
  which improves image SEO and screen-reader output. `prepare_writeup_publish`
  flags missing `cover_alt` as a nit when a `cover_image` is present.
- The vault indexer now recognizes the slim reference frontmatter shape
  (`type: reference, tags, created`) and synthesizes a `ref-<stem>` doc_id
  for those docs so they surface in `search_body` and `find_runbook` results
  without forcing them to adopt the HQ shape. Defaults: `doc_type: reference`,
  `sensitivity: public`.
- `validate_all_writeups(only_published=True)` — batch validation across every
  writeup, returning aggregated blockers/nits and the failing-slug list in one
  call. Replaces the implicit "loop validate_writeup over N slugs" pattern.
- Added portfolio-grade architecture, operator workflow, and AI tool-contract
  documentation. The docs now distinguish the reusable vault MCP surface from
  the visible jseverino.com workflow pack while showing the concrete systems
  behind the operator tooling.
- Added `list_featured_writeup_order()` as the low-token fast path for local
  models answering "what is the current featured/home writeup order?"

### Changed

- Canonicalized `schema.py` to match the downstream HQ contract: dropped the
  unused `environment: lab` value and the `secret_adjacent`/`credential_adjacent`/
  `confidential` sensitivity aliases from the write-validation sets (the runtime
  `Sensitivity.parse` still maps legacy aliases defensively). This closes a
  latent drift where the MCP would write a value `hq sync` then rejected.
- Standalone writeup console commands now import a FastMCP-free service layer
  instead of importing the full MCP server registration module.
- `server.py` now delegates generic writes, writeup operations, and the
  jseverino.com D1/header tools to focused service modules instead of carrying
  duplicate implementations; tool bodies are thin pass-throughs.
- Consolidated the write path so logic exists once: the constrained-YAML
  serializer (`serialize_frontmatter`/`yaml_escape`) lives only in
  `frontmatter.py`, and vault path validation (`validate_indexed_path`,
  `path_within_root`) lives only in the new `paths.py`. Previously each writer
  carried its own copy.
- Extracted the jseverino.com Cloudflare D1 readers, confirmed schema apply,
  and live security-header check into a FastMCP-free `site_ops_service.py`
  behind a `SiteOpsRuntime`, matching the writeup and vault-write services.
- Standardized every service failure on a single `{"ok": false, "error": "…"}`
  envelope (schema validation joins multiple messages), so the MCP, the `site`
  CLI, and `site manage` parse one shape.
- Folded the standalone `touch_reviewed` writer into `vault_write_service.py`
  (removing the duplicate `frontmatter_service.py`) and dropped dead code:
  the unused operator path-guard trio and stale writeup-dir constants in
  `server.py`, and the `_writeup_summary`/`_render_frontmatter` wrappers.
- Unified durable file replacement in a new `atomic_write.py`: `atomic_write_text`
  (single file) and the writeup `transactional_replace` (multi-file, locked,
  rollback) now share one staged-tempfile + `fsync` + `os.replace` primitive
  instead of hand-rolling the dance twice.
- Writeup frontmatter writes now escape scalars through the same
  `frontmatter.yaml_escape` the generic serializer uses, so a writeup field
  with YAML-special characters (`[`, `,`, `:`) quotes identically to the same
  text in a vault doc. Previously the writeup path used a divergent ruleset.
- Extracted the two shell-backed read tools (`recent_changes`, `search_body`)
  and the shared `doc_to_hit` projection into a FastMCP-free
  `vault_query_service.py`, leaving `server.py` as registration-and-delegation
  only (~1,180 lines, down from ~1,390).
- `get_technology_catalog` reuses a `WriteupContext` snapshot's catalog when one
  is passed, matching the context-sharing pattern of the other writeup readers
  instead of re-reading the catalog markdown.
- Condensed the writeup-workflow section of the MCP server instructions: the
  per-tool paraphrases that duplicated each tool's docstring are now a grouped
  read/validate/mutate mandate, preserving every rule (fast path, never-grep,
  never-Edit-YAML, transactional reorder, verify-before-shipping).
- Duplicate `doc_id` values are excluded from runtime lookup and search;
  direct reads return an explicit ambiguity response with all conflicting
  paths.
- Generic frontmatter writers now replace files atomically.
- `validate_all_writeups` now loads writeups, technology taxonomy, and vault
  references once per request rather than rescanning them for every writeup.
- `reorder_featured` now stages all changed files before replacement, checks
  for concurrent changes under a lock, and rolls back partial replacements.
- Updated security documentation to use the current `restricted` terminology,
  document every write boundary, and call out the jseverino.com path boundary.
- Updated testing docs for the expanded writeup coverage.
- `list_writeups` now includes compact `order` and `featured_order` fields so
  small/local models do not need to infer ordering from verbose writeup
  summaries.
- Tightened the MCP server instructions for local models: do not print fake
  tool-call text, and route "currently published writeup order" phrasing to
  `list_featured_writeup_order()`.

### Security

- jseverino.com writeup and technology-catalog paths must now resolve inside
  the configured vault root before read or write tools use them. This keeps the
  portfolio workflow pack inside the same filesystem trust boundary as the
  generic vault tools.
- Contact-submission PII now gets a release gate that mirrors the restricted
  vault-doc model. `list_contact_submissions` returns a redacted projection by
  default (abbreviated name, masked email, message preview + char count);
  `include_pii=True` releases full rows and appends a body-free audit line
  (`action=contact_pii_access rows=<n>`). `list_csp_reports` likewise omits
  `ip_address`/`user_agent`/`raw_report` unless `include_pii=True`. Closes the
  asymmetry where the vault read path was gated but the operator D1 read path
  returned PII straight into the model context. The audit writer was generalized
  to a shared `audit_event` so unlock and PII events share one 0600 log.

### Verification

- `scripts/check.sh --quick` passes.
- `uv run pytest -q` passes (91 tests).
- `uv run ruff check .` passes.

## [2.4.6] — 2026-05-30

Tighter defaults — token-budget patch. `prepare_writeup_publish` was
returning per-technology usage stats unconditionally, which added
~300-500 tokens per call to MCP sessions even when the caller only
wanted the go/no-go signal. The CLI subcommand was always
pretty-printing JSON, which is wrong for piping into bash. Both are
fixed.

### Changed

- `prepare_writeup_publish` now takes `include_tag_usage: bool = False`.
  Default omits the `tag_usage` field from the response. Callers that
  need per-tag stats pass `include_tag_usage=True`. Saves ~300-500
  tokens per call in the common path.
- `severino-vault-mcp prepare-writeup-publish <slug>` CLI now prints
  compact JSON by default (no whitespace). Pass `--pretty` for
  indented human-readable output. New `--include-tag-usage` flag mirrors
  the MCP-tool parameter.
- README documents both new flags + the parameter.
- Bumped package version to 2.4.6.

### Verification

- `uv run pytest -q` passes (66 tests; the existing tag-usage test was
  split into a default-off and an opt-in case).
- `uv run ruff check .` passes.

## [2.4.5] — 2026-05-30

CLI surface for shell tooling. Adds a `prepare-writeup-publish`
subcommand to the console script so the operator's `site` bash macro
can gate publishes on the same MCP validation that the AI session uses,
without re-implementing the check in shell or shelling out through
inline Python.

### Added

- `severino-vault-mcp prepare-writeup-publish <slug>` CLI subcommand.
  Runs `prepare_writeup_publish` for the given writeup slug, prints
  the JSON result to stdout, and exits with code 0 if `ok: true` or
  code 1 if there are blockers, missing slugs, missing images, or
  unresolved `related_projects` / `related_assets`. Intended to be
  wrapped by `site publish-writeup` in
  `~/Documents/Code/Assets/tools/site`.

### Changed

- `README.md` documents the new CLI subcommand alongside `doctor`.
- Status section bumped to v2.4.5.
- Bumped package version to 2.4.5.

### Verification

- `uv run pytest -q` still passes (65 tests, no behavior change).
- `uv run ruff check .` passes.
- Smoke-tested locally: `severino-vault-mcp prepare-writeup-publish
  building-a-custom-mcp-layer` returns ok:true (exit 0) against the
  operator's vault.

## [2.4.4] — 2026-05-30

Docs catch-up after v2.4.3. v2.4.3 shipped the write tools and the
dangling-ref validation but did not update the README tool table or the
Status section. This release brings the public docs in sync with the
v2.4.x surface — no code changes.

### Changed

- `README.md` tool table now lists `prepare_writeup_publish` (2.4.2),
  `update_writeup_frontmatter` (2.4.3), and `reorder_featured` (2.4.3),
  and the `validate_writeup` row now mentions the new
  `related_projects` / `related_assets` resolvability check.
- `README.md` Status section bumped to v2.4.4 with a summary of the
  whole 2.4.x writeup-publish surface.
- Bumped package version to 2.4.4.

### Verification

- `uv run pytest -q` still passes (65 tests, no behavior change).
- `uv run ruff check .` passes.

## [2.4.3] — 2026-05-30

Writeup-write tools + tighter validation. v2.4.2 made the read path safe;
this release closes the gap on the write path. Adds two write tools so
writeup frontmatter mutations and featured-list reordering go through
validated, atomic primitives instead of hand-editing YAML across multiple
files (which is the failure mode v2.4.2's changelog called out).

### Added

- `update_writeup_frontmatter(slug, ...)` — single-writeup scalar
  updates (title, description, published, published_at, last_reviewed,
  cover_image, featured, featured_order). Mutates only changed lines;
  surrounding formatting is preserved byte-for-byte. Supports
  `touch_last_reviewed=True` for the common "I re-read this, bump the
  date" operation.
- `reorder_featured(slug, position)` — atomic cross-file shuffling of
  the featured list. Insert at position N, move from current slot,
  or unfeature (position=0). The resulting order is guaranteed
  sequential 1..N with no gaps or duplicates. This replaces today's
  failure mode of hand-editing featured_order across 5+ files in a row.

### Changed

- `validate_writeup` now checks that `related_projects` and
  `related_assets` entries resolve to indexed vault docs. Unresolved
  references are returned in a new `unresolved_refs` field and cause
  `ok: false`. Previously these dangling references shipped silently
  and only HQ's relation-check caught them, hours after publish.
- Server instructions add a "WRITEUP MUTATIONS" section explicitly
  routing frontmatter writes through the two new tools and forbidding
  raw `Edit` on writeup YAML.
- `.gitignore` now excludes `.claude/` (Claude Code session data).
- Bumped package version to 2.4.3.

### Verification

- `uv run pytest -q` passes 65 tests (56 + 9 new for the write tools
  and the validate extension).
- `uv run ruff check .` passes.

## [2.4.2] — 2026-05-30

Writeup-tool ergonomics patch. v2.4.1 added the workflow rule but left
the individual tool docstrings descriptive instead of directive, so a
session reading the docstrings on their own (without the server-level
instructions) still got "this tool reads files" rather than "use this
instead of grepping." This release tightens that and adds one composite
tool so the common publish-prep workflow is one MCP call instead of
three.

### Added

- `prepare_writeup_publish(slug)` — composes `validate_writeup`,
  `list_writeups("featured")`, and per-tag `find_writeups_using_tag`
  checks into one response. Returns publish readiness, the current
  featured order, this writeup's position in it, and tag usage stats.
  Use this before every writeup commit instead of chaining the
  individual tools by hand.

### Changed

- `list_writeups`, `validate_writeup`, `get_technology_catalog`, and
  `find_writeups_using_tag` docstrings now open with directive
  ("USE THIS", "CALL THIS BEFORE") language matching `find_runbook`'s
  pattern. The descriptive opener was too easy to ignore.
- Server instructions add a "VERIFY BEFORE SHIPPING" paragraph telling
  callers to run validation immediately before commit/push, not after.
  This patch exists because the first session to publish under v2.4.1
  committed first, verified second.
- Bumped package version to 2.4.2 in `pyproject.toml` and
  `src/severino_vault_mcp/__init__.py`.

### Verification

- `uv run pytest -q` (53 tests + new prepare_writeup_publish test) passes.
- `uv run ruff check .` passes.

## [2.4.1] — 2026-05-30

Instruction-hardening patch. v2.4.0 shipped the four writeup-publish-prep
tools but did not tell calling AI sessions to use them, so the first real
publish workflow after v2.4.0 had an assistant grep frontmatter by hand,
miscount the featured order, and ship the wrong `featured_order` value.
This release adds an explicit "JSEVERINO.COM WRITEUP WORKFLOW" section to
the server instructions so the next session is told (not just enabled) to
call `list_writeups`, `validate_writeup`, `get_technology_catalog`, and
`find_writeups_using_tag` instead of doing manual file work.

### Changed

- Server instructions (`_SERVER_INSTRUCTIONS` in `server.py`) now include
  a mandatory writeup-workflow section listing the four v2.4.0 tools and
  the specific manual operations they replace.
- Bumped package version to 2.4.1 in `pyproject.toml` and
  `src/severino_vault_mcp/__init__.py`.

### Verification

- `uv run pytest -q` still passes (53 tests, no behavior change).

## [2.4.0] — 2026-05-29

Writeup-tooling release for the jseverino.com portfolio surface. Adds four
read tools that turn writeup-publish prep from a series of grep calls into
a single MCP query. Closes the most common loop of "is this writeup ready,
what tags does it need, what other writeups reference this tag" without
leaving the assistant.

### Added

- `list_writeups(filter)` — enumerate writeups under
  `<vault>/05 Writeups/<slug>/index.md` with `published`, `featured`,
  `featured_order`, and `technologies` summarized. Filters: `all`,
  `published`, `draft`, `featured` (the last sorts by `featured_order`).
- `get_technology_catalog()` — parse the slug catalog at
  `<vault>/06 Pages/_technology-groups.md` and return slugs grouped by
  section with their featured state.
- `find_writeups_using_tag(slug)` — list writeups whose `technologies:`
  reference a given tag. Use this to confirm a tag is referenced by at
  least one published writeup before promoting it to featured.
- `validate_writeup(slug)` — publish-readiness report covering frontmatter
  completeness, technology slugs vs the catalog, and image references vs
  files in the writeup's `images/` folder.
- New module `writeups.py` for the writeup-specific frontmatter shape
  (which differs from the doc_id-keyed schema the main vault loader uses).
- New module `tech_groups.py` for parsing the technology-groups markdown
  catalog (which the main loader skips because the filename is prefixed
  with `_`).
- Environment overrides for the writeup paths:
  `SVMC_JSEVERINO_WRITEUPS_DIR` and `SVMC_JSEVERINO_TECH_GROUPS`. Both
  default to vault-relative paths so most setups need no configuration.

### Changed

- Bumped package version to 2.4.0 in `pyproject.toml` and
  `src/severino_vault_mcp/__init__.py`.

### Verification

- `uv run pytest -q` covers the new tools via `tests/test_writeups.py`
  with fake-vault fixtures for ready / draft / lead writeups and a
  two-section technology catalog.

## [2.3.0] — 2026-05-17

Alias and restricted-sensitivity release. Adds a vault-local alias layer for
natural phrases while cleaning up the public sensitivity vocabulary from
`secret_adjacent` to `restricted`.

### Added

- Optional vault-local aliases at `<vault>/.svmc/aliases.toml`, loaded into the
  cached vault index for fast phrase-to-`doc_id` resolution.
- Safe sample aliases under `examples/sample-vault/.svmc/aliases.toml`.
- `read_doc` alias and exact-title fallback resolution for smaller local models
  that pass a human phrase instead of a stable `doc_id`.
- Helpful `read_doc` not-found guidance pointing assistants back to
  `find_runbook`, `lookup_system`, and `search_body`.
- `include_restricted=True` as the public argument for requesting restricted
  body release through the existing local unlock gate.

### Changed

- Renamed the public sensitivity label from `secret_adjacent` to `restricted`
  across current docs, examples, and sample-vault frontmatter.
- Kept backward-compatible parsing for older `secret_adjacent`,
  `credential_adjacent`, and `confidential` labels.
- Kept backward-compatible `include_secret_adjacent` and
  `SVMC_SECRET_ADJACENT_*` inputs while documenting the new
  `SVMC_RESTRICTED_*` names.
- Clarified `docs/demo.md` so reproducible sample-vault transcript content is
  separated from private-vault local-model screenshots.
- Bumped package version to 2.3.0 in `pyproject.toml`,
  `src/severino_vault_mcp/__init__.py`, `uv.lock`, and README status.

### Verification

- `scripts/check.sh --quick` passes with 36 tests.

## [2.2.2] — 2026-05-17

Documentation proof-of-use patch. Adds real local-model screenshots showing
`severino-vault-mcp` in use from a Mac-hosted MCP client.

### Added

- README usage screenshot showing `qwen2.5-7b-instruct` retrieving a VPS SSH
  runbook answer through `severino-vault-mcp`.
- Demo screenshots for VPS SSH access and homelab container restart flows.
- Security documentation noting that local-model usage is possible.

### Changed

- Bumped package version to 2.2.2 in `pyproject.toml`,
  `src/severino_vault_mcp/__init__.py`, `uv.lock`, and README status.
- Updated README, `docs/demo.md`, and `STRUCTURE.md` to cover local usage and
  the new usage assets.

### Verification

- `scripts/check.sh --quick` passes.

## [2.2.1] — 2026-05-17

Quick Index routing patch. Keeps the existing MCP resource behavior, but also
embeds matching Quick Index rows directly in `find_runbook` and `get_runbook`
responses so smaller local models see the exact operational command without
having to remember to read `vault://quick-index` first.

### Added

- Structured `quick_index_matches` and top-level `recommended` hints for
  `find_runbook` and `get_runbook` results when a Quick Index table row matches
  the user's query.
- Quick Index table parsing for common navigation tables using `Intent`,
  `Symptom`, or `Topic` columns plus `Command`, `First step`, `Start Here`,
  `Doc`, and `Then Read` cells.
- Regression coverage for an AdGuard container-status query where metadata
  ranking selects the architecture note, while the Quick Index recommendation
  carries the exact `docker compose ps` command.

### Changed

- Bumped package version to 2.2.1 in `pyproject.toml`,
  `src/severino_vault_mcp/__init__.py`, and README status.

### Verification

- `scripts/check.sh --quick` passes with 32 tests.

## [2.2.0] — 2026-05-17

Retrieval reliability and schema-consistency release. Adds a safer one-call
runbook retrieval path for local models and removes a duplicated schema enum
that let write-tool validation drift from `doctor` and the documented vault
schema.

### Added

- `get_runbook(query, limit=5)` MCP tool. It returns ranked hits plus the
  selected document body in a single call, while preserving the same
  `secret_adjacent` withholding behavior as `read_doc`. This avoids the
  local-model failure mode where an assistant calls `find_runbook`, mistypes
  the selected `doc_id`, then fills the gap from generic model memory.
- Shared `schema.py` constants for frontmatter enum vocabularies and required
  fields.
- `doctor` duplicate-`doc_id` validation. Duplicate frontmatter IDs now fail
  validation instead of silently overwriting each other in the in-memory
  `by_doc_id` index.
- Regression tests for:
  - `environment: homelab` being accepted by write tools.
  - Duplicate `doc_id` detection.
  - Normal SSH runbook retrieval ranking above SSH recovery procedures.
  - `get_runbook` body release and `secret_adjacent` withholding.

### Changed

- Bumped package version to 2.2.0 in `pyproject.toml` and
  `src/severino_vault_mcp/__init__.py`.
- README MCP surface now documents `get_runbook` and `search_body`.
- README status now reflects v2.2.0 and the single-call retrieval path.
- Release checklist now points at `scripts/release.sh` for repeatable tagged
  releases.
- Added `scripts/check.sh` as the shared local verification entry point for
  routine checks and release checks.
- Added `scripts/prepare-release.sh` to bump versions, refresh `uv.lock`, and
  draft a changelog section before release.
- Added `.gitmessage` so future commits default to a descriptive
  why/what/verification structure.
- Server and doctor validation now import the same schema constants instead of
  maintaining duplicate enum sets.
- Bumped pinned GitHub Action versions via Dependabot, verified green
  across the full CI matrix and code-scanning workflows before merge:
  - `actions/checkout`: `v4` → `v6.0.2`
  - `astral-sh/setup-uv`: `v3` → `v8.1.0`

### Fixed

- `add_frontmatter(..., environment="homelab")` no longer fails validation.
  `homelab` was already valid in the vault schema, existing docs, Severino HQ,
  and `doctor`; only the MCP server write-tool enum was stale.

### Verification

- `uv run ruff check .` passes.
- `uv run pytest -q` passes with 31 tests.
- Real-vault timing sanity check: 70 indexed docs, `doctor` around 0.05s, and
  cold-process `get_runbook` around 0.27s including Python startup.

## [2.1.0] — 2026-05-17

Security tooling and supply-chain hardening release. Adds layered,
public-repo-grade security automation and tightens repository governance
without changing the MCP surface or runtime behavior.

### Added

- `.github/workflows/codeql.yml` — GitHub CodeQL static analysis with the
  `security-and-quality` query suite for Python. Runs on push, PR, and weekly.
- `.github/workflows/pip-audit.yml` — PyPA `pip-audit` against the exported
  `uv` lockfile, so known CVEs against the *current* pinned dependency set
  surface even when no code has changed. Runs on push, PR, and weekly.
- `.github/workflows/scorecard.yml` — OSSF Scorecard governance and supply
  chain scoring. Publishes results to scorecard.dev and uploads SARIF to the
  GitHub Security tab.
- README badges for CodeQL, pip-audit, and OpenSSF Scorecard.
- README `Native Tool Dependencies` section calling out `ripgrep` and `fd`,
  the Rust binaries the server delegates to for body search and indexing.
- `Security Tooling` and `Supply Chain Hardening` sections in
  `.github/SECURITY.md` documenting the layered SAST + SCA + governance model,
  pinned-action convention, least-privilege workflow tokens, and release
  expectations.
- `Branch Protection` and `Releases` sections in `.github/SECURITY.md`
  documenting the on-`main` ruleset and the tag/release convention.
- Per-workflow documentation in `docs/testing-ci.md`.

### Changed

- README `Status` section now reflects v2.1.0 and mentions the security
  tooling layer.
- Bumped package version to 2.1.0 in `pyproject.toml` and
  `src/severino_vault_mcp/__init__.py`.

### Security

- Pinned every GitHub Action reference in `.github/workflows/` to a full
  commit SHA, with the human-readable version kept as a trailing comment so
  Dependabot can still produce update PRs. Mitigates the supply-chain risk of
  a third-party tag being moved to a malicious commit. Closes 11 OSSF
  Scorecard `Pinned-Dependencies` findings.
- Enabled branch protection on `main`: force pushes blocked, deletions
  blocked, linear history required, conversation resolution required, and
  five required status checks (`pytest + ruff` × 3, `Analyze (python)`,
  `Audit pinned dependencies`) for pull requests. Administrators are not
  enforced so the solo maintainer can publish security patches without a PR
  round-trip; this should change if the project gains contributors.

## [2.0.0] — 2026-05-17

Public release. This version turns the project from a private local MCP into a
portfolio-ready, reusable tool for safely grounding AI assistants in
Git-backed Obsidian operations vaults.

### Added

- `CONTRIBUTING.md` with local setup, test/lint commands, contribution
  expectations, and security-report guidance.
- `config.example.toml` and TOML config loading, with `SVMC_*` environment
  overrides for demos, CI, and one-off runs.
- `severino-vault-mcp doctor` for required frontmatter validation, plus
  `doctor --propose` for starter frontmatter suggestions.
- `docs/migration-guide.md` with messy-vault onboarding guidance and a
  bad-doc-to-fixed-doc example.
- `docs/release-checklist.md` for public release and portfolio-readiness checks.
- Focused GitHub repository metadata for MCP, Obsidian runbooks, local-first
  AI tooling, network security, and security operations.

### Changed

- Renamed the Python package from `severino_knowledge_router` to
  `severino_vault_mcp`.
- Renamed the local checkout and public command surface to match the GitHub
  repository: `severino-vault-mcp`.
- Reworked README and Quickstart around public adoption: local-first MCP
  positioning, copy/paste setup, MCP client examples, and network/security
  operations use cases.
- Reworked the sample vault into a generic client-edge DNS and internal TLS
  operations scenario.
- Renamed the optional downstream integration setting to
  `SVMC_METADATA_URL`.
- Updated Codex and Claude Code local MCP registrations to the new
  `severino-vault-mcp` name.

### Security

- Replaced the placeholder GitHub security policy with a professional
  vulnerability reporting policy using github@jseverino.com.
- Expanded the security policy with supported versions, scope, out-of-scope
  cases, security boundaries, sensitivity label definitions, safe
  configuration guidance, and disclosure expectations.
- Kept the local-only stdio threat model explicit: no HTTP listener, no hosted
  service, and no broad release of `secret_adjacent` document bodies.

### Verification

- `uv run pytest` passes with 26 tests.
- `uv run ruff check .` passes.
- `severino-vault-mcp doctor` validates the sample vault with no findings.
- Public-readiness scan found no private vault paths. Historical old-name
  references are limited to changelog and release-note migration context.

## [1.0.0] — 2026-05-17

First stable private release.

### Added

- MCP resources:
  - `vault://quick-index` for the navigation hub.
  - `vault://doc/{doc_id}` for stable doc reads by frontmatter ID.
- Reproducible sample vault under `examples/sample-vault/`.
- `QUICKSTART.md` for command-first adoption.
- `STRUCTURE.md` with a file-by-file repository map.
- `docs/demo.md` with an example assistant transcript.
- `docs/testing-ci.md` documenting local checks, CI matrix, and test coverage.
- `docs/ai-safety-security.md` documenting the AI safety model, sensitivity
  gate, secret-adjacent unlock flow, audit logging, and trust assumptions.
- README badges for CI, Python support, MCP transport, and license.
- FastMCP registration smoke tests for resources and resource templates.

### Changed

- Renamed the project/repository to `severino-vault-mcp` so the name clearly
  communicates that this is an MCP server.
- `secret_adjacent` bodies now require explicit `read_doc(...,
  include_secret_adjacent=True)` plus local authorization before release.
- `search_body` never searches `secret_adjacent` bodies, even when the
  deprecated compatibility flag is set.
- `sensitive` docs continue to return body content with advisory text.
- README now links the full documentation set from the front page.

### Security

- Added one-request local unlock gate for `secret_adjacent` docs:
  `SVMC_ALLOW_SECRET_ADJACENT_UNLOCK=1`, configured unlock hash, hidden macOS
  prompt, and audit logging are required before a body can be released.
- Added local audit logging for unlock attempts without recording body content
  or unlock phrases.
- Kept broad body search closed for `secret_adjacent` docs.

### Tests

- 24 pytest cases covering vault indexing, resources, resource templates,
  sensitivity policy, local unlock, audit logging, frontmatter writes,
  full-text search behavior, and sample-vault reproducibility.
- GitHub Actions runs pytest and Ruff on Python 3.11, 3.12, and 3.13.

## [0.2.0] — 2026-05-16

### Changed

- **Sensitivity gate loosened to match the actual threat model.** The MCP runs
  locally and is consumed by the operator's own Claude Code / Claude Desktop session;
  refusing every `sensitive` body was friction, not safety.
  - `public` / `internal` / `sensitive` now all release the body. `sensitive`
    responses include an `advisory` field reminding the caller to handle the
    content carefully.
  - `secret_adjacent` is still withheld by default. `read_doc` now accepts
    `include_secret_adjacent=True` to explicitly opt in; the response records
    that an override was used.
  - Conservative default for missing/unknown sensitivity labels switched from
    `sensitive` → `internal` (so untagged-but-loaded docs stay readable).

### Added

- `search_body` MCP tool: full-text search across vault doc bodies via
  ripgrep (`rg --json`). Skips matches inside frontmatter blocks; honors the
  sensitivity gate (excludes `secret_adjacent` unless `include_secret_adjacent`).
  Returns hits grouped by `doc_id` with snippets + context lines.
- Vault loader uses `fd` (if on PATH) to walk indexed dirs — falls back to
  `pathlib.rglob` if `fd` is missing.

### Tests

- 15/15 passing. New cases cover the sensitivity override, sensitive-with-body
  advisory, search_body across the body / frontmatter boundary, and
  secret_adjacent default-vs-override.

## [0.1.0] — 2026-05-16

Initial release. Local stdio MCP server that exposes vault frontmatter to AI
assistants without leaking secret-adjacent material.

### Added

- Five read tools: `find_runbook`, `lookup_system`, `read_doc`,
  `inventory_for_project`, `recent_changes`.
- Two write tools: `add_frontmatter` (prepends frontmatter to untagged docs),
  `update_frontmatter` (mutates fields in tagged docs; `doc_id` immutable).
- Sensitivity gate in `read_doc`: full body for `public`/`internal`, metadata
  only for `sensitive`, refuse with pointer for `secret_adjacent`.
- Frontmatter parser is hand-rolled — no PyYAML dependency.
- Round-trip frontmatter writer preserves unknown keys (e.g. `created:`).
- Schema-validated writes: `doc_type`, `environment`, `status`, `sensitivity`
  must be known enum values; `doc_id` must use one of the documented prefixes.
- 9 pytest cases covering loader behaviour, search ranking, sensitivity gate,
  and both write tools.

[Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.6...HEAD
[2.4.6]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.5...v2.4.6
[2.4.5]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.4...v2.4.5
[2.4.4]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.3...v2.4.4
[2.4.3]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.2...v2.4.3
[2.4.2]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.1...v2.4.2
[2.4.1]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.4.0...v2.4.1
[2.4.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.3.0...v2.4.0
[2.3.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.2.2...v2.3.0
[2.2.2]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.2.1...v2.2.2
[2.2.1]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.2.0...v2.2.1
[2.2.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.1.0...v2.2.0
[2.1.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.0.0...v2.1.0
[2.0.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v1.0.0...v2.0.0
[1.0.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v1.0.0
[0.2.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v0.2.0
[0.1.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v0.1.0
