package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// resolve compiles the JSON Schema p emits, as an off-the-shelf validator
// would.
func resolve(t *testing.T, p *schema.Profile) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(jsonx.Pretty(p.JSONSchema())), &s); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// instance is a frontmatter block as the JSON a validator reads.
func instance(t *testing.T, fm *jsonx.Obj) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(jsonx.Compact(fm)), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

type block struct {
	name, rel string
	fm        *jsonx.Obj
}

// fixtureBlocks are the frontmatter blocks of every doc the test vaults and
// the sample vault index.
func fixtureBlocks(t *testing.T) []block {
	t.Helper()
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	wd, _ := os.Getwd()
	var out []block
	for _, vaultRoot := range []string{root, filepath.Join(wd, "..", "..", "examples", "sample-vault")} {
		cfg := config.Load("", tk.Env(vaultRoot))
		for _, path := range vault.MarkdownFiles(cfg.VaultPath, cfg.IndexedDirs) {
			fm, err := frontmatter.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if fm == nil {
				continue
			}
			rel, _ := filepath.Rel(cfg.VaultPath, path)
			out = append(out, block{rel, rel, fm})
		}
	}
	if len(out) < 8 {
		t.Fatalf("only %d fixture docs; the agreement needs the vaults to hold docs", len(out))
	}
	return out
}

// brokenBlocks break one rule each, so both verdicts are exercised.
func brokenBlocks() []block {
	doc := func(extra ...any) *jsonx.Obj {
		fm := jsonx.New("doc_id", "rb-example", "title", "Example", "doc_type", "runbook", "system", "Example",
			"environment", "other", "status", "active", "sensitivity", "internal", "last_reviewed", "2026-10-01")
		for i := 0; i+1 < len(extra); i += 2 {
			if extra[i+1] == nil {
				fm.Delete(extra[i].(string))
				continue
			}
			fm.Set(extra[i].(string), extra[i+1])
		}
		return fm
	}
	task := func(extra ...any) *jsonx.Obj {
		fm := jsonx.New("doc_id", "task-example", "title", "Example", "doc_type", "task", "status", "open")
		for i := 0; i+1 < len(extra); i += 2 {
			fm.Set(extra[i].(string), extra[i+1])
		}
		return fm
	}
	// lean is a queue note with only the fields its doc_type requires.
	lean := func(extra ...any) *jsonx.Obj {
		fm := jsonx.New("doc_id", "queue-example", "title", "Example Queue", "doc_type", "queue", "system", "example",
			"status", "active")
		for i := 0; i+1 < len(extra); i += 2 {
			fm.Set(extra[i].(string), extra[i+1])
		}
		return fm
	}
	const at, index, queue = "03 Runbooks/Example.md", "01 Projects/example/index.md", "01 Projects/example/Queue.md"
	const report = "01 Projects/example/Reports/Example Audit.md"
	return []block{
		{"valid doc", at, doc()},
		{"empty optional values", at, doc("tags", []any{}, "last_reviewed", "", "effort", nil)},
		{"bad enum", at, doc("environment", "moon")},
		{"bad prefix", at, doc("doc_id", "example")},
		{"required missing", at, doc("system", nil)},
		{"required empty", at, doc("sensitivity", "")},
		{"bad date", at, doc("last_reviewed", "someday")},
		{"list as a string", at, doc("tags", "nginx")},
		{"string as a list", at, doc("title", []any{"Example"})},
		{"boolean as a string", at, doc("title", true)},
		{"task status on a doc", at, doc("status", "parked")},
		{"superseded_by without a prefix", at, doc("status", "deprecated", "superseded_by", "example-new")},
		{"superseded_by with a prefix", at, doc("status", "deprecated", "superseded_by", "report-example-new")},
		{"valid task", at, task("effort", "S", "priority", "high", "created", "2026-10-01")},
		{"task needs no environment", at, task("environment", "moon", "sensitivity", "nope")},
		{"doc status on a task", at, task("status", "deprecated")},
		{"task missing its title", at, task("title", "")},
		{"bad effort", at, task("effort", "XL")},
		{"project keys", index, doc("doc_id", "project-example", "doc_type", "architecture_note", "project_path", "~/Code/Projects/example",
			"repos", []any{"example-repo"}, "queue_prefix", "ex")},
		{"lean queue note", queue, lean()},
		{"queue note without its status", queue, lean("status", nil)},
		{"queue note with a bad environment", queue, lean("environment", "moon")},
		{"queue note with a doc prefix", queue, lean("doc_id", "note-example")},
		{"runbook without an environment", at, doc("environment", nil)},
		{"relative project path", index, doc("doc_id", "project-example", "project_path", "Code/example")},
		{"repo with an owner", index, doc("doc_id", "project-example", "repos", []any{"owner/example"})},
		{"reserved queue prefix", index, doc("doc_id", "project-example", "queue_prefix", "backlog")},
		{"uppercase queue prefix", index, doc("doc_id", "project-example", "queue_prefix", "EX")},
		{"report", report, doc("doc_id", "report-example", "doc_type", schema.ReportDocType, schema.ReportKindField, "audit")},
		{"report without its kind", report, doc("doc_id", "report-example", "doc_type", schema.ReportDocType)},
		{"report with an unknown kind", report, doc("doc_id", "report-example", "doc_type", schema.ReportDocType, schema.ReportKindField, "musing")},
		{"kind on a runbook", at, doc(schema.ReportKindField, "audit")},
		{"kind on a task", at, task(schema.ReportKindField, "audit")},
	}
}

