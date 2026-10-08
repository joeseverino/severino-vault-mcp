# Contributing

Thanks for taking the time to improve `severino-vault-mcp`.

This project is a local stdio MCP server for operational runbooks and
Obsidian-style vaults. Contributions should preserve the core safety model:
local-first operation, predictable vault reads, narrow validated writes, and no
default release of `restricted` bodies.

## Local Setup

```bash
git clone git@github.com:joeseverino/severino-vault-mcp.git
cd severino-vault-mcp
go test ./...
```

Run the sample vault:

```bash
SVMC_VAULT_PATH=examples/sample-vault go run ./cmd/severino-vault-mcp
```

The server speaks stdio, so it waits for an MCP client and does not print a
web URL.

## Development Guidelines

- Keep the server local-first. Do not add a network listener unless it is
  explicitly optional and documented with a clear security model.
- Keep write tools narrow and schema-validated.
- Do not log markdown body content, unlock phrases, or secrets.
- Do not broaden `restricted` release paths. `include_restricted=True` must
  remain insufficient without local unlock approval.
- Prefer the standard library; add a dependency only when it clearly improves
  correctness.
- Update `README.md`, `QUICKSTART.md`, and `docs/testing-ci.md` when behavior
  or setup changes.

## Tests

Run before opening a pull request:

```bash
scripts/check.sh
bash tests/golden/verify.sh
```

The gate runs `go test -race`, `govulncheck` (a `tool` directive in `go.mod`)
and `golangci-lint` (configured in `.golangci.yml`; a pinned version runs
through `go run`). Fix findings in place. A `//nolint` needs a specific linter
and a reason.

The suite covers indexing and frontmatter parsing, search and body search,
the sensitivity gate and local unlock, validated writes, the task ledger, the
CLI and its golden contracts, the MCP server through a real client, the
provider seam, and sample-vault reproducibility.

## Pull Requests

Good pull requests include:

- A concise description of the behavior change.
- Tests for new behavior or a clear note explaining why tests were not added.
- Documentation updates when user-facing setup or semantics change.
- No unrelated formatting churn.

## Security Reports

Please do not report vulnerabilities through public issues. Follow
`.github/SECURITY.md` and email github@jseverino.com.
