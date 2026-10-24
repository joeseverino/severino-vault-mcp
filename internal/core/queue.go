package core

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/queue"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// A project's work queue is one note, <project folder>/Queue.md, with an
// ## Open and a ## Closed section of item lines (internal/queue holds the
// grammar). Every change rewrites the note through write.EditNoteUnindexed,
// so it runs under the note's lock and re-reads the note before replacing
// it; ids are allocated and claims checked inside that locked edit, which
// refuses a note this binary does not fully read (queueBody.writable) and
// stamps the line format it wrote (queue_format).

// QueueActions are the queue tool's actions; the first is the default.
var QueueActions = []string{"list", "add", "set", "close", "reformat"}

// Queue note layout.
const (
	queueFile      = "Queue.md"
	queueLevel     = 2 // Open and Closed are ## sections
	queueOpen      = "Open"
	queueClosed    = "Closed"
	backlogProject = "backlog"
	queueLimit     = 50
)

// defaultStatuses are what list shows when no status is given: the items
// still waiting on someone.
var defaultStatuses = []string{"open", "returned"}

// QueueArgs are the queue tool's arguments. Nil pointers and a nil Links are
// "not given"; a link, alias or body given as "" is removed by set. ID names
// an item by its id or its alias.
type QueueArgs struct {
	Action             string
	Project, Repo, Cwd string
	All                bool
	ID, Agent          string
	Alias, Body        *string
	Origin             string
	IncludeOrigin      bool
	Kind               string
	Text, Owner        *string
	Links              map[string]string
	BlockedBy          []string
	HasBlockedBy       bool
	Repos              []string // entries may themselves be comma lists
	HasRepos           bool
	Partial            bool
	Status             string
	Ready              bool
	Contains           string
	Limit              int
	Claim              bool
	Resolution         string
	DropAliases        bool
	View
}

// QueueItemFields are the fields a queue item carries. owner is null when
// nobody holds the item; links holds the typed links given; blocked_by,
// body, alias, agent, origin, repos and resolution appear only when set.
var QueueItemFields = append(typed("id", tStr, "project", tStr, "kind", tStr, "status", tStr, "owner", tOptStr, "text", tStr),
	Field{Name: "links", Type: "object", Inner: typed("pr", tStr, "branch", tStr, "commit", tStr, "slug", tStr)},
	Field{Name: "blocked_by", Type: tList}, Field{Name: "body", Type: tStr}, Field{Name: "alias", Type: tStr},
	Field{Name: "agent", Type: tStr}, Field{Name: "origin", Type: tStr}, Field{Name: "repos", Type: tList}, Field{Name: "resolution", Type: tStr})

var (
	leanQueueItem = mustMask(QueueItemFields, "id", "kind", "status", "owner", "text")
	// leanDelegated is the default for include_origin, whose items come from
	// other projects' queues.
	leanDelegated = mustMask(QueueItemFields, "id", "project", "kind", "status", "owner", "text", "origin")
)

func qcall(args ...any) string { return Call("queue", args...) }

// Queue runs one queue action against v.
func Queue(v *Vault, a QueueArgs) *jsonx.Obj {
	if v.Profile == nil {
		return failNext("vault "+pystr.Repr(v.Name)+" has no schema profile", noProfile)
	}
	s, err := openQueues(v)
	if err != nil {
		return jsonx.Fail("read failed: " + err.Error())
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
	case "reformat":
		return s.reformat(a)
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
	id                string // the queue note's own doc_id, "" before it exists
}

func (p queueProject) rel() string { return path.Join(p.dir, queueFile) }

// docID is the queue note's doc_id: its own once it exists, since a doc_id
// never changes and a renamed project keeps the one its queue was created
// with.
func (p queueProject) docID() string {
	if p.id != "" {
		return p.id
	}
	return schema.QueueDocIDPrefix + sections.Slugify(p.name)
}

type queueStore struct {
	v        *Vault
	root     *fsx.Vault
	projects []queueProject      // by folder name, the backlog last
	docs     map[string]queueDoc // reads, by project name, for this call
	links    *noteLinks
}

func openQueues(v *Vault) (*queueStore, error) {
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return nil, err
	}
	s := &queueStore{v: v, root: root, docs: map[string]queueDoc{},
		links: newNoteLinks(func() *vault.Index { return v.Loader.Index(false) })}
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
	p := queueProject{name: name, dir: dir, prefix: sections.Slugify(name)}
	if q, err := s.root.ReadNote(p.rel()); err == nil && q.FM != nil {
		p.id = q.FM.Text("doc_id")
	}
	note, err := s.root.ReadNote(path.Join(dir, "index.md"))
	if err != nil {
		return p
	}
	fm := note.FM
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

// cached reads p's queue once per call; a change re-reads it under the lock.
func (s *queueStore) cached(p queueProject) queueDoc {
	if d, ok := s.docs[p.name]; ok {
		return d
	}
	d, err := s.read(p)
	if err != nil {
		d.exists = false
	}
	s.docs[p.name] = d
	return d
}

// readable reports whether d's items can be read here.
func (d queueDoc) readable() bool { return d.exists && !d.restricted }

// locate is the queue that holds item id. An id never changes once issued,
// and a project's queue_prefix can change after it issued some, so the item
// is found where it is: the queues whose prefix matches are read first, then
// the rest.
func (s *queueStore) locate(id string) (queueProject, queue.Item, *jsonx.Obj) {
	prefix, _, _ := queue.SplitID(id)
	ordered := slices.Clone(s.projects)
	slices.SortStableFunc(ordered, func(a, b queueProject) int {
		return cmpBool(a.prefix != prefix, b.prefix != prefix)
	})
	var hits []queueProject
	var item queue.Item
	for _, p := range ordered {
		d := s.cached(p)
		if !d.readable() {
			continue
		}
		if i := d.body.find(id); i >= 0 {
			if len(hits) == 0 {
				item = d.body.items[i].it
			}
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], item, nil
	case 0:
		// A queue this tool can't read or write (restricted, or a bad
		// queue_prefix) is still named, so the change explains why it fails.
		for _, p := range ordered {
			if p.prefix == prefix && (p.problem != "" || s.cached(p).restricted) {
				return p, item, nil
			}
		}
		return queueProject{}, item, failNext(fmt.Sprintf("no queue holds an item %s", pystr.Repr(id)),
			qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "project", "text"})+" to find the item's id")
	}
	var names []string
	for _, p := range hits {
		names = append(names, p.name)
	}
	return queueProject{}, item, jsonx.Fail(fmt.Sprintf("projects %s each hold an item %s; give one a distinct queue_prefix in its index.md and renumber the other by hand",
		pystr.ReprList(names), pystr.Repr(id)))
}

