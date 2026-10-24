package core

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// previewMove uses the same link transform as a move, against the paths the
// notes would have afterwards. It writes nothing and never asks to unlock a
// restricted body. A preview is a snapshot, not a reservation of these paths.
func previewMove(v *Vault, root *fsx.Vault, l *layout, docs []*vault.Doc) *jsonx.Obj {
	moves, rewrites := []*jsonx.Obj{}, []*jsonx.Obj{}
	for rel := range l.Paths() {
		if l.has(rel) {
			moves = append(moves, jsonx.New("from", rel, "to", l.moved(rel)))
		}
	}
	skipped, failed := []string{}, []string{}
	for _, rel := range vault.Notes(v.Config.VaultPath) {
		note, err := root.ReadNote(rel)
		if err != nil {
			failed = append(failed, rel+": "+err.Error())
			continue
		}
		if restrictedNote(note) {
			skipped = append(skipped, rel)
			continue
		}
		if _, n := l.rewriteLinks(note.Text[frontmatter.BodyOffset(note.Text):], l.moved(rel)); n > 0 {
			rewrites = append(rewrites, jsonx.New("relative_path", l.moved(rel), "count", n))
		}
	}
	archive := []string{}
	if v.Config.Archived(l.to+"/") || v.Config.Archived(l.to) {
		for _, d := range docs {
			if !v.Profile.IsTask(d.Frontmatter) && d.Status != archivedStatus {
				archive = append(archive, d.RelativePath)
			}
		}
	}
	out := jsonx.New("ok", true, "dry_run", true, "from", l.from, "to", l.to,
		"files", len(moves), "docs", len(docs), "moves", moves, "links", rewrites,
		"archive_updates", archive, "skipped_restricted", skipped)
	if !l.file && path.Dir(l.from) == vault.ProjectsDir && path.Dir(l.to) == vault.ProjectsDir {
		related, skipped := projectRelations(v, path.Base(l.from))
		paths := []string{}
		for _, d := range related {
			paths = append(paths, l.moved(d.RelativePath))
		}
		out.Set("project", jsonx.New("from", path.Base(l.from), "to", path.Base(l.to),
			"related_projects", paths, "related_projects_skipped", skipped,
			"queue", jsonx.New("relative_path", path.Join(l.to, queueFile))))
	}
	if len(failed) > 0 {
		out.Set("failed", failed)
	}
	return out
}

// moveRepairs collects failed metadata writes and their ordinary tool calls.
// Frontmatter repairs precede body repairs, since a body edit validates the
// note's metadata too. Only scalar and list replacements are used by moves.
type moveRepairs struct {
	failed []string
	calls  []*jsonx.Obj
}

func (r *moveRepairs) frontmatter(v *Vault, rel string, changes write.Changes) bool {
	out := write.SetFrontmatter(v.Loader, rel, changes, v.Profile)
	if !out.Failed() {
		return true
	}
	r.failed = append(r.failed, rel+": "+out.Str("error"))
	args := jsonx.New("vault", v.Name, "relative_path", rel)
	for _, key := range slices.Sorted(maps.Keys(changes.Values)) {
		args.Set(key, changes.Values[key])
	}
	for _, key := range slices.Sorted(maps.Keys(changes.Lists)) {
		args.Set(key, changes.Lists[key].Set)
	}
	r.calls = append(r.calls, jsonx.New("tool", "set_frontmatter", "arguments", args))
	return false
}

func restrictedNote(note frontmatter.Note) bool {
	return note.FM != nil && gate.Parse(note.FM.Str("sensitivity")) == gate.Restricted
}

// linkRepair uses write_doc's existing exact-text contract. It only offers
// calls for registered, writable docs; other files remain in failed for an
// editor to repair. The call refuses if its passage changed or was applied
// already, rather than rerunning the move against a different layout.
func linkRepair(v *Vault, rel, before, after string) *jsonx.Obj {
	if before == after || before == "" || v.outsideWritable(rel) && !v.Config.Archived(rel) {
		return nil
	}
	idx := v.Loader.Index(false)
	d, matched, _ := resolveDoc(idx, rel)
	if d == nil || matched == nil || matched.Str("source") != "path" || d.ReadOnlyDir != "" {
		return nil
	}
	if unique, _, _ := resolveDoc(idx, d.DocID); unique != d {
		return nil
	}
	old, next := replacementSpan(before, after)
	args := jsonx.New("vault", v.Name, "mode", "replace_text", "doc_id", d.DocID,
		"old_text", old, "new_text", next)
	if d.Sensitivity == gate.Restricted {
		args.Set("include_restricted", true)
	}
	return jsonx.New("tool", "write_doc", "arguments", args)
}

// replacementSpan keeps the unchanged prefix and suffix out of a repair
// call. Rune boundaries keep paths with non-ASCII names valid UTF-8. If the
// smaller passage is ambiguous, the whole body is the exact-text guard.
func replacementSpan(before, after string) (string, string) {
	a, b := []rune(before), []rune(after)
	start := 0
	for start < min(len(a), len(b)) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	old, next := string(a[start:endA]), string(b[start:endB])
	if old == "" || strings.Index(before, old) != strings.LastIndex(before, old) {
		return before, after
	}
	return old, next
}
