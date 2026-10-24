// Package testkit builds throwaway vaults for tests.
package testkit

import (
	"fmt"
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

// Env is a hermetic environment pointing at root, built on
// config.Hermetic so restricted unlock stays off unless a test sets it.
func Env(root string, extra ...string) config.Env {
	env := config.Hermetic(root)
	env["PATH"] = os.Getenv("PATH")
	env["SVMC_CACHE_SECONDS"] = "0"
	env["SVMC_RESTRICTED_UNLOCK_AUDIT_LOG"] = filepath.Join(root, ".audit.log")
	env["SVMC_EDU_CONFIG"] = filepath.Join(root, "no-edu.toml")
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

// Get walks a dotted path through Objs and lists ("hits.0.doc_id"); a
// missing step is nil.
func Get(o *jsonx.Obj, path string) any {
	v, _ := walk(o, path)
	return v
}

// Has reports whether the dotted path is present, holding null or not:
// jsonx.Obj.Has takes one key, so a dotted path there is never present.
func Has(o *jsonx.Obj, path string) bool {
	_, ok := walk(o, path)
	return ok
}

func walk(o *jsonx.Obj, path string) (any, bool) {
	var cur any = o
	for part := range strings.SplitSeq(path, ".") {
		switch x := cur.(type) {
		case *jsonx.Obj:
			v, ok := x.Get(part)
			if !ok {
				return nil, false
			}
			cur = v
		case []*jsonx.Obj:
			i := atoi(part)
			if i < 0 || i >= len(x) {
				return nil, false
			}
			cur = x[i]
		case []any:
			i := atoi(part)
			if i < 0 || i >= len(x) {
				return nil, false
			}
			cur = x[i]
		case []string:
			i := atoi(part)
			if i < 0 || i >= len(x) {
				return nil, false
			}
			cur = x[i]
		default:
			return nil, false
		}
	}
	return cur, true
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

// FakeVault builds a throwaway labs vault: a runbook, an infrastructure
// note, a Quick Index, a note without frontmatter, a daily note and aliases.
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

// MultisectionDoc adds a runbook with several H2 sections to the vault at
// root, for the section menu and section reads.
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

// RefTargets writes, in dir under root, a doc for each doc reference p
// declares to name: the doc_id Sample gives that field, so the sample value
// names a doc that exists.
func RefTargets(t testing.TB, root, dir string, p *schema.Profile) {
	t.Helper()
	for _, f := range p.Settable() {
		if !f.DocRef {
			continue
		}
		id := fmt.Sprint(Sample(f))
		Doc(t, filepath.Join(root, dir, id+".md"), "doc_id: "+id+"\ntitle: "+id+"\ndoc_type: "+p.DocTypes[0]+
			"\nsystem: example\nenvironment: "+p.Environments[0]+"\nstatus: "+p.Statuses[0]+"\nsensitivity: "+p.Sensitivities[0], "# "+id+"\n")
	}
}

// Carrier writes, under dir in root, a doc that may carry f when only some
// doc_types may (f.DocTypes): one of the first such type, in the folder that
// type lives in, carrying every field its type requires but f. It returns
// the doc's vault-relative path, "" when every doc may carry f.
func Carrier(t testing.TB, root, dir string, p *schema.Profile, f schema.Field) string {
	t.Helper()
	if len(f.DocTypes) == 0 {
		return ""
	}
	docType := schema.Sorted(f.DocTypes)[0]
	if folder, ok := p.DocTypeFolders[docType]; ok {
		dir = filepath.Join(dir, folder)
	}
	id := p.DocIDPrefixes[0] + "carrier-" + strings.ReplaceAll(f.Name, "_", "-")
	var block []string
	for _, name := range p.RequiredFor(docType) {
		value := ""
		switch name {
		case f.Name:
			continue
		case "doc_id":
			value = id
		case "doc_type":
			value = docType
		default:
			field, _ := p.Field(name)
			value = fmt.Sprint(Sample(field))
		}
		block = append(block, name+": "+value)
	}
	rel := filepath.Join(dir, id+".md")
	Doc(t, filepath.Join(root, rel), strings.Join(block, "\n"), "# "+id+"\n")
	return filepath.ToSlash(rel)
}

// Sample is a value a declared field accepts, from its own declaration: its
// example, first choice or first prefix, else one per kind. A list gets one
// item.
func Sample(f schema.Field) any {
	var v any = "example"
	switch {
	case f.Name == schema.ProjectsField:
		v = "Example" // the project folder the field tests create
	case f.Example != "":
		v = f.Example
	case len(f.Choices) > 0:
		v = f.Choices[0]
	case len(f.Prefixes) > 0:
		v = f.Prefixes[0] + "example"
	case f.Kind == schema.KindDate:
		v = "2026-01-02"
	case f.Kind == schema.KindPath:
		v = "~/Code/Archive/example"
	case f.Kind == schema.KindInteger:
		v = 3
	case f.Kind == schema.KindBoolean:
		v = true
	}
	if f.IsList() {
		return []any{v}
	}
	return v
}
