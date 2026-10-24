// Package schema is the frontmatter profile framework: a named bundle of
// enum sets, id prefixes, the task lifecycle, the fields a vault's docs carry,
// and per-doc-type field rules. The labs and education profiles are built in;
// other vaults load theirs as data (a profile contract), so domain values
// never have to live here.
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// Field kinds.
const (
	KindString  = "string"
	KindEnum    = "enum"
	KindInteger = "integer"
	KindBoolean = "boolean"
	KindList    = "list"
	KindDate    = "date"
	KindPath    = "path"
)

// Field is one frontmatter field rule: a field a profile declares (Name set),
// which set_frontmatter writes and validate checks, or a doc_type's field rule.
type Field struct {
	Name     string
	Required bool
	Kind     string // one of the Kind constants
	Choices  []string
	// TaskChoices replace Choices on a doc_type: task doc (status's lifecycle).
	TaskChoices []string
	// Prefixes: the value starts with one of these.
	Prefixes []string
	// Pattern matches the whole value, or each item of a list; Shape says it
	// in words for the error.
	Pattern, Shape string
	Minimum        *int
	Maximum        *int
	// Reserved are values no doc may take.
	Reserved []string
	// Scope is the path.Match pattern a doc's vault-relative path matches to
	// carry the field; "" is every doc.
	Scope string
	// Unique: no two docs share the value, or any one list item, compared
	// case-insensitively.
	Unique bool
	// Immutable: once set, the value never changes.
	Immutable bool
	// Touch: the date can be set to today without naming it.
	Touch bool
	// DocRef: the value, or each list item, is the doc_id of another doc in
	// the vault. It takes the profile's doc_id prefixes unless it names its own.
	DocRef bool
	// Example and Help describe the field to a caller.
	Example, Help string
}

// IsList reports a list field.
func (f Field) IsList() bool { return f.Kind == KindList }

// InScope reports whether the doc at rel may carry the field.
func (f Field) InScope(rel string) bool {
	if f.Scope == "" {
		return true
	}
	ok, _ := path.Match(f.Scope, filepath.ToSlash(path.Clean(filepath.ToSlash(rel))))
	return ok
}

// ScopeError is the refusal for a field set on a doc outside its scope.
func (f Field) ScopeError(rel string) string {
	return fmt.Sprintf("%s only applies to docs at %s, not %s", f.Name, pystr.Repr(f.Scope), rel)
}

// Validate checks one value against the rule.
func (f Field) Validate(name string, value any) []string { return f.check(name, value, false) }

// check is Validate with the task lifecycle selected by task.
func (f Field) check(name string, value any, task bool) []string {
	if missing(value) {
		if f.Required {
			return []string{"missing required field: " + name}
		}
		return nil
	}
	if f.Kind == KindList {
		items, ok := value.([]any)
		if !ok {
			return []string{name + " must be a list"}
		}
		var errs []string
		for _, item := range items {
			errs = append(errs, f.valueRules(name+" entry", item, false)...)
		}
		return errs
	}
	var errs []string
	var parsed *int
	switch f.Kind {
	case KindInteger:
		if _, isBool := value.(bool); isBool {
			errs = append(errs, name+" must be an integer")
		} else if n, err := strconv.Atoi(strings.TrimSpace(pystr.Str(value))); err != nil {
			errs = append(errs, name+" must be an integer")
		} else {
			parsed = &n
		}
	case KindBoolean:
		if _, ok := value.(bool); !ok {
			errs = append(errs, name+" must be a boolean")
		}
	case KindDate:
		if !IsISODate(pystr.Str(value)) {
			errs = append(errs, name+" must be an ISO date (YYYY-MM-DD)")
		}
	case KindPath:
		if s, ok := value.(string); !ok || !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "~/") {
			errs = append(errs, name+" must be an absolute path or start with ~/")
		}
	default:
		if _, ok := value.(string); !ok {
			errs = append(errs, name+" must be a string")
		}
	}
	errs = append(errs, f.valueRules(name, value, task)...)
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

