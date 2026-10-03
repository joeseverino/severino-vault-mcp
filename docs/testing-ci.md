# Testing and CI

This project is tested as a local stdio MCP server package. The tests exercise
the Python functions directly and also verify FastMCP resource registration
where that matters.

The suite covers vault composition and a real stdio `list_tools`
(`tests/test_server.py`), the shared tools on a fake labs vault
(`tests/test_search.py`), the edu dataset (`tests/test_education.py`), HQ
manifest generation (`tests/test_hq_manifest.py`), CLI dispatch
(`tests/test_cli_dispatch.py`), and the daily-note/doctor surfaces.
`tests/conftest.py` keeps every test off the machine's real edu and life
configs. The generic
vault-governance core is tested in the [`severino-vault-engine`](https://github.com/joeseverino/vault-engine)
repo, which this server depends on — so the core's behavior isn't re-tested here.

## Local Commands

Use the wrapper for normal local verification:

```bash
scripts/check.sh
```

Run one check by id (the rerun line in a failure report):

```bash
scripts/check.sh --only pytest
```

Install dependencies:

```bash
uv sync --extra dev
```

Run the test suite:

```bash
uv run pytest
```

Run lint:

```bash
uv run ruff check .
```

The `find(by="text")` tests require `rg` on `PATH`.

## GitHub Actions

The repository runs five workflows, each scoped to a single concern.

### `ci.yml`: the cordon gate

Runs on pushes to `main` and pull requests. It calls cordon's reusable gate
(`cordon-gate.yml@v2`), which detects the uv stack and runs ruff, pytest,
version alignment, and an import smoke of the built wheel, plus cordon's repo
invariants. `scripts/check.sh` runs the same engine locally.

### `codeql.yml` — SAST

GitHub CodeQL with the `security-and-quality` query suite for Python. Runs on
push to `main`, pull requests targeting `main`, and weekly on Monday at
06:17 UTC. Findings appear in the repository Security tab.

### `pip-audit.yml` — SCA

`pypa/gh-action-pip-audit` over the exported `uv` lock. Runs on push to `main`,
pull requests targeting `main`, and weekly on Wednesday at 08:11 UTC so that
new CVEs against pinned dependencies surface even when no code has changed.

### `scorecard.yml` — project governance

OSSF Scorecard. Runs on push to `main`, on branch protection rule changes, and
weekly on Tuesday at 07:23 UTC. Results are published to scorecard.dev and
uploaded as SARIF for the Security tab.

### `dependabot.yml` — dependency update PRs

Configured in `.github/dependabot.yml`. Opens PRs against `main` when
dependencies have available updates. Complementary to `pip-audit`, which
catches CVEs against the *current* pin.

## What the Tests Cover

`tests/test_search.py` covers:

- Vault indexing from frontmatter-bearing markdown files.
- `doctor` validation for missing and invalid frontmatter.
- Search ranking for `find`.
- `read_doc` body release for `public`, `internal`, and `sensitive` docs.
- Default withholding for `restricted` docs.
- One-request local unlock behavior for `restricted` docs.
- Audit log writing for restricted unlock attempts.
- `vault://labs/quick-index` resource behavior.
- `vault://labs/doc/{doc_id}` resource-template behavior.
- Real FastMCP registration for resources and resource templates.
- Quick Index recommendations only becoming `recommended` when they agree with
  the top-ranked doc.
- Frontmatter creation and update validation.
- Duplicate `doc_id` values are excluded from search/read results and reported
  as ambiguous with every conflicting path.
- Multiline frontmatter values survive generic mutations.
- Simulated atomic replacement failure leaves the original document unchanged.
- Full-text body search with frontmatter skipping.
- Permanent exclusion of `restricted` bodies from `find(by="text")`.
- Project lookup with `find(by="project")`.
- Reproducibility of `examples/sample-vault`.

`tests/test_hq_manifest.py` covers shared multiline parsing and fail-closed
duplicate-ID handling for HQ imports.

`tests/test_server.py` covers labs-only, labs+edu and all-three compositions,
the `vault` argument's schema, labs overrides never reaching edu, and a real
stdio `list_tools` round trip.

## Sample Vault Reproducibility

The sample vault lives at `examples/sample-vault/`.

Run the server against it:

```bash
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

Expected sample behavior:

- `vault://labs/quick-index` returns the demo navigation hub.
- `vault://labs/doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find("generate internal certificate")` ranks `rb-generate-internal-cert` first.
- `vault://labs/doc/infra-offline-ca` withholds body content because the doc is `restricted`.
- `read_doc("infra-offline-ca", include_restricted=True)` still requires local unlock.

## CI Security Signal

CI does not prove the MCP is safe by itself. It does prove that the core safety
contracts are regression-tested:

- `restricted` bodies are withheld by default.
- `include_restricted=True` is not sufficient on its own.
- `find(by="text")` cannot reveal restricted snippets.
- Path validation prevents write tools from escaping the vault root.
- Frontmatter enum validation rejects malformed metadata writes.
- A tool call only reaches the vault its `vault` argument names.
