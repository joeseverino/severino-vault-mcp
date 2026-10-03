// Package schema is the frontmatter profile framework: a named bundle of
// enum sets, id prefixes, the task lifecycle, and per-doc-type field rules.
// The labs and education profiles are built in; other vaults load theirs as
// data (a profile contract), so domain values never have to live here.
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// Field is one domain-supplied frontmatter rule.
type Field struct {
	Required bool
	Kind     string // string, integer, boolean, list, date
	Choices  []string
	Pattern  string
	Minimum  *int
	Maximum  *int
}

// Validate checks one value against the rule.
func (f Field) Validate(name string, value any) []string {
	if value == nil || value == "" {
		if f.Required {
			return []string{"missing required field: " + name}
		}
		return nil
	}
	if l, ok := value.([]any); ok && len(l) == 0 {
		if f.Required {
			return []string{"missing required field: " + name}
		}
		return nil
	}
	var errs []string
	var parsed *int
	switch f.Kind {
	case "integer":
		if _, isBool := value.(bool); isBool {
			errs = append(errs, name+" must be an integer")
		} else if n, err := strconv.Atoi(strings.TrimSpace(pystr.Str(value))); err != nil {
			errs = append(errs, name+" must be an integer")
		} else {
			parsed = &n
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			errs = append(errs, name+" must be a boolean")
		}
	case "list":
		if _, ok := value.([]any); !ok {
			errs = append(errs, name+" must be a list")
		}
	case "date":
		if !IsISODate(pystr.Str(value)) {
			errs = append(errs, name+" must be an ISO date (YYYY-MM-DD)")
		}
	default:
		if _, ok := value.(string); !ok {
			errs = append(errs, name+" must be a string")
		}
	}
	text := pystr.Str(value)
	if len(f.Choices) > 0 && !slices.Contains(f.Choices, text) {
		sorted := slices.Sorted(slices.Values(f.Choices))
		errs = append(errs, fmt.Sprintf("%s=%s must be one of: %s", name, pystr.ReprAny(value), strings.Join(sorted, ", ")))
	}
	if f.Pattern != "" {
		if re, err := regexp.Compile(`\A(?:` + f.Pattern + `)\z`); err == nil && !re.MatchString(text) {
			errs = append(errs, fmt.Sprintf("%s=%s does not match %s", name, pystr.ReprAny(value), pystr.Repr(f.Pattern)))
		}
	}
	if parsed != nil {
		if f.Minimum != nil && *parsed < *f.Minimum {
			errs = append(errs, fmt.Sprintf("%s must be at least %d", name, *f.Minimum))
		}
		if f.Maximum != nil && *parsed > *f.Maximum {
			errs = append(errs, fmt.Sprintf("%s must be at most %d", name, *f.Maximum))
		}
	}
	return errs
}

// IsISODate matches Python's date.fromisoformat for YYYY-MM-DD.
func IsISODate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil && len(s) == 10
}

// Contract is the field rule as data.
func (f Field) Contract() *jsonx.Obj {
	o := jsonx.New("kind", f.Kind, "required", f.Required)
	if len(f.Choices) > 0 {
		o.Set("choices", slices.Sorted(slices.Values(f.Choices)))
	}
	if f.Pattern != "" {
		o.Set("pattern", f.Pattern)
	}
	if f.Minimum != nil {
		o.Set("minimum", *f.Minimum)
	}
	if f.Maximum != nil {
		o.Set("maximum", *f.Maximum)
	}
	return o
}

// DocumentSchema is the field contract for one doc_type.
type DocumentSchema struct{ Fields map[string]Field }