// cmpBool orders false before true.
func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// topNumber is the largest n among the ids prefix-n written in every queue
// note but p's, read from the text so a line that does not parse, or a
// restricted note, still counts: a new id never repeats one another queue
// issued under the same prefix before a queue_prefix changed. p's own ids are
// counted under its lock.
func (s *queueStore) topNumber(p queueProject) int {
	top := 0
	for _, q := range s.projects {
		if q.name == p.name {
			continue
		}
		if d := s.cached(q); d.exists {
			top = max(top, queue.Top(d.body.String(), p.prefix))
		}
	}
	return top
}

// resolve is the queue a caller names by project, repo or cwd. fallback is
// set when a repo or cwd maps to no project and the backlog stands in; only
// list takes that, since a write filed there would be lost to the repo's
// sessions.
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
			func(q queueProject) bool { return sameProject(q.name, a.Project) },
		} {
			if i := slices.IndexFunc(s.projects, match); i >= 0 {
				return s.projects[i], "", nil
			}
		}
		return p, "", failNext(fmt.Sprintf("no project %s; one of %s", pystr.Repr(a.Project), pystr.ReprList(s.names())), listAll)
	case a.Repo != "":
		return s.byRepo(a, []string{a.Repo}, a.Repo, "repo "+pystr.Repr(a.Repo))
	case a.Cwd != "":
		return s.byRepo(a, repoCandidates(a.Cwd), repoName(a.Cwd), "cwd "+pystr.Repr(a.Cwd))
	}
	return p, "", failNext("name the queue: give project, repo or cwd", qcall("action", a.Action, "cwd", "<the session's working directory>"))
}

// byRepo is the project whose repos list holds the first candidate any
// project declares. When none does, list reads the backlog and says so;
// every other action is refused with the call that maps repo.
func (s *queueStore) byRepo(a QueueArgs, candidates []string, repo, what string) (queueProject, string, *jsonx.Obj) {
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
		return queueProject{}, "", jsonx.Fail(fmt.Sprintf("projects %s all list repo %s; keep it in one index.md's repos", pystr.ReprList(names), pystr.Repr(repo)))
	}
	unmapped := fmt.Sprintf("no project's index.md lists %s under repos", what)
	mapIt := Call("set_frontmatter", "relative_path", path.Join(tasks.ProjectsDir, "<project>", "index.md"), "add_repos", []string{repo}) +
		" on the project that owns " + pystr.Repr(repo)
	if a.Action == "list" {
		return s.backlog(), unmapped + "; these are the backlog's items, not this repo's. Map it: " + mapIt, nil
	}
	return queueProject{}, "", failNext(unmapped+"; map it before filing work", mapIt+", then retry",
		qcall("action", a.Action, "project", backlogProject)+" only for work no project owns")
}

