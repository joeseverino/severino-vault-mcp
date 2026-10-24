package core

import (
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/queue"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// A project's work queue is one note, <project folder>/Queue.md, with an
// ## Open and a ## Closed section of item lines (internal/queue holds the
// grammar). Every change rewrites the note through write.EditBodyUnindexed,
// so it runs under the note's lock and re-reads the note before replacing
// it; ids are allocated and claims checked inside that locked edit.

// QueueActions are the queue tool's actions; the first is the default.
var QueueActions = []string{"list", "add", "set", "close", "promote"}

// Queue note layout.
const (
	queueFile      = "Queue.md"
	queueOpen      = "## Open"
	queueClosed    = "## Closed"
	backlogProject = "backlog"
	queueLimit     = 50
)

// defaultStatuses are what list shows when no status is given: the items
// still waiting on someone.
var defaultStatuses = []string{"open", "returned"}

// QueueArgs are the queue tool's arguments. Nil pointers and a nil Links are
// "not given"; a link given as "" is removed by set.
type QueueArgs struct {
	Action             string
	Project, Repo, Cwd string
	All                bool
	ID, Agent          string
	Kind               string
	Text, Owner        *string
	Links              map[string]string
	BlockedBy          []string
	HasBlockedBy       bool
	Status             string
	Ready              bool
	Contains           string
	Limit              int
	Claim              bool
	Resolution         string
	Content            string
	View
}

// QueueItemFields are the fields a queue item carries. owner is null when
// nobody holds the item; links holds the typed links given; blocked_by,
// body, alias, agent and resolution appear only when set.
var QueueItemFields = append(typed("id", tStr, "project", tStr, "kind", tStr, "status", tStr, "owner", tOptStr, "text", tStr),
	Field{Name: "links", Type: "object", Inner: typed("pr", tStr, "branch", tStr, "commit", tStr, "slug", tStr)},
	Field{Name: "blocked_by", Type: tList}, Field{Name: "body", Type: tStr}, Field{Name: "alias", Type: tStr},
	Field{Name: "agent", Type: tStr}, Field{Name: "resolution", Type: tStr})

var leanQueueItem = mustMask(QueueItemFields, "id", "kind", "status", "owner", "text")

func qcall(args ...any) string { return Call("queue", args...) }

// Queue runs one queue action against v.
func Queue(v *Vault, a QueueArgs) *jsonx.Obj {
	if v.Profile == nil {
		return failNext("vault "+pystr.Repr(v.Name)+" has no schema profile", noProfile)
	}
	s, err := openQueues(v)
	if err != nil {
		return fail("read failed: " + err.Error())
	}
	defer s.root.Close()
	switch a.Action {
	case "list":
		return s.list(a)
	case "add":
		return s.add(a)
	case "set":
		return s.set(a)
	case "close":
		return s.close(a)
	case "promote":
		return s.promote(a)
	}
	return failNext(fmt.Sprintf("unknown action %s; one of %s", pystr.Repr(a.Action), strings.Join(QueueActions, ", ")),
		qcall("action", "list", "all", true))
}

// ----- projects ------------------------------------------------------------

// queueProject is a folder that holds a queue: a project under 01 Projects,
// or the cross-cutting backlog.
type queueProject struct {
	name, dir, prefix string
	repos             []string
	problem           string // why this project's index.md cannot name its queue
}

func (p queueProject) rel() string   { return path.Join(p.dir, queueFile) }
func (p queueProject) docID() string { return "queue-" + queue.Slug(p.name) }

type queueStore struct {
	v        *Vault
	root     *fsx.Vault
	projects []queueProject // by folder name, the backlog last
}

func openQueues(v *Vault) (*queueStore, error) {
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return nil, err
	}
	s := &queueStore{v: v, root: root}
	entries, _ := os.ReadDir(filepath.Join(v.Config.VaultPath, tasks.ProjectsDir))
	for _, e := range entries {
		dir := path.Join(tasks.ProjectsDir, e.Name())
		if strings.HasPrefix(e.Name(), ".") || !root.IsDir(dir) {
			continue
		}
		s.projects = append(s.projects, s.readProject(e.Name(), dir))
	}
	slices.SortFunc(s.projects, func(a, b queueProject) int { return strings.Compare(a.name, b.name) })
	s.projects = append(s.projects, s.readProject(backlogProject, tasks.BacklogDir))
	return s, nil
}

