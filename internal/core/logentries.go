package core

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// LogActions are log_entries' actions; the first is the default.
var LogActions = []string{"list", "close", "annotate"}

// LogArgs are log_entries' arguments. List reads OpenOnly, Contains and
// Limit; close reads Match and Resolution; annotate reads Match and either
// Resolution, the closed entry's new resolution, or Reopen.
type LogArgs struct {
	Action     string
	OpenOnly   bool
	Contains   string
	Limit      int
	Match      string
	Resolution string
	Reopen     bool
	View
}

// EntryFields are the fields a log entry carries: its line in the file, its
// text without the bullet, and its resolution when closed.
var EntryFields = typed("line", tInt, "text", tStr, "closed", tBool, "resolution", tOptStr)

var leanEntry = mustMask(EntryFields, "line", "text")

// maxCandidates caps the entries an ambiguous close lists.
const maxCandidates = 10

func entryObj(e sections.Entry) *jsonx.Obj {
	var resolution any
	if e.Closed {
		resolution = e.Resolution
	}
	return jsonx.New("line", e.Line, "text", e.Text, "closed", e.Closed, "resolution", resolution)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// LogEntries lists a log note's entries, closes one with its resolution, or
// rewrites or removes a closed one's resolution.
func LogEntries(v *Vault, docID string, a LogArgs) *jsonx.Obj {
	if f := v.Unreadable(); f != nil {
		return f
	}
	list := func(args ...any) string { return Call("log_entries", append([]any{"doc_id", docID}, args...)...) }
	m, err := a.selection(EntryFields, leanEntry)
	if err != nil {
		return failNext(err.Error(), withoutFields("log_entries", "doc_id", docID))
	}
	switch a.Action {
	case "list":
		if a.Limit < 0 {
			return failNext("limit: give a positive number, or 0 for no limit", list("limit", 50))
		}
	case "close":
		if pystr.Strip(a.Match) == "" {
			return failNext("close needs match: text that appears in exactly one open entry", list()+" to see the open entries")
		}
		if a.Reopen {
			return failNext("reopen only applies to annotate", list("action", "annotate", "match", a.Match, "reopen", true))
		}
		if r := pystr.Strip(a.Resolution); r == "" || strings.ContainsAny(r, "\r\n") {
			return failNext("close needs resolution: one line saying how the entry was resolved",
				list("action", "close", "match", a.Match, "resolution", "fixed: ..."))
		}
	case "annotate":
		if pystr.Strip(a.Match) == "" {
			return failNext("annotate needs match: text that appears in exactly one closed entry", list("open_only", false)+" to see the closed entries")
		}
		r := pystr.Strip(a.Resolution)
		switch {
		case a.Reopen && r != "":
			return failNext("annotate takes resolution or reopen, not both",
				list("action", "annotate", "match", a.Match, "resolution", r)+" to rewrite the resolution",
				list("action", "annotate", "match", a.Match, "reopen", true)+" to reopen the entry")
		case !a.Reopen && (r == "" || strings.ContainsAny(r, "\r\n")):
			return failNext("annotate needs resolution, one line that replaces the entry's resolution, or reopen=true to remove it",
				list("action", "annotate", "match", a.Match, "resolution", "fixed: ..."),
				list("action", "annotate", "match", a.Match, "reopen", true))
		}
	default:
		return failNext(fmt.Sprintf("unknown action %q; one of %s", a.Action, strings.Join(LogActions, ", ")), list())
	}
	d, _ := lookupDoc(v, docID)
	if d == nil {
		return failNext("unknown doc_id "+strconv.Quote(docID), Call("find", "query", docID)+" to get the log note's doc_id")
	}
	if d.Sensitivity == gate.Restricted {
		return failNext(d.DocID+" is restricted; log_entries does not read or write restricted notes",
			Call("read_doc", "doc_id", d.DocID, "include_restricted", true)+" from the main session, only if the user needs it")
	}
	head, body, found, ferr := v.splitLog(d)
	if ferr != nil {
		return ferr
	}
	if !found {
		return failNext(d.DocID+" has no frontmatter block", Call("read_doc", "doc_id", d.DocID))
	}
	if a.Action != "list" {
		return editEntry(v, d, a, head)
	}
	matched := []*jsonx.Obj{}
	for _, e := range sections.Entries(body, head+1) {
		if (a.OpenOnly && e.Closed) || (a.Contains != "" && !containsFold(e.Text, a.Contains)) {
			continue
		}
		matched = append(matched, entryObj(e))
	}
	count := len(matched)
	if a.Limit > 0 && count > a.Limit {
		matched = matched[:a.Limit]
	}
	out := jsonx.New("ok", true, "vault", v.Name, "doc_id", d.DocID, "count", count, "entries", matched)
	if m != nil {
		cutEach(out, "entries", m)
	}
	if count == 0 && a.Contains != "" {
		steer(out, list()+" without contains to see every open entry")
	}
	return out
}

// splitLog reads d from disk and splits it as the write path does: the
// number of lines before the body, where entries are numbered from, and the
// body. found is false when the file has no frontmatter block.
func (v *Vault) splitLog(d *vault.Doc) (head int, body string, found bool, failure *jsonx.Obj) {
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return 0, "", false, jsonx.Fail("read failed: " + err.Error())
	}
	defer root.Close()
	note, err := root.ReadNote(d.RelativePath)
	if err != nil {
		return 0, "", false, jsonx.Fail("read failed: " + err.Error())
	}
	cut := note.Offset
	return strings.Count(note.Text[:cut], "\n"), note.Text[cut:], cut > 0, nil
}

