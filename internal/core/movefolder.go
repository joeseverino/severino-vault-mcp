package core

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/links"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// FolderArgs are move_folder's arguments: From and To are vault-relative
// folders. Archive, for a move into the archive, sets status: archived on
// every doc in the folder first. IncludeRestricted lets the link rewrite read
// restricted notes, behind the same unlock read_doc asks for.
type FolderArgs struct {
	From, To          string
	Archive           bool
	IncludeRestricted bool
	DryRun            bool
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
		if refused := archiveReady(v, docs, a.Archive, a.DryRun, Call("move_folder", append(again, "archive", true)...)); refused != nil {
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
	l := newLayout(from, to, files)
	if a.DryRun {
		return previewMove(v, root, l, docs)
	}
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
	links, skipped, failed, repairs := rewriteVault(v, root, l, a.IncludeRestricted)
	out.Set("links", links)
	if len(skipped) > 0 {
		out.Set("skipped_restricted", skipped)
	}
	if path.Dir(from) == vault.ProjectsDir && path.Dir(to) == vault.ProjectsDir {
		project, more := renameProject(v, root, path.Base(from), path.Base(to), to)
		out.Set("project", project)
		failed = append(failed, more.failed...)
		repairs = append(more.calls, repairs...)
	}
	v.Loader.Index(true)
	if len(failed) > 0 {
		out.Set("failed", failed)
		out.Set("repairs", repairs)
		steer(out, "run the calls in repairs in order; each body repair refuses changed or already-replaced text; the folder itself has moved",
			Call("read_doc", "doc_id", "...")+" for failed notes without a repair call, or if a repair refuses because the note changed")
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
func rewriteVault(v *Vault, root *fsx.Vault, l *layout, includeRestricted bool) (links []*jsonx.Obj, skipped, failed []string, repairs []*jsonx.Obj) {
	links = []*jsonx.Obj{}
	repairs = []*jsonx.Obj{}
	allowed, result := false, ""
	for _, rel := range vault.Notes(v.Config.VaultPath) {
		note, err := root.ReadNote(rel)
		if err != nil || !strings.Contains(note.Text, "[[") && !strings.Contains(note.Text, "](") {
			continue
		}
		if fm := note.FM; fm != nil {
			if restrictedNote(note) {
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
		before := note.Text[frontmatter.BodyOffset(note.Text):]
		after, n := l.rewriteLinks(before, rel)
		if n == 0 {
			continue
		}
		_, err = root.Update(rel, func(text string) string {
			off := frontmatter.BodyOffset(text)
			before = text[off:]
			after, n = l.rewriteLinks(before, rel)
			return text[:off] + after
		})
		switch {
		case err != nil:
			failed = append(failed, rel+": "+err.Error())
			if repair := linkRepair(v, rel, before, after); repair != nil {
				repairs = append(repairs, repair)
			}
		case n > 0:
			links = append(links, jsonx.New("relative_path", rel, "count", n))
		}
	}
	return links, skipped, failed, repairs
}

// renameProject carries a project's slug from old to new: every
// related_projects entry naming old names new, and the queue note at dir
// takes the new name in its title, system and heading. Its doc_id stays, as
// every doc_id does, and so do its items' ids; when the project's index.md
// sets no queue_prefix, the old slug is pinned there so new items keep the
// prefix the old ones carry.
func renameProject(v *Vault, root *fsx.Vault, old, name, dir string) (*jsonx.Obj, *moveRepairs) {
	repairs := &moveRepairs{}
	out := jsonx.New("from", old, "to", name)
	docs, skipped := projectRelations(v, old)
	related := []string{}
	for _, d := range docs {
		next := slices.Clone(d.RelatedProjects)
		for i, p := range next {
			if strings.EqualFold(p, old) {
				next[i] = name
			}
		}
		if !repairs.frontmatter(v, d.RelativePath, write.With("related_projects", slices.Compact(next))) {
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
		return out, repairs
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
		repairs.frontmatter(v, queueRel, c)
	}
	if title := readTitle(root, queueRel); title != "" {
		queueOut.Set("title", title)
	}
	out.Set("queue", queueOut)
	if prefix := pinPrefix(v, root, dir, old, name, repairs); prefix != "" {
		out.Set("queue_prefix", prefix)
	}
	return out, repairs
}

// projectRelations is the one set of references a project rename changes,
// shared with its preview. Archived and read-only fields keep their values.
func projectRelations(v *Vault, old string) (docs []*vault.Doc, skipped []string) {
	for _, d := range v.Loader.Index(false).WithArchived() {
		if !slices.ContainsFunc(d.RelatedProjects, func(p string) bool { return strings.EqualFold(p, old) }) {
			continue
		}
		if d.Archived || v.outsideWritable(d.RelativePath) {
			skipped = append(skipped, d.RelativePath)
		} else {
			docs = append(docs, d)
		}
	}
	return docs, skipped
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
func pinPrefix(v *Vault, root *fsx.Vault, dir, old, name string, repairs *moveRepairs) string {
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
	if !repairs.frontmatter(v, index, write.With("queue_prefix", slug)) {
		return sections.Slugify(name)
	}
	return slug
}
