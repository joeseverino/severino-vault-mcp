package core

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// A failure, and a find that comes back empty, carries next_steps: the calls
// that recover from it, so an agent's next move is never a guess. Each step
// starts with a call to one of the server's tools.

// Call renders a tool call: tool(name=value, ...), each value as JSON.
func Call(tool string, args ...any) string {
	parts := make([]string, 0, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		b, _ := jsonx.Encode(args[i+1], jsonx.Options{})
		parts = append(parts, args[i].(string)+"="+string(b))
	}
	return tool + "(" + strings.Join(parts, ", ") + ")"
}

// steer sets o's next_steps.
func steer(o *jsonx.Obj, steps ...string) *jsonx.Obj {
	if len(steps) > 0 {
		o.Set("next_steps", steps)
	}
	return o
}

// contended reports a write refused because another process held the note's
// lock, or kept changing the note, through the rewrite; the same call again
// is the recovery.
func contended(msg string) bool {
	return strings.Contains(msg, fsx.ErrChanged.Error()) || strings.Contains(msg, fsx.ErrLocked.Error())
}

const contendedWhy = "; another process was writing the note"

// failNext is a failure with the calls that recover from it.
func failNext(msg string, steps ...string) *jsonx.Obj { return steer(jsonx.Fail(msg), steps...) }

// guide gives a failure from the write or task layer its next steps; steps
// runs only for a failure, since it may look at the vault on disk.
func guide(o *jsonx.Obj, steps func(msg string) []string) *jsonx.Obj {
	if !o.Failed() || o.Has("next_steps") {
		return o
	}
	return steer(o, steps(o.Str("error"))...)
}

// withoutFields is the step for a refused field mask: the error names the
// valid fields, and leaving fields out gives the default.
func withoutFields(tool string, args ...any) string {
	return Call(tool, args...) + " without fields for the default, or with names the error lists"
}

// writable is the step for a path the vault won't take a doc at: the same
// file name in the first indexed folder, naming them all.
func (v *Vault) writable(rel string) string {
	dirs := v.Config.IndexedDirs
	if len(dirs) == 0 {
		return Call("find", "query", strings.TrimSuffix(filepath.Base(rel), ".md")) + " to find where docs like this live"
	}
	return Call("write_doc", "mode", "create", "relative_path", path.Join(filepath.ToSlash(dirs[0]), filepath.Base(rel))) +
		" in one of the indexed folders: " + strings.Join(dirs, ", ")
}

// outsideWritable reports whether rel is somewhere a doc can't be written:
// a read-only folder, or not a markdown file under the indexed folders.
func (v *Vault) outsideWritable(rel string) bool {
	clean := filepath.Clean(strings.TrimLeft(rel, "/"))
	return v.Config.ReadOnlyDir(clean) != "" || !vault.IsDocPath(clean, v.Config.IndexedDirs)
}

// exists reports whether rel is a file in the vault.
func (v *Vault) exists(rel string) bool {
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = root.Stat(strings.TrimLeft(rel, "/"))
	return err == nil
}

// docAt is the indexed doc at rel, or nil.
func (v *Vault) docAt(rel string) *vault.Doc {
	clean := filepath.Clean(strings.TrimLeft(rel, "/"))
	for _, d := range v.Loader.Index(false).Docs {
		if d.RelativePath == clean {
			return d
		}
	}
	return nil
}

// emptyFind is the way out of a search with no hits.
func (v *Vault) emptyFind(q string, a FindArgs) []string {
	var steps []string
	if a.System != "" || a.Project != "" {
		steps = append(steps, Call("find", "query", q)+" without system or project")
	}
	if a.By == "relevance" {
		steps = append(steps, Call("find", "query", q, "by", "text")+" to search doc bodies")
	}
	if words := strings.Fields(q); len(words) > 1 {
		steps = append(steps, Call("find", "query", words[0])+" with fewer, broader words")
	} else {
		steps = append(steps, "find(query=...) with a synonym or a broader word")
	}
	if _, ok := v.Loader.Index(false).ByDocID[QuickIndexDocID]; ok {
		steps = append(steps, Call("read_doc", "doc_id", QuickIndexDocID)+" to browse the vault's quick index")
	}
	return steps
}

// unlockSteps is the way to a withheld restricted body for each unlock result.
func unlockSteps(docID, result string) []string {
	retry := Call("read_doc", "doc_id", docID, "include_restricted", true)
	switch result {
	case "not_requested":
		return []string{retry + " from the main session, only if the user needs this body"}
	case "unconfigured":
		return []string{retry + " after the user sets restricted.unlock_ref"}
	case "service-account-refused":
		return []string{retry + " after the user restarts the server without the service account token"}
	}
	return []string{retry + " after the user unlocks 1Password and approves the Touch ID prompt"}
}

