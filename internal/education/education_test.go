package education_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/education"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

const institution = `---
doc_id: res-gt
title: Georgia Institute of Technology
doc_type: resource
status: active
institution: Georgia Institute of Technology
slug: georgia-tech
description: Coursework, course by course.
---

# Georgia Institute of Technology
`

func course(id, code, term, status, site string) string {
	section := ""
	if site != "" {
		section = "\n## Site\n\n" + site + "\n\n## Scratch\n\nprivate notes\n"
	}
	return "---\ndoc_id: " + id + "\ntitle: Course " + code + "\ndoc_type: course\nstatus: " + status + "\ncode: " + code +
		"\nterm: " + term + "\n---\n\n# Course " + code + "\n\n## Notes\n" + section
}

func eduVault(t *testing.T) (string, func() *vault.Loader) {
	tmp := tk.Dir(t)
	root := filepath.Join(tmp, "vault", "Georgia Tech")
	tk.Write(t, filepath.Join(root, "index.md"), institution)
	tk.Write(t, filepath.Join(root, "Completed", "CS1", "index.md"), course("course-cs1", "CS1000", "Fall 2025", "completed", "- Learned things."))
	tk.Write(t, filepath.Join(root, "CS2", "index.md"), course("course-cs2", "CS2000", "Spring 2026", "active", "- Doing things."))
	tk.Write(t, filepath.Join(root, "Planned", "CS3", "index.md"), course("course-cs3", "CS3000", "Fall 2026", "upcoming", ""))
	cfg := filepath.Join(tmp, "config.toml")
	tk.Write(t, cfg, "[vault]\npath = \""+filepath.Join(tmp, "vault")+"\"\nindexed_dirs = [\"Georgia Tech\"]\n")
	return tmp, func() *vault.Loader { return vault.NewLoader(config.Load(cfg, config.Env{"SVMC_CACHE_SECONDS": "0"})) }
}

func TestDatasetGroupsSortsAndExtractsSiteBullets(t *testing.T) {
	_, l := eduVault(t)
	r := education.Dataset(l(), schema.Education.Statuses)
	gt := tk.Hits(r, "institutions")
	if !r.Bool("ok") || len(gt) != 1 || gt[0].Str("institution") != "Georgia Institute of Technology" || gt[0].Str("slug") != "georgia-tech" {
		t.Fatal(jsonx.Compact(r))
	}
	courses := tk.Hits(gt[0], "courses")
	var codes []string
	for _, c := range courses {
		codes = append(codes, c.Str("code"))
	}
	if !slices.Equal(codes, []string{"CS1000", "CS2000", "CS3000"}) || courses[0].Str("site_bullets") != "- Learned things." ||
		strings.Contains(courses[1].Str("site_bullets"), "private") || courses[2].Str("status") != "upcoming" || courses[2].Str("site_bullets") != "" {
		t.Fatal(jsonx.Compact(courses))
	}
}

func TestFailsClosed(t *testing.T) {
	cases := map[string]func(tmp string){
		"planned": func(tmp string) {
			tk.Write(t, filepath.Join(tmp, "vault", "Georgia Tech", "CS4", "index.md"), course("course-cs4", "CS4000", "Spring 2027", "planned", ""))
		},
		"slug": func(tmp string) {
			tk.Write(t, filepath.Join(tmp, "vault", "Georgia Tech", "index.md"), strings.Replace(institution, "slug: georgia-tech\n", "", 1))
		},
		"term": func(tmp string) {
			tk.Write(t, filepath.Join(tmp, "vault", "Georgia Tech", "CS5", "index.md"),
				strings.Replace(course("course-cs5", "CS5000", "Fall 2026", "upcoming", ""), "term: Fall 2026\n", "", 1))
		},
	}
	for want, mutate := range cases {
		tmp, l := eduVault(t)
		mutate(tmp)
		r := education.Dataset(l(), schema.Education.Statuses)
		errs, _ := r.Get("errors")
		if r.Bool("ok") || !slices.ContainsFunc(errs.([]string), func(e string) bool { return strings.Contains(e, want) }) {
			t.Errorf("%s: %s", want, jsonx.Compact(r))
		}
	}
}
