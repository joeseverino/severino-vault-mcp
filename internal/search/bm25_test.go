package search_test

import (
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/search"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
)

// A word few sections hold outweighs one most of them hold.
func TestBM25ARareWordOutweighsACommonOne(t *testing.T) {
	var docs []*vault.Doc
	for _, id := range []string{"a", "b", "c", "d"} {
		docs = append(docs, doc("note-"+id, "Note "+id, "", gate.Internal, "## Body\n\nThe server restarts nightly.\n"))
	}
	rare := doc("note-rare", "Rare", "", gate.Internal, "## Body\n\nThe zebrafish tank.\n")
	docs = append(docs, rare)
	hits := search.Rank(docs, "server zebrafish", 10)
	if len(hits) != 5 || hits[0].Doc != rare {
		t.Fatalf("want the rare word's doc first of five, got %v", hits)
	}
}

// A doc is one hit, at the section that scored best.
func TestBM25RanksADocOnceAtItsBestSection(t *testing.T) {
	d := doc("rb-restore", "Restore", "", gate.Internal,
		"## Overview\n\nBackups run nightly.\n\n## Restore a snapshot\n\nPick the snapshot, then restore it.\n")
	hits := search.Rank([]*vault.Doc{d}, "restore snapshot", 10)
	if len(hits) != 1 || hits[0].Section == nil || hits[0].Section.Heading != "Restore a snapshot" {
		t.Fatalf("got %+v", hits)
	}
}

// A restricted doc ranks on its title and metadata alone.
func TestBM25NeverReadsARestrictedBody(t *testing.T) {
	d := doc("rb-ca", "Certificate Authority", "pki", gate.Restricted,
		"## Root key\n\nThe zebrafish passphrase lives offline.\n")
	if hits := search.Rank([]*vault.Doc{d}, "zebrafish passphrase", 5); len(hits) != 0 {
		t.Fatalf("restricted body ranked: %v", hits)
	}
	hits := search.Rank([]*vault.Doc{d}, "certificate authority", 5)
	if len(hits) != 1 || hits[0].Section != nil {
		t.Fatalf("want the doc on its title with no section, got %+v", hits)
	}
}