// repoName is the repository cwd belongs to: the main checkout's folder for a
// git worktree, the folder holding .git for a checkout, or cwd's own folder
// when no .git is found above it.
func repoName(cwd string) string {
	dir := filepath.Clean(cwd)
	for {
		if raw, err := os.ReadFile(filepath.Join(dir, ".git")); err == nil { //nolint:gosec // a caller's working tree, read only
			if main, ok := worktreeMain(raw); ok {
				return main
			}
			return filepath.Base(dir)
		} else if fsx.IsDir(filepath.Join(dir, ".git")) {
			return filepath.Base(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Base(filepath.Clean(cwd))
		}
		dir = parent
	}
}

// worktreeMain is the main checkout's folder name a worktree's .git file
// points into (gitdir: <repo>/.git/worktrees/<name>).
func worktreeMain(gitFile []byte) (string, bool) {
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(gitFile)), "gitdir: ")
	if !ok {
		return "", false
	}
	main, _, found := strings.Cut(filepath.ToSlash(gitdir), "/.git/worktrees/")
	return path.Base(main), found
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
			if main, ok := worktreeMain(raw); ok {
				out = append(out, main)
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

// queueBody is a queue note's body, with the item lines parsed.
type queueBody struct {
	*sections.Body
	items    []bodyItem
	unparsed []int // line indexes that start "- [" and do not parse
	links    *noteLinks
}

type bodyItem struct {
	at int // line index
	it queue.Item
}

func parseBody(body string, links *noteLinks) *queueBody {
	b := &queueBody{Body: sections.NewBody(body), links: links}
	b.scan()
	return b
}

func (b *queueBody) scan() {
	b.items, b.unparsed = nil, nil
	for i, l := range b.Lines {
		if !strings.HasPrefix(l, "- [") {
			continue
		}
		if it, ok := queue.Parse(l); ok {
			b.links.resolve(&it)
			b.items = append(b.items, bodyItem{at: i, it: it})
		} else {
			b.unparsed = append(b.unparsed, i)
		}
	}
}

// noteLinks maps a body's doc_id to the note name its link writes, and back,
// from the vault index, which it reads only once an item has a body.
type noteLinks struct {
	load         func() *vault.Index
	names, byKey map[string]string
}

func newNoteLinks(load func() *vault.Index) *noteLinks { return &noteLinks{load: load} }

// build names each indexed doc by its file name without .md, as Obsidian
// resolves a link, or by its vault path without .md where another doc
// shares that file name.
func (l *noteLinks) build() {
	if l.names != nil {
		return
	}
	l.names, l.byKey = map[string]string{}, map[string]string{}
	docs := l.load().Docs
	count := map[string]int{}
	for _, d := range docs {
		count[path.Base(noteKey(d.RelativePath))]++
	}
	for _, d := range docs {
		key := noteKey(d.RelativePath)
		name := path.Base(key)
		if count[name] > 1 {
			name = key
		} else {
			l.byKey[name] = d.DocID
		}
		l.byKey[key] = d.DocID
		if queue.ValidBodyLink(name) {
			l.names[d.DocID] = name
		}
	}
}

func noteKey(rel string) string { return strings.TrimSuffix(filepath.ToSlash(rel), ".md") }

// name is the note name a link to docID writes, or "" when it has none.
func (l *noteLinks) name(docID string) string {
	if docID == "" {
		return ""
	}
	l.build()
	return l.names[docID]
}

// resolve fills it's body from its link, or its link from its body, so a
// line with either reads back the doc_id and writes the link. A link to no
// indexed doc stays as written and carries no body.
func (l *noteLinks) resolve(it *queue.Item) {
	switch {
	case it.BodyLink != "":
		l.build()
		it.Body = l.byKey[it.BodyLink]
	case it.Body != "":
		it.BodyLink = l.name(it.Body)
	}
}

func (b *queueBody) find(id string) int {
	return slices.IndexFunc(b.items, func(x bodyItem) bool { return x.it.ID == id })
}

// appendTo adds line at the end of the ## section, creating the section at
// the end of the body when it is missing.
func (b *queueBody) appendTo(section, line string) {
	b.AppendTo(queueLevel, section, line)
	b.scan()
}

// remove deletes the line at, and a blank line it leaves doubled.
func (b *queueBody) remove(at int) {
	b.Remove(at)
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
		b.Lines[at] = line
		b.scan()
		return
	}
	b.remove(at)
	b.appendTo(section, line)
}

// skewStep is the next step for a note written in a format this server does
// not read in full.
const skewStep = "this server may be older than the note's format: reconnect it (/mcp) or upgrade the binary"

// writable refuses a write to a note this binary does not fully understand:
// a queue_format newer than it knows, or an item-shaped line under ## Open or
// ## Closed that it cannot parse. Writing past either would rewrite lines it
// misread, and count ids from items it cannot see.
func (b *queueBody) writable(rel, format string) *jsonx.Obj {
	if err := queue.CheckVersion(format); err != nil {
		return failNext(rel+": "+err.Error()+"; nothing was written", skewStep)
	}
	for _, at := range b.unparsed {
		if s := b.queueSection(at); !queue.ItemShaped(b.Lines[at]) || s != queueOpen && s != queueClosed {
			continue
		}
		return failNext(fmt.Sprintf("%s: the item line %s does not parse in queue format %d; nothing was written", rel, pystr.Repr(b.Lines[at]), queue.Version),
			skewStep, "if the line was typed by hand, fix or remove it in the note, then retry")
	}
	return nil
}

