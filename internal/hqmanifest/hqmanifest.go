// Package hqmanifest builds the Severino HQ docs manifest from a vault.
// HQ will own this; it lives here until HQ has a replacement.
package hqmanifest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

var hqKeys = map[string]bool{
	"doc_id": true, "title": true, "doc_type": true, "system": true, "environment": true, "status": true,
	"sensitivity": true, "obsidian_path": true, "github_path": true, "external_url": true, "last_reviewed": true,
	"notes": true, "related_projects": true, "related_assets": true, "published_at": true, "content_type": true,
	"tags": true, "slug": true, "description": true, "excerpt": true, "published": true, "technologies": true, "topic": true,
}

// The slim content dirs, outside the index.
const (
	WriteupDir = "05 Writeups"
	PageDir    = "06 Pages"
)

func get(fm *jsonx.Obj, k string) any {
	v, _ := fm.Get(k)
	return v
}

func firstTruthy(vals ...any) any {
	for _, v := range vals {
		if pystr.Truthy(v) {
			return v
		}
	}
	return vals[len(vals)-1]
}

func slimEntry(fm *jsonx.Obj, relativePath, kind string) *jsonx.Obj {
	_, rest, _ := strings.Cut(relativePath, "/")
	slug, _, _ := strings.Cut(rest, "/")
	published := pystr.Truthy(get(fm, "published"))
	var docID any
	var contentType, externalURL string
	if kind == "writeup" {
		docID = firstTruthy(get(fm, "doc_id"), "writeup-"+slug)
		contentType = "portfolio_article"
		externalURL = "https://jseverino.com/portfolio/" + slug + "/"
	} else {
		docID = firstTruthy(get(fm, "doc_id"), "page-"+slug)
		contentType = "page"
		pagePath := firstTruthy(get(fm, "path"), "/"+slug+"/")
		externalURL = "https://jseverino.com" + pystr.Str(pagePath)
	}
	status, sensitivity := "draft", "internal"
	if published {
		status, sensitivity = "active", "public"
	}
	entry := jsonx.New(
		"doc_id", docID,
		"title", firstTruthy(get(fm, "title"), slug),
		"doc_type", "public_article_draft",
		"system", "jseverino.com",
		"environment", "cloudflare",
		"status", status,
		"sensitivity", sensitivity,
		"content_type", contentType,
		"slug", slug,
		"published", published,
	)
	if pystr.Truthy(get(fm, "description")) || pystr.Truthy(get(fm, "excerpt")) {
		entry.Set("topic", firstTruthy(get(fm, "description"), get(fm, "excerpt")))
	}
	if pystr.Truthy(get(fm, "tags")) || pystr.Truthy(get(fm, "technologies")) {
		entry.Set("tags", firstTruthy(get(fm, "tags"), get(fm, "technologies")))
	}
	if published {
		entry.Set("external_url", externalURL)
	}
	for _, k := range []string{"published_at", "last_reviewed", "related_projects", "related_assets"} {
		if pystr.Truthy(get(fm, k)) {
			entry.Set(k, get(fm, k))
		}
	}
	return entry
}

// DefaultDirs is the index's dirs plus the slim content dirs.
func DefaultDirs(indexed []string) []string {
	out := slices.Clone(indexed)
	for _, d := range []string{WriteupDir, PageDir} {
		if !slices.Contains(indexed, d) {
			out = append(out, d)
		}
	}
	return out
}

// Build returns the manifest plus warnings, failing on duplicate doc_ids.
func Build(vaultPath string, subdirs []string) *jsonx.Obj {
	if info, err := os.Stat(vaultPath); err != nil || !info.IsDir() {
		return jsonx.New("ok", false, "error", "vault root not found: "+vaultPath)
	}
	entries := []*jsonx.Obj{}
	seen := map[string]string{}
	missingFM, missingDirs := []string{}, []string{}
	duplicates := []*jsonx.Obj{}
	var present []string
	for _, sub := range subdirs {
		if info, err := os.Stat(filepath.Join(vaultPath, sub)); err == nil && info.IsDir() {
			present = append(present, sub)
		} else {
			missingDirs = append(missingDirs, sub)
		}
	}
	for _, path := range vault.MarkdownFiles(vaultPath, present) {
		fm, _ := frontmatter.Read(path)
		rel, _ := filepath.Rel(vaultPath, path)
		if fm == nil {
			missingFM = append(missingFM, rel)
			continue
		}
		top, _, _ := strings.Cut(rel, "/")
		var entry *jsonx.Obj
		switch {
		case top == WriteupDir && filepath.Base(path) == "index.md":
			entry = slimEntry(fm, rel, "writeup")
		case top == PageDir && filepath.Base(path) == "index.md":
			entry = slimEntry(fm, rel, "page")
		case !pystr.Truthy(get(fm, "doc_id")):
			missingFM = append(missingFM, rel)
			continue
		default:
			entry = jsonx.New()
			for _, k := range fm.Keys() {
				if hqKeys[k] {
					entry.Set(k, get(fm, k))
				}
			}
		}
		entry.Set("path", rel)
		id := pystr.Str(get(entry, "doc_id"))
		if first, ok := seen[id]; ok {
			duplicates = append(duplicates, jsonx.New("doc_id", id, "first", first, "second", rel))
		} else {
			seen[id] = rel
		}
		entries = append(entries, entry)
	}
	if len(duplicates) > 0 {
		return jsonx.New("ok", false, "error", "duplicate doc_id values prevent manifest generation",
			"duplicates", duplicates, "missing_frontmatter", missingFM, "missing_dirs", missingDirs)
	}
	return jsonx.New("ok", true, "entries", entries, "count", len(entries),
		"missing_frontmatter", missingFM, "missing_dirs", missingDirs)
}
