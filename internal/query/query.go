// Package query holds the read services the MCP tools and the CLI share:
// the slim hit projection, the ranked section menu, section reads behind the
// sensitivity gate, recent commits, and full-text body search.
package query

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/search"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

const maxLimit = 25

// Hit is the slim projection of a doc; never the body.
func Hit(d *vault.Doc) *jsonx.Obj {
	return jsonx.New(
		"doc_id", d.DocID,
		"title", d.Title,
		"doc_type", d.DocType,
		"system", d.System,
		"environment", d.Environment,
		"status", d.Status,
		"sensitivity", string(d.Sensitivity),
		"obsidian_path", d.RelativePath,
		"tags", slices.Clone(d.Tags),
		"last_reviewed", d.LastReviewedValue(),
	)
}

// SectionMenu is the hit's best-matching section line; empty without sections.
func SectionMenu(d *vault.Doc, query string) *jsonx.Obj {
	sec, score := search.BestSection(d, query)
	if sec == nil {
		return jsonx.New()
	}
	heading := sec.Heading
	if heading == "" {
		heading = d.Title
	}
	return jsonx.New(
		"heading", heading,
		"section", sec.Slug,
		"heading_path", sec.HeadingPath,
		"section_summary", sections.Summary(*sec),
		"section_score", score,
	)
}

// Clamp bounds n to [lo, hi].
func Clamp(n, lo, hi int) int { return max(lo, min(n, hi)) }

// FindSections is the ranked section menu over the vault.
func FindSections(l *vault.Loader, query string, limit int) *jsonx.Obj {
	idx := l.Index(false)
	hits := []*jsonx.Obj{}
	for _, h := range search.Rank(idx.Docs, query, Clamp(limit, 1, maxLimit)) {
		o := jsonx.New("score", h.Score).Merge(Hit(h.Doc)).Merge(SectionMenu(h.Doc, query))
		hits = append(hits, o)
	}
	return jsonx.New("query", query, "indexed_doc_count", len(idx.Docs), "hits", hits)
}

// AvailableSections lists a doc's section slugs.
func AvailableSections(d *vault.Doc) []*jsonx.Obj {
	out := []*jsonx.Obj{}
	for _, s := range d.Sections {
		out = append(out, jsonx.New("section", s.Slug, "heading_path", s.HeadingPath))
	}
	return out
}

// ReadSection reads one section (or the whole body) by doc_id. Restricted
// bodies are withheld with no interactive unlock.
func ReadSection(l *vault.Loader, docID, section string) *jsonx.Obj {
	idx := l.Index(false)
	d, ok := idx.ByDocID[docID]
	if !ok {
		if dups, ok := idx.DuplicateDocIDs[docID]; ok {
			return jsonx.New("ok", false, "doc_id", docID, "found", false, "ambiguous", true,
				"error", "duplicate doc_id "+pystr.Repr(docID), "paths", slices.Clone(dups))
		}
		return jsonx.New("ok", false, "doc_id", docID, "found", false,
			"error", "no indexed doc with doc_id "+pystr.Repr(docID))
	}
	base := jsonx.New("ok", true, "doc_id", d.DocID, "found", true).Merge(Hit(d))
	if !gate.Releasable(d.Sensitivity) {
		base.Set("body_released", false)
		base.Set("advisory", gate.Advisory(d.Sensitivity, false))
		return base
	}
	if section != "" {
		sec := sections.Resolve(d.Sections, section)
		if sec == nil {
			base.Set("ok", false)
			base.Set("body_released", false)
			base.Set("section_error", fmt.Sprintf("no section %s in %s", pystr.Repr(section), d.DocID))
			base.Set("available_sections", AvailableSections(d))
			return base
		}
		heading := sec.Heading
		if heading == "" {
			heading = d.Title
		}
		base.Set("body", sec.Body)
		base.Set("heading", heading)
		base.Set("section", sec.Slug)
		base.Set("heading_path", sec.HeadingPath)
		base.Set("body_scope", "section")
	} else {
		base.Set("body", d.Body)
		base.Set("body_scope", "doc")
	}
	base.Set("body_released", true)
	if d.Sensitivity == gate.Sensitive {
		base.Set("advisory", gate.Advisory(d.Sensitivity, false))
	}
	return base
}

// RecentChanges lists commits touching the indexed and read-only dirs (metadata only).
func RecentChanges(l *vault.Loader, days, limit int) *jsonx.Obj {
	days = Clamp(days, 1, 365)
	limit = Clamp(limit, 1, 500)
	args := []string{"log", fmt.Sprintf("--since=%d.days.ago", days), fmt.Sprintf("-n%d", limit),
		"--pretty=format:%H|%cI|%s", "--"}
	args = append(args, l.Config.SearchDirs()...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // fixed program; arguments follow "--" or are numeric
	cmd.Dir = l.Config.VaultPath
	cmd.WaitDelay = time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if _, isExit := errors.AsType[*exec.ExitError](err); !isExit {
			return jsonx.New("error", "git log failed: "+err.Error())
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "git log failed"
		}
		return jsonx.New("error", msg)
	}
	commits := []*jsonx.Obj{}
	for _, line := range pystr.SplitLines(string(out)) {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) == 3 {
			commits = append(commits, jsonx.New("sha", parts[0], "committed_at", parts[1], "subject", parts[2]))
		}
	}
	return jsonx.New("days", days, "commit_count", len(commits), "commits", commits)
}

