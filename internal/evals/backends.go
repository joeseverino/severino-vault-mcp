package evals

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/search"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// ours is core.Find and core.ReadDoc, as the MCP server calls them.
type ours struct{ v *core.Vault }

// OpenOurs scores the server's own find and read_doc.
func OpenOurs(cfg config.Config) (Backend, error) {
	return ours{core.NewVault("labs", cfg, schema.Labs)}, nil
}

func (ours) Name() string { return "ours" }

func (b ours) Find(query, mode string, fl Filter, limit int) (Found, error) {
	resp := core.Find(b.v, query, core.FindArgs{By: mode, Limit: limit, ContextLines: 1, System: fl.System, Project: fl.Project,
		DocType: fl.DocType, IncludeArchived: fl.IncludeArchived})
	if msg := resp.Str("error"); msg != "" {
		return Found{}, errors.New(msg)
	}
	hits, _ := resp.Get("hits")
	f := Found{Tokens: Tokens(core.Text(resp))}
	for _, h := range hits.([]*jsonx.Obj) {
		f.IDs = append(f.IDs, h.Str("doc_id"))
		f.Sections = append(f.Sections, h.Str("section"))
	}
	return f, nil
}

func (b ours) Read(docID, section string) (int, error) {
	a := core.ReadArgs{}
	if section != "" {
		a.Sections = []string{section}
	}
	r := core.ReadDoc(b.v, docID, a)
	if msg := r.Str("section_error"); msg != "" {
		return 0, errors.New(msg)
	}
	return Tokens(core.Text(r)), nil
}

// grep is what an agent with only Grep and Read gets: every markdown file in
// the vault ranked by how many distinct query words it holds, then by their
// occurrences, case-insensitive, frontmatter included and no sensitivity
// gate. Its find output is the file list a count-mode grep prints, and a read
// is the whole file.
type grep struct {
	root  string
	files []grepFile
}

type grepFile struct {
	rel, id, text, folded string
	doc                   *vault.Doc // nil when unindexed
}

// OpenGrep builds the baseline over the vault's files.
func OpenGrep(cfg config.Config) (Backend, error) {
	byPath := map[string]*vault.Doc{}
	idx := vault.NewLoader(cfg).Index(false)
	if idx.Err != nil {
		return nil, fmt.Errorf("vault %s is unreadable: %w", cfg.VaultPath, idx.Err)
	}
	for _, d := range idx.Docs {
		byPath[d.Path] = d
	}
	g := &grep{root: cfg.VaultPath}
	err := filepath.WalkDir(cfg.VaultPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != cfg.VaultPath && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		raw, err := os.ReadFile(p) //nolint:gosec // walking the vault under test is the point
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(cfg.VaultPath, p)
		text := string(raw)
		rel = filepath.ToSlash(rel)
		doc := byPath[p]
		id := "file:" + rel // unindexed: no doc_id names it, so it never counts as a hit
		if doc != nil {
			id = doc.DocID
		}
		g.files = append(g.files, grepFile{rel: rel, id: id, text: text, folded: strings.ToLower(text), doc: doc})
		return nil
	})
	return g, err
}

func (*grep) Name() string { return "grep" }

func (g *grep) Find(query, _ string, fl Filter, limit int) (Found, error) {
	// A filter is frontmatter, which grep alone can't read; the baseline gets
	// the same narrowing find applies so the two stay comparable.
	keep := core.FindArgs{System: fl.System, Project: fl.Project, DocType: fl.DocType, IncludeArchived: fl.IncludeArchived}.Keep()
	var words []string
	for w := range search.QueryTokens(query) {
		words = append(words, w)
	}
	type match struct {
		f            *grepFile
		words, count int
	}
	var ms []match
	for i := range g.files {
		f := &g.files[i]
		if keep != nil && (f.doc == nil || !keep(f.doc)) {
			continue
		}
		m := match{f: f}
		for _, w := range words {
			if n := strings.Count(f.folded, w); n > 0 {
				m.words++
				m.count += n
			}
		}
		if m.words > 0 {
			ms = append(ms, m)
		}
	}
	slices.SortStableFunc(ms, func(a, b match) int {
		if a.words != b.words {
			return b.words - a.words
		}
		if a.count != b.count {
			return b.count - a.count
		}
		return strings.Compare(a.f.rel, b.f.rel)
	})
	var out strings.Builder
	f := Found{}
	for i, m := range ms {
		out.WriteString(m.f.rel + ":" + strconv.Itoa(m.count) + "\n")
		if i < limit {
			f.IDs = append(f.IDs, m.f.id)
		}
	}
	f.Tokens = Tokens(out.String())
	return f, nil
}

func (g *grep) Read(docID, _ string) (int, error) {
	for _, f := range g.files {
		if f.id == docID {
			return Tokens(f.text), nil
		}
	}
	return 0, errors.New("no file for " + docID)
}
