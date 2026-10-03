// Package education projects the edu vault's publishable facts: institutions
// (top-level folders whose index.md carries institution + slug) and their
// courses, with public bullets from each course's "## Site" section.
// jseverino.com and resume-engine read it through `export education`.
package education

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

var (
	termSeasons = map[string]int{"spring": 0, "summer": 1, "fall": 2}
	headingRE   = regexp.MustCompile(`^#{1,6} `)
)

func termKey(term string) [2]int {
	season, year, _ := strings.Cut(pystr.Strip(term), " ")
	y, err := strconv.Atoi(strings.TrimSpace(year))
	s, ok := termSeasons[strings.ToLower(season)]
	if err != nil || !ok {
		return [2]int{9999, 9}
	}
	return [2]int{y, s}
}

func siteSection(body string) string {
	lines := strings.Split(body, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return pystr.Strip(l) == "## Site" })
	if start < 0 {
		return ""
	}
	rest := lines[start+1:]
	end := slices.IndexFunc(rest, func(l string) bool { return headingRE.MatchString(l) })
	if end < 0 {
		end = len(rest)
	}
	return pystr.Strip(strings.Join(rest[:end], "\n"))
}

func extra(d *vault.Doc, key string) any {
	v, _ := d.Extra.Get(key)
	return v
}

// Dataset builds the projection. Fails closed: a course status outside
// statuses, a course missing code or term, or an institution without slug
// makes the whole export ok: false.
func Dataset(l *vault.Loader, statuses []string) *jsonx.Obj {
	docs := l.Index(false).Docs
	sorted := slices.Clone(docs)
	slices.SortStableFunc(sorted, func(a, b *vault.Doc) int { return strings.Compare(a.RelativePath, b.RelativePath) })
	errors := []string{}
	institutions := []*jsonx.Obj{}
	for _, d := range sorted {
		if d.DocType != "resource" {
			continue
		}
		folder, leaf, _ := strings.Cut(d.RelativePath, "/")
		if leaf != "index.md" || !d.Extra.Has("institution") {
			continue
		}
		if !pystr.Truthy(extra(d, "slug")) {
			errors = append(errors, d.RelativePath+": institution note missing `slug`")
			continue
		}
		type course struct {
			o    *jsonx.Obj
			key  [2]int
			code string
		}
		var courses []course
		for _, c := range docs {
			if c.DocType != "course" || !strings.HasPrefix(c.RelativePath, folder+"/") {
				continue
			}
			var missing []string
			for _, k := range []string{"code", "term"} {
				if !pystr.Truthy(extra(c, k)) {
					missing = append(missing, k)
				}
			}
			if len(missing) > 0 {
				errors = append(errors, c.RelativePath+": course missing `"+strings.Join(missing, "`, `")+"`")
				continue
			}
			if !slices.Contains(statuses, c.Status) {
				errors = append(errors, c.RelativePath+": status `"+c.Status+"` is not in the education profile")
				continue
			}
			code, term := pystr.Str(extra(c, "code")), pystr.Str(extra(c, "term"))
			courses = append(courses, course{jsonx.New(
				"doc_id", c.DocID,
				"code", code,
				"title", c.Title,
				"short_title", extra(c, "short_title"),
				"term", term,
				"status", c.Status,
				"site_bullets", siteSection(c.Body),
			), termKey(term), code})
		}
		slices.SortStableFunc(courses, func(a, b course) int {
			if a.key[0] != b.key[0] {
				return a.key[0] - b.key[0]
			}
			if a.key[1] != b.key[1] {
				return a.key[1] - b.key[1]
			}
			return strings.Compare(a.code, b.code)
		})
		list := []*jsonx.Obj{}
		for _, c := range courses {
			list = append(list, c.o)
		}
		institutions = append(institutions, jsonx.New(
			"doc_id", d.DocID,
			"institution", pystr.Str(extra(d, "institution")),
			"slug", pystr.Str(extra(d, "slug")),
			"description", extra(d, "description"),
			"courses", list,
		))
	}
	if len(errors) > 0 {
		return jsonx.New("ok", false, "errors", errors)
	}
	return jsonx.New("ok", true, "institutions", institutions)
}
