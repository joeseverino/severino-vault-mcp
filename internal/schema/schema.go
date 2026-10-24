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
	// DocTypes are the doc_types whose docs may carry the field; empty is
	// every doc_type.
	DocTypes []string
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
	return fmt.Sprintf("%s only applies to docs at %q, not %s", f.Name, f.Scope, rel)
}

// ForDocType reports whether a doc of docType may carry the field.
func (f Field) ForDocType(docType string) bool {
	return len(f.DocTypes) == 0 || slices.Contains(f.DocTypes, docType)
}

// DocTypeError is the refusal for a field set on a doc of a doc_type that
// may not carry it.
func (f Field) DocTypeError(docType string) string {
	return fmt.Sprintf("%s only applies to doc_type %s, not %q", f.Name, pystr.Quoted(Sorted(f.DocTypes)), docType)
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
		errs = append(errs, fmt.Sprintf("%s %q must start with one of %s", name, text, pystr.Quoted(f.Prefixes)))
	}
	if f.Pattern != "" {
		if re, err := regexp.Compile(`\A(?:` + f.Pattern + `)\z`); err == nil && !re.MatchString(text) {
			if f.Shape != "" {
				errs = append(errs, fmt.Sprintf("%s %s must be %s", name, jsonx.Compact(value), f.Shape))
			} else {
				errs = append(errs, fmt.Sprintf("%s=%s does not match %q", name, jsonx.Compact(value), f.Pattern))
			}
		}
	}
	if slices.Contains(f.Reserved, text) {
		errs = append(errs, fmt.Sprintf("%s %q is reserved; pick another", name, text))
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
	}{{"choices", f.Choices, true}, {"task_choices", f.TaskChoices, true}, {"prefixes", f.Prefixes, false}, {"doc_types", f.DocTypes, true}} {
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

// DocumentSchema is the field contract for one doc_type. RequiredFields,
// when set, replace the profile's RequiredFields for docs of the type: a
// field the profile requires that the type does not is neither required nor
// defaulted by a create.
type DocumentSchema struct {
	Fields         map[string]Field
	RequiredFields []string
}

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
	// DocTypeFolders name, for a doc_type, the folder its docs live in: a doc
	// of the type sits under a folder of that name, and a doc under one is of
	// the type. Check holds live docs to it; the archive keeps docs where
	// they were archived from.
	DocTypeFolders map[string]string
}

