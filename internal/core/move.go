package core

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// MoveArgs are move_doc's arguments. DocID or RelativePath names the doc; To
// is a vault-relative .md path, or a folder ending in "/" that keeps the file
// name. WithFolder moves the doc's whole folder into To, a folder. Archive,
// for a move into the archive, sets status: archived first.
type MoveArgs struct {
	DocID        string
	RelativePath string
	To           string
	WithFolder   bool
	Archive      bool
}

// MoveDoc moves or renames a registered doc, keeping its doc_id and
// frontmatter. Obsidian resolves [[Name]] by unique file name, so a
// destination whose name another note in the vault already has is refused;
// links elsewhere that named the old location are reported, never rewritten.
// A doc moves into the archive only once it is archived (a task: closed);
// Archive sets status: archived in the same call.
func MoveDoc(v *Vault, a MoveArgs) *jsonx.Obj {
	d, msg, steps := moveSource(v, a)
	if d == nil {
		return failNext(msg, steps...)
	}
	from := path.Clean(d.RelativePath)
	to, msg, steps := moveTarget(v, d, a)
	if msg != "" {
		return failNext(msg, steps...)
	}
	if v.Config.Archived(to) {
		again := []any{"doc_id", d.DocID, "to", a.To}
		if a.WithFolder {
			again = append(again, "with_folder", true)
		}
		if refused := archiveReady(v, []*vault.Doc{d}, a.Archive, Call("move_doc", append(again, "archive", true)...)); refused != nil {
			return refused
		}
	} else if problems := newProblems(v, d, from, to); len(problems) > 0 {
		return failNext(fmt.Sprintf("at %s the frontmatter would not validate: %s", to, strings.Join(problems, "; ")),
			Call("move_doc", "doc_id", d.DocID, "to", path.Dir(from)+"/")+" or another folder the fields apply to",
			Call("set_frontmatter", "relative_path", from)+" to drop the fields the error names first")
	}
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return failNext("move failed: "+err.Error(), Call("move_doc", "doc_id", d.DocID, "to", a.To)+" again")
	}
	if a.WithFolder {
		err = root.Move(path.Dir(from), path.Dir(to))
	} else {
		err = root.Move(from, to)
	}
	_ = root.Close()
	switch {
	case errors.Is(err, fsx.ErrExists):
		return failNext("destination exists: "+to, Call("move_doc", "doc_id", d.DocID, "to", "...")+" with a path nothing occupies")
	case err != nil && contended(err.Error()):
		return failNext("move failed: "+err.Error(), Call("move_doc", "doc_id", d.DocID, "to", a.To)+" again"+contendedWhy)
	case err != nil:
		return failNext("move failed: "+err.Error(), Call("move_doc", "doc_id", d.DocID, "to", a.To)+" again once the error is fixed")
	}
	v.Loader.Index(true)
	out := jsonx.New("ok", true, "doc_id", d.DocID, "from", from, "to", to)
	if a.WithFolder {
		out.Set("folder", jsonx.New("from", path.Dir(from), "to", path.Dir(to)))
	}
	links, skipped := staleLinks(v, from, to)
	out.Set("wikilinks", links)
	if len(skipped) > 0 {
		out.Set("skipped_restricted", skipped)
	}
	return out
}

// moveSource is the registered doc a move names, or why there is none and
// what to call instead.
func moveSource(v *Vault, a MoveArgs) (*vault.Doc, string, []string) {
	idx := v.Loader.Index(true)
	var d *vault.Doc
	switch {
	case a.DocID != "" && a.RelativePath != "":
		return nil, "pass doc_id or relative_path, not both", []string{Call("move_doc", "doc_id", a.DocID, "to", a.To) + " without relative_path"}
	case a.DocID != "":
		if dups, ok := idx.DuplicateDocIDs[a.DocID]; ok {
			return nil, fmt.Sprintf("duplicate doc_id %s at %s; move one by relative_path", pystr.Repr(a.DocID), pystr.ReprList(dups)),
				[]string{Call("move_doc", "relative_path", dups[0], "to", a.To)}
		}
		d = idx.ByDocID[a.DocID]
		if d == nil {
			return nil, "unknown doc_id " + pystr.Repr(a.DocID), []string{Call("find", "query", a.DocID) + " to get a doc_id, then retry with it"}
		}
	case a.RelativePath != "":
		d = v.docAt(a.RelativePath)
		if d == nil {
			return nil, "no registered doc at " + a.RelativePath,
				[]string{Call("find", "query", strings.TrimSuffix(path.Base(a.RelativePath), ".md")) + " to get the doc's doc_id or path"}
		}
	default:
		return nil, "pass doc_id or relative_path", []string{Call("find", "query", "...") + " to get the doc's doc_id"}
	}
	if d.ReadOnlyDir != "" {
		return nil, fmt.Sprintf("%s is in %s, a read-only folder: its notes keep their own place and frontmatter", d.RelativePath, pystr.Repr(d.ReadOnlyDir)),
			[]string{Call("task_write", "action", "promote", "note_path", d.RelativePath, "title", d.Title) + " to turn an inbox capture into a task"}
	}
	return d, "", nil
}

