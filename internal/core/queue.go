package core

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/queue"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// A project's work queue is one note, <project folder>/Queue.md, with an
// ## Open and a ## Closed section of item lines (internal/queue holds the
// grammar). Every change rewrites the note through write.EditNoteUnindexed,
// so it runs under the note's lock and re-reads the note before replacing
// it; ids are allocated and claims checked inside that locked edit, which
// refuses a note holding an item line it cannot parse (queueBody.writable).

// QueueActions are the queue tool's actions; the first is the default.
var QueueActions = []string{"list", "add", "set", "close"}

// Queue note layout.
const (
	queueFile      = "Queue.md"
	queueLevel     = 2 // Open and Closed are ## sections
	queueOpen      = "Open"
	queueClosed    = "Closed"
	backlogProject = vault.BacklogProject
	queueLimit     = 50
)

// decisionClaimer is the owner of a decision the coordinating session holds, which a
// claim may hand to a worker.
const decisionClaimer = "main"

// defaultStatuses are what list shows when no status is given: the items
// still waiting on someone.
var defaultStatuses = []string{"open", "returned"}

// QueueArgs are the queue tool's arguments. Nil pointers and a nil Links are
// "not given"; a link or body given as "" is removed by set.
type QueueArgs struct {
	Action             string
	Project, Repo, Cwd string
	All                bool
	ID, Agent          string
	Body               *string
	Origin             string
	IncludeOrigin      bool
	Kind               string
	Text, Owner        *string
	Links              map[string]string
	BlockedBy          []string
	HasBlockedBy       bool
	Repos              []string // entries may themselves be comma lists
	HasRepos           bool
	Base               *string
	Transcript         *string
	Owns               []string // entries may themselves be comma lists
	HasOwns            bool
	Partial            bool
	Status             string
	Ready              bool
	Contains           string
	Limit              int
	Claim, Release     bool
	Resolution         string
	// Items closes several items at once; nil is a close of one by ID or Agent.
	Items []QueueClose
	View
}

// QueueItemFields are the fields a queue item carries. owner is null when
// nobody holds the item; links holds the typed links given; blocked_by,
// body, agent, origin, repos, base, owns, created and resolution
// appear only when set.
var QueueItemFields = append(typed("id", tStr, "project", tStr, "kind", tStr, "status", tStr, "owner", tOptStr, "text", tStr),
	Field{Name: "links", Type: "object", Inner: typed("pr", tStr, "branch", tStr, "commit", tStr, "slug", tStr)},
	Field{Name: "blocked_by", Type: tList}, Field{Name: "body", Type: tStr},
	Field{Name: "agent", Type: tStr}, Field{Name: "origin", Type: tStr}, Field{Name: "repos", Type: tList},
	Field{Name: "base", Type: tStr}, Field{Name: "owns", Type: tList}, Field{Name: "transcript", Type: tStr}, Field{Name: "created", Type: tStr}, Field{Name: "resolution", Type: tStr})

var (
	leanQueueItem = mustMask(QueueItemFields, "id", "kind", "status", "owner", "text")
	// leanDelegated is the default for include_origin, whose items come from
	// other projects' queues.
	leanDelegated = mustMask(QueueItemFields, "id", "project", "kind", "status", "owner", "text", "origin")
)

func qcall(args ...any) string { return Call("queue", args...) }

// Queue runs one queue action against v.
func Queue(v *Vault, a QueueArgs) *jsonx.Obj {
	if f := v.Unreadable(); f != nil {
		return f
	}
	if v.Profile == nil {
		return failNext("vault "+strconv.Quote(v.Name)+" has no schema profile", noProfile)
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
	}
	return failNext(fmt.Sprintf("unknown action %q; one of %s", a.Action, strings.Join(QueueActions, ", ")),
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
	for _, name := range vault.ProjectFolders(v.Config.VaultPath) {
		// The root check keeps a folder whose symlink leaves the vault out.
		if dir := path.Join(vault.ProjectsDir, name); root.IsDir(dir) {
			s.projects = append(s.projects, s.readProject(name, dir))
		}
	}
	s.projects = append(s.projects, s.readProject(backlogProject, vault.BacklogDir))
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
			p.problem = fmt.Sprintf("%s/index.md sets queue_prefix %s, which cannot start an item id", dir, jsonx.Compact(v))
		}
	}
	r, _ := fm.Get("repos")
	p.repos = slices.DeleteFunc(vault.CoerceList(r), func(s string) bool { return s == "" })
	return p
}

// projectNames are the folder names of ps, in order.
func projectNames(ps []queueProject) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.name)
	}
	return out
}

