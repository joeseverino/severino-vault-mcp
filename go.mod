module github.com/joeseverino/severino-vault-mcp

go 1.27

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	golang.org/x/crypto v0.57.0
	golang.org/x/term v0.46.0
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/telemetry v0.0.0-20260924152758-ed294f943157 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	golang.org/x/vuln v1.8.0 // indirect
)

tool (
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
)