// moveTarget is the doc's vault-relative destination, or why the move is
// refused and what to call instead.
func moveTarget(v *Vault, d *vault.Doc, a MoveArgs) (string, string, []string) {
	from := path.Clean(d.RelativePath)
	retry := func(to string, more ...any) string {
		return Call("move_doc", append([]any{"doc_id", d.DocID, "to", to}, more...)...)
	}
	raw := strings.TrimLeft(strings.TrimSpace(a.To), "/")
	folder := strings.HasSuffix(raw, "/")
	switch {
	case raw == "":
		return "", "to is required: a vault-relative .md path, or a folder ending in /", []string{retry(path.Dir(from) + "/" + d.Stem() + " (renamed).md")}
	case !folder && !strings.HasSuffix(raw, ".md"):
		return "", "to must end in .md (a new path) or / (a folder, keeping the file name): " + a.To,
			[]string{retry(raw + ".md"), retry(raw + "/")}
	case a.WithFolder && !folder:
		return "", "with_folder moves the doc's folder into a folder; give to ending in /, and rename the doc in its own call",
			[]string{retry(path.Dir(raw)+"/", "with_folder", true), retry(path.Dir(from) + "/" + path.Base(raw))}
	}
	to := path.Clean(raw)
	if a.WithFolder {
		dir := path.Dir(from)
		if dir == "." || slices.Contains(v.Config.SearchDirs(), dir) || dir == v.Config.ArchiveDir {
			return "", fmt.Sprintf("with_folder would move %s, a top-level folder; move the doc alone", pystr.Repr(dir)),
				[]string{retry(to + "/")}
		}
		if others := siblingNotes(v, dir, from); len(others) > 0 {
			return "", fmt.Sprintf("with_folder moves one doc's folder, and %s also holds %s; move each doc alone", dir, pystr.ReprList(others)),
				[]string{retry(to + "/")}
		}
		to = path.Join(to, path.Base(dir), path.Base(from))
	} else if folder {
		to = path.Join(to, path.Base(from))
	}
	if strings.HasPrefix(to, "../") || to == ".." {
		return "", "to escapes the vault root: " + a.To, []string{v.movable(d, from)}
	}
	switch {
	case to == from:
		return "", from + " is already there", []string{retry(path.Dir(from)+"/"+d.Stem()+" (renamed).md") + " to rename it"}
	case v.Config.ReadOnlyDir(to) != "":
		return "", fmt.Sprintf("%s is in %s, and %s are read-only: their notes keep their own frontmatter", to,
			pystr.Repr(v.Config.ReadOnlyDir(to)), pystr.ReprList(v.Config.ReadOnlyDirs)), []string{v.movable(d, from)}
	case !vault.IsDocPath(to, v.destinations()):
		return "", "to must be a .md path under the indexed dirs " + pystr.ReprList(v.destinations()) + ": " + to, []string{v.movable(d, from)}
	case v.exists(to):
		return "", "destination exists: " + to, []string{retry(path.Dir(to) + "/" + d.Stem() + " (moved).md")}
	case a.WithFolder && v.exists(path.Dir(to)):
		return "", "destination folder exists: " + path.Dir(to), []string{retry(path.Dir(path.Dir(to)) + "/" + path.Base(path.Dir(to)) + " (moved)/")}
	}
	stem := strings.TrimSuffix(path.Base(to), ".md")
	for _, rel := range vault.Notes(v.Config.VaultPath) {
		if rel != from && strings.EqualFold(strings.TrimSuffix(path.Base(rel), ".md"), stem) {
			return "", fmt.Sprintf("%s already names %s: Obsidian resolves [[%s]] by unique file name, so a second one would change what links point to; give the doc a name no other note has",
					pystr.Repr(stem), rel, stem),
				[]string{Call("move_doc", "doc_id", d.DocID, "to", path.Dir(from)+"/"+uniqueName(d, stem)) + " to rename it first, then move it"}
		}
	}
	return to, "", nil
}

