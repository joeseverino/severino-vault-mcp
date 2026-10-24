package core

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/links"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// FolderArgs are move_folder's arguments: From and To are vault-relative
// folders. Archive, for a move into the archive, sets status: archived on
// every doc in the folder first. IncludeRestricted lets the link rewrite read
// restricted notes, behind the same unlock read_doc asks for.
type FolderArgs struct {
	From, To          string
	Archive           bool
	IncludeRestricted bool
}

// MoveFolder moves or renames a folder with everything in it, then points
// every link in the vault that named a file in it by path at the file's new
// path. A project folder renamed under 01 Projects takes its slug along:
// related_projects entries naming the old one name the new one, and the
// project's queue note is retitled. doc_ids never change.
func MoveFolder(v *Vault, a FolderArgs) *jsonx.Obj {
	if f := v.Unreadable(); f != nil {
		return f
	}
	if v.Profile == nil {
		return failNext("vault "+strconv.Quote(v.Name)+" has no schema profile", noProfile)
	}
	from, to, msg, steps := folderTarget(v, a)
	if msg != "" {
		return failNext(msg, steps...)
	}
	again := []any{"from", from, "to", to}
	idx := v.Loader.Index(true)
	var docs []*vault.Doc
	for _, d := range idx.WithArchived() {
		if config.Under(d.RelativePath, from) {
			docs = append(docs, d)
		}
	}
	if v.Config.Archived(to + "/") {
		if refused := archiveReady(v, docs, a.Archive, Call("move_folder", append(again, "archive", true)...)); refused != nil {
			return refused
		}
	} else {
		var problems []string
		for _, d := range docs {
			for _, p := range newProblems(v, d, d.RelativePath, to+d.RelativePath[len(from):]) {
				problems = append(problems, d.RelativePath+": "+p)
			}
		}
		if len(problems) > 0 {
			return failNext(fmt.Sprintf("at %s the frontmatter would not validate: %s", to, strings.Join(problems, "; ")),
				Call("set_frontmatter", "relative_path", "...")+" to drop the fields the error names first, then the same move")
		}
	}
	files := links.VaultFiles(v.Config.VaultPath)
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return failNext("move failed: "+err.Error(), Call("move_folder", again...)+" again")
	}
	defer root.Close()
	switch err := root.Move(from, to); {
	case errors.Is(err, fsx.ErrExists):
		return failNext("destination exists: "+to, Call("move_folder", "from", from, "to", to+" (moved)")+" or another path nothing occupies")
	case err != nil && contended(err.Error()):
		return failNext("move failed: "+err.Error(), Call("move_folder", again...)+" again"+contendedWhy)
	case err != nil:
		return failNext("move failed: "+err.Error(), Call("move_folder", again...)+" again once the error is fixed")
	}
	v.Loader.Index(true)
	out := jsonx.New("ok", true, "from", from, "to", to, "files", countUnder(files, from), "docs", len(docs))
	l := newLayout(from, to, files)
	links, skipped, failed := rewriteVault(v, root, l, a.IncludeRestricted)
	out.Set("links", links)
	if len(skipped) > 0 {
		out.Set("skipped_restricted", skipped)
	}
	if path.Dir(from) == vault.ProjectsDir && path.Dir(to) == vault.ProjectsDir {
		project, more := renameProject(v, root, path.Base(from), path.Base(to), to)
		out.Set("project", project)
		failed = append(failed, more...)
	}
	v.Loader.Index(true)
	if len(failed) > 0 {
		out.Set("failed", failed)
		steer(out, Call("read_doc", "doc_id", "...")+" for each note failed names, then fix its links with write_doc or its fields with set_frontmatter; the folder itself has moved")
	}
	return out
}

func countUnder(files []string, dir string) int {
	n := 0
	for _, f := range files {
		if config.Under(f, dir) {
			n++
		}
	}
	return n
}

