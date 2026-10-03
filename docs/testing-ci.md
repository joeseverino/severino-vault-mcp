# Testing and CI

The tests live next to the code (`internal/*/*_test.go`). They exercise the
engine packages directly, drive the MCP server through a real in-memory client,
and run the CLI end to end. Every test builds its own throwaway vault through
`internal/testkit`, and `testkit.Env` points edu at a missing config and declares no
providers, so nothing reads this machine's real vaults.

## Local Commands

The gate CI runs:

```bash
scripts/check.sh
```

Run one check by id (the rerun line in a failure report):

```bash
scripts/check.sh --only go-test
```

Directly:

```bash
gofmt -l .
go vet ./...
go test ./...
bash tests/golden/verify.sh
```

`find(by="text")` tests need `rg` on `PATH`.

## Parity Harness

`internal/parity` runs the Python reference CLI and this binary side by side
and diffs them: read commands on fixture vaults, writes on twin vaults compared
file by file, and MCP tool calls (writes included) against both servers over
stdio. It skips unless `SVMC_PARITY_PY` names the reference command:

```bash
SVMC_PARITY_PY="uv run --quiet --project /path/to/python-checkout severino-vault-mcp" \
  go test ./internal/parity/ -v
```

Known, deliberate differences are listed in the harness (`describe` metadata,
and `update_link`, which no longer drops a doc's frontmatter).

## Golden Contracts

`tests/golden/` freezes the surfaces other repos bind to: `schema --json` (HQ),
`describe` (Cordon), and the MCP tool names (Claude Code). `verify.sh` re-emits
each and diffs it; the gate runs it as the `golden` check.

## GitHub Actions

### `ci.yml`: the cordon gate

Runs on pushes to `main` and pull requests. It calls cordon's reusable gate
(`cordon-gate.yml@v2`), which runs the commands in `cordon.checks.json`
(gofmt, vet, test, govulncheck, golden) with cordon's repo invariants.
`scripts/check.sh` runs the same engine locally. `go.mod` pins the Go
toolchain, so the runner fetches it if its own Go is older.

### `codeql.yml`: SAST

CodeQL with the `security-and-quality` suite for Go. Runs on push to `main`,
pull requests targeting `main`, and weekly on Monday at 06:17 UTC.

### `scorecard.yml`: project governance

OSSF Scorecard. Runs on push to `main`, on branch protection rule changes, and
weekly on Tuesday at 07:23 UTC. Results go to scorecard.dev and the Security
tab.

### `release.yml`

release-please through cordon's reusable release workflow (`release-type: go`,
versioned by tag).

### Dependabot

`.github/dependabot.yml` covers `gomod` weekly and GitHub Actions monthly.
`govulncheck` catches CVEs against the current pins.

## What the Tests Cover

- Indexing, the lenient frontmatter parse, duplicate `doc_id` exclusion, aliases.
- Section chunking, ranking, the section menu, section reads.
- `read_doc` release for `public`, `internal`, `sensitive`; default withholding
  for `restricted`; the one-request local unlock and its audit log.
- `find(by="text")` skipping frontmatter and never searching restricted bodies.
- Frontmatter creation and update against each profile; multiline values;
  a failed atomic replace leaving the original intact; path escapes rejected.
- The task ledger: add, status, promote, delete, reconcile.
- Daily notes, the brief, `doctor`, the edu dataset, the HQ manifest.
- The CLI: parsing, exit codes, `describe`, every subcommand.
- The MCP server: vault composition (labs only, labs+edu, with a provider),
  the `vault` argument's schema, labs overrides never reaching edu, resources,
  and provider passthrough against a fake provider process.
- Reproducibility of `examples/sample-vault`.

## Sample Vault Reproducibility

```bash
SVMC_VAULT_PATH=examples/sample-vault go run ./cmd/severino-vault-mcp
```

Expected:

- `vault://labs/quick-index` returns the demo navigation hub.
- `vault://labs/doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find("generate internal certificate")` ranks `rb-generate-internal-cert` first.
- `vault://labs/doc/infra-offline-ca` withholds the body because the doc is `restricted`.
- `read_doc("infra-offline-ca", include_restricted=True)` still requires local unlock.

## CI Security Signal

CI doesn't prove the server is safe. It proves the core safety contracts are
regression-tested:

- `restricted` bodies are withheld by default.
- `include_restricted=True` is not sufficient on its own.
- `find(by="text")` cannot reveal restricted snippets.
- Path validation keeps write tools inside the vault root.
- Frontmatter enum validation rejects malformed metadata writes.
- A tool call only reaches the vault its `vault` argument names.
