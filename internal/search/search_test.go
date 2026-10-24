package search_test

import (
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/search"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
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

func typed(d *vault.Doc, docType string, tags ...string) *vault.Doc {
	d.DocType, d.Tags = docType, tags
	return d
}

const deployGuide = "# Deploy Example Desk\n\n## Routine: landing on main is the deploy\n\nMerge, then watch the deploy job.\n"

// A doc whose title holds every word of the query outranks a hub whose tags,
// system and title name most of them and whose section mentions the rest.
func TestATitleThatCoversTheQueryOutranksAHubThatMentionsIt(t *testing.T) {
	hub := typed(doc("infra-example-desk", "Example Desk", "Example Desk", gate.Internal,
		"# Example Desk\n\n## Overview\n\nThe help desk.\n\n## Deploy\n\nThe deploy job ships each merge.\n"),
		"architecture_note", "example", "example-desk")
	guide := typed(doc("rb-deploy-example-desk", "Deploy Example Desk", "Example Desk", gate.Internal, deployGuide),
		"deployment_guide", "example")
	hits := search.Rank([]*vault.Doc{hub, guide}, "deploy example desk", 5)
	if len(hits) != 2 || hits[0].Doc != guide {
		t.Fatalf("want the deploy guide first, got %v", hits)
	}
}

// A procedure named for the query's action outranks notes whose tags carry
// the same word.
func TestAProcedureNamedForTheActionOutranksTaggedNotes(t *testing.T) {
	task := typed(doc("task-apply-config-in-the-deploy", "Apply the new config in the deploy that ships it", "", gate.Internal,
		"# Apply the new config in the deploy that ships it\n\nThe deploy restarts with the old config.\n"),
		"task", "deploy", "containers")
	task.Status = "open"
	guide := typed(doc("rb-deploy-example-desk", "Deploy Example Desk", "Example Desk", gate.Internal, deployGuide),
		"deployment_guide", "example")
	hits := search.Rank([]*vault.Doc{task, guide}, "deploy", 5)
	if len(hits) != 2 || hits[0].Doc != guide {
		t.Fatalf("want the deploy guide first, got %v", hits)
	}
}
