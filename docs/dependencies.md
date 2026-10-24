# Dependencies

Versions live in `go.mod`, `mise.toml`, `mise.lock` and the workflows; the
update policy lives in `renovate.json`. This page holds only the decisions
those files cannot state.

- The runtime uses Go's standard library wherever it covers the contract.
  Its outside modules are the TOML parser (Go has none) and the official MCP
  SDK, with what the SDK pulls in. `go mod why -m <module>` says why a
  module is selected.
- The frontmatter parser stays our own: a YAML library would turn
  string-valued numbers and dates into other types, which changes the
  contract.
- `jsonx.Obj` keeps insertion order over `encoding/json`, which the golden
  files and every consumer of the output rely on.
- Indexing walks the vault with the standard library and never launches
  `fd`.
- `go fix` owns modernization; golangci-lint runs the other lint rules.
- A refresh outside Renovate is `go get -u -t ./... tool`, `go mod tidy`,
  then `mise run check`.