// PlacementProblems are the problems with where a doc of docType sits at
// rel: outside the folder its type lives in, or inside one that holds only
// another type's docs. The wording leaves out rel, so a move that keeps
// the problem does not count it as new.
func (p *Profile) PlacementProblems(docType, rel string) []string {
	dirs := strings.Split(path.Dir(filepath.ToSlash(path.Clean(filepath.ToSlash(rel)))), "/")
	var errs []string
	for _, t := range slices.Sorted(mapKeys(p.DocTypeFolders)) {
		folder := p.DocTypeFolders[t]
		inside := slices.Contains(dirs, folder)
		switch {
		case t == docType && !inside:
			errs = append(errs, fmt.Sprintf("a doc_type %q doc lives in a %q folder; move it into one", t, folder))
		case t != docType && inside:
			errs = append(errs, fmt.Sprintf("a %q folder holds only doc_type %q docs, not %q; move the doc out or make it a %s",
				folder, t, docType, t))
		}
	}
	return errs
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

// Task effort and priority values, for a profile that declares doc_type
// task (a provider's).
var (
	Efforts    = []string{"L", "M", "S"}
	Priorities = []string{"high", "low", "med"}
)

// shared are the fields every profile carries, their enums taken from the
// profile's sets.
func (p *Profile) shared() []Field {
	fields := []Field{
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
	}
	if !p.HasDocType(taskDocType) {
		return fields
	}
	return append(fields,
		Field{Name: "effort", Kind: KindEnum, Choices: Efforts, Help: "A task's size."},
		Field{Name: "priority", Kind: KindEnum, Choices: Priorities, Help: "A task's priority."},
		Field{Name: "created", Kind: KindDate, Help: "When a task was filed."},
		Field{Name: "closed", Kind: KindDate, Help: "When a task was finished."},
	)
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
	return fmt.Sprintf("%s %q not in %s", field, value, pystr.Quoted(allowed))
}

// taskDocType is the doc_type a task carries. Labs and edu track work in
// the queue instead; a provider's profile may still declare tasks, with
// their own lifecycle (TaskStatuses) and fields (TaskFields).
const taskDocType = "task"

// IsTask reports a doc_type: task block in a profile that declares tasks;
// in any other profile the block is a doc with an unknown doc_type.
func (p *Profile) IsTask(fm *jsonx.Obj) bool {
	if !p.HasDocType(taskDocType) {
		return false
	}
	dt, _ := fm.Get("doc_type")
	return pystr.Str(orEmpty(dt)) == taskDocType
}

// docOnly reports a field a doc requires and a task does not carry, which a
// task's block is not held to (environment, sensitivity).
func (p *Profile) docOnly(name string) bool {
	return slices.Contains(p.RequiredFields, name) && !slices.Contains(p.TaskRequiredFields, name) && !slices.Contains(p.TaskFields, name)
}

// RequiredFor is the field set a doc of docType must carry: a task's own
// set for tasks, the doc_type's own where its document schema names one,
// and the profile's otherwise.
func (p *Profile) RequiredFor(docType string) []string {
	if docType == taskDocType && p.HasDocType(taskDocType) {
		return p.TaskRequiredFields
	}
	if ds, ok := p.DocumentSchemas[docType]; ok && len(ds.RequiredFields) > 0 {
		return ds.RequiredFields
	}
	return p.RequiredFields
}

// Omits reports a field docType's own required set leaves out, which a
// create does not default.
func (p *Profile) Omits(docType, name string) bool {
	ds, ok := p.DocumentSchemas[docType]
	return ok && len(ds.RequiredFields) > 0 && !slices.Contains(ds.RequiredFields, name)
}

// ownRequired are the doc_types whose document schema names its own
// required set, sorted.
func (p *Profile) ownRequired() []string {
	var out []string
	for _, name := range slices.Sorted(mapKeys(p.DocumentSchemas)) {
		if len(p.DocumentSchemas[name].RequiredFields) > 0 {
			out = append(out, name)
		}
	}
	return out
}

// Check validates a whole frontmatter block at rel with the rules
// set_frontmatter writes by: required fields (RequiredFor the block's
// doc_type), every declared field's rule, path scope and doc_types (a task
// skips the doc-only fields), the per-doc-type field rules, and the doc's
// placement (PlacementProblems).
func (p *Profile) Check(fm *jsonx.Obj, rel string) []string {
	task := p.IsTask(fm)
	dt, _ := fm.Get("doc_type")
	docType := pystr.Str(orEmpty(dt))
	var errs []string
	for _, name := range p.RequiredFor(docType) {
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
		if !f.ForDocType(docType) {
			errs = append(errs, f.DocTypeError(docType))
		}
	}
	errs = append(errs, p.ValidateDocument(fm)...)
	return append(errs, p.PlacementProblems(docType, rel)...)
}

// ArchivedStatus is the status a doc carries in the archive;
// ClosedTaskStatuses are the task statuses that go there as they are, since
// archived is not a task status and a closed task is already history.
const ArchivedStatus = "archived"

var ClosedTaskStatuses = []string{"done", "wontfix"}

// ArchiveRequiredFields are the fields a note in the archive carries: enough
// to be found and read, since the archive keeps a doc's frontmatter as it was.
var ArchiveRequiredFields = []string{"doc_id", "title", "doc_type", "status"}

// Archivable reports whether a block may stand in the archive: status:
// archived, or a closed task.
func (p *Profile) Archivable(fm *jsonx.Obj) bool {
	status := fm.Text("status")
	if p.IsTask(fm) {
		return slices.Contains(ClosedTaskStatuses, status)
	}
	return status == ArchivedStatus
}

// ArchiveProblems are the problem with a block that may not stand in the
// archive dir archive, none when it may.
func (p *Profile) ArchiveProblems(fm *jsonx.Obj, archive string) []string {
	if p.Archivable(fm) {
		return nil
	}
	if p.IsTask(fm) {
		return []string{fmt.Sprintf("a task in %s is closed (status %s), not %q", archive, strings.Join(ClosedTaskStatuses, " or "), fm.Text("status"))}
	}
	return []string{fmt.Sprintf("a doc in %s has status: %s, not %q; set status %s in the same call", archive, ArchivedStatus, fm.Text("status"), ArchivedStatus)}
}

// CheckArchived validates a block in the archive dir archive:
// ArchiveRequiredFields, every declared field's rule but not its path scope
// (a doc keeps the fields it carried where it lived), the per-doc-type
// field rules, and Archivable.
func (p *Profile) CheckArchived(fm *jsonx.Obj, archive string) []string {
	task := p.IsTask(fm)
	var errs []string
	for _, name := range ArchiveRequiredFields {
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
	}
	errs = append(errs, p.ArchiveProblems(fm, archive)...)
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
				errs = append(errs, fmt.Sprintf("%s %q names the doc itself", f.Name, id))
			case !known(id):
				errs = append(errs, NoDoc(f.Name, id))
			}
		}
	}
	return errs
}