// scopeRefusal is schema.Field.ScopeError's wording; its group is the scope.
var scopeRefusal = regexp.MustCompile(`only applies to docs at '([^']+)'`)

// setSteps recovers a refused set_frontmatter.
func (v *Vault) setSteps(rel, docID string) func(string) []string {
	return func(msg string) []string {
		switch {
		case contended(msg):
			return []string{Call("set_frontmatter", "relative_path", rel) + " again with the same fields" + contendedWhy}
		case v.outsideWritable(rel):
			return []string{v.writable(rel)}
		case !v.exists(rel):
			return []string{Call("write_doc", "mode", "create", "relative_path", rel) +
				" with body, doc_id, title, doc_type and system to create the doc"}
		case strings.HasPrefix(msg, "doc_id ") && strings.Contains(msg, "already exists at"):
			return []string{Call("read_doc", "doc_id", docID) + " to see the doc holding that id, then pick another doc_id"}
		case strings.Contains(msg, "already exists at"):
			return []string{Call("set_frontmatter", "relative_path", rel) + " with another value, or remove it from the doc the error names first"}
		case strings.Contains(msg, "immutable"):
			return []string{Call("set_frontmatter", "relative_path", rel) + " without that field"}
		case scopeRefusal.MatchString(msg):
			return []string{Call("set_frontmatter", "relative_path", scopeRefusal.FindStringSubmatch(msg)[1]) + " naming the doc that path pattern matches"}
		}
		return []string{Call("set_frontmatter", "relative_path", rel) + " again with the fields the error names corrected"}
	}
}

// createSteps recovers a refused write_doc create.
func (v *Vault) createSteps(rel, docID string) func(string) []string {
	return func(msg string) []string {
		switch {
		case v.outsideWritable(rel):
			return []string{v.writable(rel)}
		case v.exists(rel):
			ref := rel
			if d := v.docAt(rel); d != nil {
				ref = d.DocID
			}
			return []string{
				Call("read_doc", "doc_id", ref, "fields", []string{"outline"}) + " to see its sections",
				Call("write_doc", "mode", "replace_section", "relative_path", rel) + " with heading and body to edit one",
			}
		case strings.Contains(msg, "already exists at"):
			return []string{Call("read_doc", "doc_id", docID) + " to see the doc holding that id, then pick another doc_id"}
		}
		return []string{Call("write_doc", "mode", "create", "relative_path", rel) + " again with what the error names fixed"}
	}
}

// sectionSteps recovers a refused section edit of d.
func (v *Vault) sectionSteps(d *vault.Doc, mode string) func(string) []string {
	return func(msg string) []string {
		switch {
		case contended(msg):
			return []string{Call("write_doc", "mode", mode, "doc_id", d.DocID) + " again with the same heading and body" + contendedWhy}
		case v.outsideWritable(d.RelativePath):
			return []string{Call("read_doc", "doc_id", d.DocID) + " to read it; docs in read-only folders are not edited here"}
		case strings.Contains(msg, "set_frontmatter"):
			return []string{Call("set_frontmatter", "relative_path", d.RelativePath) + " with the fields the error names, then retry"}
		case strings.Contains(msg, "nothing to append"):
			return []string{Call("write_doc", "mode", mode, "doc_id", d.DocID) + " with a non-empty body"}
		}
		return []string{
			Call("read_doc", "doc_id", d.DocID, "fields", []string{"outline"}) + " to see its headings",
			Call("write_doc", "mode", mode, "doc_id", d.DocID) + " with heading set to one of them exactly",
		}
	}
}

// taskSteps recovers a refused task change.
func taskSteps(a TaskWriteArgs) func(string) []string {
	return func(msg string) []string {
		if contended(msg) {
			return []string{Call("task_write", "action", a.Action, "doc_id", a.DocID) + " again" + contendedWhy}
		}
		switch a.Action {
		case "add":
			if strings.Contains(msg, "no such project") {
				return []string{Call("task_board", "limit", 1) + " to list the projects a task can go in"}
			}
			return []string{Call("task_write", "action", "add", "title", a.Title) + " again with what the error names fixed"}
		case "promote":
			return []string{Call("find", "query", strings.TrimSuffix(filepath.Base(a.NotePath), ".md")) + " to get the note's path"}
		}
		return []string{Call("task_board", "include_all", true, "fields", []string{"doc_id", "title", "status"}) +
			" to get the task's doc_id"}
	}
}