func emptySearch(query string) *jsonx.Obj {
	return jsonx.New("query", query, "doc_count", 0, "total_match_count", 0,
		"excluded", jsonx.New("restricted_skipped", 0, "secret_adjacent_skipped", 0, "unindexed_skipped", 0),
		"hits_by_doc", []*jsonx.Obj{})
}

type snippet struct {
	line int
	kind string
	text string
}

// regexMeta are the characters that make a query a regex rather than words.
const regexMeta = `.*+?()[]{}|^$\`

// QueryWords splits a natural-language query into its distinct non-filler
// words; nil for a single token or a regex, which ripgrep matches as given.
func QueryWords(query string, caseSensitive bool) []string {
	if !strings.ContainsFunc(query, pystr.IsSpace) || strings.ContainsAny(query, regexMeta) {
		return nil
	}
	var words []string
	for _, w := range strings.FieldsFunc(query, pystr.IsSpace) {
		if !caseSensitive {
			w = strings.ToLower(w)
		}
		if search.IsStopword(strings.ToLower(w)) || slices.Contains(words, w) {
			continue
		}
		words = append(words, w)
	}
	return words
}

// SearchBody searches indexed doc bodies: a multi-word query matches docs
// whose body holds every word, anything else is a ripgrep pattern. Matches
// inside frontmatter are dropped and restricted bodies are never searched.
func SearchBody(l *vault.Loader, query string, limit, contextLines int, caseSensitive bool) *jsonx.Obj {
	query = pystr.Strip(query)
	if query == "" {
		return emptySearch(query)
	}
	if words := QueryWords(query, caseSensitive); len(words) > 0 {
		return searchWords(l, query, words, limit, caseSensitive)
	}
	rg, err := exec.LookPath("rg")
	if err != nil {
		return jsonx.New("error", "ripgrep (`rg`) not found on PATH. Install via `brew install ripgrep` or skip this tool.")
	}
	idx := l.Index(false)
	root := fsx.Resolve(l.Config.VaultPath)
	var roots []string
	for _, sub := range l.Config.SearchDirs() {
		r := filepath.Join(root, sub)
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			roots = append(roots, r)
		}
	}
	if len(roots) == 0 {
		return emptySearch(query)
	}
	args := []string{"--json", "--type", "md", "--max-count", "10",
		fmt.Sprintf("--context=%d", Clamp(contextLines, 0, 5)), "--no-ignore-vcs"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	args = append(args, query)
	args = append(args, roots...)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rg, args...) //nolint:gosec // rg resolved by LookPath; the query is a pattern argument
	cmd.WaitDelay = time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if runErr != nil {
		exit, isExit := errors.AsType[*exec.ExitError](runErr)
		if !isExit {
			return jsonx.New("error", "ripgrep failed: "+runErr.Error())
		}
		if exit.ExitCode() > 1 {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = "ripgrep returncode=" + strconv.Itoa(exit.ExitCode())
			}
			return jsonx.New("error", msg)
		}
	}

	var order []string
	matches := map[string][]snippet{}
	current := ""
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var evt struct {
			Type string `json:"type"`
			Data struct {
				Path       *struct{ Text string } `json:"path"`
				LineNumber *int                   `json:"line_number"`
				Lines      *struct{ Text string } `json:"lines"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}
		switch evt.Type {
		case "begin":
			current = ""
			if evt.Data.Path != nil {
				current = evt.Data.Path.Text
			}
			if current != "" {
				if _, ok := matches[current]; !ok {
					order = append(order, current)
					matches[current] = []snippet{}
				}
			}
		case "match", "context":
			if current == "" || evt.Data.LineNumber == nil {
				continue
			}
			text := ""
			if evt.Data.Lines != nil {
				text = strings.TrimRight(evt.Data.Lines.Text, "\n")
			}
			matches[current] = append(matches[current], snippet{*evt.Data.LineNumber, evt.Type, text})
		}
	}

	byPath := map[string]*vault.Doc{}
	for _, d := range idx.Docs {
		byPath[fsx.Resolve(d.Path)] = d
	}
	type docHits struct {
		hit   *jsonx.Obj
		count int
		lr    string
		title string
	}
	var hits []docHits
	restricted, unindexed := 0, 0
	for _, p := range order {
		d, ok := byPath[p]
		if !ok {
			unindexed++
			continue
		}
		if d.Sensitivity == gate.Restricted {
			restricted++
			continue
		}
		var inBody []*jsonx.Obj
		count := 0
		for _, s := range matches[p] {
			if s.line < d.BodyStartLine {
				continue
			}
			inBody = append(inBody, jsonx.New("line_number", s.line, "kind", s.kind, "text", s.text))
			if s.kind == "match" {
				count++
			}
		}
		if len(inBody) == 0 || count == 0 {
			continue
		}
		hit := Hit(d).Set("match_count", count).Set("snippets", inBody)
		if d.Sensitivity == gate.Sensitive {
			hit.Set("advisory", gate.Advisory(d.Sensitivity, false))
		}
		hits = append(hits, docHits{hit, count, d.LastReviewed, d.Title})
	}
	slices.SortStableFunc(hits, func(a, b docHits) int {
		if a.count != b.count {
			return b.count - a.count
		}
		if c := strings.Compare(b.lr, a.lr); c != 0 {
			return c
		}
		return strings.Compare(b.title, a.title)
	})
	capped := hits[:min(len(hits), Clamp(limit, 1, 50))]
	out2 := []*jsonx.Obj{}
	total := 0
	for _, h := range capped {
		out2 = append(out2, h.hit)
		total += h.count
	}
	return jsonx.New(
		"query", query,
		"doc_count", len(out2),
		"total_match_count", total,
		"excluded", jsonx.New("restricted_skipped", restricted, "secret_adjacent_skipped", restricted, "unindexed_skipped", unindexed),
		"hits_by_doc", out2,
	)
}