// ProjectsField is the frontmatter field whose entries name project folders.
const ProjectsField = "related_projects"

// NoProject is the one wording of a related_projects entry that names no
// project folder, with the folder names closest to it.
func NoProject(entry string, closest []string) string {
	return fmt.Sprintf("%s %q names no project folder (closest: %s)", ProjectsField, entry, strings.Join(closest, ", "))
}

// closestProjects are the n names of projects nearest entry by edit
// distance, ignoring case, then alphabetically.
func closestProjects(entry string, projects []string, n int) []string {
	lower := strings.ToLower(entry)
	out := slices.Clone(projects)
	slices.SortStableFunc(out, func(a, b string) int {
		return pystr.EditDistance(lower, strings.ToLower(a)) - pystr.EditDistance(lower, strings.ToLower(b))
	})
	return out[:min(n, len(out))]
}

// ProjectProblems checks every related_projects entry fm carries against the
// project folder names projects, case-sensitively. Check is the per-file
// half; validate, doctor and every write add this one, which needs the
// vault's folders. No projects (a vault without a projects folder) checks
// nothing.
func (p *Profile) ProjectProblems(fm *jsonx.Obj, projects []string) []string {
	if len(projects) == 0 {
		return nil
	}
	v, _ := fm.Get(ProjectsField)
	if missing(v) {
		return nil
	}
	items, isList := v.([]any)
	if !isList {
		items = []any{v}
	}
	var errs []string
	for _, item := range items {
		if entry := pystr.Str(item); !slices.Contains(projects, entry) {
			errs = append(errs, NoProject(entry, closestProjects(entry, projects, 3)))
		}
	}
	return errs
}

// NoDoc is the one wording of a doc reference that names no doc.
func NoDoc(name, docID string) string {
	return fmt.Sprintf("%s %q names no doc in the vault", name, docID)
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

// AsDict is the shape schema --json prints and HQ commits: sets sorted,
// ordered fields as declared.
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
	if own := p.ownRequired(); len(own) > 0 {
		required := jsonx.New()
		for _, name := range own {
			required.Set(name, slices.Clone(p.DocumentSchemas[name].RequiredFields))
		}
		o.Set("doc_type_required_fields", required)
	}
	if len(p.DocTypeFolders) > 0 {
		folders := jsonx.New()
		for _, t := range slices.Sorted(mapKeys(p.DocTypeFolders)) {
			folders.Set(t, p.DocTypeFolders[t])
		}
		o.Set("doc_type_folders", folders)
	}
	return o
}

