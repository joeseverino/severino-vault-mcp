// Package daily writes the generated brief region of a daily note. Daily notes are a capture surface outside the index.
package daily

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// RegionID names the generated brief region.
const RegionID = "daily-brief"

// Markers are a region's begin and end comments.
func Markers(regionID string) (string, string) {
	return fmt.Sprintf("<!-- MIRROR:BEGIN %s (generated — do not edit) -->", regionID),
		fmt.Sprintf("<!-- MIRROR:END %s -->", regionID)
}

func replaceRegion(text, begin, end, content string) (string, bool) {
	bi, ei := strings.Index(text, begin), strings.Index(text, end)
	if bi == -1 || ei == -1 || ei < bi {
		return "", false
	}
	return text[:bi+len(begin)] + "\n" + content + "\n" + text[ei:], true
}

func frontmatterEnd(text string) int {
	if !strings.HasPrefix(pystr.LStrip(text), "---") {
		return 0
	}
	seenOpen := false
	pos := 0
	for pos < len(text) {
		next := strings.IndexByte(text[pos:], '\n')
		line, end := text[pos:], len(text)
		if next >= 0 {
			line, end = text[pos:pos+next], pos+next+1
		}
		if pystr.Strip(line) == "---" {
			if seenOpen {
				return end
			}
			seenOpen = true
		}
		pos = end
	}
	return 0
}

// UpsertRegion replaces a region, or inserts it after the frontmatter (else
// at the top) on first run. Text outside the region is preserved.
func UpsertRegion(text, regionID, content string) (string, bool) {
	content = strings.TrimRight(content, "\n")
	begin, end := Markers(regionID)
	if replaced, ok := replaceRegion(text, begin, end, content); ok {
		return replaced, false
	}
	block := begin + "\n" + content + "\n" + end + "\n"
	cut := frontmatterEnd(text)
	if cut == 0 {
		if text == "" {
			return block, true
		}
		return block + "\n" + strings.TrimLeft(text, "\n"), true
	}
	head, rest := text[:cut], strings.TrimLeft(text[cut:], "\n")
	if rest != "" {
		return head + "\n" + block + "\n" + rest, true
	}
	return head + "\n" + block, true
}

// Write replaces the brief region of a daily note, creating the note from
// the daily-note contract when absent.
func Write(cfg config.Config, content, noteDate string) *jsonx.Obj {
	day := clock.Today()
	if noteDate != "" {
		t, err := clock.ParseDate(noteDate)
		if err != nil {
			return fsx.Fail(fmt.Sprintf("invalid date %s (want YYYY-MM-DD)", pystr.Repr(noteDate)))
		}
		day = t
	}
	relative := cfg.DailyNotesDir + "/" + day.Format("2006-01-02") + ".md"
	path := filepath.Join(cfg.VaultPath, relative)
	if errObj := fsx.PathWithinRoot(cfg.VaultPath, path, "daily note", "any"); errObj != nil {
		return errObj
	}
	v, err := fsx.OpenVault(cfg.VaultPath)
	if err != nil {
		return fsx.Fail("daily note write failed: " + err.Error())
	}
	defer v.Close()
	info, err := v.Stat(path)
	created := err != nil || !info.Mode().IsRegular()
	var text string
	if created {
		text = frontmatter.Serialize(jsonx.New(
			"doc_id", "daily-"+day.Format("20060102"),
			"created", clock.Now().Format("2006-01-02 15:04:05"),
			"date", day.Format("2006-01-02"),
		))
	} else {
		text, _ = v.ReadText(path)
	}
	newText, inserted := UpsertRegion(text, RegionID, content)
	if newText == text {
		return jsonx.New("ok", true, "wrote", relative, "changed", false, "created", false, "inserted", false)
	}
	if err := v.MkdirAll(filepath.Dir(path)); err != nil {
		return fsx.Fail("daily note write failed: " + err.Error())
	}
	if err := v.AtomicWrite(path, newText); err != nil {
		return fsx.Fail("daily note write failed: " + err.Error())
	}
	return jsonx.New("ok", true, "wrote", relative, "changed", true, "created", created, "inserted", inserted)
}