// ambiguous refuses a call that two or more projects in hits answer: each
// is what they all hold, and fix the one-line repair.
func ambiguous(hits []queueProject, each, fix string) *jsonx.Obj {
	return jsonx.Fail(fmt.Sprintf("projects %s each %s; %s", pystr.Quoted(projectNames(hits)), each, fix))
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
		if f := s.unknownPrefix(prefix, id); f != nil {
			return queueProject{}, item, f
		}
		return queueProject{}, item, failNext(fmt.Sprintf("no queue holds an item %q", id),
			qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "project", "text"})+" to find the item's id")
	}
	return queueProject{}, item, ambiguous(hits, "hold an item "+strconv.Quote(id),
		"give one a distinct queue_prefix in its index.md and renumber the other by hand")
}

// unknownPrefix is the failure for an id whose prefix no queue uses: it lists
// the prefixes, and names the project's own when the id starts with a project
// name. nil when some queue uses the prefix.
func (s *queueStore) unknownPrefix(prefix, id string) *jsonx.Obj {
	var prefixes []string
	hint := ""
	for _, p := range s.projects {
		if p.prefix == prefix {
			return nil
		}
		if p.prefix == "" {
			continue
		}
		prefixes = append(prefixes, p.prefix)
		if hint == "" && sameProject(p.name, prefix) {
			hint = fmt.Sprintf("; %q is the project name, its prefix is %q", prefix, p.prefix)
		}
	}
	slices.Sort(prefixes)
	prefixes = slices.Compact(prefixes)
	return failNext(fmt.Sprintf("no queue uses the id prefix %q (id %q); the prefixes are %s%s", prefix, id, strings.Join(prefixes, ", "), hint),
		qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "project", "text"})+" to find the item's id")
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
		return p, "", failNext(fmt.Sprintf("no project %q; one of %s", a.Project, pystr.Quoted(projectNames(s.projects))), listAll)
	case a.Repo != "":
		return s.byRepo(a, []string{a.Repo}, a.Repo, "repo "+strconv.Quote(a.Repo))
	case a.Cwd != "":
		names, _ := repoCandidates(a.Cwd)
		return s.byRepo(a, names, repoName(a.Cwd), "cwd "+strconv.Quote(a.Cwd))
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
		return queueProject{}, "", ambiguous(hits, "list repo "+strconv.Quote(repo), "keep it in one index.md's repos")
	}
	unmapped := fmt.Sprintf("no project's index.md lists %s under repos", what)
	mapIt := Call("set_frontmatter", "relative_path", path.Join(vault.ProjectsDir, "<project>", "index.md"), "add_repos", []string{repo}) +
		" on the project that owns " + strconv.Quote(repo)
	if a.Action == "list" {
		return s.backlog(), unmapped + "; these are the backlog's items, not this repo's. Map it: " + mapIt, nil
	}
	return queueProject{}, "", failNext(unmapped+"; map it before filing work", mapIt+", then retry",
		qcall("action", a.Action, "project", backlogProject)+" only for work no project owns")
}

