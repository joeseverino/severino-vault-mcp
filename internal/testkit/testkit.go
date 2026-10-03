// Package testkit builds throwaway vaults for tests.
package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// Write creates a file and its parent dirs.
func Write(t testing.TB, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Read returns a file's text.
func Read(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Doc writes a doc with a frontmatter block and body.
func Doc(t testing.TB, path, front, body string) {
	t.Helper()
	Write(t, path, "---\n"+strings.TrimSpace(front)+"\n---\n\n"+body)
}

// Dir returns a fresh temp dir with symlinks resolved.
func Dir(t testing.TB) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Env is a hermetic environment pointing at root.
func Env(root string, extra ...string) config.Env {
	env := config.Env{
		"HOME":                             os.Getenv("HOME"),
		"PATH":                             os.Getenv("PATH"),
		"SVMC_CONFIG":                      filepath.Join(root, "absent.toml"),
		"SVMC_VAULT_PATH":                  root,
		"SVMC_CACHE_SECONDS":               "0",
		"SVMC_ALLOW_RESTRICTED_UNLOCK":     "0",
		"SVMC_RESTRICTED_UNLOCK_AUDIT_LOG": filepath.Join(root, ".audit.log"),
		"SVMC_RESTRICTED_UNLOCK_HASH_FILE": filepath.Join(root, ".no-unlock-hash"),
		"SVMC_EDU_CONFIG":                  filepath.Join(root, "no-edu.toml"),
	}
	for i := 0; i+1 < len(extra); i += 2 {
		env[extra[i]] = extra[i+1]
	}
	return env
}

// Loader is a loader over root with the hermetic env.
func Loader(root string, extra ...string) *vault.Loader {
	return vault.NewLoader(config.Load("", Env(root, extra...)))
}

// Vault is a governed labs vault over root.
func Vault(root string, extra ...string) *core.Vault {
	return core.NewVault("labs", config.Load("", Env(root, extra...)), schema.Labs)
}

// Get walks a dotted path through Objs and lists ("hits.0.doc_id").
func Get(o *jsonx.Obj, path string) any {
	var cur any = o
	for _, part := range strings.Split(path, ".") {
		switch x := cur.(type) {
		case *jsonx.Obj:
			cur, _ = x.Get(part)
		case []*jsonx.Obj:
			i := atoi(part)
			if i < 0 || i >= len(x) {
				return nil
			}
			cur = x[i]
		case []any:
			i := atoi(part)
			if i < 0 || i >= len(x) {
				return nil
			}
			cur = x[i]
		case []string:
			i := atoi(part)
			if i < 0 || i >= len(x) {
				return nil
			}
			cur = x[i]
		default:
			return nil
		}
	}
	return cur
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// Hits returns the objects at a dotted path.
func Hits(o *jsonx.Obj, path string) []*jsonx.Obj {
	h, _ := Get(o, path).([]*jsonx.Obj)
	return h
}

// IDs maps hits to their doc_id.
func IDs(hits []*jsonx.Obj) []string {
	out := []string{}
	for _, h := range hits {
		out = append(out, h.Str("doc_id"))
	}
	return out
}

// FakeVault is the Python suite's fake_vault fixture.
func FakeVault(t testing.TB) string {
	t.Helper()
	root := Dir(t)
	for _, d := range []string{"01 Projects", "02 Infrastructure", "03 Runbooks", "00 Inbox/Daily Note", ".svmc"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	Write(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md"), `---
doc_id: rb-add-nginx-proxy-host
title: Add Nginx Proxy Host
doc_type: runbook
system: Nginx Proxy Manager
environment: other
status: active
sensitivity: internal
last_reviewed: 2025-01-01
related_projects: []
related_assets: []
tags:
  - nginx
  - network-operations
---

## Goal

Expose an internal service over HTTPS via NPM.
`)
	Write(t, filepath.Join(root, "02 Infrastructure", "Local PKI.md"), `---
doc_id: infra-local-pki
title: Local PKI
doc_type: architecture_note
system: Local PKI
environment: local_mac
status: active
sensitivity: restricted
last_reviewed: 2026-04-01
tags: [pki, ca]
---

# Local PKI

CA private key lives offline.
`)
	Write(t, filepath.Join(root, "03 Runbooks", "Quick Index.md"), `---
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [index, mcp, navigation]
---

# Example Operations Vault Quick Index

| Intent | Start Here |
|---|---|
| Add HTTPS service | rb-add-nginx-proxy-host |
`)
	Write(t, filepath.Join(root, "01 Projects", "untagged.md"), "# Untagged\n\nNo frontmatter yet.\n")
	Write(t, filepath.Join(root, "00 Inbox", "Daily Note", "2026-06-19.md"), `---
doc_id: daily-20260619
created: 2026-06-19 22:39:04
date: 2026-06-19
---

- [x] Split daily notes from inbox captures.
- Added a dedicated Daily Note template.
- Fixed Obsidian archive command escaping.
`)
	Write(t, filepath.Join(root, ".svmc", "aliases.toml"), `[aliases]
"https proxy" = "rb-add-nginx-proxy-host"
"offline ca" = "infra-local-pki"
"missing target" = "rb-does-not-exist"
`)
	return root
}

// MultisectionDoc is the Python suite's _write_multisection_doc.
func MultisectionDoc(t testing.TB, root string) {
	t.Helper()
	Write(t, filepath.Join(root, "03 Runbooks", "Backup Ops.md"), `---
doc_id: rb-backup-ops
title: Backup Ops
doc_type: runbook
system: Backup
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [backup]
---

Overview line before any heading.

## Routine operations

Run the daily job to keep things current.

## Troubleshooting

Check the resolver logs first when latency spikes.
`)
}
