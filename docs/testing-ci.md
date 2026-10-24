# Testing and CI

The tests live next to the code (`internal/*/*_test.go`). They exercise the
engine packages directly, drive the MCP server through a real in-memory client,
and run the CLI end to end. The commands that run them, and the test idioms,
are in [`AGENTS.md`](../AGENTS.md#verify). `govulncheck` and `deadcode` are
`tool` directives in `go.mod` (`go tool deadcode -test ./...` lists unreachable
functions).

## Fuzzing and Benchmarks

Fuzz targets cover the frontmatter parser, section chunking, the daily-note brief region and path ordering. Run one with:

```bash
go test ./internal/frontmatter -run '^$' -fuzz FuzzSplit -fuzztime 20s
```

A crashing input is written under the package's `testdata/fuzz/` and replays
as a regular test. The vault index build has a benchmark:

```bash
go test ./internal/vault -run '^$' -bench IndexBuild
```

`find(by="text")` tests need `rg` on `PATH`.

## GitHub Actions

### `ci.yml`: the cordon gate

Runs on pushes to `main` and pull requests. It calls cordon's reusable gate
(`cordon-gate.yml@v2`), which runs the commands in `cordon.checks.json` with cordon's repo invariants.
`mise run check` runs the same engine locally. `go.mod` pins the Go
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
  for `restricted`; the one-request 1Password unlock (approved, withheld,
  unconfigured, refused under a service account) and its audit log.
- `find(by="text")` skipping frontmatter and never searching restricted bodies.
- Frontmatter creation and update against each profile; multiline values;
  a failed atomic replace leaving the original intact; `..` traversal and
  symlinks that leave the vault rejected by every entry point that takes a path.
- Provider child processes ending on handshake timeout and on context
  cancellation.
- The work queue: add, list, set, claim, close, and its line grammar.
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
- `read_doc("infra-offline-ca", include_restricted=True)` still requires a Touch ID approval.

## CI Security Signal

CI doesn't prove the server is safe. It proves the core safety contracts are
regression-tested:

- `restricted` bodies are withheld by default.
- `include_restricted=True` is not sufficient on its own.
- `find(by="text")` cannot reveal restricted snippets.
- Path validation keeps write tools inside the vault root.
- Frontmatter enum validation rejects malformed metadata writes.
- A tool call only reaches the vault its `vault` argument names.
