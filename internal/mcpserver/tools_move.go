package mcpserver

import (
	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
)

func (h *handlers) moveDoc() coreTool {
	input := object(jsonx.New(
		"to", str(`Destination from the vault root: a .md path ("Security/Reports/Example Audit.md"), or a folder ending in / to keep the file name.`),
		"vault", h.vaultProp("Which vault."),
		"doc_id", optStr(core.DocRefHelp("The doc to move")),
		"relative_path", optStr("The doc's current path from the vault root, instead of doc_id."),
		"with_folder", boolean("Move the doc's whole folder, files beside it included, into `to` (a folder)."),
		"archive", boolean("Into the archive: set status: archived first instead of refusing a doc without it."),
		"dry_run", boolean("Preview moved files, link counts and archive updates without writing. Restricted bodies stay withheld."),
	), []string{"to"})
	return coreTool{"move_doc", descMoveDoc, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		m := core.MoveArgs{
			To:           p.required("to"),
			DocID:        p.str("doc_id"),
			RelativePath: p.str("relative_path"),
			WithFolder:   p.boolean("with_folder"),
			Archive:      p.boolean("archive"),
			DryRun:       p.boolean("dry_run"),
		}
		if p.err != nil {
			return nil, p.err
		}
		return core.MoveDoc(v, m), nil
	}}
}

func (h *handlers) moveFolder() coreTool {
	input := object(jsonx.New(
		"from", str(`The folder to move, from the vault root ("01 Projects/example-old").`),
		"to", str(`Its new path from the vault root ("01 Projects/example-new", "99 Archive/example-old").`),
		"vault", h.vaultProp("Which vault."),
		"archive", boolean("Into the archive: set status: archived on every doc in the folder first instead of refusing."),
		"include_restricted", boolean("Also rewrite links inside restricted notes, behind a Touch ID approval through 1Password."),
		"dry_run", boolean("Preview moved files, link counts and archive updates without writing. Restricted bodies stay withheld."),
	), []string{"from", "to"})
	return coreTool{"move_folder", descMoveFolder, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		m := core.FolderArgs{
			From:              p.required("from"),
			To:                p.required("to"),
			Archive:           p.boolean("archive"),
			IncludeRestricted: p.boolean("include_restricted"),
			DryRun:            p.boolean("dry_run"),
		}
		if p.err != nil {
			return nil, p.err
		}
		return core.MoveFolder(v, m), nil
	}}
}

const descMoveDoc = `Move or rename a registered doc; its doc_id and frontmatter stay as they are.

to is a .md path, or a folder ending in / that keeps the file name; missing
folders are created. Refused: a destination that exists, one outside the
indexed folders or inside a read-only one, and a file name another note in
the vault already has (Obsidian resolves [[Name]] by unique file name). A
rename in the same folder is how a shared name like Audit.md gets a unique
one. with_folder moves the doc's folder, evidence files included, into to.
The archive (99 Archive) takes only archived docs: status archived, or a
closed task; archive=true sets status: archived first. find leaves archived
docs out unless include_archived=true.
Every [[path/link]], ![[embed]] and [text](path) in the vault that named the
doc by path names its new path, alias and #heading kept; links by file name
alone need nothing. links lists each rewritten note with a count. Restricted
notes are not read: they are listed in skipped_restricted.
dry_run previews the files and link counts without writes or restricted
unlocks. If a move's link writes fail, repairs holds exact write_doc calls
for the registered, writable notes; run those, not the move again.`

const descMoveFolder = `Move or rename a folder with everything in it (notes, Reports/, assets), then point every link at the new path.

Afterwards each [[path/link]], ![[embed]] and [text](path) anywhere in the
vault that named a file in the folder by path names its new path, and a
relative link the move broke is recomputed; links by file name alone need
nothing. links lists each rewritten note with a count. Restricted notes are
rewritten only with include_restricted and Touch ID, else listed in
skipped_restricted. Refused: a destination that exists, one outside the
indexed folders and the archive, and a read-only folder.
A project renamed under 01 Projects also updates every related_projects entry
naming the old slug and retitles its queue note; doc_ids and queue item ids
never change, and an index.md without queue_prefix gets the old slug pinned
so new items keep their prefix. Into 99 Archive every doc must be archived
(a task: closed); archive=true sets status: archived first.
dry_run previews the files and link counts without writes or restricted
unlocks. If a move's link writes fail, repairs holds exact write_doc calls
for the registered, writable notes; run those, not the move again.`
