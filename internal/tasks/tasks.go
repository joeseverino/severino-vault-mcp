// Package tasks is the vault's task ledger. A task is a doc_type: task doc;
// the board derives from the index and enriches each task with the profile
// fields (effort, priority, created, closed) read on demand. Writes go
// through the one serializer and atomic writer.
package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/contracts"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// Folder layout.
const (
	ProjectsDir = "01 Projects"
	BacklogDir  = "07 Backlog"
	Cross       = "cross"
)

var (
	efforts       = []string{"L", "M", "S"}
	priorities    = []string{"high", "low", "med"}
	statusOrder   = map[string]int{"active": 0, "open": 1, "parked": 2, "done": 3, "wontfix": 4}
	priorityOrder = map[string]int{"high": 0, "med": 1, "low": 2, "": 3}
	live          = map[string]bool{"open": true, "active": true}
	closed        = map[string]bool{"done": true, "wontfix": true}
)

func fail(msg string) *jsonx.Obj { return fsx.Fail(msg) }

func filedDir(currentDir, status string) string {
	base := currentDir
	if filepath.Base(currentDir) == "done" {
		base = filepath.Dir(currentDir)
	}
	if closed[status] {
		return filepath.Join(base, "done")
	}
	return base
}

func projectOf(relativePath string) string {
	parts := strings.Split(relativePath, "/")
	if len(parts) >= 2 && parts[0] == ProjectsDir {
		return parts[1]
	}
	return Cross
}

func ageDays(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return int(time.Since(info.ModTime()).Hours() / 24)
}

type record struct {
	o        *jsonx.Obj
	status   string
	priority string
	created  string
	closed   string
	docID    string
	project  string
	related  []string
	stale    bool
	slug     string
}

func strField(fm *jsonx.Obj, key string) string {
	if fm == nil {
		return ""
	}
	v, _ := fm.Get(key)
	if !pystr.Truthy(v) {
		return ""
	}
	return pystr.Str(v)
}

func taskRecord(d *vault.Doc, staleDays int) record {
	fm, _ := frontmatter.Read(d.Path)
	age := ageDays(d.Path)
	status := d.Status
	if status == "" {
		status = "open"
	}
	r := record{
		status:   status,
		priority: strField(fm, "priority"),
		created:  strField(fm, "created"),
		closed:   strField(fm, "closed"),
		docID:    d.DocID,
		project:  projectOf(d.RelativePath),
		related:  slices.Clone(d.RelatedProjects),
		stale:    live[status] && age > staleDays,
		slug:     strings.TrimPrefix(d.DocID, "task-"),
	}
	r.o = jsonx.New(
		"doc_id", d.DocID,
		"slug", r.slug,
		"title", d.Title,
		"status", status,
		"project", r.project,
		"related_projects", r.related,
		"effort", strField(fm, "effort"),
		"priority", r.priority,
		"created", r.created,
		"closed", r.closed,
		"tags", slices.Clone(d.Tags),
		"relative_path", d.RelativePath,
		"age_days", age,
		"stale", r.stale,
	)
	return r
}

func orderOr(m map[string]int, k string) int {
	if v, ok := m[k]; ok {
		return v
	}
	return 99
}

func compareRecords(a, b record) int {
	if c := orderOr(statusOrder, a.status) - orderOr(statusOrder, b.status); c != 0 {
		return c
	}
	if c := orderOr(priorityOrder, a.priority) - orderOr(priorityOrder, b.priority); c != 0 {
		return c
	}
	ac, bc := a.created, b.created
	if ac == "" {
		ac = "9999"
	}
	if bc == "" {
		bc = "9999"
	}
	if c := strings.Compare(ac, bc); c != 0 {
		return c
	}
	return strings.Compare(a.docID, b.docID)
}

// Filter narrows the board.
type Filter struct {
	Status      string // "" = any
	Project     string // "" = any
	StaleOnly   bool
	IncludeAll  bool
	StaleDays   int
	ShippedDays int
}

func taskDocs(idx *vault.Index) []*vault.Doc {
	var out []*vault.Doc
	for _, d := range idx.Docs {
		if d.DocType == "task" {
			out = append(out, d)
		}
	}
	return out
}

