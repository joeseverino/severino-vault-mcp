package core

import (
	"fmt"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// A log note is append-only: each top-level bullet in its body is one entry,
// and an entry is closed once its line carries the resolution mark followed
// by text.

// ResolutionMark separates an entry from its resolution.
const ResolutionMark = " → "

// LogActions are log_entries' actions; the first is the default.
var LogActions = []string{"list", "close"}

// LogArgs are log_entries' arguments. List reads OpenOnly, Contains and
// Limit; close reads Match and Resolution.
type LogArgs struct {
	Action     string
	OpenOnly   bool
	Contains   string
	Limit      int
	Match      string
	Resolution string
	View
}

// EntryFields are the fields a log entry carries: its line in the file, its
// text without the bullet, and its resolution when closed.
var EntryFields = typed("line", tInt, "text", tStr, "closed", tBool, "resolution", tOptStr)

var leanEntry = mustMask(EntryFields, "line", "text")

// maxCandidates caps the entries an ambiguous close lists.
const maxCandidates = 10

type logEntry struct {
	index      int // line index in the body
	line       int // 1-based line in the file
	text       string
	resolution string
	closed     bool
}

func (e logEntry) obj() *jsonx.Obj {
	var resolution any
	if e.closed {
		resolution = e.resolution
	}
	return jsonx.New("line", e.line, "text", e.text, "closed", e.closed, "resolution", resolution)
}

// parseEntries reads a log body whose first line is line first of the file.
func parseEntries(body string, first int) []logEntry {
	var out []logEntry
	for i, raw := range sections.Lines(body) {
		rest, ok := strings.CutPrefix(raw, "- ")
		if !ok {
			rest, ok = strings.CutPrefix(raw, "* ")
		}
		if !ok {
			continue
		}
		text := strings.TrimRight(rest, " \t\r")
		e := logEntry{index: i, line: first + i, text: text}
		if _, after, found := strings.Cut(text, ResolutionMark); found && pystr.Strip(after) != "" {
			e.closed, e.resolution = true, pystr.Strip(after)
		}
		out = append(out, e)
	}
	return out
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// LogEntries lists a log note's entries, or closes one with its resolution.
func LogEntries(v *Vault, docID string, a LogArgs) *jsonx.Obj {
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
		if r := pystr.Strip(a.Resolution); r == "" || strings.ContainsAny(r, "\r\n") {
			return failNext("close needs resolution: one line saying how the entry was resolved",
				list("action", "close", "match", a.Match, "resolution", "fixed: ..."))
		}
	default:
		return failNext(fmt.Sprintf("unknown action %s; one of %s", pystr.Repr(a.Action), strings.Join(LogActions, ", ")), list())
	}
	d, _ := lookupDoc(v, docID)
	if d == nil {
		return failNext("unknown doc_id "+pystr.Repr(docID), Call("find", "query", docID)+" to get the log note's doc_id")
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
	if a.Action == "close" {
		return closeEntry(v, d, a, head)
	}
	matched := []*jsonx.Obj{}
	for _, e := range parseEntries(body, head+1) {
		if (a.OpenOnly && e.closed) || (a.Contains != "" && !containsFold(e.text, a.Contains)) {
			continue
		}
		matched = append(matched, e.obj())
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

// closeEntry appends the resolution to the one open entry a.Match names,
// through the validated atomic write section edits use.
func closeEntry(v *Vault, d *vault.Doc, a LogArgs, head int) *jsonx.Obj {
	if v.Profile == nil {
		return failNext("vault "+pystr.Repr(v.Name)+" has no schema profile", noProfile)
	}
	list := func(args ...any) string { return Call("log_entries", append([]any{"doc_id", d.DocID}, args...)...) }
	resolution := pystr.Strip(a.Resolution)
	var closed logEntry
	out := write.EditBody(v.Loader, d.RelativePath, v.Profile, func(body, _ string) (string, *jsonx.Obj) {
		var hits []logEntry
		closedHits := 0
		for _, e := range parseEntries(body, head+1) {
			if !containsFold(e.text, a.Match) {
				continue
			}
			if e.closed {
				closedHits++
				continue
			}
			hits = append(hits, e)
		}
		switch len(hits) {
		case 1:
		case 0:
			msg := "no open entry contains " + pystr.Repr(a.Match)
			if closedHits > 0 {
				msg += fmt.Sprintf("; %d closed entries do", closedHits)
			}
			return "", failNext(msg, list("contains", a.Match, "open_only", false)+" to see the entries that mention it")
		default:
			candidates := []*jsonx.Obj{}
			for i, e := range hits {
				if i == maxCandidates {
					break
				}
				candidates = append(candidates, jsonx.New("line", e.line, "text", e.text))
			}
			return "", failNext(fmt.Sprintf("%d open entries contain %s; give match text only one of them has", len(hits), pystr.Repr(a.Match)),
				list("action", "close", "match", candidates[0].Str("text"), "resolution", resolution)+
					" with the text of the entry you mean").Set("candidates", candidates)
		}
		closed = hits[0]
		b := sections.NewBody(body)
		b.Lines[closed.index] = strings.TrimRight(b.Lines[closed.index], " \t\r") + ResolutionMark + resolution
		closed.text += ResolutionMark + resolution
		return b.String(), nil
	}, "action", "close")
	if !out.Failed() {
		return out.Set("line", closed.line).Set("text", closed.text)
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
