package search_test

import (
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/search"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

func doc(id, title, system string, sens gate.Sensitivity, body string) *vault.Doc {
	return &vault.Doc{DocID: id, Title: title, System: system, Status: "active", Sensitivity: sens,
		Body: body, Sections: sections.Parse(body, 1)}
}

// A hub doc names the subject in its title, system and id; a specific doc
// under the same system answers the question in one section.
func TestASpecificSectionOutranksAHubThatNamesTheSubject(t *testing.T) {
	hub := doc("infra-example-hub", "Example Hub", "example-hub", gate.Internal,
		"# Example Hub\n\n## Overview\n\nExample Hub runs the services and their environment.\n\n"+
			"## Routine operations\n\nStart and stop services with the CLI.\n")
	specific := doc("note-dev-server", "Dev Server Design", "example-hub", gate.Internal,
		"# Dev Server Design\n\n## Background\n\nWhy the server exists.\n\n"+
			"## Start the dev environment\n\nRun the dev server for Example Hub locally: start it, open the dev URL.\n")
	hits := search.Rank([]*vault.Doc{hub, specific}, "start Example Hub dev environment hub dev", 5)
	if len(hits) != 2 || hits[0].Doc != specific {
		t.Fatalf("want the specific doc first, got %v", hits)
	}
}

func TestRestrictedBodiesNeverFeedRankingOrMenus(t *testing.T) {
	d := doc("rb-ca", "Certificate Authority", "pki", gate.Restricted,
		"## Root key\n\nThe zebrafish passphrase lives offline.\n")
	if hits := search.Rank([]*vault.Doc{d}, "zebrafish passphrase", 5); len(hits) != 0 {
		t.Fatalf("restricted body ranked: %v", hits)
	}
	if sec, _ := search.BestSection(d, "zebrafish"); sec != nil {
		t.Fatalf("restricted section offered: %+v", sec)
	}
}
