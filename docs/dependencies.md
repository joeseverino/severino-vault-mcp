# Dependencies

Versions belong in `go.mod`, `mise.toml`, `mise.lock` and the workflows, not a
second list here. The runtime uses Go's standard library wherever it covers
the contract; external libraries cover TOML and MCP, which Go does not.

## Go modules

| Module | Purpose |
| --- | --- |
| `github.com/BurntSushi/toml` | Operator configuration, aliases and mise-file inspection. Go has no TOML parser. |
| `github.com/modelcontextprotocol/go-sdk` | The official MCP server, client, protocol schemas and provider subprocess transport. Keep protocol handling in the SDK. |
| `github.com/google/jsonschema-go` | The SDK's JSON Schema implementation; tests also use it to validate emitted schemas and tool results. Go's JSON decoder does not validate JSON Schema. |
| `github.com/segmentio/encoding`, `github.com/segmentio/asm` | The MCP SDK's JSON implementation and its optimized primitives. |
| `github.com/yosida95/uritemplate/v3` | The SDK's standard URI-template matching for resources. |
| `golang.org/x/oauth2`, `golang.org/x/sync`, `golang.org/x/sys`, `golang.org/x/time` | SDK transport, authentication and concurrency dependencies. The stdio binary does not implement its own equivalents. |
| `golang.org/x/tools` | The development-only `deadcode` tool, declared with Go's `tool` directive. |
| `golang.org/x/mod`, `golang.org/x/telemetry` | Development-tool dependencies. They are not imported into the server. |

`go mod why -m <module>` explains why a selected module is needed;
`go list -deps ./cmd/severino-vault-mcp` shows the binary's actual packages.
`go list -m -u all` also lists modules retained in upstream module graphs
whose packages this project never imports. Do not add those as direct pins
just to make that report look current.

The constrained frontmatter parser remains local: replacing it with a full
YAML parser would change the contract for string-valued numbers and dates.
`jsonx.Obj` retains ordered output over `encoding/json`, as existing consumers
and golden tests require. Neither is replaced merely to change libraries.

## Tools and built-ins

| Surface | Owner |
| --- | --- |
| Filesystem traversal, atomic counters, root confinement, JSON, testing and benchmarks | Go standard library. Indexing never launches `fd`. |
| Formatting, modernization, vet, module tidiness and checksum verification | `gofmt`, `go fix`, `go vet`, `go mod tidy -diff`, `go mod verify`. |
| Additional Go lint rules | golangci-lint; modernization belongs to `go fix`. |
| Reachable known Go vulnerabilities | govulncheck. |
| Literal body search | ripgrep, needed for `find(by="text")`. Relevance search runs in the binary. |
| Shell and workflow validation | ShellCheck and actionlint. |
| Tool installation, locks and task validation | mise's built-in locked mode and `mise tasks validate`. |
| CI checkout, Go release builds, artifacts, provenance, dependency review, SAST and governance | Pinned upstream GitHub Actions. |

The Go release in mise agrees with `go.mod`'s language and toolchain;
`TestToolchainPinsAgree` guards that relationship. `GOTOOLCHAIN=local` keeps
Go from downloading a second compiler. `mise.lock` covers macOS arm64 and
Linux x64; locked installation is enforced locally and in CI. Go-installed
tools use their Go module checksums rather than a prebuilt archive checksum.

## Updates and verification

`renovate.json` uses Renovate's built-in managers for Go, mise, Actions and
runner labels. A small regex manager covers mise's own `min_version`, which
the mise manager does not update.

- Routine minor, patch and digest updates are grouped in one Monday PR,
  after seven days. Major upgrades remain separate.
- Lock maintenance runs in the same weekly window. The package manager
  performs lock refreshes; release-age guarantees depend on that manager.
- Vulnerability alerts and Go patch releases do not wait for the weekly
  schedule. Automatic merging is disabled.
- `go mod tidy` refreshes the module files; `mise lock` refreshes tool locks.
  `mise run check` checks tidiness, checksums, modernizations, race tests,
  schemas, golden contracts, lint and known reachable vulnerabilities.

For a deliberate module refresh, use `go get -u -t ./... tool`, then
`go mod tidy` and the full gate. Review major-release notes separately.
`mise run fix` applies Go's built-in modernization and formatting. Linux runs
the full CI gate; macOS also runs the race suite. A local pass does not claim
the hosted jobs have run.