// repoName is the repository cwd belongs to: the nearest folder holding
// .git (its main checkout's folder for a git worktree), or cwd's own folder
// when none is found above it.
func repoName(cwd string) string {
	names, root := repoCandidates(cwd)
	return names[max(root, 0)]
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
// worker's worktree maps to the repository it was cut from. root is the
// index of the nearest checkout's name, -1 when no folder holds .git.
func repoCandidates(cwd string) (names []string, root int) {
	root = -1
	dir := filepath.Clean(cwd)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		git := filepath.Join(dir, ".git")
		raw, err := os.ReadFile(git) //nolint:gosec // a caller's working tree, read only
		if root < 0 && (err == nil || fsx.IsDir(git)) {
			root = len(names)
		}
		if main, ok := worktreeMain(raw); err == nil && ok {
			names = append(names, main)
		}
		names = append(names, filepath.Base(dir))
		parent := filepath.Dir(dir)
		if parent == dir {
			return names, root
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

// writable refuses a write to a note holding an item-shaped line under
// ## Open or ## Closed that does not parse: writing past it would count ids
// from items it cannot see.
func (b *queueBody) writable(rel string) *jsonx.Obj {
	for _, at := range b.unparsed {
		if s := b.queueSection(at); !queue.ItemShaped(b.Lines[at]) || s != queueOpen && s != queueClosed {
			continue
		}
		return failNext(fmt.Sprintf("%s: the item line %q does not parse; nothing was written", rel, b.Lines[at]),
			"fix or remove the line in the note, then retry")
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

// change rewrites p's queue under its lock. edit gets the body as it is on
// disk at that moment and may run more than once; it returns a failure, or
// nil once it has changed the body. A note this binary does not fully read
// is refused before edit runs, a body that would carry an id twice is
// refused after.
func (s *queueStore) change(p queueProject, edit func(b *queueBody) *jsonx.Obj) *jsonx.Obj {
	out := write.EditNoteUnindexed(s.v.Loader, p.rel(), s.v.Profile, func(_ *jsonx.Obj, body, _ string) (string, []write.HeadField, *jsonx.Obj) {
		b := parseBody(body, s.links)
		if f := b.writable(p.rel()); f != nil {
			return "", nil, f
		}
		if f := edit(b); f != nil {
			return "", nil, f
		}
		if id := b.duplicateID(); id != "" {
			return "", nil, failNext(fmt.Sprintf("%s would carry id %q on more than one item line; an id never repeats, so nothing was written", p.rel(), id),
				"give one of those lines a new id by hand in the note, then retry; "+
					qcall("action", "list", "all", true, "status", "all", "fields", []string{"id", "project", "text"})+" shows the ids in use")
		}
		return b.String(), nil, nil
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
		case strings.HasPrefix(msg, "frontmatter does not validate"):
			return []string{p.repairCall(msg) + " to repair the queue note, then retry"}
		case strings.Contains(msg, "set_frontmatter"):
			return []string{Call("set_frontmatter", "relative_path", p.rel()) + " with the fields the error names, then retry"}
		}
		return []string{qcall("action", "list", "project", p.name, "status", "all") + " to see the queue as it is now, then retry"}
	})
}

// repairCall is the set_frontmatter call that gives p's queue note the
// fields a queue note carries: its doc_type and system always, and each
// required field msg reports missing.
func (p queueProject) repairCall(msg string) string {
	args := []any{"relative_path", p.rel(), "doc_type", schema.QueueDocType, "system", p.name}
	for _, f := range []struct{ name, value string }{
		{"doc_id", p.docID()}, {"title", queueTitle(p.name)}, {"status", "active"},
	} {
		if strings.Contains(msg, "missing required field: "+f.name) {
			args = append(args, f.name, f.value)
		}
	}
	return Call("set_frontmatter", args...)
}

// queueTitle is the title a project's queue note (or the backlog's) is
// created with, and the one a folder rename carries over.
func queueTitle(name string) string {
	if name == backlogProject {
		return "Backlog Queue"
	}
	return name + " Queue"
}

// create makes p's queue note through the doc create path, with only the
// fields the profile requires of a queue note and no H1.
func (s *queueStore) create(p queueProject) *jsonx.Obj {
	r := write.CreateNote(s.v.Loader, p.rel(), "## "+queueOpen+"\n\n## "+queueClosed+"\n", write.With(
		"doc_id", p.docID(), "title", queueTitle(p.name), "doc_type", schema.QueueDocType, "system", p.name,
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
	for _, f := range [][2]string{{"body", it.Body}, {"agent", it.Agent}, {"origin", it.Origin}} {
		if f[1] != "" {
			o.Set(f[0], f[1])
		}
	}
	if len(it.Repos) > 0 {
		o.Set("repos", slices.Clone(it.Repos))
	}
	if it.Base != "" {
		o.Set("base", it.Base)
	}
	if len(it.Owns) > 0 {
		o.Set("owns", slices.Clone(it.Owns))
	}
	if it.Transcript != "" {
		o.Set("transcript", it.Transcript)
	}
	if it.Created != "" {
		o.Set("created", it.Created)
	}
	if it.Resolution != "" {
		o.Set("resolution", it.Resolution)
	}
	return o
}

// originRepo is the repository an origin argument names: a value with a path
// separator, or "." or "..", is a working directory, resolved as cwd is.
func originRepo(s string) string {
	s = pystr.Strip(s)
	if strings.ContainsAny(s, `/\`) || s == "." || s == ".." {
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
			return fmt.Errorf("owner %q must be %s", o, queue.OwnerRule)
		}
	}
	for k, v := range a.Links {
		if !slices.Contains(queue.LinkKeys, k) {
			return fmt.Errorf("links: %q is not one of %s", k, strings.Join(queue.LinkKeys, ", "))
		}
		if v != "" && !queue.ValidToken(v) {
			return fmt.Errorf("links.%s %q must be %s", k, v, queue.TokenRule)
		}
	}
	for _, ref := range a.BlockedBy {
		if _, _, ok := queue.SplitID(ref); !ok {
			return fmt.Errorf("blocked_by %q is not an item id such as example-3", ref)
		}
	}
	if a.Body != nil && a.Action != "list" {
		if v := pystr.Strip(*a.Body); v != "" && !queue.ValidToken(v) {
			return fmt.Errorf("body %q must be %s", v, queue.TokenRule)
		}
	}
	for _, r := range commaList(a.Repos) {
		if !queue.ValidRepo(r) {
			return fmt.Errorf("repos %q must each be a repository name, %s, without @", r, queue.TokenRule)
		}
	}
	if a.Base != nil && a.Action != "list" {
		if b := pystr.Strip(*a.Base); b != "" && !queue.ValidToken(b) {
			return fmt.Errorf("base %q must be a branch name, %s", b, queue.TokenRule)
		}
	}
	if a.Transcript != nil && a.Action != "list" {
		if t := pystr.Strip(*a.Transcript); t != "" && (!queue.ValidPath(t) || !strings.HasPrefix(t, "/")) {
			return fmt.Errorf("transcript %q must be an absolute path, %s", t, queue.PathRule)
		}
	}
	for _, o := range commaList(a.Owns) {
		if !queue.ValidPath(o) {
			return fmt.Errorf("owns %q must each be %s", o, queue.PathRule)
		}
	}
	if o := originRepo(a.Origin); o != "" && !queue.ValidToken(o) {
		return fmt.Errorf("origin %q must be a repository name, %s, or a working directory", o, queue.TokenRule)
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

// commaList flattens a list argument (repos, owns), whose entries may be
// comma lists.
func commaList(given []string) []string {
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

// checkBody refuses a body that names no doc.
func (s *queueStore) checkBody(a QueueArgs) error {
	if a.Body != nil {
		if body := pystr.Strip(*a.Body); body != "" {
			if errs := s.v.Profile.DocRefProblems("body", body, s.v.Loader.Index(false).Has); len(errs) > 0 {
				return errors.New(strings.Join(errs, "; "))
			}
		}
	}
	return nil
}

// setBody applies a given body to it, written as a link to its note; ""
// removes it.
func (s *queueStore) setBody(it *queue.Item, a QueueArgs) {
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
		return failNext("agent "+strconv.Quote(a.Agent)+" must be "+queue.TokenRule, retry("with the agent id as one word"))
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
	if err := s.checkBody(a); err != nil {
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
	existing := false
	floor := s.topNumber(p)
	if f := s.change(p, func(b *queueBody) *jsonx.Obj {
		existing = false
		top := max(floor, queue.Top(b.String(), p.prefix))
		// A relaunched worker files its goal again; the item it already
		// holds for that goal answers instead of a second one.
		for _, x := range b.items {
			if a.Agent != "" && x.it.Agent == a.Agent && !queue.Done(x.it.Status) && x.it.Text == pystr.Strip(*a.Text) {
				item, existing = x.it, true
				return nil
			}
		}
		item = queue.Item{ID: fmt.Sprintf("%s-%d", p.prefix, top+1), Kind: a.Kind, Status: "open",
			Text: pystr.Strip(*a.Text), BlockedBy: a.BlockedBy, Agent: a.Agent, Origin: originRepo(a.Origin),
			Repos: commaList(a.Repos), Owns: commaList(a.Owns), Created: clock.TodayISO()}
		if a.Base != nil {
			item.Base = pystr.Strip(*a.Base)
		}
		if a.Transcript != nil {
			item.Transcript = pystr.Strip(*a.Transcript)
		}
		if a.Owner != nil {
			item.Owner = owner(*a.Owner)
		}
		applyLinks(&item, a.Links)
		s.setBody(&item, a)
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
	if existing {
		out.Set("no_op", true)
	}
	return out
}

// target is the item a set or close names, by id or by agent.
func (s *queueStore) target(a QueueArgs) (queueProject, string, *jsonx.Obj) {
	findIt := qcall("action", "list", "all", true, "fields", []string{"id", "project", "text", "agent"}) + " to find the item's id"
	// set with both names the item by id and writes the agent onto it.
	if (a.ID == "") == (a.Agent == "") && (a.Action != "set" || a.ID == "") {
		return queueProject{}, "", failNext(a.Action+" names one item: give id, or agent for a worker's item", findIt)
	}
	if a.ID != "" {
		if _, _, ok := queue.SplitID(a.ID); !ok {
			msg := fmt.Sprintf("id %q is not <prefix>-<number>, such as example-3", a.ID)
			if strings.ContainsAny(a.ID, " \t\n") {
				msg += "; pass one id per call, not several joined"
				if a.Action == "close" {
					msg += ", or close several as items"
				}
			}
			return queueProject{}, "", failNext(msg, findIt)
		}
		p, _, f := s.locate(a.ID)
		return p, a.ID, f
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
		msg := "no open item carries agent " + strconv.Quote(a.Agent)
		if closedID != "" {
			msg += "; " + closedID + " does, and it is done"
		}
		return queueProject{}, "", failNext(msg, qcall("action", "add", "kind", "delegated", "agent", a.Agent, "text", "...")+" to track the worker first")
	}
	var ids []string
	for _, h := range open {
		ids = append(ids, h.id)
	}
	return queueProject{}, "", failNext(fmt.Sprintf("agent %q carries %s; give id instead", a.Agent, pystr.Quoted(ids)),
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
			return failNext(fmt.Sprintf("no item %q in %s", id, p.rel()),
				qcall("action", "list", "project", p.name, "status", "all", "fields", []string{"id", "status", "text"})+" to see its ids")
		}
		it := b.items[i].it
		if a.ID == "" && a.Agent != "" && it.Agent != a.Agent {
			return failNext(fmt.Sprintf("%s no longer carries agent %q", id, a.Agent), qcall("action", a.Action, "id", id))
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
		return failNext(fmt.Sprintf("status %q is not one of %s", a.Status, strings.Join(queue.Statuses, ", ")),
			qcall("action", "set", "id", a.ID, "status", "open"))
	}
	if queue.Done(a.Status) {
		return failNext("an item closes with its resolution", qcall("action", "close", "id", a.ID, "status", a.Status, "resolution", "..."))
	}
	if a.Release && (a.Claim || a.Owner != nil || a.Status != "" || a.Kind != "") {
		return failNext("release hands the item back on its own; give no claim, owner, status or kind with it",
			qcall("action", "set", "id", a.ID, "release", true))
	}
	if a.Claim && (a.Owner == nil || owner(*a.Owner) == "") {
		return failNext("claim needs owner: who takes the item", qcall("action", "set", "id", a.ID, "claim", true, "owner", "..."))
	}
	if a.ID != "" && a.Agent != "" && !queue.ValidToken(a.Agent) {
		return failNext("agent "+strconv.Quote(a.Agent)+" must be "+queue.TokenRule, qcall("action", "set", "id", a.ID, "agent", "...")+" with the agent id as one word")
	}
	if a.Kind != "" && !slices.Contains(queue.Kinds, a.Kind) {
		return failNext(fmt.Sprintf("kind %q is not one of %s", a.Kind, strings.Join(queue.Kinds, ", ")), qcall("action", "set", "id", a.ID, "kind", "delegated"))
	}
	p, id, f := s.target(a)
	if f != nil {
		return f
	}
	if err := s.checkBody(a); err != nil {
		return failNext(err.Error(), qcall("action", "set", "id", id)+" with what the error names fixed")
	}
	held := ""
	return warnHeld(s.edit(p, id, a, func(it *queue.Item) *jsonx.Obj {
		held = holder(*it, a.Agent)
		if a.Resolution != "" {
			if !queue.Done(it.Status) {
				return failNext(fmt.Sprintf("%s is %s; a resolution is recorded by close, and corrected by set only on a closed or dropped item", id, it.Status),
					qcall("action", "close", "id", id, "resolution", "..."))
			}
			if a.Claim || a.Release || a.Status != "" || a.Agent != "" || a.Kind != "" {
				return failNext(fmt.Sprintf("%s is %s; set corrects its resolution and takes no status, claim, release, agent or kind with it", id, it.Status),
					qcall("action", "set", "id", id, "resolution", a.Resolution))
			}
			resolution := pystr.Strip(a.Resolution)
			if err := queue.OneLine("resolution", resolution); err != nil {
				return failNext(err.Error(), qcall("action", "set", "id", id, "resolution", "example-repo@abc1234"))
			}
			it.Resolution = resolution
		}
		if queue.Done(it.Status) && (a.Claim || a.Status != "") {
			return failNext(fmt.Sprintf("%s is %s; a done item is not reopened or claimed", id, it.Status),
				qcall("action", "add", "kind", "next", "text", "...")+" to file the work as a new item")
		}
		if a.Claim {
			pick := qcall("action", "list", "project", p.name, "ready", true, "owner", queue.Unowned) + " to pick unowned work"
			switch {
			case it.Agent != "" && it.Agent != a.Agent:
				return failNext(fmt.Sprintf("%s is held by agent %s", id, it.Agent), pick).Set("agent", it.Agent)
			case it.Kind == "decision" && a.Kind == "delegated" && it.Owner != "" && it.Owner != decisionClaimer:
				return failNext(fmt.Sprintf("%s is owned by %s; only a decision the coordinating session (%s) owns or nobody owns is claimed", id, it.Owner, decisionClaimer), pick).Set("owner", it.Owner)
			case it.Kind == "decision" && a.Kind == "delegated":
				// The coordinating session's own decision passes to the worker that takes it.
			case it.Owner != "" && it.Owner != owner(*a.Owner):
				return failNext(fmt.Sprintf("%s is owned by %s", id, it.Owner), pick).Set("owner", it.Owner)
			}
			it.Status = "open"
		}
		if a.Release {
			switch {
			case queue.Done(it.Status):
				return failNext(fmt.Sprintf("%s is %s; only a held item is released", id, it.Status),
					qcall("action", "list", "project", p.name, "status", "all", "fields", []string{"id", "status", "agent"}))
			case a.ID != "" && a.Agent != "" && it.Agent != a.Agent:
				by := "nobody"
				if it.Agent != "" {
					by = "agent " + it.Agent
				}
				return failNext(fmt.Sprintf("%s is held by %s, not agent %q", id, by, a.Agent),
					qcall("action", "set", "id", id, "release", true)+" to release it from whoever holds it").Set("agent", it.Agent)
			}
			it.Agent, it.Owner, it.Status = "", "", "open"
			if it.Kind == "delegated" {
				it.Kind = "next"
			}
		}
		if a.Kind != "" && a.Kind != it.Kind {
			claimedDecision := a.Claim && it.Kind == "decision"
			if a.Kind != "delegated" || (it.Kind != "next" && it.Kind != "follow-up" && !claimedDecision) {
				return failNext(fmt.Sprintf("set changes a next or follow-up item, or a decision claimed with claim, to delegated; %s is %s, not %s", id, it.Kind, a.Kind),
					qcall("action", "close", "id", id, "resolution", "...")+" and add the "+a.Kind+" item instead")
			}
			it.Kind = a.Kind
		}
		if a.ID != "" && a.Agent != "" && !a.Release {
			it.Agent = a.Agent
		}
		if a.Owner != nil {
			it.Owner = owner(*a.Owner)
		}
		if a.Text != nil {
			it.Text = pystr.Strip(*a.Text)
		}
		applyLinks(it, a.Links)
		s.setBody(it, a)
		if a.HasBlockedBy {
			it.BlockedBy = a.BlockedBy
		}
		if a.HasRepos {
			it.Repos = commaList(a.Repos)
		}
		if a.Base != nil {
			it.Base = pystr.Strip(*a.Base)
		}
		if a.Transcript != nil {
			it.Transcript = pystr.Strip(*a.Transcript)
		}
		if a.HasOwns {
			it.Owns = commaList(a.Owns)
		}
		if a.Status != "" {
			if a.Status == "returned" && it.Kind != "delegated" {
				return failNext(fmt.Sprintf("only a delegated item is returned; %s is %s", id, it.Kind),
					qcall("action", "set", "id", id, "status", "open"))
			}
			it.Status = a.Status
		}
		return nil
	}), &held)
}

// QueueClose is one item of a close over several: its id, and its own
// resolution and status, each the call's when left empty.
type QueueClose struct{ ID, Resolution, Status string }

// closeArgs checks a close's status and resolution before anything is read,
// and gives the status it closes with (closed by default).
func closeArgs(id, status, resolution string, partial bool) (string, *jsonx.Obj) {
	if err := queue.OneLine("resolution", resolution); err != nil {
		return "", failNext("close needs resolution: one line such as repo@sha, a PR, a doc id, or why it was dropped; on the CLI: queue close "+
			cmp.Or(id, "<id>")+" --resolution 'example-repo@abc1234'",
			qcall("action", "close", "id", id, "resolution", "example-repo@abc1234"))
	}
	given := status
	status = cmp.Or(status, "closed")
	if !queue.Done(status) {
		return "", failNext(fmt.Sprintf("close takes status closed or dropped, not %q", given),
			qcall("action", "close", "id", id, "status", "dropped", "resolution", "..."))
	}
	if partial {
		if status != "closed" {
			return "", failNext("partial records landings; drop the item with status=dropped and no partial",
				qcall("action", "close", "id", id, "status", "dropped", "resolution", "..."))
		}
		if _, ok := queue.Landings(resolution); !ok {
			return "", failNext("partial takes the landed repos as repo@sha, comma-separated",
				qcall("action", "close", "id", id, "partial", true, "resolution", "example-repo@abc1234"))
		}
	}
	return status, nil
}

func (s *queueStore) close(a QueueArgs) *jsonx.Obj {
	if a.Items != nil {
		return s.closeMany(a)
	}
	resolution := pystr.Strip(a.Resolution)
	status, f := closeArgs(a.ID, a.Status, resolution, a.Partial)
	if f != nil {
		return f
	}
	if err := checkInput(a); err != nil {
		return failNext(err.Error(), qcall("action", "close", "id", a.ID)+" with what the error names fixed")
	}
	p, id, f := s.target(a)
	if f != nil {
		return f
	}
	held := ""
	return warnHeld(s.edit(p, id, a, func(it *queue.Item) *jsonx.Obj {
		held = holder(*it, a.Agent)
		return closeItem(it, status, resolution, a.Partial, a.Links)
	}), &held)
}

// closeItem closes it with status and resolution, or refuses why it can't:
// an item with repos lands them, and a done item takes only the resolution
// it already has (or, a box ticked in Obsidian, its first).
func closeItem(it *queue.Item, status, resolution string, partial bool, links map[string]string) *jsonx.Obj {
	id := it.ID
	if len(it.Repos) > 0 && !queue.Done(it.Status) {
		applyLinks(it, links)
		return land(it, status, resolution, partial)
	}
	if partial && len(it.Repos) == 0 {
		return failNext(id+" lists no repos; partial is for one worker's item spanning several",
			qcall("action", "close", "id", id, "resolution", resolution))
	}
	if queue.Done(it.Status) && it.Resolution != "" {
		if it.Status == status && (it.Resolution == resolution || partial && recorded(it.Resolution, resolution)) {
			return nil
		}
		return failNext(fmt.Sprintf("%s is already %s → %s", id, it.Status, it.Resolution),
			qcall("action", "add", "kind", "next", "text", "...")+" to file the work as a new item")
	}
	applyLinks(it, links)
	it.Status, it.Resolution = status, resolution
	return nil
}

// closeMany closes each of a.Items with its own resolution and status, every
// change to one queue in one locked rewrite. Each item gets its own result,
// so one refused item leaves the others closed.
func (s *queueStore) closeMany(a QueueArgs) *jsonx.Obj {
	example := []*jsonx.Obj{jsonx.New("id", "example-3", "resolution", "example-repo@abc1234"),
		jsonx.New("id", "example-4", "status", "dropped", "resolution", "superseded by example-9")}
	if a.ID != "" || a.Agent != "" || a.Partial || a.Links != nil {
		return failNext("items closes each item by its id; give no id, agent, partial or links with it",
			qcall("action", "close", "items", example))
	}
	if len(a.Items) == 0 {
		return failNext("items: give at least one item to close", qcall("action", "close", "items", example))
	}
	type job struct {
		at                     int
		id, status, resolution string
	}
	results := make([]*jsonx.Obj, len(a.Items))
	var order []queueProject
	jobs := map[string][]job{}
	for i, c := range a.Items {
		id := pystr.Strip(c.ID)
		resolution := pystr.Strip(cmp.Or(c.Resolution, a.Resolution))
		status, f := closeArgs(id, cmp.Or(c.Status, a.Status), resolution, false)
		if f == nil {
			if _, _, ok := queue.SplitID(id); !ok {
				f = failNext(fmt.Sprintf("id %q is not <prefix>-<number>, such as example-3", id),
					qcall("action", "list", "all", true, "contains", id, "fields", []string{"id", "project", "text"})+" to find the item's id")
			} else if slices.ContainsFunc(a.Items[:i], func(o QueueClose) bool { return pystr.Strip(o.ID) == id }) {
				f = failNext(fmt.Sprintf("%s is given more than once; the first stands", id), qcall("action", "close", "items", example))
			}
		}
		var p queueProject
		if f == nil {
			p, _, f = s.locate(id)
		}
		if f != nil {
			results[i] = f.Set("id", id)
			continue
		}
		if _, seen := jobs[p.name]; !seen {
			order = append(order, p)
		}
		jobs[p.name] = append(jobs[p.name], job{i, id, status, resolution})
	}
	for _, p := range order {
		js := jobs[p.name]
		if _, f := s.readWritable(p); f != nil {
			for _, j := range js {
				results[j.at] = jsonx.Failure().Merge(f).Set("id", j.id)
			}
			continue
		}
		// edit may run more than once; each run starts the results over, and
		// a run that changes no item writes nothing.
		ran, changed := false, false
		f := s.change(p, func(b *queueBody) *jsonx.Obj {
			ran, changed = false, false
			for _, j := range js {
				i := b.find(j.id)
				if i < 0 {
					results[j.at] = failNext(fmt.Sprintf("no item %q in %s", j.id, p.rel()),
						qcall("action", "list", "project", p.name, "status", "all", "fields", []string{"id", "status", "text"})+" to see its ids").Set("id", j.id)
					continue
				}
				it := b.items[i].it
				held := holder(it, "")
				before := queue.Format(it)
				f := closeItem(&it, j.status, j.resolution, false, nil)
				if f == nil {
					if err := it.Validate(); err != nil {
						f = failNext(err.Error(), qcall("action", "close", "id", j.id)+" with what the error names fixed")
					}
				}
				if f != nil {
					results[j.at] = f.Set("id", j.id)
					continue
				}
				out := jsonx.New("ok", true, "id", j.id, "project", p.name, "status", it.Status, "resolution", it.Resolution)
				if queue.Format(it) == before {
					out.Set("no_op", true)
				} else {
					b.put(i, it)
					changed = true
				}
				results[j.at] = warnHeld(out, &held)
			}
			ran = true
			if !changed {
				return jsonx.Fail("nothing to write")
			}
			return nil
		})
		if f != nil && (!ran || changed) {
			for _, j := range js {
				results[j.at] = jsonx.Failure().Merge(f).Set("id", j.id)
			}
		}
	}
	var refused []string
	for i, r := range results {
		if r.Failed() {
			refused = append(refused, pystr.Strip(a.Items[i].ID))
		}
	}
	if len(refused) == 0 {
		return jsonx.New("ok", true, "count", len(results), "results", results)
	}
	return failNext(fmt.Sprintf("%d of %d items were not closed: %s; the others were", len(refused), len(results), pystr.Quoted(refused)),
		"each refused result's error and next_steps say how to close it; retry those alone").
		Set("count", len(results)).Set("results", results)
}

// holder is the agent holding it (open or returned, with an agent) when
// caller is not that agent, or "".
func holder(it queue.Item, caller string) string {
	if it.Agent == "" || it.Agent == caller || it.Status != "open" && it.Status != "returned" {
		return ""
	}
	return it.Agent
}

// warnHeld adds a warning to a change made to an item another agent held:
// the change stands, and the caller learns whose work it touched.
func warnHeld(out *jsonx.Obj, held *string) *jsonx.Obj {
	if *held != "" && out.Bool("ok") && !out.Bool("no_op") {
		out.Set("warning", fmt.Sprintf("%s was held by agent %s", out.Str("id"), *held))
	}
	return out
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
			return failNext(fmt.Sprintf("%q is not one of %s's repos %s", l.Repo, it.ID, strings.Join(it.Repos, ", ")),
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

// gistChars is about how much of an item's text the default list shows.
const gistChars = 100

// gist is the text the default list shows: the whole text when it fits in
// gistChars, else its first clause (up to the first "; ") when that clause is
// long enough to name the work, else its words up to gistChars. A cut text
// ends in "…"; fields with text gives it whole.
func gist(s string) string {
	if utf8.RuneCountInString(s) <= gistChars {
		return s
	}
	if clause, _, ok := strings.Cut(s, "; "); ok {
		if n := utf8.RuneCountInString(clause); n >= gistChars/3 && n <= gistChars {
			return clause + "…"
		}
	}
	head := pystr.Prefix(s, gistChars)
	if i := strings.LastIndexByte(head, ' '); i > len(head)/2 {
		head = head[:i]
	}
	return strings.TrimRight(head, " ,;:") + "…"
}

// linksMatch reports whether an item carries each wanted link exactly; a
// wanted "" matches an item without that link.
func linksMatch(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
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
		return failNext(fmt.Sprintf("kind %q is not one of %s", a.Kind, strings.Join(queue.Kinds, ", ")), qcall("action", "list", "all", true))
	}
	statuses := defaultStatuses
	switch {
	case a.Status == "all":
		statuses = queue.Statuses
	case a.Status != "":
		statuses = strings.Split(a.Status, ",")
		for _, st := range statuses {
			if !slices.Contains(queue.Statuses, st) {
				return failNext(fmt.Sprintf("status %q is not one of %s, or all", st, strings.Join(queue.Statuses, ", ")),
					qcall("action", "list", "all", true, "status", "open,returned"))
			}
		}
	}
	scoped := a.Project != "" || a.Repo != "" || a.Cwd != ""
	var targets []queueProject
	fallback := ""
	switch {
	case a.IncludeOrigin && !scoped:
		return failNext("include_origin adds to one queue: give project, repo or cwd",
			qcall("action", "list", "cwd", "<the session's working directory>", "include_origin", true))
	case a.All && scoped:
		return failNext("give all, or one of project, repo or cwd", qcall("action", "list", "all", true))
	case a.All || (!scoped && (a.Agent != "" || a.Origin != "" || len(a.Links) > 0)):
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
				!linksMatch(it.Links, a.Links),
				a.Owner != nil && it.Owner != owner(*a.Owner),
				a.Contains != "" && !containsFold(it.Text, a.Contains) && !containsFold(it.ID, a.Contains):
				continue
			}
			if a.Ready && (it.Status != "open" || (it.Kind != "next" && it.Kind != "follow-up") ||
				slices.ContainsFunc(it.BlockedBy, func(b string) bool { return !closedIDs(b) })) {
				continue
			}
			o := itemObj(it, p.name)
			if m != nil && a.Fields == nil {
				o.Set("text", gist(it.Text))
			}
			matched = append(matched, o)
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