// List is the board: every task, filtered and ranked, plus whole-ledger
// counts and the recently shipped feed.
func List(l *vault.Loader, f Filter) *jsonx.Obj {
	if f.StaleDays == 0 {
		f.StaleDays = 14
	}
	if f.ShippedDays == 0 {
		f.ShippedDays = 7
	}
	idx := l.Index(false)
	var all []record
	for _, d := range taskDocs(idx) {
		all = append(all, taskRecord(d, f.StaleDays))
	}
	statusCounts, projectCounts := jsonx.New(), jsonx.New()
	stale := 0
	for _, t := range all {
		n, _ := statusCounts.Get(t.status)
		statusCounts.Set(t.status, toInt(n)+1)
		n, _ = projectCounts.Get(t.project)
		projectCounts.Set(t.project, toInt(n)+1)
		if t.stale {
			stale++
		}
	}
	inProject := func(t record) bool {
		return f.Project == "" || t.project == f.Project || slices.Contains(t.related, f.Project)
	}
	visible := func(t record) bool {
		if f.Status != "" {
			if t.status != f.Status {
				return false
			}
		} else if !f.IncludeAll && !live[t.status] {
			return false
		}
		if f.StaleOnly && !t.stale {
			return false
		}
		return inProject(t)
	}
	var shown []record
	for _, t := range all {
		if visible(t) {
			shown = append(shown, t)
		}
	}
	slices.SortStableFunc(shown, compareRecords)

	today, _ := time.ParseInLocation("2006-01-02", write.Today(), time.Local)
	var shipped []record
	for _, t := range all {
		if t.status != "done" || t.closed == "" || !inProject(t) {
			continue
		}
		c := t.closed
		if len(c) > 10 {
			c = c[:10]
		}
		closedOn, err := time.ParseInLocation("2006-01-02", c, time.Local)
		if err != nil {
			continue
		}
		days := int(today.Sub(closedOn).Hours() / 24)
		if days >= 0 && days <= f.ShippedDays {
			shipped = append(shipped, t)
		}
	}
	slices.SortStableFunc(shipped, func(a, b record) int { return strings.Compare(b.closed, a.closed) })

	return jsonx.New(
		"ok", true,
		"stale_days", f.StaleDays,
		"shipped_days", f.ShippedDays,
		"count", len(shown),
		"total", len(all),
		"counts", jsonx.New("status", statusCounts, "project", projectCounts, "stale", stale),
		"tasks", objs(shown),
		"shipped", objs(shipped),
	)
}

func toInt(v any) int {
	n, _ := v.(int)
	return n
}

func objs(rs []record) []*jsonx.Obj {
	out := []*jsonx.Obj{}
	for _, r := range rs {
		out = append(out, r.o)
	}
	return out
}

