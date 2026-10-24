// Package daily writes the generated brief region of a daily note. Daily notes are a capture surface outside the index.
package daily

import (
	"errors"
	"fmt"
	"io/fs"
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
			return jsonx.Fail(fmt.Sprintf("invalid date %q (want YYYY-MM-DD)", noteDate))
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
		return jsonx.Fail("daily note write failed: " + err.Error())
	}
	defer v.Close()
	writeFailed := func(err error) *jsonx.Obj { return jsonx.Fail("daily note write failed: " + err.Error()) }
	// A note that appears between the failed update and the create is
	// updated on the second pass.
	for range 2 {
		inserted := false
		wrote, err := v.Update(path, func(text string) string {
			next, ins := UpsertRegion(text, RegionID, content)
			inserted = ins
			return next
		})
		if err == nil {
			return jsonx.New("ok", true, "wrote", relative, "changed", wrote, "created", false, "inserted", wrote && inserted)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return writeFailed(err)
		}
		text := frontmatter.Serialize(jsonx.New(
			"doc_id", "daily-"+day.Format("20060102"),
			"created", clock.Now().Format("2006-01-02 15:04:05"),
			"date", day.Format("2006-01-02"),
		))
		newText, inserted := UpsertRegion(text, RegionID, content)
		if err := v.MkdirAll(filepath.Dir(path)); err != nil {
			return writeFailed(err)
		}
		err = v.AtomicCreate(path, newText)
		if err == nil {
			return jsonx.New("ok", true, "wrote", relative, "changed", true, "created", true, "inserted", inserted)
		}
		if !errors.Is(err, fsx.ErrExists) {
			return writeFailed(err)
		}
	}
	return writeFailed(fsx.ErrChanged)
}