// duplicateID is the first id written as an id field on more than one item
// line, or "".
func (b *queueBody) duplicateID() string {
	seen := map[string]bool{}
	for _, l := range b.Lines {
		if !queue.ItemShaped(l) {
			continue
		}
		for _, id := range queue.IDsIn(l) {
			if seen[id] {
				return id
			}
			seen[id] = true
		}
	}
	return ""
}

// queueSection is the text of the ## heading whose section holds line at,
// through any deeper headings, or "".
func (b *queueBody) queueSection(at int) string {
	out := ""
	for _, h := range b.Headings() {
		if h.Line >= at {
			break
		}
		switch {
		case h.Level == queueLevel:
			out = h.Text
		case h.Level < queueLevel:
			out = ""
		}
	}
	return out
}

// sectionOf is the text of the ## heading line at sits directly under, or "".
func (b *queueBody) sectionOf(at int) string {
	if h, ok := b.Under(at); ok && h.Level == queueLevel {
		return h.Text
	}
	return ""
}

// queueDoc is one queue as read from disk.
type queueDoc struct {
	p          queueProject
	exists     bool
	restricted bool
	format     string // queue_format as written, "" when absent
	body       *queueBody
}

func (s *queueStore) read(p queueProject) (queueDoc, error) {
	d := queueDoc{p: p, body: parseBody("", s.links)}
	note, err := s.root.ReadNote(p.rel())
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	d.exists = true
	if note.FM != nil {
		sens, _ := note.FM.Get("sensitivity")
		d.restricted = !gate.Releasable(gate.Parse(pystr.Str(sens)))
		d.format = note.FM.Text(schema.QueueFormatField)
	}
	d.body = parseBody(note.Text[note.Offset:], s.links)
	return d, nil
}

// readWritable reads p's queue for a change: a failure when it can't be read
// or is restricted.
func (s *queueStore) readWritable(p queueProject) (queueDoc, *jsonx.Obj) {
	if p.problem != "" {
		return queueDoc{}, jsonx.Fail(p.problem)
	}
	d, err := s.read(p)
	if err != nil {
		return d, jsonx.Fail("read failed: " + err.Error())
	}
	if d.restricted {
		return d, failNext(p.docID()+" is restricted; the queue tool does not read or write restricted notes",
			Call("read_doc", "doc_id", p.docID(), "include_restricted", true)+" from the main session, only if the user needs it")
	}
	return d, nil
}

// currentFormat is queue_format as this binary writes it.
var currentFormat = strconv.Itoa(queue.Version)

