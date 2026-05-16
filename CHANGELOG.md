# Changelog

## [Unreleased]

## [0.2.0] — 2026-05-16

### Changed

- **Sensitivity gate loosened to match the actual threat model.** The MCP runs
  locally and is consumed by Joe's own Claude Code / Claude Desktop session;
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

[Unreleased]: https://github.com/joeseverino/severino-knowledge-router/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/joeseverino/severino-knowledge-router/releases/tag/v0.1.0