// readProject reads the queue_prefix and repos keys of dir/index.md.
func (s *queueStore) readProject(name, dir string) queueProject {
	p := queueProject{name: name, dir: dir, prefix: queue.Slug(name)}
	text, err := s.root.ReadText(path.Join(dir, "index.md"))
	if err != nil {
		return p
	}
	fm, _, _ := frontmatter.Split(text)
	if fm == nil {
		return p
	}
	if v, ok := fm.Get("queue_prefix"); ok && v != nil {
		var errs []string
		if s.v.Profile != nil {
			errs = s.v.Profile.Problems("queue_prefix", v, false)
		}
		if pre, isStr := v.(string); len(errs) == 0 && isStr && queue.ValidPrefix(pre) {
			p.prefix = pre
		} else if len(errs) > 0 {
			p.problem = dir + "/index.md: " + strings.Join(errs, "; ")
		} else {
			p.problem = fmt.Sprintf("%s/index.md sets queue_prefix %s, which cannot start an item id", dir, pystr.ReprAny(v))
		}
	}
	r, _ := fm.Get("repos")
	p.repos = slices.DeleteFunc(vault.CoerceList(r), func(s string) bool { return s == "" })
	return p
}

func (s *queueStore) names() []string {
	out := make([]string, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p.name)
	}
	return out
}

func (s *queueStore) backlog() queueProject { return s.projects[len(s.projects)-1] }

// byPrefix is the one project whose ids start with prefix.
func (s *queueStore) byPrefix(prefix string) (queueProject, *jsonx.Obj) {
	var hits []queueProject
	for _, p := range s.projects {
		if p.prefix == prefix && p.problem == "" {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return queueProject{}, failNext(fmt.Sprintf("no queue uses the id prefix %s", pystr.Repr(prefix)),
			qcall("action", "list", "all", true, "fields", []string{"id", "project", "text"})+" to find the item's id")
	}
	var names []string
	for _, p := range hits {
		names = append(names, p.name)
	}
	return queueProject{}, fail(fmt.Sprintf("projects %s share the id prefix %s; give one a distinct queue_prefix in its index.md",
		pystr.ReprList(names), pystr.Repr(prefix)))
}

// resolve is the queue a caller names by project, repo or cwd. fallback is
// set when a repo or cwd maps to no project and the backlog stands in.
func (s *queueStore) resolve(a QueueArgs) (p queueProject, fallback string, failure *jsonx.Obj) {
	given := 0
	for _, v := range []string{a.Project, a.Repo, a.Cwd} {
		if v != "" {
			given++
		}
	}
	listAll := qcall("action", "list", "all", true, "fields", []string{"id", "project"}) + " to see the projects that have queues"
	switch {
	case given > 1:
		return p, "", failNext("give one of project, repo or cwd", qcall("action", a.Action, "project", "..."))
	case a.Project != "":
		for _, match := range []func(queueProject) bool{
			func(q queueProject) bool { return q.name == a.Project },
			func(q queueProject) bool {
				return strings.EqualFold(q.name, a.Project) || queue.Slug(q.name) == queue.Slug(a.Project)
			},
		} {
			if i := slices.IndexFunc(s.projects, match); i >= 0 {
				return s.projects[i], "", nil
			}
		}
		return p, "", failNext(fmt.Sprintf("no project %s; one of %s", pystr.Repr(a.Project), pystr.ReprList(s.names())), listAll)
	case a.Repo != "":
		return s.byRepo([]string{a.Repo}, "repo "+pystr.Repr(a.Repo))
	case a.Cwd != "":
		return s.byRepo(repoCandidates(a.Cwd), "cwd "+pystr.Repr(a.Cwd))
	}
	return p, "", failNext("name the queue: give project, repo or cwd", qcall("action", a.Action, "cwd", "<the session's working directory>"))
}

// byRepo is the project whose repos list holds the first candidate any
// project declares, or the backlog when none does.
func (s *queueStore) byRepo(candidates []string, what string) (queueProject, string, *jsonx.Obj) {
	for _, repo := range candidates {
		var hits []queueProject
		for _, p := range s.projects {
			if slices.ContainsFunc(p.repos, func(r string) bool { return strings.EqualFold(r, repo) }) {
				hits = append(hits, p)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], "", nil
		}
		var names []string
		for _, p := range hits {
			names = append(names, p.name)
		}
		return queueProject{}, "", fail(fmt.Sprintf("projects %s all list repo %s; keep it in one index.md's repos", pystr.ReprList(names), pystr.Repr(repo)))
	}
	b := s.backlog()
	return b, fmt.Sprintf("no project's index.md lists %s under repos; using the backlog queue %s", what, b.rel()), nil
}

// repoCandidates are the repository names cwd may belong to, nearest first:
// each folder from cwd up, and for a git worktree (whose .git is a file
// naming <repo>/.git/worktrees/<name>) its main checkout's folder, so a
// worker's worktree maps to the repository it was cut from.
func repoCandidates(cwd string) []string {
	var out []string
	dir := filepath.Clean(cwd)
	for {
		if raw, err := os.ReadFile(filepath.Join(dir, ".git")); err == nil { //nolint:gosec // a caller's working tree, read only
			if gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: "); ok {
				if main, _, found := strings.Cut(filepath.ToSlash(gitdir), "/.git/worktrees/"); found {
					out = append(out, path.Base(main))
				}
			}
		}
		out = append(out, filepath.Base(dir))
		parent := filepath.Dir(dir)
		if parent == dir {
			return out
		}
		dir = parent
	}
}

