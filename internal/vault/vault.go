// Package vault walks a vault's indexed folders, parses each doc's
// frontmatter, and caches the index. It is the one definition of "which
// files are vault docs" for the index, the doctor, and the HQ manifest.
package vault

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
)

// Path components that are never vault docs: templates, metadata trees, and
// a static mirror's source/.
var skipDirParts = map[string]bool{"00 Templates": true, "Templates": true, ".git": true, ".obsidian": true, "source": true}

// ComparePaths orders paths by component, as Python's sorted(Path) does.
func ComparePaths(a, b string) int {
	for {
		ca, restA, moreA := strings.Cut(a, "/")
		cb, restB, moreB := strings.Cut(b, "/")
		if c := strings.Compare(ca, cb); c != 0 {
			return c
		}
		switch {
		case !moreA && !moreB:
			return 0
		case !moreA:
			return -1
		case !moreB:
			return 1
		}
		a, b = restA, restB
	}
}

func sortPaths(paths []string) {
	slices.SortStableFunc(paths, ComparePaths)
}

// walkMD lists .md files under root with fd when available (hidden and
// ignored files skipped), else every *.md entry.
func walkMD(root string) []string {
	if fd, err := exec.LookPath("fd"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, fd, "--type", "f", "--extension", "md", ".", root) //nolint:gosec // fd resolved by LookPath; root is the configured vault
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		if err == nil {
			var paths []string
			for line := range strings.SplitSeq(string(out), "\n") {
				if line != "" {
					paths = append(paths, filepath.Clean(line))
				}
			}
			sortPaths(paths)
			return paths
		}
	}
	var paths []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p != root && strings.HasSuffix(d.Name(), ".md") {
			paths = append(paths, p)
		}
		return nil
	})
	sortPaths(paths)
	return paths
}

func hasSkipPart(p string) bool {
	for part := range strings.SplitSeq(p, "/") {
		if skipDirParts[part] {
			return true
		}
	}
	return false
}

// MarkdownFiles is every vault-doc file under the indexed dirs. "." indexes
// the vault root itself, non-recursively.
func MarkdownFiles(vaultPath string, indexedDirs []string) []string {
	var out []string
	for _, sub := range indexedDirs {
		if sub == "." {
			matches, _ := filepath.Glob(filepath.Join(globEscape(vaultPath), "*.md"))
			sortPaths(matches)
			for _, p := range matches {
				if !strings.HasPrefix(filepath.Base(p), "_") {
					out = append(out, p)
				}
			}
			continue
		}
		root := filepath.Join(vaultPath, sub)
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		for _, p := range walkMD(root) {
			if hasSkipPart(p) || strings.HasPrefix(filepath.Base(p), "_") {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

func globEscape(p string) string {
	r := strings.NewReplacer(`*`, `\*`, `?`, `\?`, `[`, `\[`)
	return r.Replace(p)
}

// Doc is one indexed vault doc.
type Doc struct {
	DocID           string
	Title           string
	DocType         string
	System          string
	Environment     string
	Status          string
	Sensitivity     gate.Sensitivity
	LastReviewed    string // "" when absent
	HasLastReviewed bool
	Tags            []string
	RelatedProjects []string
	RelatedAssets   []string
	Path            string // absolute
	RelativePath    string // vault-root relative
	Body            string
	BodyStartLine   int
	Sections        []sections.Section
	Extra           *jsonx.Obj // frontmatter keys the engine doesn't consume
}

// Stem is the file name without its extension.
func (d *Doc) Stem() string {
	base := filepath.Base(d.RelativePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// LastReviewedValue is last_reviewed or nil.
func (d *Doc) LastReviewedValue() any {
	if !d.HasLastReviewed {
		return nil
	}
	return d.LastReviewed
}

var coreKeys = map[string]bool{
	"doc_id": true, "title": true, "doc_type": true, "system": true, "environment": true, "status": true,
	"sensitivity": true, "last_reviewed": true, "tags": true, "related_projects": true, "related_assets": true,
}

// CoerceList turns a parsed value into a list of strings.
func CoerceList(v any) []string {
	switch x := v.(type) {
	case nil:
		return []string{}
	case []any:
		out := []string{}
		for _, item := range x {
			if item != nil {
				out = append(out, pystr.Str(item))
			}
		}
		return out
	}
	return []string{pystr.Str(v)}
}

// NormalizeAlias lowercases and collapses whitespace.
func NormalizeAlias(v string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(pystr.Strip(v)), pystr.IsSpace), " ")
}

func strOr(fm *jsonx.Obj, key, def string) string {
	v, _ := fm.Get(key)
	if pystr.Truthy(v) {
		return pystr.Str(v)
	}
	return def
}

// Index is a built vault index.
type Index struct {
	Docs            []*Doc
	ByDocID         map[string]*Doc
	Aliases         map[string]string
	InvalidAliases  map[string]string
	DuplicateDocIDs map[string][]string
	loadedAt        time.Time
}

func newIndex() *Index {
	return &Index{ByDocID: map[string]*Doc{}, Aliases: map[string]string{}, InvalidAliases: map[string]string{}, DuplicateDocIDs: map[string][]string{}}
}

func (idx *Index) add(doc *Doc) {
	if existing, ok := idx.ByDocID[doc.DocID]; ok {
		if _, seen := idx.DuplicateDocIDs[doc.DocID]; !seen {
			idx.DuplicateDocIDs[doc.DocID] = []string{existing.RelativePath}
		}
		idx.DuplicateDocIDs[doc.DocID] = append(idx.DuplicateDocIDs[doc.DocID], doc.RelativePath)
		delete(idx.ByDocID, doc.DocID)
		idx.Docs = slices.DeleteFunc(idx.Docs, func(d *Doc) bool { return d.DocID == doc.DocID })
		return
	}
	if _, dup := idx.DuplicateDocIDs[doc.DocID]; dup {
		idx.DuplicateDocIDs[doc.DocID] = append(idx.DuplicateDocIDs[doc.DocID], doc.RelativePath)
		return
	}
	idx.Docs = append(idx.Docs, doc)
	idx.ByDocID[doc.DocID] = doc
}

// Loader builds and caches one vault's index.
type Loader struct {
	Config config.Config
	index  *Index
}

// NewLoader returns a loader for cfg.
func NewLoader(cfg config.Config) *Loader { return &Loader{Config: cfg} }

// Index returns the cached index, rebuilding past cache_seconds or on force.
func (l *Loader) Index(force bool) *Index {
	now := time.Now()
	if !force && l.index != nil && now.Sub(l.index.loadedAt).Seconds() < float64(l.Config.CacheSeconds) {
		return l.index
	}
	l.index = l.build()
	l.index.loadedAt = now
	return l.index
}

func (l *Loader) build() *Index {
	idx := newIndex()
	for _, p := range MarkdownFiles(l.Config.VaultPath, l.Config.IndexedDirs) {
		text, err := pystr.ReadText(p)
		if err != nil {
			continue
		}
		fm, body, start := frontmatter.Split(text)
		if fm == nil || fm.Len() == 0 {
			continue
		}
		if id, _ := fm.Get("doc_id"); !pystr.Truthy(id) {
			typ, _ := fm.Get("type")
			if strings.ToLower(pystr.Strip(strOr(fm, "type", ""))) != "reference" || typ == nil {
				continue
			}
			base := filepath.Base(p)
			slug := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base))), " ", "-"), "_", "-")
			fm.Set("doc_id", "ref-"+slug)
			if !fm.Has("doc_type") {
				fm.Set("doc_type", "reference")
			}
			if !fm.Has("sensitivity") {
				fm.Set("sensitivity", "public")
			}
		}
		idx.add(l.makeDoc(p, fm, body, start))
	}
	idx.Aliases, idx.InvalidAliases = l.loadAliases(idx)
	return idx
}

