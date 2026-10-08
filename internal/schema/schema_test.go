package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

// Computed by the Python engine (LABS_PROFILE.fingerprint()) at the port.
const pythonLabsFingerprint = "c1c784ddd8072ff36b9809dcb57eec8979d2c240c74608dd1688bd3cf0b7c80d"

func TestAsDictIsSortedAndStable(t *testing.T) {
	d := Labs.AsDict()
	for _, k := range []string{"doc_types", "environments", "statuses", "sensitivities"} {
		v, _ := d.Get(k)
		if !slices.IsSorted(v.([]string)) {
			t.Fatalf("%s not sorted", k)
		}
	}
	prefixes, _ := d.Get("doc_id_prefixes")
	if !slices.Equal(prefixes.([]string), Labs.DocIDPrefixes) {
		t.Fatal("prefix order changed")
	}
}

func TestSchemaIsCanonicalized(t *testing.T) {
	if Labs.HasEnvironment("lab") || !slices.Equal(Sorted(Labs.Sensitivities), []string{"internal", "public", "restricted", "sensitive"}) {
		t.Fatal("labs drifted")
	}
}

func schemaDoc(env, sens string) string {
	if env == "" {
		env = "environment:   " + strings.Join(Sorted(Labs.Environments), " | ")
	}
	if sens == "" {
		sens = "sensitivity:   " + strings.Join(Sorted(Labs.Sensitivities), " | ")
	}
	return strings.Join([]string{"## Schema", "```yaml",
		"doc_type:      " + strings.Join(Sorted(Labs.DocTypes), " | "), env,
		"status:        " + strings.Join(Sorted(Labs.Statuses), " | "), sens, "```"}, "\n")
}

func TestCheckDocEnums(t *testing.T) {
	if m := Labs.CheckDocEnums(schemaDoc("", "")); len(m) != 0 {
		t.Fatal(m)
	}
	m := Labs.CheckDocEnums(schemaDoc("environment:   homelab | lab | other", ""))
	if !slices.ContainsFunc(m, func(s string) bool { return strings.Contains(s, "environment") && strings.Contains(s, "lab") }) {
		t.Fatal(m)
	}
	m = Labs.CheckDocEnums(schemaDoc("", "sensitivity:   public | internal"))
	if !slices.ContainsFunc(m, func(s string) bool { return strings.Contains(s, "sensitivity") && strings.Contains(s, "missing") }) {
		t.Fatal(m)
	}
	m = Labs.CheckDocEnums("doc_type: runbook | architecture_note | decision_record")
	if !slices.ContainsFunc(m, func(s string) bool { return strings.HasPrefix(s, "environment:") }) {
		t.Fatal(m)
	}
}

var domain = DocumentSchema{Fields: map[string]Field{
	"category": {Required: true, Kind: "string", Choices: []string{"a", "b"}},
	"renews":   {Required: true, Kind: "date"},
	"notice":   {Kind: "integer", Minimum: new(0), Maximum: new(365)},
	"horizon":  {Kind: "string", Pattern: `\d{4}(-Q[1-4])?`},
}}

func TestDocumentSchemaValidatesComposableFieldRules(t *testing.T) {
	ok := jsonx.New("category", "a", "renews", "2027-01-02", "notice", "90", "horizon", "2027-Q2")
	if errs := domain.Validate(ok); len(errs) != 0 {
		t.Fatal(errs)
	}
	errs := domain.Validate(jsonx.New("category", "x", "renews", "someday", "notice", "999", "horizon", "soon"))
	for _, want := range []string{"one of", "ISO date", "at most 365", "does not match"} {
		if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, want) }) {
			t.Fatalf("missing %q in %v", want, errs)
		}
	}
	if errs := domain.Validate(jsonx.New()); len(errs) != 2 {
		t.Fatal(errs)
	}
}

func TestContractFingerprintIsDeterministic(t *testing.T) {
	p := *Labs
	p.DocumentSchemas = map[string]DocumentSchema{"runbook": domain}
	c := p.ContractDict()
	if c.Str("name") != "labs" || jsonx.Compact(func() any { v, _ := c.Get("contract_version"); return v }()) != "1" {
		t.Fatal(jsonx.Compact(c))
	}
	first, second := p.Fingerprint(), p.Fingerprint()
	if first != second || len(first) != 64 || first == Labs.Fingerprint() {
		t.Fatal("fingerprint")
	}
	if jsonx.Compact(p.AsDict()) != jsonx.Compact(Labs.AsDict()) {
		t.Fatal("document schemas leaked into the legacy shape")
	}
}

func TestFromContractRoundTripsAndRejectsUnknownDocTypes(t *testing.T) {
	p := *Labs
	p.DocumentSchemas = map[string]DocumentSchema{"runbook": domain}
	back, err := FromContract(p.ContractDict())
	if err != nil || back.Fingerprint() != p.Fingerprint() {
		t.Fatalf("%v", err)
	}
	c := p.ContractDict()
	schemas, _ := c.Get("document_schemas")
	schemas.(*jsonx.Obj).Set("renewal", jsonx.New())
	if _, err := FromContract(c); err == nil || !strings.Contains(err.Error(), "unknown doc_types") {
		t.Fatal(err)
	}
}

func TestEducationIsADistinctContract(t *testing.T) {
	if !slices.Equal(Sorted(Education.DocTypes), []string{"assignment", "course", "course_note", "resource", "task"}) ||
		!slices.Equal(Education.DocIDPrefixes, []string{"course-", "cnote-", "asg-", "res-", "task-"}) ||
		!slices.Equal(Sorted(Education.TaskStatuses), Sorted(Labs.TaskStatuses)) {
		t.Fatal("education profile")
	}
}

func TestLabsFingerprintMatchesThePythonEngine(t *testing.T) {
	if pythonLabsFingerprint == "" {
		t.Skip()
	}
	if got := Labs.Fingerprint(); got != pythonLabsFingerprint {
		t.Fatalf("got %s", got)
	}
}