// ----- the note ------------------------------------------------------------

// queueBody is a queue note's body as lines, with the item lines parsed.
type queueBody struct {
	lines    []string
	items    []bodyItem
	unparsed int
}

type bodyItem struct {
	at int // line index
	it queue.Item
}

var headingRe = regexp.MustCompile(`^#{1,6}(\s|$)`)

func parseBody(body string) *queueBody {
	b := &queueBody{lines: strings.Split(body, "\n")}
	b.scan()
	return b
}

func (b *queueBody) scan() {
	b.items, b.unparsed = nil, 0
	for i, l := range b.lines {
		if !strings.HasPrefix(l, "- [") {
			continue
		}
		if it, ok := queue.Parse(l); ok {
			b.items = append(b.items, bodyItem{at: i, it: it})
		} else {
			b.unparsed++
		}
	}
}

func (b *queueBody) String() string { return strings.Join(b.lines, "\n") }

func (b *queueBody) find(id string) int {
	return slices.IndexFunc(b.items, func(x bodyItem) bool { return x.it.ID == id })
}

func (b *queueBody) heading(h string) int {
	return slices.IndexFunc(b.lines, func(l string) bool { return strings.TrimRight(l, " \t\r") == h })
}

func blank(l string) bool { return strings.TrimSpace(l) == "" }

// appendTo adds line at the end of section h, creating the section at the
// end of the body when it is missing.
func (b *queueBody) appendTo(h, line string) {
	at := b.heading(h)
	if at < 0 {
		n := len(b.lines)
		for n > 0 && blank(b.lines[n-1]) {
			n--
		}
		b.lines = append(b.lines[:n], "", h, "")
		at = n + 1
	}
	end := len(b.lines)
	for i := at + 1; i < len(b.lines); i++ {
		if headingRe.MatchString(b.lines[i]) {
			end = i
			break
		}
	}
	pos := end
	for pos > at+1 && blank(b.lines[pos-1]) {
		pos--
	}
	var ins []string
	if pos == at+1 {
		ins = append(ins, "")
	}
	ins = append(ins, line)
	if pos == end {
		ins = append(ins, "")
	}
	b.lines = slices.Insert(b.lines, pos, ins...)
	b.scan()
}

// remove deletes the line at, and a blank line it leaves doubled.
func (b *queueBody) remove(at int) {
	b.lines = slices.Delete(b.lines, at, at+1)
	if at > 0 && at < len(b.lines) && blank(b.lines[at-1]) && blank(b.lines[at]) {
		b.lines = slices.Delete(b.lines, at, at+1)
	}
	b.scan()
}

// put writes it back over its line, moving it to the section its status
// belongs in when it sits elsewhere (a box ticked in Obsidian stays under
// ## Open until the queue next writes it).
func (b *queueBody) put(i int, it queue.Item) {
	at := b.items[i].at
	line := queue.Format(it)
	section := queueOpen
	if queue.Done(it.Status) {
		section = queueClosed
	}
	if b.sectionOf(at) == section {
		b.lines[at] = line
		b.scan()
		return
	}
	b.remove(at)
	b.appendTo(section, line)
}

// sectionOf is the heading line at sits under, or "".
func (b *queueBody) sectionOf(at int) string {
	for i := at - 1; i >= 0; i-- {
		if headingRe.MatchString(b.lines[i]) {
			return strings.TrimRight(b.lines[i], " \t\r")
		}
	}
	return ""
}

