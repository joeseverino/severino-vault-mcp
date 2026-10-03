// Package daily reads daily notes for progress questions and writes their
// generated brief region. Daily notes are a capture surface outside the index.
package daily

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

var (
	isoDateRE = regexp.MustCompile(`\b(20\d{2}-\d{2}-\d{2})\b`)
	usDateRE  = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(20\d{2})\b`)
)

// Weekday tokens in Python's dict order (the first match wins).
var weekdays = []struct {
	token string
	day   time.Weekday
}{
	{"monday", time.Monday}, {"mon", time.Monday},
	{"tuesday", time.Tuesday}, {"tue", time.Tuesday}, {"tues", time.Tuesday},
	{"wednesday", time.Wednesday}, {"wed", time.Wednesday},
	{"thursday", time.Thursday}, {"thu", time.Thursday}, {"thur", time.Thursday}, {"thurs", time.Thursday},
	{"friday", time.Friday}, {"fri", time.Friday},
	{"saturday", time.Saturday}, {"sat", time.Saturday},
	{"sunday", time.Sunday}, {"sun", time.Sunday},
}

// pyWeekday maps Go's Sunday-first weekday to Python's Monday-first one.
func pyWeekday(d time.Weekday) int { return (int(d) + 6) % 7 }

func resolveWeekday(anchor time.Time, target time.Weekday, previous bool) time.Time {
	delta := ((pyWeekday(anchor.Weekday())-pyWeekday(target))%7 + 7) % 7
	if previous && delta == 0 {
		delta = 7
	}
	return anchor.AddDate(0, 0, -delta)
}

// ResolveDate turns a day reference into a date and how it was resolved.
func ResolveDate(query, today string) (time.Time, string, error) {
	anchor := clock.Today()
	if today != "" {
		t, err := clock.ParseDate(today)
		if err != nil {
			return time.Time{}, "", err
		}
		anchor = t
	}
	q := strings.ToLower(pystr.Strip(query))
	if m := isoDateRE.FindStringSubmatch(q); m != nil {
		t, err := clock.ParseDate(m[1])
		return t, "iso_date", err
	}
	if m := usDateRE.FindStringSubmatch(q); m != nil {
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		year, _ := strconv.Atoi(m[3])
		t, err := clock.Date(year, month, day)
		return t, "us_date", err
	}
	if strings.Contains(q, "yesterday") {
		return anchor.AddDate(0, 0, -1), "yesterday", nil
	}
	if strings.Contains(q, "today") {
		return anchor, "today", nil
	}
	for _, w := range weekdays {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(w.token) + `\b`).MatchString(q) {
			prev := regexp.MustCompile(`\b(last|previous)\s+` + regexp.QuoteMeta(w.token) + `\b`).MatchString(q)
			return resolveWeekday(anchor, w.day, prev), "weekday", nil
		}
	}
	return anchor, "default_today", nil
}

func progressLines(body string) []string {
	out := []string{}
	for _, raw := range pystr.SplitLines(body) {
		line := pystr.Strip(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "- [x]") || strings.HasPrefix(line, "- [X]") || strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "*") || pystr.Len(line) > 3 {
			out = append(out, line)
		}
	}
	return out
}

// Progress reads the daily note a progress question refers to.
func Progress(cfg config.Config, query, today string) *jsonx.Obj {
	day, resolution, err := ResolveDate(query, today)
	if err != nil {
		return jsonx.New("ok", false, "error", err.Error())
	}
	iso := day.Format("2006-01-02")
	relative := cfg.DailyNotesDir + "/" + iso + ".md"
	response := jsonx.New("query", query, "resolved_date", iso, "date_resolution", resolution, "daily_notes_dir", cfg.DailyNotesDir)
	path := filepath.Join(cfg.VaultPath, relative)
	info, statErr := os.Stat(path)
	if statErr != nil || !info.Mode().IsRegular() {
		return response.Merge(jsonx.New("found", false, "expected_path", relative, "progress_items", []string{}))
	}
	text, _ := pystr.ReadText(path)
	fm, body, _ := frontmatter.Split(text)
	docID := "daily-" + day.Format("20060102")
	var created any
	if fm != nil {
		if v, _ := fm.Get("doc_id"); pystr.Truthy(v) {
			docID = pystr.Str(v)
		}
		if v, _ := fm.Get("created"); pystr.Truthy(v) {
			created = pystr.Str(v)
		}
	}
	return response.Merge(jsonx.New(
		"found", true,
		"doc_id", docID,
		"obsidian_path", relative,
		"created", created,
		"body", body,
		"body_released", true,
		"progress_items", progressLines(body),
		"answer_guidance", "Summarize progress from progress_items/body. If the body is empty, "+
			"say no progress was recorded in the daily note for this date.",
	))
}

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
	info, err := os.Stat(path)
	created := err != nil || !info.Mode().IsRegular()
	var text string
	if created {
		text = frontmatter.Serialize(jsonx.New(
			"doc_id", "daily-"+day.Format("20060102"),
			"created", clock.Now().Format("2006-01-02 15:04:05"),
			"date", day.Format("2006-01-02"),
		))
	} else {
		text, _ = pystr.ReadText(path)
	}
	newText, inserted := UpsertRegion(text, RegionID, content)
	if newText == text {
		return jsonx.New("ok", true, "wrote", relative, "changed", false, "created", false, "inserted", false)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fsx.Fail("daily note write failed: " + err.Error())
	}
	if err := fsx.WriteFile(path, newText); err != nil {
		return fsx.Fail("daily note write failed: " + err.Error())
	}
	return jsonx.New("ok", true, "wrote", relative, "changed", true, "created", created, "inserted", inserted)
}
