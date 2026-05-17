# Testing and CI

This project is tested as a local stdio MCP server package. The tests exercise
the Python functions directly and also verify FastMCP resource registration
where that matters.

## Local Commands

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

CI is defined in `.github/workflows/ci.yml`.

It runs on:

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

## What the Tests Cover

`tests/test_search.py` covers:

- Vault indexing from frontmatter-bearing markdown files.
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
SKR_VAULT_PATH=examples/sample-vault uv run severino-knowledge-router
```

Expected sample behavior:

- `vault://quick-index` returns the demo navigation hub.
- `vault://doc/rb-generate-homelab-cert` returns the sample certificate runbook.
- `find_runbook("generate homelab certificate")` ranks `rb-generate-homelab-cert` first.
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
