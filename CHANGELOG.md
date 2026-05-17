# Changelog

## [Unreleased]

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

[Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.2.2...HEAD
[2.2.2]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.2.1...v2.2.2
[2.2.1]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.2.0...v2.2.1
[2.2.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.1.0...v2.2.0
[2.1.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v2.0.0...v2.1.0
[2.0.0]: https://github.com/joeseverino/severino-vault-mcp/compare/v1.0.0...v2.0.0
[1.0.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v1.0.0
[0.2.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v0.2.0
[0.1.0]: https://github.com/joeseverino/severino-vault-mcp/releases/tag/v0.1.0
