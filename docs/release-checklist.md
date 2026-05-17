# Release Checklist

Use this before tagging a public release or linking the project from a
portfolio.

## Repository Hygiene

- `README.md` explains the value proposition in the first screen.
- `QUICKSTART.md` gets a new user from clone to sample vault without private
  paths.
- `config.example.toml` contains only generic, safe defaults.
- `.github/SECURITY.md` has the current vulnerability-reporting email.
- No private vault paths, hostnames, tokens, or client data are present.
- The sample vault contains safe demo data only.

## Verification

```bash
uv sync --extra dev
uv run pytest
uv run ruff check .
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

In an MCP client, verify:

- `vault://quick-index` is visible.
- `vault://doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find_runbook("generate internal certificate")` ranks
  `rb-generate-internal-cert` first.
- `vault://doc/infra-offline-ca` withholds the body by default.
- `read_doc("infra-offline-ca", include_secret_adjacent=True)` still requires
  local unlock.

## Packaging

```bash
uv tool install --from . severino-vault-mcp --force
severino-vault-mcp --help
```

Then confirm MCP client examples in `README.md` and `QUICKSTART.md` still match
the installed command name.

## GitHub Release

- Update `CHANGELOG.md`.
- Tag the release.
- Attach a short release note focused on user impact.
- Link to `QUICKSTART.md`, `docs/ai-safety-security.md`, and
  `config.example.toml`.
