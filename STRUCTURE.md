# Repository Structure

One Go module, one binary.

## Code

| Path | Purpose |
|---|---|
| `cmd/severino-vault-mcp/` | `main`: hands `os.Args` to `cli.Main`. |
| `sources.go` | Embeds the Go sources so `--fingerprint` can hash them. |
| `internal/cli/` | The command table (`table.go`) that drives parsing, `--help` and `describe`; one runner per subcommand. |
| `internal/mcpserver/` | The MCP server: the 8 shared tools, the resources, `education_dataset`, provider passthrough, instructions. |
| `internal/vaults/` | Composes the vaults: labs always, edu when its config exists, one per connected provider. |
| `internal/provider/` | Connects to a provider: reads its profile, lists its tools, forwards calls. |
| `internal/core/` | The shared tools, transport-free. |
| `internal/vault/` | The walk (`fd` when present), the lenient doc parse, duplicates, aliases, the cached index. |
| `internal/sections/`, `internal/search/` | H2 section chunking; the keyword ranker and section scoring. |
| `internal/query/` | Hit projection, the section menu, section reads, `git log`, ripgrep body search. |
| `internal/frontmatter/` | The constrained YAML subset: parse, serialize, body offset. |
| `internal/schema/` | Profiles: built-in labs and education, `FromContract` for provider profiles. |
| `internal/gate/` | Sensitivity policy, unlock hash, the hidden-input prompt, the audit log. |
| `internal/write/`, `internal/tasks/` | Frontmatter writes and the task ledger. |
| `internal/daily/`, `internal/brief/`, `internal/doctor/` | Daily notes and their brief region; the vault brief; frontmatter validation. |
| `internal/education/`, `internal/hqmanifest/` | The edu dataset; the HQ docs manifest (moves to HQ). |
| `internal/{config,fsx,jsonx,pystr,clock,contracts,tabular}/` | Config, vault-confined file access (`os.Root`) and atomic writes, Python-exact JSON, Python string semantics, time, receipts, table rows. |
| `internal/testkit/` | Test vault builders. |
| `internal/fuzzseed/` | Sample-vault markdown as fuzz seeds. |
| `internal/parity/` | Opt-in side-by-side harness against the Python reference. |

## Everything else

| Path | Purpose |
|---|---|
| `tests/golden/` | Frozen public surfaces (`schema --json`, `describe`, MCP tool names) and `verify.sh`. |
| `docs/` | Architecture, safety model, AI tool contract, testing, migration, release checklist, release notes. |
| `examples/sample-vault/` | Safe demo vault on the frontmatter contract. |
| `scripts/check.sh` | The CI gate, run locally. |
| `cordon.checks.json` | The gate's Go commands (gofmt, vet, test -race, govulncheck, golangci-lint, golden). |
| `.golangci.yml` | golangci-lint v2 configuration. |
| `.github/workflows/` | `ci.yml`, `release.yml`, `codeql.yml`, `scorecard.yml`. |
| `config.example.toml` | Labs config template (edu and providers use the same `[vault]` shape). |