// valueRules are the rules on a value's text: choices, prefixes, pattern and
// reserved values.
func (f Field) valueRules(name string, value any, task bool) []string {
	var errs []string
	text := pystr.Str(value)
	choices, suffix := f.Choices, ""
	if task && len(f.TaskChoices) > 0 {
		choices, suffix = f.TaskChoices, " (task lifecycle)"
	}
	if len(choices) > 0 {
		if e := NotIn(name, text, Sorted(choices)); e != "" {
			errs = append(errs, e+suffix)
		}
	}
	if len(f.Prefixes) > 0 && !slices.ContainsFunc(f.Prefixes, func(p string) bool { return strings.HasPrefix(text, p) }) {
		errs = append(errs, fmt.Sprintf("%s %s must start with one of %s", name, pystr.Repr(text), pystr.ReprList(f.Prefixes)))
	}
	if f.Pattern != "" {
		if re, err := regexp.Compile(`\A(?:` + f.Pattern + `)\z`); err == nil && !re.MatchString(text) {
			if f.Shape != "" {
				errs = append(errs, fmt.Sprintf("%s %s must be %s", name, pystr.ReprAny(value), f.Shape))
			} else {
				errs = append(errs, fmt.Sprintf("%s=%s does not match %s", name, pystr.ReprAny(value), pystr.Repr(f.Pattern)))
			}
		}
	}
	if slices.Contains(f.Reserved, text) {
		errs = append(errs, fmt.Sprintf("%s %s is reserved; pick another", name, pystr.Repr(text)))
	}
	return errs
}

// missing reports an absent value: nil, "", or an empty list.
func missing(value any) bool {
	if value == nil || value == "" {
		return true
	}
	l, ok := value.([]any)
	return ok && len(l) == 0
}

// IsISODate matches Python's date.fromisoformat for YYYY-MM-DD.
func IsISODate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil && len(s) == 10
}

