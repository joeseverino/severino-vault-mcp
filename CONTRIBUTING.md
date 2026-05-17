# Contributing

Thanks for taking the time to improve `severino-vault-mcp`.

This project is a local stdio MCP server for operational runbooks and
Obsidian-style vaults. Contributions should preserve the core safety model:
local-first operation, predictable vault reads, narrow validated writes, and no
default release of `secret_adjacent` bodies.

## Local Setup

```bash
git clone git@github.com:joeseverino/severino-vault-mcp.git
cd severino-vault-mcp
uv sync --extra dev
uv run pytest
uv run ruff check .
```

Run the sample vault:

```bash
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

The server speaks stdio, so it waits for an MCP client and does not print a
web URL.

## Development Guidelines

- Keep the server local-first. Do not add a network listener unless it is
  explicitly optional and documented with a clear security model.
- Keep write tools narrow and schema-validated.
- Do not log markdown body content, unlock phrases, or secrets.
- Do not broaden `secret_adjacent` release paths. `include_secret_adjacent=True`
  must remain insufficient without local unlock approval.
- Prefer standard-library code for lightweight parsing unless a dependency
  clearly improves correctness.
- Update `README.md`, `QUICKSTART.md`, and `docs/testing-ci.md` when behavior
  or setup changes.

## Tests

Run before opening a pull request:

```bash
uv run pytest
uv run ruff check .
```

The test suite covers:

- Vault indexing and frontmatter parsing.
- MCP resource registration.
- Search and body search behavior.
- Sensitivity gates and local unlock flow.
- Validated frontmatter writes.
- Sample-vault reproducibility.

## Pull Requests

Good pull requests include:

- A concise description of the behavior change.
- Tests for new behavior or a clear note explaining why tests were not added.
- Documentation updates when user-facing setup or semantics change.
- No unrelated formatting churn.

## Security Reports

Please do not report vulnerabilities through public issues. Follow
`.github/SECURITY.md` and email github@jseverino.com.