func (l *Loader) loadAliases(idx *Index) (map[string]string, map[string]string) {
	aliases, invalid := map[string]string{}, map[string]string{}
	raw, err := os.ReadFile(l.Config.AliasesPath)
	if err != nil {
		return aliases, invalid
	}
	var data map[string]any
	if _, err := toml.Decode(string(raw), &data); err != nil {
		return aliases, invalid
	}
	table, ok := data["aliases"].(map[string]any)
	if !ok {
		return aliases, invalid
	}
	for rawAlias, rawID := range table {
		alias := NormalizeAlias(rawAlias)
		docID := pystr.Strip(tomlStr(rawID))
		if alias == "" || docID == "" {
			continue
		}
		if _, ok := idx.ByDocID[docID]; ok {
			aliases[alias] = docID
		} else {
			invalid[alias] = docID
		}
	}
	return aliases, invalid
}

func tomlStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return pystr.Str(x)
	case bool:
		return pystr.Str(x)
	}
	return ""
}

func (l *Loader) makeDoc(p string, fm *jsonx.Obj, body string, start int) *Doc {
	extra := jsonx.New()
	for _, k := range fm.Keys() {
		if !coreKeys[k] {
			v, _ := fm.Get(k)
			extra.Set(k, v)
		}
	}
	idv, _ := fm.Get("doc_id")
	docID := pystr.Str(idv)
	sensRaw, _ := fm.Get("sensitivity")
	sens := ""
	if pystr.Truthy(sensRaw) {
		sens = pystr.Str(sensRaw)
	}
	lr, _ := fm.Get("last_reviewed")
	tags, _ := fm.Get("tags")
	rp, _ := fm.Get("related_projects")
	ra, _ := fm.Get("related_assets")
	rel, err := filepath.Rel(l.Config.VaultPath, p)
	if err != nil {
		rel = p
	}
	return &Doc{
		DocID:           docID,
		Title:           strOr(fm, "title", docID),
		DocType:         strOr(fm, "doc_type", "runbook"),
		System:          strOr(fm, "system", ""),
		Environment:     strOr(fm, "environment", "other"),
		Status:          strOr(fm, "status", "active"),
		Sensitivity:     gate.Parse(sens),
		LastReviewed:    map[bool]string{true: pystr.Str(lr), false: ""}[pystr.Truthy(lr)],
		HasLastReviewed: pystr.Truthy(lr),
		Tags:            CoerceList(tags),
		RelatedProjects: CoerceList(rp),
		RelatedAssets:   CoerceList(ra),
		Path:            p,
		RelativePath:    rel,
		Body:            body,
		BodyStartLine:   start,
		Sections:        sections.Parse(body, start),
		Extra:           extra,
	}
}