// editEntry edits the one entry a.Match names, through the validated atomic
// write section edits use: close appends the resolution to an open entry,
// annotate replaces a closed entry's resolution or, with Reopen, removes it.
func editEntry(v *Vault, d *vault.Doc, a LogArgs, head int) *jsonx.Obj {
	if v.Profile == nil {
		return failNext("vault "+strconv.Quote(v.Name)+" has no schema profile", noProfile)
	}
	list := func(args ...any) string { return Call("log_entries", append([]any{"doc_id", d.DocID}, args...)...) }
	resolution := pystr.Strip(a.Resolution)
	// close works the open entries, annotate the closed ones.
	want, other := "open", "closed"
	if a.Action == "annotate" {
		want, other = "closed", "open"
	}
	retry := []any{"action", a.Action, "match", "", "resolution", resolution}
	if a.Reopen {
		retry = []any{"action", a.Action, "match", "", "reopen", true}
	}
	var edited sections.Entry
	out := write.EditBody(v.Loader, d.RelativePath, v.Profile, func(body, _ string) (string, *jsonx.Obj) {
		var hits []sections.Entry
		otherHits := 0
		for _, e := range sections.Entries(body, head+1) {
			if !containsFold(e.Text, a.Match) {
				continue
			}
			if e.Closed != (want == "closed") {
				otherHits++
				continue
			}
			hits = append(hits, e)
		}
		switch len(hits) {
		case 1:
		case 0:
			msg := "no " + want + " entry contains " + strconv.Quote(a.Match)
			if otherHits > 0 {
				msg += fmt.Sprintf("; %d %s entries do", otherHits, other)
			}
			return "", failNext(msg, list("contains", a.Match, "open_only", false)+" to see the entries that mention it")
		default:
			candidates := []*jsonx.Obj{}
			for i, e := range hits {
				if i == maxCandidates {
					break
				}
				candidates = append(candidates, jsonx.New("line", e.Line, "text", e.Text))
			}
			retry[3] = candidates[0].Str("text")
			return "", failNext(fmt.Sprintf("%d %s entries contain %q; give match text only one of them has", len(hits), want, a.Match),
				list(retry...)+" with the text of the entry you mean").Set("candidates", candidates)
		}
		edited = hits[0]
		b := sections.NewBody(body)
		line := strings.TrimRight(b.Lines[edited.Index], " \t\r")
		text := edited.Text
		if a.Action == "annotate" {
			// The resolution is everything after the first mark, as
			// sections.Entries reads it.
			before, _, _ := strings.Cut(line, sections.ResolutionMark)
			line = strings.TrimRight(before, " \t")
			before, _, _ = strings.Cut(text, sections.ResolutionMark)
			text = strings.TrimRight(before, " \t")
		}
		if !a.Reopen {
			line += sections.ResolutionMark + resolution
			text += sections.ResolutionMark + resolution
		}
		b.Lines[edited.Index] = line
		edited.Text = text
		return b.String(), nil
	}, "action", a.Action)
	if !out.Failed() {
		return out.Set("line", edited.Line).Set("text", edited.Text)
	}
	return guide(out, func(msg string) []string {
		if v.outsideWritable(d.RelativePath) {
			return []string{list() + " to read it; notes in read-only folders are not edited here"}
		}
		if strings.Contains(msg, "set_frontmatter") {
			return []string{Call("set_frontmatter", "relative_path", d.RelativePath) + " with the fields the error names, then retry"}
		}
		return []string{list() + " to see the entries as they are now, then retry"}
	})
}
