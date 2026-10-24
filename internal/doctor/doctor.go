// Package doctor validates a vault's frontmatter against a profile.
package doctor

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// Finding is one problem.
type Finding struct {
	RelativePath string
	Severity     string
	Message      string
	Proposal     string
}

// Report is a doctor run.
type Report struct {
	VaultPath    string
	CheckedFiles int
	IndexedDocs  int
	Findings     []Finding
}

// OK reports whether no finding is an error.
func (r *Report) OK() bool {
	return !slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Severity == "error" })
}

func (r *Report) add(f Finding) { r.Findings = append(r.Findings, f) }

func relative(p, root string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return rel
}

// Validate checks every vault-doc file under the indexed dirs: the field rules
// are schema.Profile.Check; doctor adds the whole-vault ones (unreadable
// files, a missing block, duplicate doc_ids).
func Validate(cfg config.Config, p *schema.Profile, propose bool) *Report {
	r := &Report{VaultPath: cfg.VaultPath}
	seen := map[string]string{}
	for _, path := range vault.MarkdownFiles(cfg.VaultPath, cfg.IndexedDirs) {
		r.CheckedFiles++
		rel := relative(path, cfg.VaultPath)
		text, err := pystr.ReadText(path)
		if err != nil {
			r.add(Finding{rel, "error", "cannot read file: " + err.Error(), ""})
			continue
		}
		fm, _, _ := frontmatter.Split(text)
		if fm == nil || fm.Len() == 0 {
			proposal := ""
			if propose {
				proposal = proposalFor(path, cfg.VaultPath)
			}
			r.add(Finding{rel, "error", "missing YAML frontmatter", proposal})
			continue
		}
		if idv, _ := fm.Get("doc_id"); pystr.Truthy(idv) {
			r.IndexedDocs++
			id := pystr.Str(idv)
			if first, dup := seen[id]; dup {
				r.add(Finding{rel, "error", fmt.Sprintf("duplicate doc_id %s; already used by %s", pystr.Repr(id), first), ""})
			} else {
				seen[id] = rel
			}
		}
		for _, msg := range p.Check(fm) {
			r.add(Finding{rel, "error", msg, ""})
		}
	}
	return r
}

// Run prints a report and returns the exit code.
func Run(w io.Writer, cfg config.Config, p *schema.Profile, propose bool) int {
	r := Validate(cfg, p, propose)
	fmt.Fprintf(w, "Vault: %s\n", r.VaultPath)
	fmt.Fprintf(w, "Checked markdown files: %d\n", r.CheckedFiles)
	fmt.Fprintf(w, "Indexed docs: %d\n", r.IndexedDocs)
	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "No frontmatter issues found.")
		return 0
	}
	fmt.Fprintln(w)
	for _, f := range r.Findings {
		fmt.Fprintf(w, "[%s] %s: %s\n", f.Severity, f.RelativePath, f.Message)
		if f.Proposal != "" {
			fmt.Fprintln(w, strings.TrimRight(f.Proposal, " \t\n\r"))
			fmt.Fprintln(w)
		}
	}
	if !r.OK() {
		return 1
	}
	return 0
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func proposalFor(path, root string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title := pystr.Strip(strings.ReplaceAll(strings.ReplaceAll(stem, "_", " "), "-", " "))
	if title == "" {
		title = "Untitled"
	}
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if slug == "" {
		slug = "untitled"
	}
	return fmt.Sprintf(`Suggested frontmatter:
---
doc_id: %s%s
title: %s
doc_type: %s
system: %s
environment: other
status: draft
sensitivity: internal
tags: []
related_projects: []
related_assets: []
---
Path: %s
`, prefixFor(path), slug, title, docTypeFor(path), title, relative(path, root))
}

func hasPart(path, part string) bool {
	for p := range strings.SplitSeq(path, "/") {
		if p == part {
			return true
		}
	}
	return false
}

func prefixFor(path string) string {
	switch {
	case hasPart(path, "03 Runbooks"):
		return "rb-"
	case hasPart(path, "02 Infrastructure"):
		return "infra-"
	case hasPart(path, "01 Projects"):
		return "project-"
	}
	return "note-"
}

func docTypeFor(path string) string {
	switch {
	case hasPart(path, "03 Runbooks"):
		return "runbook"
	case hasPart(path, "02 Infrastructure"), hasPart(path, "01 Projects"):
		return "architecture_note"
	}
	return "decision_record"
}