// Validate checks every field rule, in name order.
func (d DocumentSchema) Validate(data *jsonx.Obj) []string {
	var errs []string
	for _, name := range slices.Sorted(mapKeys(d.Fields)) {
		v, _ := data.Get(name)
		errs = append(errs, d.Fields[name].Validate(name, v)...)
	}
	return errs
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// Profile is one vault's frontmatter contract.
type Profile struct {
	Name               string
	DocTypes           []string
	Environments       []string
	Statuses           []string
	Sensitivities      []string
	DocIDPrefixes      []string
	RequiredFields     []string
	TaskStatuses       []string
	TaskRequiredFields []string
	TaskFields         []string
	DocumentSchemas    map[string]DocumentSchema
}

// HasDocType and friends test membership.
func (p *Profile) HasDocType(v string) bool     { return slices.Contains(p.DocTypes, v) }
func (p *Profile) HasEnvironment(v string) bool { return slices.Contains(p.Environments, v) }
func (p *Profile) HasStatus(v string) bool      { return slices.Contains(p.Statuses, v) }
func (p *Profile) HasSensitivity(v string) bool { return slices.Contains(p.Sensitivities, v) }
func (p *Profile) HasTaskStatus(v string) bool  { return slices.Contains(p.TaskStatuses, v) }

// HasPrefix reports whether docID starts with one of the profile prefixes.
func (p *Profile) HasPrefix(docID string) bool {
	for _, pre := range p.DocIDPrefixes {
		if strings.HasPrefix(docID, pre) {
			return true
		}
	}
	return false
}

// Sorted returns a sorted copy.
func Sorted(v []string) []string { return slices.Sorted(slices.Values(v)) }

// AsDict is the legacy HQ wire shape: sets sorted, ordered fields as declared.
func (p *Profile) AsDict() *jsonx.Obj {
	return jsonx.New(
		"doc_types", Sorted(p.DocTypes),
		"environments", Sorted(p.Environments),
		"statuses", Sorted(p.Statuses),
		"sensitivities", Sorted(p.Sensitivities),
		"doc_id_prefixes", slices.Clone(p.DocIDPrefixes),
		"required_fields", slices.Clone(p.RequiredFields),
		"task_statuses", Sorted(p.TaskStatuses),
		"task_required_fields", slices.Clone(p.TaskRequiredFields),
	)
}

// ContractDict is the complete versioned contract.
func (p *Profile) ContractDict() *jsonx.Obj {
	schemas := jsonx.New()
	for _, name := range slices.Sorted(mapKeys(p.DocumentSchemas)) {
		fields := jsonx.New()
		ds := p.DocumentSchemas[name]
		for _, f := range slices.Sorted(mapKeys(ds.Fields)) {
			fields.Set(f, ds.Fields[f].Contract())
		}
		schemas.Set(name, fields)
	}
	o := jsonx.New("contract_version", 1, "name", p.Name)
	o.Merge(p.AsDict())
	o.Set("task_fields", slices.Clone(p.TaskFields))
	o.Set("document_schemas", schemas)
	return o
}

// Fingerprint is the SHA-256 of the sorted, compact contract.
func (p *Profile) Fingerprint() string {
	b, _ := jsonx.Encode(p.ContractDict(), jsonx.Options{SortKeys: true, EnsureASCII: true})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ValidateDocument applies the per-doc-type field rules.
func (p *Profile) ValidateDocument(data *jsonx.Obj) []string {
	dt, _ := data.Get("doc_type")
	ds, ok := p.DocumentSchemas[pystr.Str(orEmpty(dt))]
	if !ok {
		return nil
	}
	return ds.Validate(data)
}

func orEmpty(v any) any {
	if !pystr.Truthy(v) {
		return ""
	}
	return v
}

var enumLine = regexp.MustCompile(`^\s*(doc_type|environment|status|sensitivity)\s*:\s*(.+?)\s*$`)

// CheckDocEnums compares a human schema doc's enum lines to the profile.
func (p *Profile) CheckDocEnums(text string) []string {
	found := map[string]map[string]bool{}
	taskSet := toSet(p.TaskStatuses)
	for _, line := range pystr.SplitLines(text) {
		m := enumLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		field, rhs := m[1], strings.SplitN(m[2], "#", 2)[0]
		tokens := map[string]bool{}
		for _, tok := range strings.Split(rhs, "|") {
			if t := strings.TrimSpace(tok); t != "" {
				tokens[t] = true
			}
		}
		if len(tokens) <= 1 {
			continue
		}
		if field == "status" && equalSets(tokens, taskSet) {
			continue
		}
		if _, ok := found[field]; !ok {
			found[field] = tokens
		}
	}
	var out []string
	for _, f := range []struct {
		name  string
		canon []string
	}{{"doc_type", p.DocTypes}, {"environment", p.Environments}, {"status", p.Statuses}, {"sensitivity", p.Sensitivities}} {
		doc, ok := found[f.name]
		if !ok {
			out = append(out, f.name+": no enum list found in the doc")
			continue
		}
		canon := toSet(f.canon)
		if equalSets(doc, canon) {
			continue
		}
		var parts []string
		if extra := diff(doc, canon); len(extra) > 0 {
			parts = append(parts, "doc lists unknown "+pystr.ReprList(extra))
		}
		if missing := diff(canon, doc); len(missing) > 0 {
			parts = append(parts, "doc is missing "+pystr.ReprList(missing))
		}
		out = append(out, f.name+": "+strings.Join(parts, "; "))
	}
	return out
}

func toSet(v []string) map[string]bool {
	s := map[string]bool{}
	for _, x := range v {
		s[x] = true
	}
	return s
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func diff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

var universalTaskStatuses = []string{"open", "active", "parked", "done", "wontfix"}

// Labs is the Severino Labs ops profile. HQ commits Labs.AsDict().
var Labs = &Profile{
	Name: "labs",
	DocTypes: []string{"runbook", "architecture_note", "deployment_guide", "troubleshooting_guide",
		"recovery_procedure", "public_article_draft", "decision_record", "task"},
	Environments:       []string{"homelab", "vps", "wordpress", "cloudflare", "tailscale", "adguard", "unifi", "local_mac", "other"},
	Statuses:           []string{"draft", "active", "deprecated", "archived"},
	Sensitivities:      []string{"public", "internal", "sensitive", "restricted"},
	DocIDPrefixes:      []string{"rb-", "infra-", "report-", "project-", "note-", "task-"},
	RequiredFields:     []string{"doc_id", "title", "doc_type", "system", "environment", "status", "sensitivity"},
	TaskStatuses:       universalTaskStatuses,
	TaskRequiredFields: []string{"doc_id", "title", "doc_type", "status"},
	TaskFields:         []string{"status", "related_projects", "effort", "priority", "created", "closed"},
	DocumentSchemas:    map[string]DocumentSchema{},
}

// Education is the coursework profile.
var Education = &Profile{
	Name:               "education",
	DocTypes:           []string{"course", "course_note", "assignment", "resource", "task"},
	Environments:       []string{"gatech", "cert", "other"},
	Statuses:           []string{"upcoming", "active", "completed", "dropped", "draft", "archived"},
	Sensitivities:      []string{"public", "internal"},
	DocIDPrefixes:      []string{"course-", "cnote-", "asg-", "res-", "task-"},
	RequiredFields:     []string{"doc_id", "title", "doc_type", "status"},
	TaskStatuses:       universalTaskStatuses,
	TaskRequiredFields: []string{"doc_id", "title", "doc_type", "status"},
	TaskFields:         []string{"status", "related_projects", "effort", "priority", "created", "closed"},
	DocumentSchemas:    map[string]DocumentSchema{},
}

// FromContract builds a profile from its contract (ContractDict's shape).
func FromContract(c *jsonx.Obj) (*Profile, error) {
	str := func(k string) string { return c.Str(k) }
	list := func(k string) ([]string, error) {
		v, ok := c.Get(k)
		if !ok {
			return nil, fmt.Errorf("profile contract is missing %s", k)
		}
		if strs, ok := v.([]string); ok {
			return slices.Clone(strs), nil
		}
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("profile contract %s must be a list", k)
		}
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("profile contract %s must hold strings", k)
			}
			out = append(out, s)
		}
		return out, nil
	}
	p := &Profile{Name: str("name"), DocumentSchemas: map[string]DocumentSchema{}}
	if p.Name == "" {
		return nil, fmt.Errorf("profile contract is missing name")
	}
	var err error
	for _, f := range []struct {
		key string
		dst *[]string
	}{
		{"doc_types", &p.DocTypes}, {"environments", &p.Environments}, {"statuses", &p.Statuses},
		{"sensitivities", &p.Sensitivities}, {"doc_id_prefixes", &p.DocIDPrefixes},
		{"required_fields", &p.RequiredFields}, {"task_statuses", &p.TaskStatuses},
		{"task_required_fields", &p.TaskRequiredFields}, {"task_fields", &p.TaskFields},
	} {
		if *f.dst, err = list(f.key); err != nil {
			return nil, err
		}
	}
	schemas, _ := c.Get("document_schemas")
	if so, ok := schemas.(*jsonx.Obj); ok {
		for _, docType := range so.Keys() {
			fv, _ := so.Get(docType)
			fo, ok := fv.(*jsonx.Obj)
			if !ok {
				return nil, fmt.Errorf("document_schemas.%s must be an object", docType)
			}
			ds := DocumentSchema{Fields: map[string]Field{}}
			for _, name := range fo.Keys() {
				rv, _ := fo.Get(name)
				ro, ok := rv.(*jsonx.Obj)
				if !ok {
					return nil, fmt.Errorf("document_schemas.%s.%s must be an object", docType, name)
				}
				field := Field{Kind: ro.Str("kind"), Required: ro.Bool("required"), Pattern: ro.Str("pattern")}
				if field.Kind == "" {
					field.Kind = "string"
				}
				switch ch, _ := ro.Get("choices"); x := ch.(type) {
				case []string:
					field.Choices = slices.Clone(x)
				case []any:
					for _, item := range x {
						field.Choices = append(field.Choices, pystr.Str(item))
					}
				}
				for _, bound := range []struct {
					key string
					dst **int
				}{{"minimum", &field.Minimum}, {"maximum", &field.Maximum}} {
					if v, ok := ro.Get(bound.key); ok {
						n, err := strconv.Atoi(fmt.Sprint(v))
						if err != nil {
							return nil, fmt.Errorf("document_schemas.%s.%s.%s must be an integer", docType, name, bound.key)
						}
						*bound.dst = &n
					}
				}
				ds.Fields[name] = field
			}
			if !p.HasDocType(docType) {
				return nil, fmt.Errorf("document schemas reference unknown doc_types: ['%s']", docType)
			}
			p.DocumentSchemas[docType] = ds
		}
	}
	return p, nil
}
