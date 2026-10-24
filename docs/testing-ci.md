# Testing and CI

The tests live next to the code (`internal/*/*_test.go`). They exercise the
engine packages directly, drive the MCP server through a real in-memory client,
and run the CLI end to end. The commands that run them, and the test idioms,
are in [`AGENTS.md`](../AGENTS.md#verify). `deadcode` is a `tool` directive in
`go.mod` (`go tool deadcode -test ./...` lists unreachable functions).

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

`find(by="text")` tests need `rg` on `PATH`; `mise.toml` pins it.

## GitHub Actions

### `ci.yml`: the gate

Runs on pushes to `main` and pull requests. One job, `mise run check`, the
required status check: `jdx/mise-action` installs and caches the tools
`mise.lock` pins, then runs the same `mise run check` as a laptop. `check`
runs every `check:*` task and `golden` in parallel and lets each finish, so
one run names every failure:

- `check:gofmt`, `check:vet`, `check:test` (`-race`), `check:govulncheck`,
  `check:lint` (golangci-lint), `golden` (the contract snapshots);
- `check:shellcheck` and `check:actionlint` over the scripts and workflows;
- `check:secrets`, `check:build-output` and `check:conflict-copies`, which
  refuse a tracked `.env`, build output, or a file named like a sync
  conflict copy (`name 2.md`).

### `codeql.yml`: SAST

CodeQL with the `security-and-quality` suite for Go. Runs on push to `main`,
pull requests targeting `main`, and weekly on Monday at 06:17 UTC.

### `scorecard.yml`: project governance

OSSF Scorecard. Runs on push to `main`, on branch protection rule changes, and
weekly on Tuesday at 07:23 UTC. Results go to scorecard.dev and the Security
tab.

### `release.yml`

Runs on a pushed `vX.Y.Z` tag. The `build` job checks that go.mod's module
path carries the tag's major (`/v3` for v3.x.y), then compiles the darwin and
linux (amd64, arm64) binaries with `-trimpath`; `publish` attests the
archives' provenance and creates the GitHub Release with generated notes, the
archives and `SHA256SUMS`. `docs/release-checklist.md` is the procedure.

### Renovate

`renovate.json` covers `gomod`, GitHub Actions and the `mise.toml` tools, each
release held 7 days.
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