// queueDoc is one queue as read from disk.
type queueDoc struct {
	p          queueProject
	exists     bool
	restricted bool
	body       *queueBody
}

func (s *queueStore) read(p queueProject) (queueDoc, error) {
	d := queueDoc{p: p, body: parseBody("")}
	text, err := s.root.ReadText(p.rel())
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	d.exists = true
	fm, _, _ := frontmatter.Split(text)
	if fm != nil {
		sens, _ := fm.Get("sensitivity")
		d.restricted = !gate.Releasable(gate.Parse(pystr.Str(sens)))
	}
	d.body = parseBody(text[frontmatter.BodyOffset(text):])
	return d, nil
}

// readWritable reads p's queue for a change: a failure when it can't be read
// or is restricted.
func (s *queueStore) readWritable(p queueProject) (queueDoc, *jsonx.Obj) {
	if p.problem != "" {
		return queueDoc{}, fail(p.problem)
	}
	d, err := s.read(p)
	if err != nil {
		return d, fail("read failed: " + err.Error())
	}
	if d.restricted {
		return d, failNext(p.docID()+" is restricted; the queue tool does not read or write restricted notes",
			Call("read_doc", "doc_id", p.docID(), "include_restricted", true)+" from the main session, only if the user needs it")
	}
	return d, nil
}

// change rewrites p's queue under its lock. edit gets the body as it is on
// disk at that moment and may run more than once; it returns a failure, or
// nil once it has changed the body.
func (s *queueStore) change(p queueProject, edit func(b *queueBody) *jsonx.Obj) *jsonx.Obj {
	out := write.EditBodyUnindexed(s.v.Loader, p.rel(), s.v.Profile, func(body, _ string) (string, *jsonx.Obj) {
		b := parseBody(body)
		if f := edit(b); f != nil {
			return "", f
		}
		return b.String(), nil
	})
	if !failed(out) {
		return nil
	}
	return guide(out, func(msg string) []string {
		switch {
		case contended(msg):
			return []string{"the same queue call again" + contendedWhy}
		case s.v.outsideWritable(p.rel()):
			return []string{qcall("action", "list", "project", p.name) + " to read it; notes in read-only folders are not edited here"}
		case strings.Contains(msg, "set_frontmatter"):
			return []string{Call("set_frontmatter", "relative_path", p.rel()) + " with the fields the error names, then retry"}
		}
		return []string{qcall("action", "list", "project", p.name, "status", "all") + " to see the queue as it is now, then retry"}
	})
}

// create makes p's queue note through the doc create path.
func (s *queueStore) create(p queueProject) *jsonx.Obj {
	title := p.name + " Queue"
	if p.name == backlogProject {
		title = "Backlog Queue"
	}
	var related []string
	if p.name != backlogProject {
		related = []string{p.name}
	}
	r := write.CreateDoc(s.v.Loader, p.rel(), "# "+title+"\n\n"+queueOpen+"\n\n"+queueClosed+"\n", write.With(
		"doc_id", p.docID(), "title", title, "doc_type", "architecture_note", "system", p.name,
		"tags", []string{"queue"}, "related_projects", related,
	), s.v.Profile)
	return guide(r, s.v.createSteps(p.rel(), p.docID()))
}

// ----- items ---------------------------------------------------------------

func itemObj(it queue.Item, project string) *jsonx.Obj {
	var owner any
	if it.Owner != "" {
		owner = it.Owner
	}
	links := jsonx.New()
	for _, k := range queue.LinkKeys {
		if v := it.Links[k]; v != "" {
			links.Set(k, v)
		}
	}
	o := jsonx.New("id", it.ID, "project", project, "kind", it.Kind, "status", it.Status, "owner", owner, "text", it.Text, "links", links)
	if len(it.BlockedBy) > 0 {
		o.Set("blocked_by", slices.Clone(it.BlockedBy))
	}
	for _, f := range [][2]string{{"body", it.Body}, {"alias", it.Alias}, {"agent", it.Agent}, {"resolution", it.Resolution}} {
		if f[1] != "" {
			o.Set(f[0], f[1])
		}
	}
	return o
}

// owner normalizes an owner argument: "" and "-" are nobody.
func owner(s string) string {
	s = pystr.Strip(s)
	if s == queue.Unowned {
		return ""
	}
	return s
}

