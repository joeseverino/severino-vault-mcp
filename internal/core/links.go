package core

import (
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/sections"
)

// Links that name a file by path, as Obsidian writes them: a wikilink or an
// embed, its target ending at the first | or # (a table cell escapes the
// pipe as \|), and a markdown link or image, its target in angle brackets
// or bare up to whitespace or the closing parenthesis.
var (
	wikiLinkRE = regexp.MustCompile(`(!?\[\[)([^\[\]|#\n]*)((?:[#|][^\[\]\n]*)?\]\])`)
	mdLinkRE   = regexp.MustCompile(`(!?\[[^\]\n]*\]\()(<[^>\n]*>|[^)\s]+)((?:\s+"[^"\n]*")?\))`)
	schemeRE   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// layout is the vault's files before a folder moved, and the move: from and
// to are clean vault-relative folders.
type layout struct {
	from, to string
	files    map[string]string // lowercased path -> path, before the move
	sorted   []string          // the paths, sorted
}

// vaultFiles is every file in the vault outside its hidden trees (the note
// locks, .obsidian, .trash), as clean vault-relative paths.
func vaultFiles(vaultPath string) []string {
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

func newLayout(from, to string, files []string) *layout {
	l := &layout{from: from, to: to, files: map[string]string{}, sorted: files}
	for _, f := range files {
		l.files[strings.ToLower(f)] = f
	}
	return l
}

// under reports whether rel is inside dir.
func under(rel, dir string) bool { return strings.HasPrefix(rel, dir+"/") }

// moved is where rel, a path before the move, is after it.
func (l *layout) moved(rel string) string {
	if under(rel, l.from) {
		return l.to + rel[len(l.from):]
	}
	return rel
}

// before is where rel, a path after the move, was before it.
func (l *layout) before(rel string) string {
	if under(rel, l.to) {
		return l.from + rel[len(l.to):]
	}
	return rel
}

// exact is the file at rel, or at rel.md, compared case-insensitively.
func (l *layout) exact(rel string) (string, bool) {
	low := strings.ToLower(rel)
	if f, ok := l.files[low]; ok {
		return f, true
	}
	f, ok := l.files[low+".md"]
	return f, ok
}

// bySuffix is the first file whose path ends in /rel or /rel.md, as
// Obsidian resolves a partial path.
func (l *layout) bySuffix(rel string) (string, bool) {
	low := "/" + strings.ToLower(rel)
	for _, f := range l.sorted {
		lf := strings.ToLower(f)
		if strings.HasSuffix(lf, low) || strings.HasSuffix(lf, low+".md") {
			return f, true
		}
	}
	return "", false
}

// endsWith reports whether a partial path still names target.
func endsWith(target, partial string) bool {
	lt, lp := strings.ToLower(target), "/"+strings.ToLower(partial)
	return strings.HasSuffix(lt, lp) || strings.HasSuffix(lt, lp+".md")
}

// sameExt keeps the written form's extension: a link written without .md
// gets a path without it.
func sameExt(written, rel string) string {
	if !strings.HasSuffix(strings.ToLower(written), ".md") {
		return strings.TrimSuffix(rel, ".md")
	}
	return rel
}

// wikiTarget is the new target of a wikilink written as target in a note,
// or "" when it needs none. A link by file name alone resolves wherever the
// file is; one by path changes when the file it names moved.
func (l *layout) wikiTarget(target string) string {
	t := strings.TrimSuffix(target, `\`)
	clean := strings.TrimLeft(strings.TrimSpace(t), "/")
	if !strings.Contains(clean, "/") {
		return ""
	}
	start := strings.Index(t, clean)
	lead, tail := t[:start], target[start+len(clean):]
	if f, ok := l.exact(clean); ok {
		if !under(f, l.from) {
			return ""
		}
		return lead + l.to + clean[len(l.from):] + tail
	}
	if under(strings.ToLower(clean), strings.ToLower(l.from)) {
		return lead + l.to + clean[len(l.from):] + tail
	}
	f, ok := l.bySuffix(clean)
	if !ok || !under(f, l.from) || endsWith(l.moved(f), clean) {
		return ""
	}
	return lead + sameExt(clean, l.moved(f)) + tail
}

// mdTarget is the new target of a markdown link written as raw in the note
// at note (its path after the move), or "" when it needs none. A target that
// starts with ./ or ../, or that resolves from the note's folder, is
// relative and stays relative.
func (l *layout) mdTarget(raw, note string) string {
	angle := strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">")
	inner := raw
	if angle {
		inner = raw[1 : len(raw)-1]
	}
	link, _, _ := strings.Cut(inner, "#")
	frag := inner[len(link):]
	encoded := strings.Contains(link, "%")
	p := link
	if encoded {
		if d, err := url.PathUnescape(link); err == nil {
			p = d
		}
	}
	if p == "" || schemeRE.MatchString(p) {
		return ""
	}
	oldNote, newNote := l.before(note), note
	explicit := strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
	var next string
	if f, ok := l.exact(path.Join(path.Dir(oldNote), p)); ok || explicit {
		if !ok {
			return ""
		}
		target := l.moved(f)
		if path.Join(path.Dir(newNote), p) == target || strings.EqualFold(path.Join(path.Dir(newNote), p), target) {
			return ""
		}
		next = relPath(path.Dir(newNote), target)
		next = sameExt(p, next)
	} else {
		clean := strings.TrimLeft(p, "/")
		f, ok := l.exact(clean)
		switch {
		case ok && under(f, l.from):
			next = l.to + clean[len(l.from):]
		case !ok && under(strings.ToLower(clean), strings.ToLower(l.from)):
			next = l.to + clean[len(l.from):]
		case !ok && strings.Contains(clean, "/"):
			g, found := l.bySuffix(clean)
			if !found || !under(g, l.from) || endsWith(l.moved(g), clean) {
				return ""
			}
			next = sameExt(clean, l.moved(g))
		default:
			return ""
		}
		next = p[:len(p)-len(clean)] + next
	}
	switch {
	case angle:
		return "<" + next + frag + ">"
	case encoded:
		return escapePath(next) + frag
	case strings.ContainsAny(next, " ()<>"):
		return "<" + next + frag + ">"
	}
	return next + frag
}

// relPath is target written relative to the folder dir.
func relPath(dir, target string) string {
	rel, err := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(target))
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

// escapePath percent-encodes each segment of p.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// rewriteLinks is body, the body of the note at note (its path after the
// move), with every link into the moved folder, and every relative link the
// move broke, pointing where its file is now; n is how many changed. Fenced
// code and inline code spans are left as they are.
func (l *layout) rewriteLinks(body, note string) (string, int) {
	lines := sections.Lines(body)
	n := 0
	for i, code := range sections.Fenced(lines) {
		if code || !strings.Contains(lines[i], "](") && !strings.Contains(lines[i], "[[") {
			continue
		}
		spans := strings.Split(lines[i], "`")
		for j := 0; j < len(spans); j += 2 {
			spans[j] = wikiLinkRE.ReplaceAllStringFunc(spans[j], func(m string) string {
				g := wikiLinkRE.FindStringSubmatch(m)
				if t := l.wikiTarget(g[2]); t != "" && t != g[2] {
					n++
					return g[1] + t + g[3]
				}
				return m
			})
			spans[j] = mdLinkRE.ReplaceAllStringFunc(spans[j], func(m string) string {
				g := mdLinkRE.FindStringSubmatch(m)
				if t := l.mdTarget(g[2], note); t != "" && t != g[2] {
					n++
					return g[1] + t + g[3]
				}
				return m
			})
		}
		lines[i] = strings.Join(spans, "`")
	}
	if n == 0 {
		return body, 0
	}
	return strings.Join(lines, "\n"), n
}
