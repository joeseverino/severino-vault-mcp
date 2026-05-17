# Changelog

## [Unreleased]

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
  `SKR_ALLOW_SECRET_ADJACENT_UNLOCK=1`, configured unlock hash, hidden macOS
  prompt, and audit logging are required before a body can be released.
- Added local audit logging for unlock attempts without recording body content
  or unlock phrases.
- Kept broad body search closed for `secret_adjacent` docs.

### Tests

- 23 pytest cases covering vault indexing, resources, resource templates,
  sensitivity policy, local unlock, audit logging, frontmatter writes,
  full-text search behavior, and sample-vault reproducibility.
- GitHub Actions runs pytest and Ruff on Python 3.11, 3.12, and 3.13.

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

[Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v1.0.0
[0.2.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v0.2.0
[0.1.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v0.1.0