// folderTarget is the clean source and destination of a folder move, or why
// the move is refused and what to call instead.
func folderTarget(v *Vault, a FolderArgs) (from, to, msg string, steps []string) {
	from = path.Clean(strings.Trim(strings.TrimSpace(a.From), "/"))
	to = path.Clean(strings.Trim(strings.TrimSpace(a.To), "/"))
	retry := func(t string) string { return Call("move_folder", "from", from, "to", t) }
	homes := v.destinations()
	home := func(rel string) string {
		for _, d := range homes {
			if config.Under(rel, d) {
				return d
			}
		}
		return ""
	}
	exampleTo := path.Join(path.Dir(from), path.Base(from)+" (renamed)")
	switch {
	case a.From == "" || from == ".":
		return "", "", "from is required: a vault-relative folder, such as a project's", []string{Call("find", "query", "...") + " to find the folder's docs"}
	case a.To == "" || to == ".":
		return "", "", "to is required: the folder's new vault-relative path", []string{retry(exampleTo)}
	case strings.HasPrefix(from, "../") || from == ".." || strings.HasPrefix(to, "../") || to == "..":
		return "", "", "from and to stay inside the vault", []string{retry(exampleTo)}
	case !v.isDir(from):
		return "", "", "no folder at " + from, []string{Call("find", "query", path.Base(from)) + " to find where it lives"}
	case v.Config.ReadOnlyDir(from+"/") != "":
		return "", "", fmt.Sprintf("%s is in %q, a read-only folder: its notes keep their own place", from, v.Config.ReadOnlyDir(from+"/")),
			[]string{Call("queue", "action", "add", "project", "...", "kind", "next", "text", "...") + " to track an inbox capture as work"}
	case home(from) == "":
		return "", "", fmt.Sprintf("from must be a folder inside one of %s, not one of them: %s", pystr.Quoted(homes), from),
			[]string{Call("move_doc", "doc_id", "...", "to", "...") + " to move one doc"}
	case v.Config.ReadOnlyDir(to+"/") != "":
		return "", "", fmt.Sprintf("%s is in %q, and %s are read-only", to, v.Config.ReadOnlyDir(to+"/"), pystr.Quoted(v.Config.ReadOnlyDirs)),
			[]string{retry(path.Join(homes[0], path.Base(from)))}
	case home(to) == "":
		return "", "", fmt.Sprintf("to must be a folder inside one of %s: %s", pystr.Quoted(homes), to), []string{retry(path.Join(homes[0], path.Base(from)))}
	case to == from:
		return "", "", from + " is already there", []string{retry(exampleTo)}
	case config.Under(to, from):
		return "", "", to + " is inside " + from + "; a folder cannot move into itself", []string{retry(path.Join(path.Dir(from), path.Base(to)))}
	case v.exists(to):
		return "", "", "destination exists: " + to, []string{retry(to + " (moved)")}
	}
	return from, to, "", nil
}

// isDir reports whether rel is a folder in the vault.
func (v *Vault) isDir(rel string) bool {
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return false
	}
	defer root.Close()
	return root.IsDir(rel)
}

// rewriteVault points every note's links at the files' paths after the move,
// each note through fsx.Vault.Update. links lists each rewritten note with
// how many links changed. A restricted note is read only when the caller
// asked and the unlock approved; otherwise it is listed in skipped.
func rewriteVault(v *Vault, root *fsx.Vault, l *layout, includeRestricted bool) (links []*jsonx.Obj, skipped, failed []string) {
	links = []*jsonx.Obj{}
	allowed, result := false, ""
	for _, rel := range vault.Notes(v.Config.VaultPath) {
		note, err := root.ReadNote(rel)
		if err != nil || !strings.Contains(note.Text, "[[") && !strings.Contains(note.Text, "](") {
			continue
		}
		if fm := note.FM; fm != nil {
			if s, _ := fm.Get("sensitivity"); gate.Parse(pystr.Str(orNone(s))) == gate.Restricted {
				if !includeRestricted {
					skipped = append(skipped, rel)
					continue
				}
				if result == "" {
					allowed, result, _ = unlock(v, "", "")
				}
				gate.AuditUnlock(v.Config.RestrictedAuditLog, fm.Text("doc_id"), result)
				if !allowed {
					skipped = append(skipped, rel)
					continue
				}
			}
		}
		n := 0
		_, err = root.Update(rel, func(text string) string {
			off := frontmatter.BodyOffset(text)
			body, changed := l.rewriteLinks(text[off:], rel)
			n = changed
			return text[:off] + body
		})
		switch {
		case err != nil:
			failed = append(failed, rel+": "+err.Error())
		case n > 0:
			links = append(links, jsonx.New("relative_path", rel, "count", n))
		}
	}
	return links, skipped, failed
}