// checkInput refuses arguments that could never make a valid line, before
// anything is read.
func checkInput(a QueueArgs) error {
	if a.Owner != nil {
		if o := owner(*a.Owner); o != "" && !queue.ValidToken(o) {
			return fmt.Errorf("owner %s must be %s", pystr.Repr(o), queue.TokenRule)
		}
	}
	for k, v := range a.Links {
		if !slices.Contains(queue.LinkKeys, k) {
			return fmt.Errorf("links: %s is not one of %s", pystr.Repr(k), strings.Join(queue.LinkKeys, ", "))
		}
		if v != "" && !queue.ValidToken(v) {
			return fmt.Errorf("links.%s %s must be %s", k, pystr.Repr(v), queue.TokenRule)
		}
	}
	for _, id := range a.BlockedBy {
		if _, _, ok := queue.SplitID(id); !ok {
			return fmt.Errorf("blocked_by %s is not an item id such as example-3", pystr.Repr(id))
		}
	}
	if a.Text != nil && a.Action != "list" {
		if err := queue.OneLine("text", pystr.Strip(*a.Text)); err != nil {
			return err
		}
	}
	return nil
}

// applyLinks sets or, for "", removes each given link, into a new map so the
// parsed line's map is never changed.
func applyLinks(it *queue.Item, links map[string]string) {
	if links == nil {
		return
	}
	next := map[string]string{}
	maps.Copy(next, it.Links)
	for k, v := range links {
		if v == "" {
			delete(next, k)
		} else {
			next[k] = v
		}
	}
	it.Links = nil
	if len(next) > 0 {
		it.Links = next
	}
}

func blockers(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	return slices.Clone(ids)
}

// ----- actions -------------------------------------------------------------

func (s *queueStore) add(a QueueArgs) *jsonx.Obj {
	retry := func(why string) string { return qcall("action", "add", "kind", "next", "text", "...") + " " + why }
	if !slices.Contains(queue.Kinds, a.Kind) {
		return failNext(fmt.Sprintf("add needs kind: one of %s", strings.Join(queue.Kinds, ", ")), retry("with kind set"))
	}
	if a.Text == nil {
		return failNext("add needs text: one line saying what the work is", retry("with text set"))
	}
	if a.Agent != "" && !queue.ValidToken(a.Agent) {
		return failNext("agent "+pystr.Repr(a.Agent)+" must be "+queue.TokenRule, retry("with the agent id as one word"))
	}
	if err := checkInput(a); err != nil {
		return failNext(err.Error(), retry("with what the error names fixed"))
	}
	p, fallback, f := s.resolve(a)
	if f != nil {
		return f
	}
	d, f := s.readWritable(p)
	if f != nil {
		return f
	}
	created := false
	if !d.exists {
		if r := s.create(p); failed(r) {
			// A concurrent first add may have created it; anything else stands.
			if again, err := s.read(p); err != nil || !again.exists {
				return r
			}
		} else {
			created = true
		}
	}
	var item queue.Item
	if f := s.change(p, func(b *queueBody) *jsonx.Obj {
		top := 0
		for _, x := range b.items {
			if pre, n, ok := queue.SplitID(x.it.ID); ok && pre == p.prefix && n > top {
				top = n
			}
			if a.Agent != "" && x.it.Agent == a.Agent && !queue.Done(x.it.Status) {
				return failNext(fmt.Sprintf("agent %s already has %s", pystr.Repr(a.Agent), x.it.ID),
					qcall("action", "set", "agent", a.Agent)+" to change that item instead")
			}
		}
		item = queue.Item{ID: fmt.Sprintf("%s-%d", p.prefix, top+1), Kind: a.Kind, Status: "open",
			Text: pystr.Strip(*a.Text), BlockedBy: blockers(a.BlockedBy), Agent: a.Agent}
		if a.Owner != nil {
			item.Owner = owner(*a.Owner)
		}
		applyLinks(&item, a.Links)
		if err := item.Validate(); err != nil {
			return failNext(err.Error(), retry("with what the error names fixed"))
		}
		b.appendTo(queueOpen, queue.Format(item))
		return nil
	}); f != nil {
		return f
	}
	out := jsonx.New("ok", true, "id", item.ID, "project", p.name, "doc_id", p.docID(), "relative_path", p.rel(), "item", itemObj(item, p.name))
	if created {
		out.Set("created_queue", true)
	}
	if fallback != "" {
		out.Set("fallback", fallback)
	}
	return out
}