// destinations are the folders a move may land in: the indexed dirs, then
// the archive.
func (v *Vault) destinations() []string {
	if v.Config.ArchiveDir == "" {
		return v.Config.IndexedDirs
	}
	return append(slices.Clone(v.Config.IndexedDirs), v.Config.ArchiveDir)
}

// uniqueName suggests a file name for d that says which doc it is.
func uniqueName(d *vault.Doc, stem string) string {
	if d.Title != "" && !strings.EqualFold(d.Title, stem) && !strings.ContainsAny(d.Title, `/\:`) {
		return d.Title + ".md"
	}
	return path.Base(path.Dir(d.RelativePath)) + " " + stem + ".md"
}

// movable is the step for a destination the vault won't take: the same file
// name in the first indexed folder, naming them all.
func (v *Vault) movable(d *vault.Doc, from string) string {
	dirs := v.Config.IndexedDirs
	if len(dirs) == 0 {
		return Call("find", "query", d.Stem()) + " to find where docs like this live"
	}
	return Call("move_doc", "doc_id", d.DocID, "to", strings.TrimSuffix(dirs[0], "/")+"/") + " or a folder under another indexed dir: " + strings.Join(dirs, ", ")
}

// siblingNotes are the markdown files under dir other than the doc at from.
func siblingNotes(v *Vault, dir, from string) []string {
	var out []string
	for _, rel := range vault.Notes(v.Config.VaultPath) {
		if rel != from && strings.HasPrefix(rel, dir+"/") {
			out = append(out, rel)
		}
	}
	return out
}

// newProblems are the problems the doc's frontmatter would have at to and
// does not have at from: a field scoped to a path the doc leaves.
func newProblems(v *Vault, d *vault.Doc, from, to string) []string {
	if v.Profile == nil || d.Frontmatter == nil {
		return nil
	}
	before := v.Profile.Check(d.Frontmatter, from)
	var out []string
	for _, p := range v.Profile.Check(d.Frontmatter, to) {
		if !slices.Contains(before, p) {
			out = append(out, p)
		}
	}
	return out
}

// staleLinks are the notes whose [[links]] reached the doc at from and will
// not reach it at to, each with how many such links it holds. A restricted
// note's body is not read; its path is listed in skipped.
func staleLinks(v *Vault, from, to string) (links []*jsonx.Obj, skipped []string) {
	links = []*jsonx.Obj{}
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return links, nil
	}
	defer root.Close()
	for _, rel := range vault.Notes(v.Config.VaultPath) {
		if rel == to {
			continue
		}
		note, err := root.ReadNote(rel)
		if err != nil || !strings.Contains(note.Text, "[[") {
			continue
		}
		text := note.Text
		if fm := note.FM; fm != nil {
			if s, _ := fm.Get("sensitivity"); gate.Parse(pystr.Str(orNone(s))) == gate.Restricted {
				skipped = append(skipped, rel)
				continue
			}
		}
		n := 0
		for _, target := range wikiTargets(text) {
			if linksTo(target, from) && !linksTo(target, to) {
				n++
			}
		}
		if n > 0 {
			links = append(links, jsonx.New("relative_path", rel, "count", n))
		}
	}
	return links, skipped
}

// linksTo reports whether a [[target]] resolves to the note at rel: its name,
// or a trailing part of its path, without the .md, compared as Obsidian does,
// case-insensitively.
func linksTo(target, rel string) bool {
	t := strings.ToLower(strings.TrimSuffix(strings.TrimLeft(target, "/"), ".md"))
	p := strings.ToLower(strings.TrimSuffix(rel, ".md"))
	return t != "" && (p == t || strings.HasSuffix(p, "/"+t))
}

func orNone(v any) any {
	if v == nil {
		return ""
	}
	return v
}