// renameProject carries a project's slug from old to new: every
// related_projects entry naming old names new, and the queue note at dir
// takes the new name in its title, system and heading. Its doc_id stays, as
// every doc_id does, and so do its items' ids; when the project's index.md
// sets no queue_prefix, the old slug is pinned there so new items keep the
// prefix the old ones carry.
func renameProject(v *Vault, root *fsx.Vault, old, name, dir string) (*jsonx.Obj, []string) {
	var failed []string
	out := jsonx.New("from", old, "to", name)
	idx := v.Loader.Index(true)
	related, skipped := []string{}, []string{}
	for _, d := range idx.WithArchived() {
		if !slices.ContainsFunc(d.RelatedProjects, func(p string) bool { return strings.EqualFold(p, old) }) {
			continue
		}
		if d.Archived || v.outsideWritable(d.RelativePath) {
			skipped = append(skipped, d.RelativePath)
			continue
		}
		next := slices.Clone(d.RelatedProjects)
		for i, p := range next {
			if strings.EqualFold(p, old) {
				next[i] = name
			}
		}
		if r := write.SetFrontmatter(v.Loader, d.RelativePath, write.With("related_projects", slices.Compact(next)), v.Profile); r.Failed() {
			failed = append(failed, d.RelativePath+": "+r.Str("error"))
			continue
		}
		related = append(related, d.RelativePath)
	}
	out.Set("related_projects", related)
	if len(skipped) > 0 {
		out.Set("related_projects_skipped", skipped)
	}
	queueRel := path.Join(dir, queueFile)
	note, err := root.ReadNote(queueRel)
	if err != nil || note.FM == nil {
		return out, failed
	}
	queueOut := jsonx.New("doc_id", note.FM.Text("doc_id"), "relative_path", queueRel)
	c := write.Changes{}
	if note.FM.Text("title") == queueTitle(old) {
		c = c.Set("title", queueTitle(name))
	}
	if note.FM.Text("system") == old {
		c = c.Set("system", name)
	}
	if !c.Empty() {
		if r := write.SetFrontmatter(v.Loader, queueRel, c, v.Profile); r.Failed() {
			failed = append(failed, queueRel+": "+r.Str("error"))
		}
	}
	if title := readTitle(root, queueRel); title != "" {
		queueOut.Set("title", title)
	}
	out.Set("queue", queueOut)
	if prefix := pinPrefix(v, root, dir, old, name); prefix != "" {
		out.Set("queue_prefix", prefix)
	}
	return out, failed
}

func readTitle(root *fsx.Vault, rel string) string {
	note, err := root.ReadNote(rel)
	if err != nil || note.FM == nil {
		return ""
	}
	return note.FM.Text("title")
}

// pinPrefix sets queue_prefix on the project's index.md to the slug its
// queue items were issued under, when it sets none and the slug is one
// queue_prefix can hold. It returns the prefix the project's new items take.
func pinPrefix(v *Vault, root *fsx.Vault, dir, old, name string) string {
	index := path.Join(dir, "index.md")
	note, err := root.ReadNote(index)
	if err != nil || note.FM == nil {
		return sections.Slugify(name)
	}
	if p := note.FM.Text("queue_prefix"); p != "" {
		return p
	}
	slug := sections.Slugify(old)
	if slug == sections.Slugify(name) || len(v.Profile.Problems("queue_prefix", slug, false)) > 0 {
		return sections.Slugify(name)
	}
	if r := write.SetFrontmatter(v.Loader, index, write.With("queue_prefix", slug), v.Profile); r.Failed() {
		return sections.Slugify(name)
	}
	return slug
}