// Contract is the field rule as data.
func (f Field) Contract() *jsonx.Obj {
	o := jsonx.New("kind", f.Kind, "required", f.Required)
	for _, l := range []struct {
		key    string
		values []string
		sorted bool
	}{{"choices", f.Choices, true}, {"task_choices", f.TaskChoices, true}, {"prefixes", f.Prefixes, false}} {
		if len(l.values) > 0 {
			if l.sorted {
				o.Set(l.key, Sorted(l.values))
			} else {
				o.Set(l.key, slices.Clone(l.values))
			}
		}
	}
	for _, s := range [][2]string{{"pattern", f.Pattern}, {"shape", f.Shape}} {
		if s[1] != "" {
			o.Set(s[0], s[1])
		}
	}
	if f.Minimum != nil {
		o.Set("minimum", *f.Minimum)
	}
	if f.Maximum != nil {
		o.Set("maximum", *f.Maximum)
	}
	if len(f.Reserved) > 0 {
		o.Set("reserved", Sorted(f.Reserved))
	}
	if f.Scope != "" {
		o.Set("scope", f.Scope)
	}
	for _, b := range []struct {
		key string
		on  bool
	}{{"unique", f.Unique}, {"immutable", f.Immutable}, {"touch", f.Touch}, {"doc_ref", f.DocRef}} {
		if b.on {
			o.Set(b.key, true)
		}
	}
	for _, s := range [][2]string{{"example", f.Example}, {"help", f.Help}} {
		if s[1] != "" {
			o.Set(s[0], s[1])
		}
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
	// Fields are the profile's own frontmatter fields, after the ones every
	// profile shares (Settable lists both).
	Fields          []Field
	DocumentSchemas map[string]DocumentSchema
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

// Task effort and priority values.
var (
	Efforts    = []string{"L", "M", "S"}
	Priorities = []string{"high", "low", "med"}
)

// shared are the fields every profile carries, their enums taken from the
// profile's sets.
func (p *Profile) shared() []Field {
	return []Field{
		{Name: "doc_id", Kind: KindString, Prefixes: p.DocIDPrefixes, Unique: true, Immutable: true,
			Help: "Stable id; required to create, and fixed once set."},
		{Name: "title", Kind: KindString, Help: "The doc's title."},
		{Name: "doc_type", Kind: KindEnum, Choices: p.DocTypes, Help: "What kind of doc this is."},
		{Name: "system", Kind: KindString, Help: "The system the doc is about."},
		{Name: "environment", Kind: KindEnum, Choices: p.Environments, Help: "Where that system runs."},
		{Name: "status", Kind: KindEnum, Choices: p.Statuses, TaskChoices: p.TaskStatuses,
			Help: "The doc's lifecycle state (a task's own lifecycle for tasks)."},
		{Name: "sensitivity", Kind: KindEnum, Choices: p.Sensitivities, Help: "Who may read the body."},
		{Name: "last_reviewed", Kind: KindDate, Touch: true, Help: "When the doc was last checked against reality."},
		{Name: "tags", Kind: KindList, Help: "Tags."},
		{Name: "related_projects", Kind: KindList, Help: "Projects the doc belongs to, as folder names under the projects folder."},
		{Name: "related_assets", Kind: KindList, Help: "Assets the doc touches."},
		{Name: "aliases", Kind: KindList, Help: "Other names Obsidian resolves links by."},
		{Name: "effort", Kind: KindEnum, Choices: Efforts, Help: "A task's size."},
		{Name: "priority", Kind: KindEnum, Choices: Priorities, Help: "A task's priority."},
		{Name: "created", Kind: KindDate, Help: "When a task was filed."},
		{Name: "closed", Kind: KindDate, Help: "When a task was finished."},
	}
}

// Settable is every frontmatter field the profile declares, in order: the
// shared fields, then its own. Required follows RequiredFields, and a doc
// reference without prefixes of its own takes the doc_id prefixes.
func (p *Profile) Settable() []Field {
	out := append(p.shared(), p.Fields...)
	for i := range out {
		out[i].Required = slices.Contains(p.RequiredFields, out[i].Name)
		if out[i].DocRef && len(out[i].Prefixes) == 0 {
			out[i].Prefixes = p.DocIDPrefixes
		}
	}
	return out
}

// Union is every field the profiles declare, in order of first declaration;
// a name keeps its first profile's rule.
func Union(profiles ...*Profile) []Field {
	var out []Field
	for _, p := range profiles {
		for _, f := range p.Settable() {
			if !slices.ContainsFunc(out, func(o Field) bool { return o.Name == f.Name }) {
				out = append(out, f)
			}
		}
	}
	return out
}

// Field is the declared field name.
func (p *Profile) Field(name string) (Field, bool) {
	for _, f := range p.Settable() {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Problems checks one value of a declared field, in set_frontmatter's words;
// nil when it is allowed or the field is not declared. task selects the task
// lifecycle for status.
func (p *Profile) Problems(name string, value any, task bool) []string {
	f, ok := p.Field(name)
	if !ok {
		return nil
	}
	f.Required = false
	return f.check(name, value, task)
}

// NotIn is the one wording of an enum problem, "" when value is allowed.
// allowed is listed in the order given.
func NotIn(field, value string, allowed []string) string {
	if slices.Contains(allowed, value) {
		return ""
	}
	return fmt.Sprintf("%s %s not in %s", field, pystr.Repr(value), pystr.ReprList(allowed))
}

// IsTask reports a doc_type: task block.
func IsTask(fm *jsonx.Obj) bool {
	dt, _ := fm.Get("doc_type")
	return pystr.Str(orEmpty(dt)) == "task"
}

// docOnly reports a field a doc requires and a task does not carry, which a
// task's block is not held to (environment, sensitivity).
func (p *Profile) docOnly(name string) bool {
	return slices.Contains(p.RequiredFields, name) && !slices.Contains(p.TaskRequiredFields, name) && !slices.Contains(p.TaskFields, name)
}

// Check validates a whole frontmatter block at rel with the rules
// set_frontmatter writes by: required fields (a task's own set for tasks),
// every declared field's rule and scope (a task skips the doc-only fields),
// and the per-doc-type field rules.
func (p *Profile) Check(fm *jsonx.Obj, rel string) []string {
	task := IsTask(fm)
	required := p.RequiredFields
	if task {
		required = p.TaskRequiredFields
	}
	var errs []string
	for _, name := range required {
		if v, _ := fm.Get(name); missing(v) {
			errs = append(errs, "missing required field: "+name)
		}
	}
	for _, f := range p.Settable() {
		v, _ := fm.Get(f.Name)
		if missing(v) || (task && p.docOnly(f.Name)) {
			continue
		}
		f.Required = false
		errs = append(errs, f.check(f.Name, v, task)...)
		if !f.InScope(rel) {
			errs = append(errs, f.ScopeError(rel))
		}
	}
	return append(errs, p.ValidateDocument(fm)...)
}

// RefProblems checks every doc reference fm carries against the vault: each
// must name a doc_id that known holds, and not the doc's own. Check is the
// per-file half; validate, doctor and set_frontmatter add this one, which
// needs the vault's index.
func (p *Profile) RefProblems(fm *jsonx.Obj, known func(docID string) bool) []string {
	own, _ := fm.Get("doc_id")
	var errs []string
	for _, f := range p.Settable() {
		v, _ := fm.Get(f.Name)
		if !f.DocRef || missing(v) {
			continue
		}
		items, isList := v.([]any)
		if !isList {
			items = []any{v}
		}
		for _, item := range items {
			id := pystr.Str(item)
			switch {
			case own != nil && id == pystr.Str(own):
				errs = append(errs, fmt.Sprintf("%s %s names the doc itself", f.Name, pystr.Repr(id)))
			case !known(id):
				errs = append(errs, NoDoc(f.Name, id))
			}
		}
	}
	return errs
}

// NoDoc is the one wording of a doc reference that names no doc.
func NoDoc(name, docID string) string {
	return fmt.Sprintf("%s %s names no doc in the vault", name, pystr.Repr(docID))
}

// DocRefProblems checks one doc reference outside a frontmatter block (a
// queue item's body, say) the way RefProblems and Check check a DocRef
// field: it starts with one of the profile's doc_id prefixes and names a doc
// that known holds.
func (p *Profile) DocRefProblems(name, docID string, known func(docID string) bool) []string {
	errs := Field{Kind: KindString, Prefixes: p.DocIDPrefixes}.Validate(name, docID)
	if len(errs) == 0 && !known(docID) {
		errs = append(errs, NoDoc(name, docID))
	}
	return errs
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
	fields := jsonx.New()
	for _, f := range p.Settable() {
		fields.Set(f.Name, f.Contract())
	}
	o := jsonx.New("contract_version", 1, "name", p.Name)
	o.Merge(p.AsDict())
	o.Set("task_fields", slices.Clone(p.TaskFields))
	o.Set("fields", fields)
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
		rhs, _, _ := strings.Cut(m[2], "#")
		field := m[1]
		tokens := map[string]bool{}
		for tok := range strings.SplitSeq(rhs, "|") {
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

// projectIndex is where a labs project's index note lives.
const projectIndex = "01 Projects/*/index.md"

// Labs is the Severino Labs ops profile. HQ commits Labs.AsDict().
var Labs = &Profile{
	Name: "labs",
	DocTypes: []string{"runbook", "architecture_note", "deployment_guide", "troubleshooting_guide",
		"recovery_procedure", "public_article_draft", "decision_record", "task", "queue"},
	Environments:       []string{"homelab", "vps", "wordpress", "cloudflare", "tailscale", "adguard", "unifi", "local_mac", "other"},
	Statuses:           []string{"draft", "active", "deprecated", "archived"},
	Sensitivities:      []string{"public", "internal", "sensitive", "restricted"},
	DocIDPrefixes:      []string{"rb-", "infra-", "report-", "project-", "note-", "task-", QueueDocIDPrefix},
	RequiredFields:     []string{"doc_id", "title", "doc_type", "system", "environment", "status", "sensitivity"},
	TaskStatuses:       universalTaskStatuses,
	TaskRequiredFields: []string{"doc_id", "title", "doc_type", "status"},
	TaskFields:         []string{"status", "related_projects", "effort", "priority", "created", "closed"},
	Fields: []Field{
		{Name: "project_path", Kind: KindPath, Scope: projectIndex, Example: "~/Code/Projects/example",
			Help: "Where the project's working copy lives on this machine."},
		{Name: "repos", Kind: KindList, Scope: projectIndex, Unique: true, Pattern: `[^/\\]+`,
			Shape: `a bare repository directory name, with no "/"`, Example: "example-repo",
			Help: "Repository directory names whose sessions use this project's work queue (see the queue tool)."},
		{Name: "queue_prefix", Kind: KindString, Scope: projectIndex, Unique: true, Pattern: `[a-z][a-z0-9]{0,11}`,
			Shape: "lowercase letters and digits, starting with a letter, at most 12 characters", Reserved: []string{"backlog"},
			Example: "hq", Help: "The id prefix of this project's work queue items."},
		{Name: "superseded_by", Kind: KindString, DocRef: true, Example: "report-example-audit-2026-10-01",
			Help: "The doc_id of the doc that replaces this one; the older report of a series carries it with status: deprecated."},
	},
	DocumentSchemas: map[string]DocumentSchema{
		// A project's work queue note (the queue tool's Queue.md).
		QueueDocType: {Fields: map[string]Field{"doc_id": {Kind: KindString, Required: true, Prefixes: []string{QueueDocIDPrefix}}}},
	},
}

// QueueDocType and QueueDocIDPrefix mark a work queue note in the labs profile.
const (
	QueueDocType     = "queue"
	QueueDocIDPrefix = "queue-"
)

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
	Fields: []Field{
		{Name: "course", Kind: KindString, Example: "CS6035", Help: "The course code a note, assignment or resource belongs to."},
		{Name: "code", Kind: KindString, Example: "CS6035", Help: "A course's own code."},
		{Name: "term", Kind: KindString, Example: "Fall 2026", Help: "When a course is taken."},
		{Name: "track", Kind: KindString, Help: "The degree requirement a course fills."},
		{Name: "credits", Kind: KindInteger, Help: "A course's credit hours."},
		{Name: "grade", Kind: KindString, Help: "A finished course's grade."},
	},
	DocumentSchemas: map[string]DocumentSchema{},
}

// Builtin are the profiles built into the server, by the vault that uses
// each.
var Builtin = map[string]*Profile{"labs": Labs, "edu": Education}

// strs reads a contract list of strings.
func strs(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return slices.Clone(x), true
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// fieldFromContract reads one field rule; where names it in errors.
func fieldFromContract(where string, ro *jsonx.Obj) (Field, error) {
	field := Field{Kind: ro.Str("kind"), Required: ro.Bool("required"), Pattern: ro.Str("pattern"), Shape: ro.Str("shape"),
		Scope: ro.Str("scope"), Unique: ro.Bool("unique"), Immutable: ro.Bool("immutable"), Touch: ro.Bool("touch"),
		DocRef: ro.Bool("doc_ref"), Example: ro.Str("example"), Help: ro.Str("help")}
	if field.Kind == "" {
		field.Kind = KindString
	}
	for _, l := range []struct {
		key string
		dst *[]string
	}{{"choices", &field.Choices}, {"task_choices", &field.TaskChoices}, {"prefixes", &field.Prefixes}, {"reserved", &field.Reserved}} {
		v, ok := ro.Get(l.key)
		if !ok {
			continue
		}
		// Choices may be numbers in a hand-written contract; they compare as text.
		if arr, isArr := v.([]any); isArr {
			for _, item := range arr {
				*l.dst = append(*l.dst, pystr.Str(item))
			}
			continue
		}
		s, isList := strs(v)
		if !isList {
			return field, fmt.Errorf("%s.%s must be a list", where, l.key)
		}
		*l.dst = s
	}
	for _, bound := range []struct {
		key string
		dst **int
	}{{"minimum", &field.Minimum}, {"maximum", &field.Maximum}} {
		if v, ok := ro.Get(bound.key); ok {
			n, err := strconv.Atoi(fmt.Sprint(v))
			if err != nil {
				return field, fmt.Errorf("%s.%s must be an integer", where, bound.key)
			}
			*bound.dst = &n
		}
	}
	return field, nil
}

// FromContract builds a profile from its contract (ContractDict's shape).
// The contract's fields list repeats the shared fields, which the profile
// derives from its sets; only its own fields are read from it.
func FromContract(c *jsonx.Obj) (*Profile, error) {
	list := func(k string) ([]string, error) {
		v, ok := c.Get(k)
		if !ok {
			return nil, fmt.Errorf("profile contract is missing %s", k)
		}
		if _, isList := v.([]any); !isList {
			if _, isStrs := v.([]string); !isStrs {
				return nil, fmt.Errorf("profile contract %s must be a list", k)
			}
		}
		out, ok := strs(v)
		if !ok {
			return nil, fmt.Errorf("profile contract %s must hold strings", k)
		}
		return out, nil
	}
	p := &Profile{Name: c.Str("name"), DocumentSchemas: map[string]DocumentSchema{}}
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
	if fv, ok := c.Get("fields"); ok {
		fo, isObj := fv.(*jsonx.Obj)
		if !isObj {
			return nil, fmt.Errorf("profile contract fields must be an object")
		}
		shared := map[string]bool{}
		for _, f := range p.shared() {
			shared[f.Name] = true
		}
		for _, name := range fo.Keys() {
			if shared[name] {
				continue
			}
			rv, _ := fo.Get(name)
			ro, ok := rv.(*jsonx.Obj)
			if !ok {
				return nil, fmt.Errorf("fields.%s must be an object", name)
			}
			field, err := fieldFromContract("fields."+name, ro)
			if err != nil {
				return nil, err
			}
			field.Name, field.Required = name, false
			p.Fields = append(p.Fields, field)
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
				field, err := fieldFromContract("document_schemas."+docType+"."+name, ro)
				if err != nil {
					return nil, err
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