// target is the item a set, close or promote names, by id or by agent.
func (s *queueStore) target(a QueueArgs) (queueProject, string, *jsonx.Obj) {
	findIt := qcall("action", "list", "all", true, "fields", []string{"id", "project", "text", "agent"}) + " to find the item's id"
	if (a.ID == "") == (a.Agent == "") {
		return queueProject{}, "", failNext(a.Action+" names one item: give id, or agent for a worker's item", findIt)
	}
	if a.ID != "" {
		prefix, _, ok := queue.SplitID(a.ID)
		if !ok {
			return queueProject{}, "", failNext(fmt.Sprintf("id %s is not <prefix>-<number>, such as example-3", pystr.Repr(a.ID)), findIt)
		}
		p, f := s.byPrefix(prefix)
		return p, a.ID, f
	}
	type hit struct {
		p  queueProject
		id string
	}
	var open []hit
	closedID := ""
	for _, p := range s.projects {
		d, err := s.read(p)
		if err != nil || !d.exists || d.restricted {
			continue
		}
		for _, x := range d.body.items {
			if x.it.Agent != a.Agent {
				continue
			}
			if queue.Done(x.it.Status) {
				closedID = x.it.ID
				continue
			}
			open = append(open, hit{p, x.it.ID})
		}
	}
	switch len(open) {
	case 1:
		return open[0].p, open[0].id, nil
	case 0:
		msg := "no open item carries agent " + pystr.Repr(a.Agent)
		if closedID != "" {
			msg += "; " + closedID + " does, and it is done"
		}
		return queueProject{}, "", failNext(msg, qcall("action", "add", "kind", "delegated", "agent", a.Agent, "text", "...")+" to track the worker first")
	}
	var ids []string
	for _, h := range open {
		ids = append(ids, h.id)
	}
	return queueProject{}, "", failNext(fmt.Sprintf("agent %s carries %s; give id instead", pystr.Repr(a.Agent), pystr.ReprList(ids)),
		qcall("action", a.Action, "id", ids[0]))
}

// edit runs fn on the item id in p's queue under the lock; fn changes the
// item it is given and returns a failure, or nil. The result carries the
// item as written, or no_op when fn left it as it was.
func (s *queueStore) edit(p queueProject, id string, a QueueArgs, fn func(it *queue.Item) *jsonx.Obj) *jsonx.Obj {
	if _, f := s.readWritable(p); f != nil {
		return f
	}
	var item queue.Item
	noOp := false
	if f := s.change(p, func(b *queueBody) *jsonx.Obj {
		noOp = false
		i := b.find(id)
		if i < 0 {
			return failNext(fmt.Sprintf("no item %s in %s", pystr.Repr(id), p.rel()),
				qcall("action", "list", "project", p.name, "status", "all", "fields", []string{"id", "status", "text"})+" to see its ids")
		}
		it := b.items[i].it
		if a.Agent != "" && it.Agent != a.Agent {
			return failNext(fmt.Sprintf("%s no longer carries agent %s", id, pystr.Repr(a.Agent)), qcall("action", a.Action, "id", id))
		}
		before := queue.Format(it)
		if f := fn(&it); f != nil {
			return f
		}
		if err := it.Validate(); err != nil {
			return failNext(err.Error(), qcall("action", a.Action, "id", id)+" with what the error names fixed")
		}
		item = it
		if queue.Format(it) == before {
			noOp = true
			return nil
		}
		b.put(i, it)
		return nil
	}); f != nil {
		return f
	}
	out := jsonx.New("ok", true, "id", item.ID, "project", p.name, "item", itemObj(item, p.name))
	if noOp {
		out.Set("no_op", true)
	}
	return out
}

