// Package links reads the links in a note's body that name a file by path,
// as Obsidian writes them, and resolves them against the vault's files the
// way Obsidian does. move_folder rewrites them and doctor checks them.
package links

import (
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/sections"
)

// Wiki matches a wikilink or an embed: the opener, the target ending at the
// first | or # (a table cell escapes the pipe as \|), and the rest up to the
// closing brackets. Markdown matches a markdown link or image, its target in
// angle brackets or bare up to whitespace or the closing parenthesis.
var (
	Wiki     = regexp.MustCompile(`(!?\[\[)([^\[\]|#\n]*)((?:[#|][^\[\]\n]*)?\]\])`)
	Markdown = regexp.MustCompile(`(!?\[[^\]\n]*\]\()(<[^>\n]*>|[^)\s]+)((?:\s+"[^"\n]*")?\))`)
)

// VaultFiles is every file in the vault outside its hidden trees (the note
// locks, .obsidian, .trash), as clean vault-relative paths, sorted.
func VaultFiles(vaultPath string) []string {
	var out []string
	_ = filepath.WalkDir(vaultPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != vaultPath && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(vaultPath, p); err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// Files are a vault's file paths, looked up as Obsidian resolves a link.
type Files struct {
	byLower map[string]string
	sorted  []string
}

// NewFiles indexes files, which are sorted.
func NewFiles(files []string) *Files {
	f := &Files{byLower: map[string]string{}, sorted: files}
	for _, p := range files {
		f.byLower[strings.ToLower(p)] = p
	}
	return f
}

// Exact is the file at rel, or at rel.md, compared case-insensitively.
func (f *Files) Exact(rel string) (string, bool) {
	low := strings.ToLower(rel)
	if p, ok := f.byLower[low]; ok {
		return p, true
	}
	p, ok := f.byLower[low+".md"]
	return p, ok
}

// BySuffix is the first file whose path ends in /rel or /rel.md, as
// Obsidian resolves a partial path.
func (f *Files) BySuffix(rel string) (string, bool) {
	low := "/" + strings.ToLower(rel)
	for _, p := range f.sorted {
		lp := strings.ToLower(p)
		if strings.HasSuffix(lp, low) || strings.HasSuffix(lp, low+".md") {
			return p, true
		}
	}
	return "", false
}

// PathTarget splits a wikilink target into the clean vault path it names
// and the text around it; ok is false for a link by file name alone, which
// resolves wherever the file is.
func PathTarget(target string) (lead, clean, tail string, ok bool) {
	t := strings.TrimSuffix(target, `\`)
	clean = strings.TrimLeft(strings.TrimSpace(t), "/")
	if !strings.Contains(clean, "/") {
		return "", "", "", false
	}
	start := strings.Index(t, clean)
	return t[:start], clean, target[start+len(clean):], true
}

// Prose is body with fn applied to every stretch of text outside fenced
// code and inline code spans that holds a link, and n is fn's count of what
// it changed. Lines without a link are left as they are.
func Prose(body string, fn func(span string) (string, int)) (string, int) {
	lines := sections.Lines(body)
	n := 0
	for i, code := range sections.Fenced(lines) {
		if code || !strings.Contains(lines[i], "](") && !strings.Contains(lines[i], "[[") {
			continue
		}
		spans := strings.Split(lines[i], "`")
		for j := 0; j < len(spans); j += 2 {
			var k int
			spans[j], k = fn(spans[j])
			n += k
		}
		lines[i] = strings.Join(spans, "`")
	}
	if n == 0 {
		return body, 0
	}
	return strings.Join(lines, "\n"), n
}

// Dangling are the path wikilinks in the body of the note at note, outside
// code, whose target names no file in files: not the path from the vault
// root or from the note's folder, nor a file whose path it ends.
func Dangling(body, note string, files *Files) []string {
	var out []string
	Prose(body, func(span string) (string, int) {
		for _, g := range Wiki.FindAllStringSubmatch(span, -1) {
			_, clean, _, ok := PathTarget(g[2])
			if !ok {
				continue
			}
			if _, found := files.Exact(clean); found {
				continue
			}
			if _, found := files.Exact(path.Join(path.Dir(note), strings.TrimSpace(g[2]))); found {
				continue
			}
			if _, found := files.BySuffix(clean); found {
				continue
			}
			out = append(out, clean)
		}
		return span, 0
	})
	return out
}