const maxWordSnippets = 5

// searchWords ranks the docs whose body holds every word, in any order. A
// doc's score is its best section's: the words that section covers, then
// their occurrences per thousand words, so a short section about the query
// beats a long doc that mentions each word once.
func searchWords(l *vault.Loader, query string, words []string, limit int, caseSensitive bool) *jsonx.Obj {
	fold := func(s string) string {
		if caseSensitive {
			return s
		}
		return strings.ToLower(s)
	}
	type docHits struct {
		hit          *jsonx.Obj
		score, count int
		lr, title    string
	}
	var hits []docHits
	restricted := 0
	for _, d := range l.Index(false).Docs {
		if !search.Searchable(d) {
			restricted++
			continue
		}
		body := fold(d.Body)
		count := 0
		for _, w := range words {
			n := strings.Count(body, w)
			if n == 0 {
				count = 0
				break
			}
			count += n
		}
		if count == 0 {
			continue
		}
		hit := Hit(d).Set("match_count", count)
		score := 0
		if sec, sc := bestWordSection(d.Sections, words, fold); sec != nil {
			score = sc
			heading := sec.Heading
			if heading == "" {
				heading = d.Title
			}
			hit.Set("heading", heading).Set("section", sec.Slug).Set("heading_path", sec.HeadingPath).
				Set("snippets", wordSnippets(sec, words, fold))
		}
		if d.Sensitivity == gate.Sensitive {
			hit.Set("advisory", gate.Advisory(d.Sensitivity, false))
		}
		hits = append(hits, docHits{jsonx.New("score", score).Merge(hit), score, count, d.LastReviewed, d.Title})
	}
	slices.SortStableFunc(hits, func(a, b docHits) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if a.count != b.count {
			return b.count - a.count
		}
		if c := strings.Compare(b.lr, a.lr); c != 0 {
			return c
		}
		return strings.Compare(b.title, a.title)
	})
	out := []*jsonx.Obj{}
	total := 0
	for _, h := range hits[:min(len(hits), Clamp(limit, 1, 50))] {
		out = append(out, h.hit)
		total += h.count
	}
	return jsonx.New(
		"query", query,
		"words", slices.Clone(words),
		"doc_count", len(out),
		"total_match_count", total,
		"excluded", jsonx.New("restricted_skipped", restricted, "secret_adjacent_skipped", restricted, "unindexed_skipped", 0),
		"hits_by_doc", out,
	)
}

func bestWordSection(secs []sections.Section, words []string, fold func(string) string) (*sections.Section, int) {
	var best *sections.Section
	bestScore := 0
	for i := range secs {
		body := fold(secs[i].Body)
		covered, occ := 0, 0
		for _, w := range words {
			if n := strings.Count(body, w); n > 0 {
				covered++
				occ += n
			}
		}
		if covered == 0 {
			continue
		}
		density := min(999, occ*1000/max(1, len(strings.FieldsFunc(body, pystr.IsSpace))))
		if sc := covered*1000 + density; sc > bestScore {
			best, bestScore = &secs[i], sc
		}
	}
	return best, bestScore
}

func wordSnippets(sec *sections.Section, words []string, fold func(string) string) []*jsonx.Obj {
	out := []*jsonx.Obj{}
	for i, line := range strings.Split(sec.Body, "\n") {
		folded := fold(line)
		if !slices.ContainsFunc(words, func(w string) bool { return strings.Contains(folded, w) }) {
			continue
		}
		out = append(out, jsonx.New("line_number", sec.StartLine+i, "kind", "match", "text", line))
		if len(out) == maxWordSnippets {
			break
		}
	}
	return out
}
