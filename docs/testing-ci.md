# Testing and CI

This project is tested as a local stdio MCP server package. The tests exercise
the Python functions directly and also verify FastMCP resource registration
where that matters.

## Local Commands

Use the wrapper for normal local verification:

```bash
scripts/check.sh
```

For a faster edit loop:

```bash
scripts/check.sh --quick
```

For release-grade local verification, including installed-tool smoke checks:

```bash
scripts/check.sh --release
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

The `search_body` tests require `rg` on `PATH`.

## GitHub Actions

The repository runs five workflows, each scoped to a single concern.

### `ci.yml` — pytest + ruff

Runs on:

- push to `main`
- pull requests targeting `main`

Matrix:

- Python 3.11
- Python 3.12
- Python 3.13

Steps:

1. Check out the repository.
2. Install `ripgrep`, because body search shells out to `rg`.
3. Install `uv` with cache enabled.
4. Install the matrix Python version.
5. Run `uv sync --extra dev`.
6. Run `uv run ruff check src tests`.
7. Run `uv run pytest -q`.
8. Reinstall the project non-editably and smoke-test the installed package:
   import `severino_vault_mcp` and run `severino-vault-mcp doctor` against
   `examples/sample-vault`.

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
- Search ranking for `find_runbook`.
- `read_doc` body release for `public`, `internal`, and `sensitive` docs.
- Default withholding for `secret_adjacent` docs.
- One-request local unlock behavior for `secret_adjacent` docs.
- Audit log writing for secret-adjacent unlock attempts.
- `vault://quick-index` resource behavior.
- `vault://doc/{doc_id}` resource-template behavior.
- Real FastMCP registration for resources and resource templates.
- Frontmatter creation and update validation.
- Full-text body search with frontmatter skipping.
- Permanent exclusion of `secret_adjacent` bodies from `search_body`.
- Project inventory lookup.
- Reproducibility of `examples/sample-vault`.

## Sample Vault Reproducibility

The sample vault lives at `examples/sample-vault/`.

Run the server against it:

```bash
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

Expected sample behavior:

- `vault://quick-index` returns the demo navigation hub.
- `vault://doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find_runbook("generate internal certificate")` ranks `rb-generate-internal-cert` first.
- `vault://doc/infra-offline-ca` withholds body content because the doc is `secret_adjacent`.
- `read_doc("infra-offline-ca", include_secret_adjacent=True)` still requires local unlock.

## CI Security Signal

CI does not prove the MCP is safe by itself. It does prove that the core safety
contracts are regression-tested:

- `secret_adjacent` bodies are withheld by default.
- `include_secret_adjacent=True` is not sufficient on its own.
- `search_body` cannot reveal secret-adjacent snippets.
- Path validation prevents write tools from escaping the vault root.
- Frontmatter enum validation rejects malformed metadata writes.
