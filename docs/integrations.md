# Integrations

Programs outside this repo read the binary's output. Each reads the installed
binary, so a change to one of these shapes is a contract change for its reader.

| Consumer | Reads | Notes |
|---|---|---|
| Severino HQ (ops app) | `schema --json` (the frozen enum contract, committed as `docs_index/schema.json`) and `hq-manifest` | The docs manifest lives in `internal/hqmanifest` until HQ owns it. |
| jseverino.com (site) | `export education` | Read by its `bin/content-sync`. Site work such as writeups, D1, CSP, contact and headers lives in that repo's `site` CLI, not here. |
| resume-engine | `export education` | Read by its `lib/reconcile-coursework`. |
| Cordon | `describe` | Emits the CLI surface as a [Cordon](https://github.com/joeseverino/cordon) contract, generated from the same command table that parses arguments. |
| `tools doctor` | `--fingerprint` | Hashes the binary's embedded Go sources so a stale install can be told from the source tree. `go run ./cmd/severino-vault-mcp --fingerprint` is the source side. |
| Session and worker hooks | `queue`, `validate <path>...` | `validate` prints `<relative_path>: <problem>` lines and exits 1; it is silent for paths no local vault indexes. |

`tests/golden/` pins `schema.json`, `labs.schema.json`, `cli-describe.json` and
`mcp-tools.txt`; a diff there is a deliberate contract change. Outside writers
that append to a vault note follow the lock described in
[`ai-tool-contract.md`](ai-tool-contract.md).
