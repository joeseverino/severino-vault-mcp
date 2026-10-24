// Package doctor validates a vault's frontmatter against a profile.
package doctor

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/links"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/queue"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
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
// are schema.Profile.Check and RefProblems; doctor adds the whole-vault ones
// (unreadable files, a missing block, duplicate doc_ids, a path wikilink to
// no file) and warns on a superseded report the vault can't follow to a
// live successor, and on code a living doc names that its project's
// checkout does not have (references).
// The archive and the read-only folders are not indexed dirs, so only live
// docs are checked.
func Validate(cfg config.Config, p *schema.Profile, propose bool) *Report {
	r := &Report{VaultPath: cfg.VaultPath}
	idx := vault.NewLoader(cfg).Index(false)
	files := links.NewFiles(links.VaultFiles(cfg.VaultPath))
	refs := newReferences(cfg.VaultPath)
	seen := map[string]string{}
	for _, path := range vault.MarkdownFiles(cfg.VaultPath, cfg.IndexedDirs) {
		r.CheckedFiles++
		rel := relative(path, cfg.VaultPath)
		note, err := frontmatter.ReadFile(path)
		if err != nil {
			r.add(Finding{rel, "error", "cannot read file: " + err.Error(), ""})
			continue
		}
		fm := note.FM
		r.body(rel, note.Text[note.Offset:], files)
		refs.check(r, rel, note.Text[note.Offset:], fm)
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
				r.add(Finding{rel, "error", fmt.Sprintf("duplicate doc_id %q; already used by %s", id, first), ""})
			} else {
				seen[id] = rel
			}
		}
		for _, msg := range slices.Concat(p.Check(fm, rel), p.RefProblems(fm, idx.Has), p.ProjectProblems(fm, idx.Projects)) {
			r.add(Finding{rel, "error", msg, ""})
		}
		for _, msg := range supersession(fm, idx) {
			r.add(Finding{rel, "warning", msg, ""})
		}
	}
	return r
}

// body adds the findings in a doc's body: each path wikilink that names no
// file.
func (r *Report) body(rel, body string, files *links.Files) {
	for _, target := range links.Dangling(body, filepath.ToSlash(rel), files) {
		r.add(Finding{rel, "error", fmt.Sprintf("path wikilink %q names no file in the vault; point it at the note's current path",
			"[["+target+"]]"), ""})
	}
}

// living is the part of body that describes the vault as it is now, with
// its line numbers kept: none of a report, or of a deprecated or archived
// doc, which record the past, and none of a closed log entry.
func living(body string, fm *jsonx.Obj) string {
	if status := fm.Text("status"); status == "deprecated" || status == schema.ArchivedStatus || fm.Text("doc_type") == schema.ReportDocType {
		return ""
	}
	b := sections.NewBody(body)
	for _, e := range sections.Entries(body, 1) {
		if e.Closed {
			b.Lines[e.Index] = ""
		}
	}
	for i, l := range b.Lines {
		if it, ok := queue.Parse(l); ok && queue.Done(it.Status) {
			b.Lines[i] = ""
		}
	}
	return b.String()
}

// supersession is what keeps a reader from following a superseded report to
// the current one: a deprecated report that names no successor, or a
// successor that is not live (deprecated or archived).
func supersession(fm *jsonx.Obj, idx *vault.Index) []string {
	next, _ := fm.Get("superseded_by")
	if !pystr.Truthy(next) {
		if fm.Text("status") == "deprecated" && fm.Text("doc_type") == schema.ReportDocType {
			return []string{"deprecated report names no superseded_by; set it to the doc_id of the report that replaces this one"}
		}
		return nil
	}
	if d, ok := idx.ByDocID[pystr.Str(next)]; ok && (d.Status == "deprecated" || d.Status == schema.ArchivedStatus) {
		return []string{fmt.Sprintf("superseded_by %q is %s, not live; point it at the current report (%s)",
			d.DocID, d.Status, d.RelativePath)}
	}
	return nil
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

func proposalFor(path, root string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title := pystr.Strip(strings.ReplaceAll(strings.ReplaceAll(stem, "_", " "), "-", " "))
	if title == "" {
		title = "Untitled"
	}
	slug := sections.Slugify(title)
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