// Fingerprint is the SHA-256 of the sorted, compact contract.
func (p *Profile) Fingerprint() string {
	sum := sha256.Sum256([]byte(jsonx.Compact(jsonx.Sorted(p.ContractDict()))))
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
			parts = append(parts, fmt.Sprintf("doc lists unknown %s", pystr.Quoted(extra)))
		}
		if missing := diff(canon, doc); len(missing) > 0 {
			parts = append(parts, fmt.Sprintf("doc is missing %s", pystr.Quoted(missing)))
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

// projectIndex is where a labs project's index note lives.
const projectIndex = "01 Projects/*/index.md"

// Labs is the Severino Labs ops profile. HQ commits Labs.AsDict().
var Labs = &Profile{
	Name: "labs",
	DocTypes: []string{"runbook", "architecture_note", "deployment_guide", "troubleshooting_guide",
		"recovery_procedure", "public_article_draft", "decision_record", QueueDocType, ReportDocType},
	Environments:   []string{"homelab", "vps", "wordpress", "cloudflare", "tailscale", "adguard", "unifi", "local_mac", "other"},
	Statuses:       []string{"draft", "active", "deprecated", "archived"},
	Sensitivities:  []string{"public", "internal", "sensitive", "restricted"},
	DocIDPrefixes:  []string{"rb-", "infra-", "report-", "project-", "note-", "task-", QueueDocIDPrefix},
	RequiredFields: []string{"doc_id", "title", "doc_type", "system", "environment", "status", "sensitivity"},
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
		{Name: ReportKindField, Kind: KindEnum, Choices: ReportKinds, DocTypes: []string{ReportDocType}, Example: "audit",
			Help: "What run a report records."},
	},
	DocumentSchemas: map[string]DocumentSchema{
		// A project's work queue note (the queue tool's Queue.md): its folder
		// names the project and the queue tool owns the body, so it needs no
		// environment, sensitivity or relation lists.
		QueueDocType: {
			Fields:         map[string]Field{"doc_id": {Kind: KindString, Required: true, Prefixes: []string{QueueDocIDPrefix}}},
			RequiredFields: []string{"doc_id", "title", "doc_type", "system", "status"},
		},
		// A dated record of one run, written once and superseded by the
		// next in its series.
		ReportDocType: {
			Fields:         map[string]Field{},
			RequiredFields: []string{"doc_id", "title", "doc_type", "system", "environment", "status", "sensitivity", ReportKindField},
		},
	},
	DocTypeFolders: map[string]string{ReportDocType: "Reports"},
}

// QueueDocType and QueueDocIDPrefix mark a work queue note in the labs
// profile.
const (
	QueueDocType     = "queue"
	QueueDocIDPrefix = "queue-"
)

// ReportDocType marks a report in the labs profile: a dated record of one
// run (an audit, review, research pass, session or evaluation), written once
// and superseded by the next in its series. ReportKindField says which run,
// one of ReportKinds.
const (
	ReportDocType   = "report"
	ReportKindField = "kind"
)

// ReportKinds are the runs a report records.
var ReportKinds = []string{"audit", "review", "research", "pass", "session_log", "evaluation"}

// Education is the coursework profile.
var Education = &Profile{
	Name:           "education",
	DocTypes:       []string{"course", "course_note", "assignment", "resource"},
	Environments:   []string{"gatech", "cert", "other"},
	Statuses:       []string{"upcoming", "active", "completed", "dropped", "draft", "archived"},
	Sensitivities:  []string{"public", "internal"},
	DocIDPrefixes:  []string{"course-", "cnote-", "asg-", "res-"},
	RequiredFields: []string{"doc_id", "title", "doc_type", "status"},
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
	}{{"choices", &field.Choices}, {"task_choices", &field.TaskChoices}, {"prefixes", &field.Prefixes}, {"reserved", &field.Reserved}, {"doc_types", &field.DocTypes}} {
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
				return nil, fmt.Errorf("document schemas reference unknown doc_types: %q", docType)
			}
			p.DocumentSchemas[docType] = ds
		}
	}
	if rv, ok := c.Get("doc_type_required_fields"); ok {
		ro, isObj := rv.(*jsonx.Obj)
		if !isObj {
			return nil, fmt.Errorf("profile contract doc_type_required_fields must be an object")
		}
		for _, docType := range ro.Keys() {
			v, _ := ro.Get(docType)
			names, isList := strs(v)
			if !isList {
				return nil, fmt.Errorf("doc_type_required_fields.%s must be a list of strings", docType)
			}
			if !p.HasDocType(docType) {
				return nil, fmt.Errorf("doc_type_required_fields references unknown doc_types: %q", docType)
			}
			ds := p.DocumentSchemas[docType]
			ds.RequiredFields = names
			p.DocumentSchemas[docType] = ds
		}
	}
	if rv, ok := c.Get("doc_type_folders"); ok {
		ro, isObj := rv.(*jsonx.Obj)
		if !isObj {
			return nil, fmt.Errorf("profile contract doc_type_folders must be an object")
		}
		p.DocTypeFolders = map[string]string{}
		for _, docType := range ro.Keys() {
			v, _ := ro.Get(docType)
			folder, isStr := v.(string)
			if !isStr || folder == "" || strings.Contains(folder, "/") {
				return nil, fmt.Errorf("doc_type_folders.%s must be one folder name", docType)
			}
			if !p.HasDocType(docType) {
				return nil, fmt.Errorf("doc_type_folders references unknown doc_types: %q", docType)
			}
			p.DocTypeFolders[docType] = folder
		}
	}
	return p, nil
}
