package core

import (
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// archivedStatus is the status a doc carries in the archive.
const archivedStatus = schema.ArchivedStatus

func openTask(d *vault.Doc) bool {
	return schema.IsTask(d.Frontmatter) && !schema.Archivable(d.Frontmatter)
}

// unarchived are the docs among docs that cannot go into the archive as they
// stand, each with why. With archive, a doc that only lacks status: archived
// passes, since the move sets it; an open task never does.
func unarchived(docs []*vault.Doc, archive bool) []string {
	var out []string
	for _, d := range docs {
		switch {
		case openTask(d):
			out = append(out, d.RelativePath+" is a task with status "+pystr.Repr(d.Status)+"; close it (done or wontfix) first")
		case !schema.IsTask(d.Frontmatter) && d.Status != archivedStatus && !archive:
			out = append(out, d.RelativePath+" has status "+pystr.Repr(d.Status)+", not "+pystr.Repr(archivedStatus))
		}
	}
	return out
}

// archiveSteps are the ways into the archive for a refused move: close an
// open task, or set the status, here or in the same call with archive=true.
func archiveSteps(docs []*vault.Doc, retry string) []string {
	for _, d := range docs {
		if openTask(d) {
			return []string{Call("task_write", "action", "status", "doc_id", d.DocID, "status", "done") + " for each open task the error names, then the same move"}
		}
	}
	steps := []string{retry + " to set status: archived in the same call"}
	for _, d := range docs {
		if d.Status != archivedStatus {
			return append(steps, Call("set_frontmatter", "relative_path", d.RelativePath, "status", archivedStatus)+" for each doc the error names, then the same move")
		}
	}
	return steps
}

// archiveAll sets status: archived, through set_frontmatter's path, on each
// doc among docs that is not a task and lacks it, before they move. The
// failure is the first refused write; nil when every write landed.
func archiveAll(v *Vault, docs []*vault.Doc) *jsonx.Obj {
	for _, d := range docs {
		if schema.IsTask(d.Frontmatter) || d.Status == archivedStatus {
			continue
		}
		if r := write.SetFrontmatter(v.Loader, d.RelativePath, write.With("status", archivedStatus), v.Profile); r.Failed() {
			return failNext("setting status: archived at "+d.RelativePath+" failed: "+r.Str("error"),
				Call("set_frontmatter", "relative_path", d.RelativePath, "status", archivedStatus)+" to see why, then the same move")
		}
	}
	return nil
}

// archiveReady is the refusal for a move of docs into the archive, or nil
// once every one of them is archived (with archive, once status is set).
func archiveReady(v *Vault, docs []*vault.Doc, archive bool, retry string) *jsonx.Obj {
	if problems := unarchived(docs, archive); len(problems) > 0 {
		return failNext("only archived docs go into "+v.Config.ArchiveDir+": "+strings.Join(problems, "; "), archiveSteps(docs, retry)...)
	}
	return archiveAll(v, docs)
}
