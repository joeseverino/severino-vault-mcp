package core

import (
	"slices"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
)

// FindBudget is the most characters a find result spends, as the client
// reads it (Text): enough for 25 lean hits, so only snippets, wide field
// masks or the unmasked view reach it. A result is a menu to pick a read
// from, and the read is where the text belongs.
const FindBudget = 12_000

// snippetChars is how much of a matched line a trimmed snippet keeps.
const snippetChars = 200

// budget fits out within FindBudget: first each hit's snippets shrink to
// its matched lines, each cut short, then they go, then hits go from the
// bottom, keeping the best one whatever its size. What it took out is
// reported under truncated, with the calls that get the rest.
func budget(out *jsonx.Obj, q string, a FindArgs) {
	if len(Text(out)) <= FindBudget {
		return
	}
	raw, _ := out.Get("hits")
	hits, _ := raw.([]*jsonx.Obj)
	prior, _ := out.Get("next_steps")
	have, _ := prior.([]string)
	trimmed, dropped := false, 0
	// report sets truncated and the steps for what the result holds now, so
	// each size check counts them too.
	report := func() bool {
		out.Set("hits", hits)
		out.Set("truncated", jsonx.New("budget_chars", FindBudget, "snippets_trimmed", trimmed, "hits_dropped", dropped))
		out.Set("next_steps", append(slices.Clone(have), budgetSteps(hits, q, a, trimmed, dropped)...))
		return len(Text(out)) <= FindBudget
	}
	for _, stage := range []func(*jsonx.Obj) bool{matchedLinesOnly, withoutSnippets} {
		for _, h := range hits {
			if stage(h) {
				trimmed = true
			}
		}
		if report() {
			return
		}
	}
	for len(hits) > 1 {
		hits = hits[:len(hits)-1]
		dropped++
		if report() {
			return
		}
	}
}

// budgetSteps are the calls that get what budget took out of a result.
func budgetSteps(hits []*jsonx.Obj, q string, a FindArgs, trimmed bool, dropped int) []string {
	var steps []string
	if len(hits) > 0 {
		top := hits[0]
		if sec := top.Str("section"); sec != "" {
			steps = append(steps, Call("read_doc", "doc_id", top.Str("doc_id"), "sections", []string{sec})+" to read the best hit's section")
		} else if id := top.Str("doc_id"); id != "" {
			steps = append(steps, Call("read_doc", "doc_id", id)+" to read the best hit")
		}
	}
	if dropped > 0 {
		steps = append(steps, Call("find", "query", q, "by", a.By, "limit", len(hits))+
			" for the hits that fit whole, or add system, project or doc_type to narrow the search")
	}
	if trimmed {
		steps = append(steps, Call("find", "query", q, "by", a.By, "fields", []string{"doc_id", "section", "snippet"})+
			" for one matched line per hit")
	}
	return steps
}

// matchedLinesOnly keeps a hit's matched snippet lines, the first two, each
// and the hit's snippet cut to snippetChars. It reports whether it changed
// anything.
func matchedLinesOnly(h *jsonx.Obj) bool {
	changed := false
	if s := h.Str("snippet"); len(s) > snippetChars {
		h.Set("snippet", cutText(s))
		changed = true
	}
	raw, ok := h.Get("snippets")
	if !ok {
		return changed
	}
	snips, _ := raw.([]*jsonx.Obj)
	var kept []*jsonx.Obj
	for _, s := range snips {
		if s.Has("kind") && s.Str("kind") != "match" {
			continue
		}
		if len(kept) == 2 {
			break
		}
		c := s.Clone()
		if t := c.Str("text"); len(t) > snippetChars {
			c.Set("text", cutText(t))
		}
		kept = append(kept, c)
	}
	if kept == nil {
		kept = []*jsonx.Obj{}
	}
	if Text(jsonx.New("s", kept)) != Text(jsonx.New("s", snips)) {
		h.Set("snippets", kept)
		changed = true
	}
	return changed
}

// withoutSnippets drops a hit's snippet list, keeping its one snippet line.
func withoutSnippets(h *jsonx.Obj) bool {
	if !h.Has("snippets") {
		return false
	}
	h.Delete("snippets")
	return true
}

// cutText is s cut to snippetChars bytes at a rune boundary, marked with an
// ellipsis.
func cutText(s string) string {
	n := snippetChars
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
