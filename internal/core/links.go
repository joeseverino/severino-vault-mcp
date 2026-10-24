package core

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/links"
)

// schemeRE marks a markdown link to a URL rather than a file.
var schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// layout is the vault's files before a folder moved, and the move: from and
// to are clean vault-relative folders.
type layout struct {
	from, to string
	*links.Files
}

func newLayout(from, to string, files []string) *layout {
	return &layout{from: from, to: to, Files: links.NewFiles(files)}
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
	lead, clean, tail, ok := links.PathTarget(target)
	if !ok {
		return ""
	}
	if f, ok := l.Exact(clean); ok {
		if !under(f, l.from) {
			return ""
		}
		return lead + l.to + clean[len(l.from):] + tail
	}
	if under(strings.ToLower(clean), strings.ToLower(l.from)) {
		return lead + l.to + clean[len(l.from):] + tail
	}
	f, ok := l.BySuffix(clean)
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
	if f, ok := l.Exact(path.Join(path.Dir(oldNote), p)); ok || explicit {
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
		f, ok := l.Exact(clean)
		switch {
		case ok && under(f, l.from):
			next = l.to + clean[len(l.from):]
		case !ok && under(strings.ToLower(clean), strings.ToLower(l.from)):
			next = l.to + clean[len(l.from):]
		case !ok && strings.Contains(clean, "/"):
			g, found := l.BySuffix(clean)
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
	return links.Prose(body, func(span string) (string, int) {
		n := 0
		span = links.Wiki.ReplaceAllStringFunc(span, func(m string) string {
			g := links.Wiki.FindStringSubmatch(m)
			if t := l.wikiTarget(g[2]); t != "" && t != g[2] {
				n++
				return g[1] + t + g[3]
			}
			return m
		})
		span = links.Markdown.ReplaceAllStringFunc(span, func(m string) string {
			g := links.Markdown.FindStringSubmatch(m)
			if t := l.mdTarget(g[2], note); t != "" && t != g[2] {
				n++
				return g[1] + t + g[3]
			}
			return m
		})
		return span, n
	})
}