// change rewrites p's queue under its lock. edit gets the body as it is on
// disk at that moment and may run more than once; it returns a failure, or
// nil once it has changed the body. A note this binary does not fully read
// is refused before edit runs, a body that would carry an id twice is
// refused after, and the note is stamped with the format it was written in.
func (s *queueStore) change(p queueProject, edit func(b *queueBody) *jsonx.Obj) *jsonx.Obj {
	out := write.EditNoteUnindexed(s.v.Loader, p.rel(), s.v.Profile, func(fm *jsonx.Obj, body, _ string) (string, []write.HeadField, *jsonx.Obj) {
		b := parseBody(body, s.links)
		format := fm.Text(schema.QueueFormatField)
		if f := b.writable(p.rel(), format); f != nil {
			return "", nil, f
		}
		if f := edit(b); f != nil {
			return "", nil, f
		}
		if id := b.duplicateID(); id != "" {
			return "", nil, failNext(fmt.Sprintf("%s would carry id %s on more than one item line; an id never repeats, so nothing was written", p.rel(), pystr.Repr(id)),
				"give one of those lines a new id by hand in the note, then retry; "+
					qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "project", "text"})+" shows the ids in use")
		}
		var stamp []write.HeadField
		if format != currentFormat {
			stamp = []write.HeadField{{Key: schema.QueueFormatField, Value: currentFormat}}
		}
		return b.String(), stamp, nil
	})
	if !out.Failed() {
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
	r := write.CreateDoc(s.v.Loader, p.rel(), "# "+title+"\n\n## "+queueOpen+"\n\n## "+queueClosed+"\n", write.With(
		"doc_id", p.docID(), "title", title, "doc_type", schema.QueueDocType, "system", p.name,
		"tags", []string{"queue"}, "related_projects", related, schema.QueueFormatField, queue.Version,
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
	for _, f := range [][2]string{{"body", it.Body}, {"alias", it.Alias}, {"agent", it.Agent}, {"origin", it.Origin}} {
		if f[1] != "" {
			o.Set(f[0], f[1])
		}
	}
	if len(it.Repos) > 0 {
		o.Set("repos", slices.Clone(it.Repos))
	}
	if it.Resolution != "" {
		o.Set("resolution", it.Resolution)
	}
	return o
}

// originRepo is the repository an origin argument names: a value with a path
// separator is a working directory, resolved as cwd is.
func originRepo(s string) string {
	s = pystr.Strip(s)
	if strings.ContainsAny(s, `/\`) {
		return repoName(s)
	}
	return s
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
		if o := owner(*a.Owner); o != "" && !queue.ValidOwner(o) {
			return fmt.Errorf("owner %s must be %s", pystr.Repr(o), queue.OwnerRule)
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
	for _, ref := range a.BlockedBy {
		if !queue.ValidToken(ref) {
			return fmt.Errorf("blocked_by %s is not an item id such as example-3, or an item's alias", pystr.Repr(ref))
		}
	}
	for _, f := range []struct {
		name string
		v    *string
	}{{"alias", a.Alias}, {"body", a.Body}} {
		if f.v != nil && a.Action != "list" {
			if v := pystr.Strip(*f.v); v != "" && !queue.ValidToken(v) {
				return fmt.Errorf("%s %s must be %s", f.name, pystr.Repr(v), queue.TokenRule)
			}
		}
	}
	for _, r := range repoList(a.Repos) {
		if !queue.ValidRepo(r) {
			return fmt.Errorf("repos %s must each be a repository name, %s, without @", pystr.Repr(r), queue.TokenRule)
		}
	}
	if o := originRepo(a.Origin); o != "" && !queue.ValidToken(o) {
		return fmt.Errorf("origin %s must be a repository name, %s, or a working directory", pystr.Repr(o), queue.TokenRule)
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

// repoList flattens a repos argument, whose entries may be comma lists.
func repoList(given []string) []string {
	var out []string
	for _, g := range given {
		for _, r := range queue.SplitList(g) {
			if r != "" && !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

// byAlias is the item whose alias is alias. ok is false when none has it; a
// failure means several do.
func (s *queueStore) byAlias(alias string) (p queueProject, id string, ok bool, failure *jsonx.Obj) {
	var ids []string
	for _, q := range s.projects {
		d := s.cached(q)
		if !d.readable() {
			continue
		}
		for _, x := range d.body.items {
			if x.it.Alias == alias {
				if len(ids) == 0 {
					p, id = q, x.it.ID
				}
				ids = append(ids, x.it.ID)
			}
		}
	}
	if len(ids) > 1 {
		return queueProject{}, "", false, failNext(fmt.Sprintf("items %s each carry alias %s; give the id", pystr.ReprList(ids), pystr.Repr(alias)),
			qcall("action", "list", "all", true, "alias", alias, "status", "all"))
	}
	return p, id, len(ids) == 1, nil
}

// lookup is the item ref names: the item with that id, or else the one
// with that alias.
func (s *queueStore) lookup(ref string) (queueProject, string, *jsonx.Obj) {
	_, _, isID := queue.SplitID(ref)
	var byID *jsonx.Obj
	if isID {
		p, _, f := s.locate(ref)
		if f == nil {
			return p, ref, nil
		}
		byID = f
	}
	p, id, ok, f := s.byAlias(ref)
	switch {
	case f != nil:
		return queueProject{}, "", f
	case ok:
		return p, id, nil
	case byID != nil:
		return queueProject{}, "", byID
	}
	return queueProject{}, "", failNext(fmt.Sprintf("no queue holds an item with id or alias %s", pystr.Repr(ref)),
		qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "alias", "project", "text"})+" to find the item's id")
}

// blockers are the item ids refs name, each an id or an alias; an id no
// queue holds stays as given.
func (s *queueStore) blockers(refs []string) ([]string, *jsonx.Obj) {
	var out []string
	for _, ref := range refs {
		_, _, isID := queue.SplitID(ref)
		if isID {
			if _, _, f := s.locate(ref); f == nil {
				out = append(out, ref)
				continue
			}
		}
		_, id, ok, f := s.byAlias(ref)
		switch {
		case f != nil:
			return nil, f
		case ok:
			out = append(out, id)
		case isID:
			out = append(out, ref)
		default:
			return nil, failNext(fmt.Sprintf("blocked_by %s is neither an item id nor any item's alias", pystr.Repr(ref)),
				qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "alias", "text"})+" to find the blocker's id")
		}
	}
	return out, nil
}

// checkRefs refuses an alias another item already answers to, as its id or
// its alias, and a body that names no doc; self is the item being changed.
func (s *queueStore) checkRefs(a QueueArgs, self string) error {
	if a.Alias != nil {
		if alias := pystr.Strip(*a.Alias); alias != "" {
			if _, id, ok, _ := s.byAlias(alias); ok && id != self {
				return fmt.Errorf("alias %s is already %s's", pystr.Repr(alias), id)
			}
			if _, _, isID := queue.SplitID(alias); isID && alias != self {
				if _, _, f := s.locate(alias); f == nil {
					return fmt.Errorf("alias %s is another item's id", pystr.Repr(alias))
				}
			}
		}
	}
	if a.Body != nil {
		if body := pystr.Strip(*a.Body); body != "" {
			if errs := s.v.Profile.DocRefProblems("body", body, s.v.Loader.Index(false).Has); len(errs) > 0 {
				return errors.New(strings.Join(errs, "; "))
			}
		}
	}
	return nil
}

// setRefs applies a given alias and body to it; "" removes one. A body is
// written as a link to its note.
func (s *queueStore) setRefs(it *queue.Item, a QueueArgs) {
	if a.Alias != nil {
		it.Alias = pystr.Strip(*a.Alias)
	}
	if a.Body != nil {
		it.Body = pystr.Strip(*a.Body)
		it.BodyLink = s.links.name(it.Body)
	}
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
	p, _, f := s.resolve(a)
	if f != nil {
		return f
	}
	d, f := s.readWritable(p)
	if f != nil {
		return f
	}
	blockedBy, f := s.blockers(a.BlockedBy)
	if f != nil {
		return f
	}
	if err := s.checkRefs(a, ""); err != nil {
		return failNext(err.Error(), retry("with what the error names fixed"))
	}
	created := false
	if !d.exists {
		if r := s.create(p); r.Failed() {
			// A concurrent first add may have created it; anything else stands.
			if again, err := s.read(p); err != nil || !again.exists {
				return r
			}
		} else {
			created = true
		}
	}
	var item queue.Item
	floor := s.topNumber(p)
	if f := s.change(p, func(b *queueBody) *jsonx.Obj {
		top := max(floor, queue.Top(b.String(), p.prefix))
		for _, x := range b.items {
			if a.Agent != "" && x.it.Agent == a.Agent && !queue.Done(x.it.Status) {
				return failNext(fmt.Sprintf("agent %s already has %s", pystr.Repr(a.Agent), x.it.ID),
					qcall("action", "set", "agent", a.Agent)+" to change that item instead")
			}
		}
		item = queue.Item{ID: fmt.Sprintf("%s-%d", p.prefix, top+1), Kind: a.Kind, Status: "open",
			Text: pystr.Strip(*a.Text), BlockedBy: blockedBy, Agent: a.Agent, Origin: originRepo(a.Origin),
			Repos: repoList(a.Repos)}
		if a.Owner != nil {
			item.Owner = owner(*a.Owner)
		}
		applyLinks(&item, a.Links)
		s.setRefs(&item, a)
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
	return out
}

// target is the item a set or close names, by id or by agent.
func (s *queueStore) target(a QueueArgs) (queueProject, string, *jsonx.Obj) {
	findIt := qcall("action", "list", "all", true, "fields", []string{"id", "project", "text", "agent"}) + " to find the item's id"
	if (a.ID == "") == (a.Agent == "") {
		return queueProject{}, "", failNext(a.Action+" names one item: give id, or agent for a worker's item", findIt)
	}
	if a.ID != "" {
		if !queue.ValidToken(a.ID) {
			return queueProject{}, "", failNext(fmt.Sprintf("id %s is not <prefix>-<number>, such as example-3, or an item's alias", pystr.Repr(a.ID)), findIt)
		}
		return s.lookup(a.ID)
	}
	type hit struct {
		p  queueProject
		id string
	}
	var open []hit
	closedID := ""
	for _, p := range s.projects {
		d := s.cached(p)
		if !d.readable() {
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
	blockedBy, f := s.blockers(a.BlockedBy)
	if f != nil {
		return f
	}
	if err := s.checkRefs(a, id); err != nil {
		return failNext(err.Error(), qcall("action", "set", "id", id)+" with what the error names fixed")
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
		s.setRefs(it, a)
		if a.HasBlockedBy {
			it.BlockedBy = blockedBy
		}
		if a.HasRepos {
			it.Repos = repoList(a.Repos)
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
	if a.Partial {
		if status != "closed" {
			return failNext("partial records landings; drop the item with status=dropped and no partial",
				qcall("action", "close", "id", a.ID, "status", "dropped", "resolution", "..."))
		}
		if _, ok := queue.Landings(resolution); !ok {
			return failNext("partial takes the landed repos as repo@sha, comma-separated",
				qcall("action", "close", "id", a.ID, "partial", true, "resolution", "example-repo@abc1234"))
		}
	}
	if err := checkInput(a); err != nil {
		return failNext(err.Error(), qcall("action", "close", "id", a.ID)+" with what the error names fixed")
	}
	p, id, f := s.target(a)
	if f != nil {
		return f
	}
	return s.edit(p, id, a, func(it *queue.Item) *jsonx.Obj {
		if len(it.Repos) > 0 && !queue.Done(it.Status) {
			applyLinks(it, a.Links)
			return land(it, status, resolution, a.Partial)
		}
		if a.Partial && len(it.Repos) == 0 {
			return failNext(id+" lists no repos; partial is for one worker's item spanning several",
				qcall("action", "close", "id", id, "resolution", resolution))
		}
		// A box ticked in Obsidian is done without a resolution; close records one.
		if queue.Done(it.Status) && it.Resolution != "" {
			if it.Status == status && (it.Resolution == resolution || a.Partial && recorded(it.Resolution, resolution)) {
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

// land adds resolution's entries to a multi-repo item's resolution. The item
// closes once every repo in its repos has a repo@sha entry; a partial close
// short of that records the landings and leaves it open, and a whole close
// short of it is refused. Dropping keeps the landings and adds the reason.
func land(it *queue.Item, status, resolution string, partial bool) *jsonx.Obj {
	entries := []string{}
	if it.Resolution != "" {
		entries = queue.SplitList(it.Resolution)
	}
	for _, e := range queue.SplitList(resolution) {
		if !slices.Contains(entries, e) {
			entries = append(entries, e)
		}
	}
	merged := strings.Join(entries, ",")
	if status == "dropped" {
		it.Status, it.Resolution = status, merged
		return nil
	}
	retry := qcall("action", "close", "id", it.ID, "partial", true, "resolution", "<repo>@<sha>")
	landings, ok := queue.Landings(merged)
	if !ok {
		return failNext(fmt.Sprintf("%s spans repos %s; its resolution is repo@sha for each, comma-separated", it.ID, strings.Join(it.Repos, ", ")),
			retry, qcall("action", "close", "id", it.ID, "status", "dropped", "resolution", "...")+" for work not done")
	}
	var missing []string
	for _, r := range it.Repos {
		if !slices.ContainsFunc(landings, func(l queue.Landing) bool { return strings.EqualFold(l.Repo, r) }) {
			missing = append(missing, r)
		}
	}
	for _, l := range landings {
		if !slices.ContainsFunc(it.Repos, func(r string) bool { return strings.EqualFold(l.Repo, r) }) {
			return failNext(fmt.Sprintf("%s is not one of %s's repos %s", pystr.Repr(l.Repo), it.ID, strings.Join(it.Repos, ", ")),
				qcall("action", "set", "id", it.ID, "repos", append(slices.Clone(it.Repos), l.Repo))+" if the work spans it too")
		}
	}
	if len(missing) > 0 && !partial {
		return failNext(fmt.Sprintf("%s has not landed in %s", it.ID, strings.Join(missing, ", ")), retry+" to record these landings first")
	}
	it.Resolution = merged
	if len(missing) == 0 {
		it.Status = status
	}
	return nil
}

// recorded reports whether every entry of given already stands in resolution.
func recorded(resolution, given string) bool {
	have := queue.SplitList(resolution)
	return !slices.ContainsFunc(queue.SplitList(given), func(e string) bool { return !slices.Contains(have, e) })
}

// lineEdit is one item line a reformat rewrites.
type lineEdit struct {
	at   int
	line string
}

// reformatted are the item lines of b that Format writes differently,
// without their aliases when drop is set. A line that does not parse is left
// as it is.
func (b *queueBody) reformatted(drop bool) (edits []lineEdit, aliases int) {
	for _, x := range b.items {
		it := x.it
		if drop && it.Alias != "" {
			it.Alias = ""
			aliases++
		}
		if line := queue.Format(it); line != b.Lines[x.at] {
			edits = append(edits, lineEdit{x.at, line})
		}
	}
	return edits, aliases
}

// reformat rewrites every item line of the queues named (by project, repo or
// cwd, or all) in the current format, through the locked write path, and
// stamps it with that format; a note already in it is not written at all.
func (s *queueStore) reformat(a QueueArgs) *jsonx.Obj {
	scoped := a.Project != "" || a.Repo != "" || a.Cwd != ""
	var targets []queueProject
	switch {
	case a.All && scoped:
		return failNext("give all, or one of project, repo or cwd", qcall("action", "reformat", "all", true))
	case a.All:
		targets = s.projects
	case !scoped:
		return failNext("name the queue: give project, repo or cwd, or all=true", qcall("action", "reformat", "all", true))
	default:
		p, _, f := s.resolve(a)
		if f != nil {
			return f
		}
		targets = []queueProject{p}
	}
	changed, skipped := []*jsonx.Obj{}, []string{}
	unparsed, dropped := 0, 0
	for _, p := range targets {
		d := s.cached(p)
		if d.restricted {
			skipped = append(skipped, p.docID())
		}
		if !d.readable() {
			continue
		}
		// A note with nothing to rewrite is still refused when this binary
		// does not fully read it, so its lines are never reported current.
		if f := d.body.writable(p.rel(), d.format); f != nil {
			return f.Set("changed", changed)
		}
		if edits, _ := d.body.reformatted(a.DropAliases); len(edits) == 0 && d.format == currentFormat {
			unparsed += len(d.body.unparsed)
			continue
		}
		lines, aliases, left := 0, 0, 0
		if f := s.change(p, func(b *queueBody) *jsonx.Obj {
			var edits []lineEdit
			edits, aliases = b.reformatted(a.DropAliases)
			for _, e := range edits {
				b.Lines[e.at] = e.line
			}
			lines, left = len(edits), len(b.unparsed)
			return nil
		}); f != nil {
			return f.Set("changed", changed)
		}
		unparsed += left
		dropped += aliases
		changed = append(changed, jsonx.New("project", p.name, "relative_path", p.rel(), "lines", lines))
	}
	out := jsonx.New("ok", true, "count", len(changed), "changed", changed)
	if a.DropAliases {
		out.Set("aliases_dropped", dropped)
	}
	if len(skipped) > 0 {
		out.Set("skipped_restricted", skipped)
	}
	if unparsed > 0 {
		out.Set("unparsed_lines", unparsed)
	}
	return out
}

// boardQueue is the open and returned items of every queue, or of project's
// when it names one, as task_board shows them.
func boardQueue(v *Vault, project string) []*jsonx.Obj {
	r := Queue(v, QueueArgs{Action: "list", All: true, Fields: []string{"id", "project", "kind", "status", "owner", "text"}})
	raw, _ := r.Get("items")
	items, _ := raw.([]*jsonx.Obj)
	out := []*jsonx.Obj{}
	for _, it := range items {
		if project == "" || sameProject(it.Str("project"), project) {
			out = append(out, it)
		}
	}
	return out
}

// sameProject matches a project's folder name the way a caller may type it.
func sameProject(name, given string) bool {
	return strings.EqualFold(name, given) || sections.Slugify(name) == sections.Slugify(given)
}

func (s *queueStore) list(a QueueArgs) *jsonx.Obj {
	lean := leanQueueItem
	if a.IncludeOrigin {
		lean = leanDelegated
	}
	m, err := a.selection(QueueItemFields, lean)
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
	alias := ""
	if a.Alias != nil {
		alias = pystr.Strip(*a.Alias)
	}
	var targets []queueProject
	fallback := ""
	switch {
	case a.IncludeOrigin && !scoped:
		return failNext("include_origin adds to one queue: give project, repo or cwd",
			qcall("action", "list", "cwd", "<the session's working directory>", "include_origin", true))
	case a.All && scoped:
		return failNext("give all, or one of project, repo or cwd", qcall("action", "list", "all", true))
	case a.All || (!scoped && (a.Agent != "" || a.Origin != "" || alias != "")):
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
	skipped := []string{}
	unparsed := 0
	// ready needs each blocker's status, which may sit in another queue, or
	// carry the prefix its project used before its queue_prefix changed.
	closedIDs := func(id string) bool {
		_, it, f := s.locate(id)
		return f == nil && queue.Done(it.Status)
	}
	// include_origin also reads every other queue for the delegated items
	// this repo (or, by project, any of its repos) sent there.
	scan, origins := targets, []string{}
	if a.IncludeOrigin {
		scan = s.projects
		switch {
		case a.Repo != "":
			origins = []string{a.Repo}
		case a.Cwd != "":
			origins = []string{repoName(a.Cwd)}
		default:
			origins = targets[0].repos
		}
	}
	origin := originRepo(a.Origin)
	matched := []*jsonx.Obj{}
	for _, p := range scan {
		home := slices.ContainsFunc(targets, func(q queueProject) bool { return q.name == p.name })
		d := s.cached(p)
		if d.restricted {
			skipped = append(skipped, p.docID())
		}
		if !d.readable() {
			continue
		}
		if home {
			unparsed += len(d.body.unparsed)
		}
		for _, x := range d.body.items {
			it := x.it
			switch {
			case !home && (it.Kind != "delegated" || !slices.ContainsFunc(origins, func(o string) bool { return strings.EqualFold(o, it.Origin) })),
				origin != "" && !strings.EqualFold(it.Origin, origin),
				!slices.Contains(statuses, it.Status),
				a.Kind != "" && it.Kind != a.Kind,
				a.Agent != "" && it.Agent != a.Agent,
				alias != "" && it.Alias != alias,
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
		out.Set("project", p.name).Set("doc_id", p.docID()).Set("relative_path", p.rel()).Set("exists", s.cached(p).exists)
	}
	out.Set("count", count).Set("items", matched)
	if m != nil {
		cutEach(out, "items", m)
	}
	if fallback != "" {
		out.Set("fallback", targets[0].rel()).Set("warning", fallback)
	}
	if len(skipped) > 0 {
		out.Set("skipped_restricted", skipped)
	}
	if unparsed > 0 {
		out.Set("unparsed_lines", unparsed)
	}
	return out
}