// schema.Profile.Check and an off-the-shelf validator running the emitted
// JSON Schema agree on every block: the schema is the profile's rules, not a
// second copy that drifts.
// Labs declares no tasks, so its task blocks are refused by both; the task
// profile, declared as a provider declares one, holds them to the task
// branch.
func TestTheJSONSchemaAgreesWithCheck(t *testing.T) {
	for _, p := range []*schema.Profile{schema.Labs, tk.TaskProfile()} {
		r := resolve(t, p)
		blocks := append(fixtureBlocks(t), brokenBlocks()...)
		var valid, invalid int
		for _, b := range blocks {
			problems := p.Check(b.fm, b.rel)
			err := r.Validate(instance(t, b.fm))
			if (len(problems) == 0) != (err == nil) {
				t.Errorf("%s with %s: Check %v, JSON Schema %v\n%s", b.name, p.Name, problems, err, jsonx.Compact(b.fm))
			}
			if err == nil {
				valid++
			} else {
				invalid++
			}
		}
		if valid < 5 || invalid < 5 {
			t.Fatalf("%d valid and %d invalid blocks; both verdicts need cases", valid, invalid)
		}
	}
}

func TestTheEducationJSONSchemaAgreesWithCheck(t *testing.T) {
	r := resolve(t, schema.Education)
	course := func(extra ...any) *jsonx.Obj {
		fm := jsonx.New("doc_id", "course-cs1000", "title", "Example Course", "doc_type", "course", "status", "active", "code", "CS1000")
		for i := 0; i+1 < len(extra); i += 2 {
			fm.Set(extra[i].(string), extra[i+1])
		}
		return fm
	}
	for name, fm := range map[string]*jsonx.Obj{
		"valid":          course(),
		"credits":        course("credits", "3"),
		"bad credits":    course("credits", "three"),
		"boolean credit": course("credits", true),
		"labs status":    course("status", "deprecated"),
		"labs prefix":    course("doc_id", "rb-x"),
	} {
		problems := schema.Education.Check(fm, "Courses/Example.md")
		if err := r.Validate(instance(t, fm)); (len(problems) == 0) != (err == nil) {
			t.Errorf("%s: Check %v, JSON Schema %v", name, problems, err)
		}
	}
}

// What a schema can't see is said in the field's description.
func TestTheJSONSchemaDescribesWhatItCannotCheck(t *testing.T) {
	text := jsonx.Pretty(schema.Labs.JSONSchema())
	for _, want := range []string{"Only on docs at 01 Projects/*/index.md.", "Unique across docs.", "Names the doc_id of another doc in the vault.",
		"Only on doc_type report.", "A doc_type report doc lives in a Reports folder, and only those docs do.",
		`"$schema": "https://json-schema.org/draft/2020-12/schema"`} {
		if !strings.Contains(text, want) {
			t.Errorf("schema does not say %q", want)
		}
	}
}