func (s *queueStore) set(a QueueArgs) *jsonx.Obj {
	if err := checkInput(a); err != nil {
		return failNext(err.Error(), qcall("action", "set", "id", a.ID)+" with what the error names fixed")
	}
	if a.Status != "" && !slices.Contains(queue.Statuses, a.Status) {
		return failNext(fmt.Sprintf("status %s is not one of %s", pystr.Repr(a.Status), strings.Join(queue.Statuses, ", ")),
			qcall("action", "set", "id", a.ID, "status", "open"))
	}
	if queue.Done(a.Status) {
		return failNext("an item closes with its resolution", qcall("action", "close", "id", a.ID, "status", a.Status, "resolution", "..."))
	}
	if a.Claim && (a.Owner == nil || owner(*a.Owner) == "") {
		return failNext("claim needs owner: who takes the item", qcall("action", "set", "id", a.ID, "claim", true, "owner", "..."))
	}
	p, id, f := s.target(a)
	if f != nil {
		return f
	}
	return s.edit(p, id, a, func(it *queue.Item) *jsonx.Obj {
		if a.Claim && it.Owner != "" && it.Owner != owner(*a.Owner) {
			return failNext(fmt.Sprintf("%s is owned by %s", id, it.Owner),
				qcall("action", "list", "project", p.name, "ready", true, "owner", queue.Unowned)+" to pick unowned work").Set("owner", it.Owner)
		}
		if a.Owner != nil {
			it.Owner = owner(*a.Owner)
		}
		if a.Text != nil {
			it.Text = pystr.Strip(*a.Text)
		}
		applyLinks(it, a.Links)
		if a.HasBlockedBy {
			it.BlockedBy = blockers(a.BlockedBy)
		}
		if a.Status != "" {
			if a.Status == "returned" && it.Kind != "delegated" {
				return failNext(fmt.Sprintf("only a delegated item is returned; %s is %s", id, it.Kind),
					qcall("action", "set", "id", id, "status", "open"))
			}
			if queue.Done(it.Status) {
				it.Reopen(a.Status)
			}
			it.Status = a.Status
		}
		return nil
	})
}

func (s *queueStore) close(a QueueArgs) *jsonx.Obj {
	resolution := pystr.Strip(a.Resolution)
	if err := queue.OneLine("resolution", resolution); err != nil {
		return failNext("close needs resolution: one line such as repo@sha, a PR, a doc id, or why it was dropped",
			qcall("action", "close", "id", a.ID, "resolution", "example-repo@abc1234"))
	}
	status := a.Status
	if status == "" {
		status = "closed"
	}
	if !queue.Done(status) {
		return failNext(fmt.Sprintf("close takes status closed or dropped, not %s", pystr.Repr(a.Status)),
			qcall("action", "close", "id", a.ID, "status", "dropped", "resolution", "..."))
	}
	if err := checkInput(a); err != nil {
		return failNext(err.Error(), qcall("action", "close", "id", a.ID)+" with what the error names fixed")
	}
	p, id, f := s.target(a)
	if f != nil {
		return f
	}
	return s.edit(p, id, a, func(it *queue.Item) *jsonx.Obj {
		// A box ticked in Obsidian is done without a resolution; close records one.
		if queue.Done(it.Status) && it.Resolution != "" {
			if it.Status == status && it.Resolution == resolution {
				return nil
			}
			return failNext(fmt.Sprintf("%s is already %s → %s", id, it.Status, it.Resolution),
				qcall("action", "set", "id", id, "status", "open")+" to reopen it first")
		}
		applyLinks(it, a.Links)
		it.Status, it.Resolution = status, resolution
		return nil
	})
}

func (s *queueStore) promote(a QueueArgs) *jsonx.Obj {
	p, id, f := s.target(a)
	if f != nil {
		return f
	}
	d, f := s.readWritable(p)
	if f != nil {
		return f
	}
	i := d.body.find(id)
	if i < 0 {
		return failNext(fmt.Sprintf("no item %s in %s", pystr.Repr(id), p.rel()),
			qcall("action", "list", "project", p.name, "status", "all", "fields", []string{"id", "status", "text"})+" to see its ids")
	}
	it := d.body.items[i].it
	if it.Body != "" {
		return failNext(fmt.Sprintf("%s already has the body %s", id, it.Body), Call("read_doc", "doc_id", it.Body))
	}
	docID, rel := "note-"+id, path.Join(p.dir, "Queue", id+".md")
	content := strings.TrimSpace(a.Content)
	if content == "" {
		content = "Prose for queue item `" + id + "` in " + p.rel() + ".\n\n## Notes"
	}
	r := write.CreateDoc(s.v.Loader, rel, content+"\n", write.With(
		"doc_id", docID, "title", it.Text, "doc_type", "architecture_note", "system", p.name, "tags", []string{"queue"},
	), s.v.Profile)
	if failed(r) {
		return guide(r, s.v.createSteps(rel, docID))
	}
	out := s.edit(p, id, a, func(it *queue.Item) *jsonx.Obj {
		if it.Body != "" && it.Body != docID {
			return failNext(fmt.Sprintf("%s got the body %s meanwhile; %s is left unlinked", id, it.Body, docID), Call("read_doc", "doc_id", it.Body))
		}
		it.Body = docID
		return nil
	})
	if failed(out) {
		return out
	}
	return out.Set("body", docID).Set("relative_path", rel)
}