// Projects lists every 01 Projects/<project>/ folder with its live task count.
func Projects(l *vault.Loader) *jsonx.Obj {
	idx := l.Index(false)
	open := map[string]int{}
	for _, d := range taskDocs(idx) {
		status := d.Status
		if status == "" {
			status = "open"
		}
		if !live[status] {
			continue
		}
		if p := projectOf(d.RelativePath); p != Cross {
			open[p]++
		}
	}
	entries, err := os.ReadDir(filepath.Join(l.Config.VaultPath, ProjectsDir))
	var names []string
	if err == nil {
		for _, e := range entries {
			if isDir(filepath.Join(l.Config.VaultPath, ProjectsDir, e.Name())) {
				names = append(names, e.Name())
			}
		}
	}
	slices.Sort(names)
	projects := []*jsonx.Obj{}
	for _, name := range names {
		projects = append(projects, jsonx.New("slug", name, "open", open[name]))
	}
	return jsonx.New("ok", true, "count", len(projects), "projects", projects)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func slugify(title string) string {
	var sb strings.Builder
	for _, ch := range strings.ToLower(title) {
		if pystr.IsAlnum(ch) {
			sb.WriteRune(ch)
		} else {
			sb.WriteByte('-')
		}
	}
	slug := sb.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	return strings.Trim(pystr.Prefix(slug, 60), "-")
}

const templateBody = "**Problem.** \n\n**Fix.** \n\n**Principle.** \n\n**Source.** \n"

// New is a task to create.
type New struct {
	Title           string
	Project         string
	RelatedProjects []string
	Effort          string
	Priority        string
	Tags            []string
	Body            string
}

func create(l *vault.Loader, n New) *jsonx.Obj {
	title := pystr.Strip(n.Title)
	if title == "" {
		return fail("title is required")
	}
	if !slices.Contains(efforts, n.Effort) {
		return fail(fmt.Sprintf("effort %s not in %s", pystr.Repr(n.Effort), pystr.ReprList(efforts)))
	}
	if !slices.Contains(priorities, n.Priority) {
		return fail(fmt.Sprintf("priority %s not in %s", pystr.Repr(n.Priority), pystr.ReprList(priorities)))
	}
	slug := slugify(title)
	if slug == "" {
		return fail("title produced an empty slug")
	}
	docID := "task-" + slug
	vaultPath := l.Config.VaultPath
	v, verr := fsx.OpenVault(vaultPath)
	if verr != nil {
		return fail("write failed: " + verr.Error())
	}
	defer v.Close()
	project := n.Project
	if project == "" && len(n.RelatedProjects) == 1 && v.IsDir(filepath.Join(vaultPath, ProjectsDir, n.RelatedProjects[0])) {
		project = n.RelatedProjects[0]
	}
	var targetDir string
	var related []string
	if project != "" {
		projectDir := filepath.Join(vaultPath, ProjectsDir, project)
		if !v.IsDir(projectDir) {
			return fail(fmt.Sprintf("no such project: %s (expected %s/%s/)", pystr.Repr(project), ProjectsDir, project))
		}
		targetDir = filepath.Join(projectDir, "tasks")
		related = n.RelatedProjects
		if len(related) == 0 {
			related = []string{project}
		}
		if !slices.Contains(related, project) {
			related = append([]string{project}, related...)
		}
	} else {
		targetDir = filepath.Join(vaultPath, BacklogDir)
		related = n.RelatedProjects
		if related == nil {
			related = []string{}
		}
	}
	idx := l.Index(false)
	if existing, ok := idx.ByDocID[docID]; ok {
		return fail(fmt.Sprintf("doc_id %s already exists at %s", pystr.Repr(docID), existing.RelativePath))
	}
	filePath := filepath.Join(targetDir, docID+".md")
	if _, err := v.Stat(filePath); err == nil {
		return fail("file already exists: " + filepath.Base(filePath))
	}
	tags := n.Tags
	if len(tags) == 0 {
		tags = []string{"backlog"}
	}
	fm := jsonx.New(
		"doc_id", docID,
		"title", title,
		"doc_type", "task",
		"status", "open",
		"related_projects", anyList(related),
		"effort", n.Effort,
		"priority", n.Priority,
		"created", write.Today(),
		"tags", anyList(tags),
	)
	if err := v.MkdirAll(targetDir); err != nil {
		return fail("write failed: " + err.Error())
	}
	if err := v.AtomicCreate(filePath, frontmatter.Serialize(fm)+"# "+title+"\n\n"+n.Body); err != nil {
		return fail("write failed: " + err.Error())
	}
	l.Index(true)
	rel, _ := filepath.Rel(vaultPath, filePath)
	proj := project
	if proj == "" {
		proj = Cross
	}
	result := jsonx.New("ok", true, "doc_id", docID, "relative_path", rel, "project", proj, "status", "open")
	result.Set("receipt", contracts.Receipt{
		Operation:           "task.create",
		EntityType:          "task",
		EntityID:            docID,
		ChangedFields:       fm.Keys(),
		AfterFingerprint:    contracts.Fingerprint(fm),
		AffectedProjections: []string{"task_board", "daily_progress", "brief"},
		Metadata:            jsonx.New("relative_path", rel),
	}.AsDict())
	return result
}

func anyList(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

// Sections fill a new task's body at write time.
type Sections struct{ Problem, Fix, Principle, Source string }

// Add files a task in its project's tasks/ folder, or the backlog bucket.
func Add(l *vault.Loader, n New, s Sections) *jsonx.Obj {
	if n.Effort == "" {
		n.Effort = "S"
	}
	if n.Priority == "" {
		n.Priority = "med"
	}
	n.Body = fmt.Sprintf("**Problem.** %s\n\n**Fix.** %s\n\n**Principle.** %s\n\n**Source.** %s\n", s.Problem, s.Fix, s.Principle, s.Source)
	return create(l, n)
}

// Promote turns a captured note into a task, keeping its body, then deletes
// the note.
func Promote(l *vault.Loader, source string, n New) *jsonx.Obj {
	v, verr := fsx.OpenVault(l.Config.VaultPath)
	if verr != nil {
		return fail("read failed: " + verr.Error())
	}
	defer v.Close()
	src := filepath.Join(l.Config.VaultPath, source)
	if info, err := v.Stat(src); err != nil || !info.Mode().IsRegular() {
		return fail("no such note: " + source)
	}
	text, err := v.ReadText(src)
	if err != nil {
		return fail("read failed: " + err.Error())
	}
	_, body, _ := frontmatter.Split(text)
	if n.Effort == "" {
		n.Effort = "S"
	}
	if n.Priority == "" {
		n.Priority = "med"
	}
	n.RelatedProjects, n.Tags = nil, nil
	if b := pystr.Strip(body); b != "" {
		n.Body = b + "\n"
	} else {
		n.Body = templateBody
	}
	result := create(l, n)
	if !result.Bool("ok") {
		return result
	}
	if err := v.Remove(src); err != nil {
		return fail("task created but source delete failed: " + err.Error())
	}
	l.Index(true)
	result.Set("promoted_from", source)
	return result
}

func resolve(l *vault.Loader, docID string) (*vault.Doc, *jsonx.Obj) {
	idx := l.Index(false)
	d, ok := idx.ByDocID[docID]
	if !ok {
		d, ok = idx.ByDocID["task-"+docID]
	}
	if !ok {
		return nil, fail("no task matches: " + pystr.Repr(docID))
	}
	if d.DocType != "task" {
		return nil, fail(fmt.Sprintf("%s is not a task (doc_type %s)", pystr.Repr(d.DocID), d.DocType))
	}
	return d, nil
}

// SetStatus moves a task; done stamps closed:, reopening clears it, and the
// file moves into or out of done/.
func SetStatus(l *vault.Loader, docID, status string) *jsonx.Obj {
	if !schema.Labs.HasTaskStatus(status) {
		return fail(fmt.Sprintf("status %s not in %s", pystr.Repr(status), pystr.ReprList(schema.Sorted(schema.Labs.TaskStatuses))))
	}
	d, err := resolve(l, docID)
	if err != nil {
		return err
	}
	v, verr := fsx.OpenVault(l.Config.VaultPath)
	if verr != nil {
		return fail("write failed: " + verr.Error())
	}
	defer v.Close()
	text, rerr := v.ReadText(d.Path)
	if rerr != nil {
		return fail("write failed: " + rerr.Error())
	}
	fm, body, _ := frontmatter.Split(text)
	if fm == nil {
		return fail("task file has no frontmatter")
	}
	prevV, _ := fm.Get("status")
	previous := ""
	if pystr.Truthy(prevV) {
		previous = pystr.Str(prevV)
	}
	fm.Set("status", status)
	if status == "done" {
		fm.Set("closed", write.Today())
	} else {
		fm.Delete("closed")
	}
	target := filedDir(filepath.Dir(d.Path), status)
	final := filepath.Join(target, filepath.Base(d.Path))
	if err := v.MkdirAll(target); err != nil {
		return fail("write failed: " + err.Error())
	}
	if err := v.AtomicWrite(final, frontmatter.Serialize(fm)+body); err != nil {
		return fail("write failed: " + err.Error())
	}
	if final != d.Path {
		if err := v.Remove(d.Path); err != nil {
			return fail("write failed: " + err.Error())
		}
	}
	l.Index(true)
	rel, _ := filepath.Rel(l.Config.VaultPath, final)
	return jsonx.New("ok", true, "doc_id", d.DocID, "relative_path", rel, "status", status, "previous", previous)
}

// Reconcile re-homes every task into the folder its status implies.
func Reconcile(l *vault.Loader) *jsonx.Obj {
	idx := l.Index(false)
	moved := 0
	v, err := fsx.OpenVault(l.Config.VaultPath)
	if err != nil {
		return fail("reconcile failed: " + err.Error())
	}
	defer v.Close()
	for _, d := range taskDocs(idx) {
		status := d.Status
		if status == "" {
			status = "open"
		}
		target := filedDir(filepath.Dir(d.Path), status)
		if filepath.Dir(d.Path) == target {
			continue
		}
		if v.MkdirAll(target) != nil {
			continue
		}
		if v.Rename(d.Path, filepath.Join(target, filepath.Base(d.Path))) == nil {
			moved++
		}
	}
	if moved > 0 {
		l.Index(true)
	}
	return jsonx.New("ok", true, "moved", moved)
}

// Delete removes a task file permanently (mistakes and junk only).
func Delete(l *vault.Loader, docID string) *jsonx.Obj {
	d, err := resolve(l, docID)
	if err != nil {
		return err
	}
	v, verr := fsx.OpenVault(l.Config.VaultPath)
	if verr != nil {
		return fail("delete failed: " + verr.Error())
	}
	defer v.Close()
	if err := v.Remove(d.Path); err != nil {
		return fail("delete failed: " + err.Error())
	}
	l.Index(true)
	return jsonx.New("ok", true, "doc_id", d.DocID, "relative_path", d.RelativePath, "deleted", true)
}
