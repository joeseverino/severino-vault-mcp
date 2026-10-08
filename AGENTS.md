# AGENTS.md: house rules for severino-vault-mcp

`CLAUDE.md` is a symlink to this file.

One Go binary: a stdio MCP server for every vault (labs, edu, and any vault a
provider adds) plus a CLI over the labs vault. Go 1.27, the official MCP SDK, and one
TOML parser; nothing else.

## Shape

```
cmd/severino-vault-mcp   main: cli.Main
internal/cli             command table -> parsing, --help, describe; one runner per subcommand
internal/mcpserver       the server: core tools, resources, education_dataset, provider passthrough
internal/vaults          composes vaults: labs, edu (config present), providers
internal/provider        connects a provider: profile resource, tools, instructions
internal/core            the 8 shared tools, transport-free (the MCP adapter and tests both call these)
internal/{vault,sections,search,query,frontmatter,schema,gate,write,tasks,daily,brief,doctor,education,hqmanifest}
                         the governance engine
internal/{config,fsx,jsonx,pystr,clock,contracts,tabular}  shared plumbing
```

- Output is byte-compatible with the Python implementation this replaced, and
  that is a contract: `jsonx` reproduces `json.dumps` (key order,
  `ensure_ascii`), `pystr` reproduces `repr()` in error messages, and the
  frontmatter parser is the same constrained YAML subset (numbers and dates
  stay strings). Don't swap in a YAML library or `encoding/json` for these.
- Every service returns one `*jsonx.Obj`; failures are `{"ok": false, "error": "..."}`.
  CLI subcommands exit 0/1 on `ok`, 2 on argument errors.
- The labs profile is `schema.Labs`. `schema --json` is the frozen shape HQ
  commits (`docs_index/schema.json`); `tests/golden/schema.json` pins it.
- No private domain lives here, not even by name. Providers are declared only
  in the operator's config (`[[providers]]`); the vault name, profile, tools
  and instructions arrive at runtime. Tests use a fake provider fixture.

## Contracts

- Consumers run the installed binary. `--fingerprint` (checked by `tools doctor`)
  hashes the embedded Go sources; `go run ./cmd/severino-vault-mcp --fingerprint`
  is the source side.
- `export education` JSON is read by jseverino.com (`bin/content-sync`) and
  resume-engine (`lib/reconcile-coursework`). Changing its shape is a contract
  change for both.
- `tests/golden/`: `schema.json`, `cli-describe.json`, `mcp-tools.txt`. A diff
  there is a deliberate contract change; regenerate with the binary.

## Safety

- `public`/`internal`/`sensitive` bodies are released; `restricted` is withheld
  unless the caller asks and the local unlock succeeds. Text search never reads
  restricted bodies.
- Writes are schema-specific: validate against the vault's profile, keep
  `doc_id` immutable, serialize with `frontmatter.Serialize`, replace through
  `fsx.Vault.AtomicWrite` (atomic, confined to the vault by `os.Root`). Never
  write vault files any other way.

## Verify

```bash
scripts/check.sh            # the CI gate: gofmt, vet, test -race, govulncheck, golangci-lint, repo invariants
bash tests/golden/verify.sh
```

## Test idioms

- Hermetic: `internal/testkit` builds throwaway vaults (`FakeVault`,
  `MultisectionDoc`) and an env (`Env`) that points edu at a missing config
  and declares no providers, so no test reads this machine's real vaults.
- `mcpserver` tests drive the real server through an in-memory client; the
  provider seam is tested against the test binary re-executed as a provider.
- Seams: `gate.PromptPhrase` (unlock prompt), `clock.Now`. A failed atomic
  write is simulated with a read-only directory.
- `internal/parity` diffs this binary against a Python reference CLI. It skips
  unless `SVMC_PARITY_PY` names one.