func (s *queueStore) list(a QueueArgs) *jsonx.Obj {
	m, err := a.selection(QueueItemFields, leanQueueItem)
	if err != nil {
		return failNext(err.Error(), withoutFields("queue", "action", "list", "all", true))
	}
	if a.Limit < 0 {
		return failNext("limit: give a positive number, or 0 for no limit", qcall("action", "list", "all", true, "limit", queueLimit))
	}
	if a.Kind != "" && !slices.Contains(queue.Kinds, a.Kind) {
		return failNext(fmt.Sprintf("kind %s is not one of %s", pystr.Repr(a.Kind), strings.Join(queue.Kinds, ", ")), qcall("action", "list", "all", true))
	}
	statuses := defaultStatuses
	switch {
	case a.Status == "all":
		statuses = queue.Statuses
	case a.Status != "":
		statuses = strings.Split(a.Status, ",")
		for _, st := range statuses {
			if !slices.Contains(queue.Statuses, st) {
				return failNext(fmt.Sprintf("status %s is not one of %s, or all", pystr.Repr(st), strings.Join(queue.Statuses, ", ")),
					qcall("action", "list", "all", true, "status", "open,returned"))
			}
		}
	}
	scoped := a.Project != "" || a.Repo != "" || a.Cwd != ""
	var targets []queueProject
	fallback := ""
	switch {
	case a.All && scoped:
		return failNext("give all, or one of project, repo or cwd", qcall("action", "list", "all", true))
	case a.All || (!scoped && a.Agent != ""):
		targets = s.projects
	case !scoped:
		return failNext("name the queue: give project, repo or cwd, or all=true",
			qcall("action", "list", "cwd", "<the session's working directory>"), qcall("action", "list", "all", true))
	default:
		p, fb, f := s.resolve(a)
		if f != nil {
			return f
		}
		targets, fallback = []queueProject{p}, fb
	}
	docs := map[string]queueDoc{}
	skipped := []string{}
	unparsed := 0
	load := func(p queueProject) (queueDoc, bool) {
		if d, ok := docs[p.name]; ok {
			return d, d.exists && !d.restricted
		}
		d, err := s.read(p)
		if err != nil {
			d.exists = false
		}
		docs[p.name] = d
		if d.restricted {
			skipped = append(skipped, p.docID())
		}
		if d.exists && !d.restricted {
			unparsed += d.body.unparsed
		}
		return d, d.exists && !d.restricted
	}
	// ready needs each blocker's status, which may sit in another queue.
	closedIDs := func(id string) bool {
		prefix, _, ok := queue.SplitID(id)
		if !ok {
			return false
		}
		p, f := s.byPrefix(prefix)
		if f != nil {
			return false
		}
		d, ok := load(p)
		if !ok {
			return false
		}
		i := d.body.find(id)
		return i >= 0 && queue.Done(d.body.items[i].it.Status)
	}
	matched := []*jsonx.Obj{}
	for _, p := range targets {
		d, ok := load(p)
		if !ok {
			continue
		}
		for _, x := range d.body.items {
			it := x.it
			switch {
			case !slices.Contains(statuses, it.Status),
				a.Kind != "" && it.Kind != a.Kind,
				a.Agent != "" && it.Agent != a.Agent,
				a.Owner != nil && it.Owner != owner(*a.Owner),
				a.Contains != "" && !containsFold(it.Text, a.Contains):
				continue
			}
			if a.Ready && (it.Status != "open" || (it.Kind != "next" && it.Kind != "follow-up") ||
				slices.ContainsFunc(it.BlockedBy, func(b string) bool { return !closedIDs(b) })) {
				continue
			}
			matched = append(matched, itemObj(it, p.name))
		}
	}
	count := len(matched)
	if a.Limit > 0 && count > a.Limit {
		matched = matched[:a.Limit]
	}
	out := jsonx.New("ok", true)
	if len(targets) == 1 {
		p := targets[0]
		out.Set("project", p.name).Set("doc_id", p.docID()).Set("relative_path", p.rel()).Set("exists", docs[p.name].exists)
	}
	out.Set("count", count).Set("items", matched)
	if m != nil {
		cutEach(out, "items", m)
	}
	if fallback != "" {
		out.Set("fallback", fallback)
	}
	if len(skipped) > 0 {
		out.Set("skipped_restricted", skipped)
	}
	if unparsed > 0 {
		out.Set("unparsed_lines", unparsed)
	}
	return out
}
