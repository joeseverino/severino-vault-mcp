package doctor

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// references checks the code a living doc names in its inline code spans (a
// repository path, a Go or Python test, a `mise run` task) against the
// checkout of each project the doc belongs to: the project folder it is in
// and its related_projects. A project's checkout is its index.md's
// project_path. Each checkout is read at most once per run.
type references struct {
	vault     string
	checkouts map[string]*checkout // by project folder name
	byRepo    map[string]*checkout // by repository name; nil until a path needs it
	found     map[string]bool      // whether a path is there, by kind and absolute path
}

func newReferences(vaultPath string) *references {
	return &references{vault: vaultPath, checkouts: map[string]*checkout{}, found: map[string]bool{}}
}

// checkout is one project's working copy, read lazily: the tests, tasks and
// ignore rules only when a doc needs them.
type checkout struct {
	index   string // the project's index.md, vault-relative
	written string // project_path as the index writes it
	root    string // project_path expanded; "" when the index sets none
	repos   []string
	present bool
	told    bool // the missing checkout was reported
	tests   map[string]bool
	tasks   map[string]bool
	ignore  []string // root .gitignore patterns; nil until read
}

// ref is one reference found in a code span.
type ref struct {
	kind string // refPath, refDir, refTest or refTask
	name string
	line int
}

const (
	refPath = "path"
	refDir  = "dir"
	refTest = "test"
	refTask = "task"
)

func (x ref) String() string {
	if x.kind == refTask {
		return pystr.Repr("mise run " + x.name)
	}
	return pystr.Repr(x.name)
}

// check adds a warning naming each reference in a living part of the doc at
// rel that none of its projects' checkouts has, and once per run for a
// project whose checkout is not on this machine.
func (rs *references) check(r *Report, rel, body string, fm *jsonx.Obj) {
	if fm == nil || fm.Text("doc_type") == schema.QueueDocType {
		return // a queue names the paths its open work will create
	}
	if s := cmp.Or(fm.Text("status"), "active"); s != "active" && s != "draft" {
		return
	}
	rel = filepath.ToSlash(rel)
	var outs []*checkout
	projects := rs.projects(rel, fm)
	for _, name := range projects {
		if c := rs.checkout(name); c.root != "" {
			outs = append(outs, c)
		}
	}
	if len(outs) == 0 {
		return
	}
	found := codeRefs(living(body, fm))
	if len(found) == 0 {
		return
	}
	var present []*checkout
	for _, c := range outs {
		if c.present {
			present = append(present, c)
		} else if !c.told {
			c.told = true
			r.add(Finding{c.index, "warning", fmt.Sprintf("project_path %s is not a folder on this machine, so the code its docs name is not checked",
				pystr.Repr(c.written)), ""})
		}
	}
	if len(present) == 0 {
		return
	}
	// A doc also names notes beside it, in its project's folder, or by a
	// path from the projects folder or the vault root.
	notes := []string{path.Dir(rel), vault.ProjectsDir, "."}
	for _, p := range projects {
		notes = append(notes, path.Join(vault.ProjectsDir, p))
	}
	var missing []string
	at := map[string][]string{}
	for _, x := range found {
		if rs.has(x, present, notes) {
			continue
		}
		k := x.String()
		if _, seen := at[k]; !seen {
			missing = append(missing, k)
		}
		at[k] = append(at[k], strconv.Itoa(x.line))
	}
	if len(missing) == 0 {
		return
	}
	items := make([]string, len(missing))
	for i, k := range missing {
		items[i] = fmt.Sprintf("%s (body line %s)", k, strings.Join(slices.Compact(at[k]), ", "))
	}
	where := make([]string, len(present))
	for i, c := range present {
		where[i] = pystr.Repr(c.written)
	}
	r.add(Finding{rel, "warning", fmt.Sprintf("names code missing from %s: %s; point each at what the code has now, or drop it",
		strings.Join(where, " or "), strings.Join(items, ", ")), ""})
}

// projects are the project folders the doc at rel belongs to: the one it
// sits in, then its related_projects.
func (rs *references) projects(rel string, fm *jsonx.Obj) []string {
	var out []string
	if p := vault.ProjectOf(rel); p != "" {
		out = append(out, p)
	}
	related, _ := fm.Get("related_projects")
	for _, p := range vault.CoerceList(related) {
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (rs *references) checkout(name string) *checkout {
	if c, ok := rs.checkouts[name]; ok {
		return c
	}
	index := path.Join(vault.ProjectsDir, name, "index.md")
	c := &checkout{index: index}
	rs.checkouts[name] = c
	if strings.ContainsAny(name, `/\`) || name == ".." {
		return c
	}
	note, err := frontmatter.ReadFile(filepath.Join(rs.vault, filepath.FromSlash(index)))
	if err != nil || note.FM == nil {
		return c
	}
	repos, _ := note.FM.Get("repos")
	c.repos = vault.CoerceList(repos)
	if c.written = note.FM.Text("project_path"); c.written != "" {
		c.root = config.ExpandUser(c.written)
		c.present = fsx.IsDir(c.root)
	}
	return c
}

// repo is the checkout on this machine of the repository named name: a
// project folder, one of its repos, or its project_path's last element.
func (rs *references) repo(name string) *checkout {
	if rs.byRepo == nil {
		rs.byRepo = map[string]*checkout{}
		for _, name := range vault.ProjectFolders(rs.vault) {
			if c := rs.checkout(name); c.present {
				for _, n := range append([]string{name, filepath.Base(c.root)}, c.repos...) {
					if _, taken := rs.byRepo[n]; !taken && n != "" {
						rs.byRepo[n] = c
					}
				}
			}
		}
	}
	return rs.byRepo[name]
}

// has reports whether x is in one of the checkouts, a path in one of the
// vault folders notes, or a path that starts with a repository's name and
// is in that repository. A path the checkout's .gitignore covers is output
// the code makes, so it counts as there.
func (rs *references) has(x ref, in []*checkout, notes []string) bool {
	switch x.kind {
	case refTest:
		return slices.ContainsFunc(in, func(c *checkout) bool {
			if c.tests == nil {
				c.tests = testNames(c.root)
			}
			return c.tests[x.name]
		})
	case refTask:
		return slices.ContainsFunc(in, func(c *checkout) bool {
			if c.tasks == nil {
				c.tasks = miseTasks(c.root)
			}
			return c.tasks[x.name]
		})
	}
	if slices.ContainsFunc(in, func(c *checkout) bool { return rs.at(c.root, x.kind, x.name) || c.ignored(x.name) }) ||
		slices.ContainsFunc(notes, func(d string) bool { return rs.at(filepath.Join(rs.vault, filepath.FromSlash(d)), x.kind, x.name) }) {
		return true
	}
	first, rest, _ := strings.Cut(x.name, "/")
	if c := rs.repo(first); c != nil && (rest == "" || rs.at(c.root, x.kind, rest) || c.ignored(rest)) {
		return true
	}
	last := path.Base(x.name)
	return x.kind == refPath && !strings.Contains(rest, "/") && rs.repo(last) != nil // a GitHub owner/repo
}

// at reports whether root holds p: a folder for refDir, anything for refPath.
func (rs *references) at(root, kind, p string) bool {
	full := filepath.Join(root, filepath.FromSlash(p))
	key := kind + ":" + full
	if v, ok := rs.found[key]; ok {
		return v
	}
	v := fsx.IsDir(full)
	if kind == refPath && !v {
		v = exists(full)
	}
	rs.found[key] = v
	return v
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ignored reports whether the checkout's root .gitignore covers p: a pattern
// with no inner "/" matches any segment, one with a "/" matches from the
// root.
func (c *checkout) ignored(p string) bool {
	if c.ignore == nil {
		c.ignore = []string{}
		if b, err := os.ReadFile(filepath.Join(c.root, ".gitignore")); err == nil {
			for _, line := range sections.Lines(string(b)) {
				if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "!") {
					c.ignore = append(c.ignore, strings.TrimSuffix(line, "/"))
				}
			}
		}
	}
	segs := strings.Split(p, "/")
	for _, pat := range c.ignore {
		anchored := strings.Contains(pat, "/")
		pat = strings.TrimPrefix(pat, "/")
		for i := range segs {
			target := segs[i]
			if anchored {
				target = strings.Join(segs[:i+1], "/")
			}
			if ok, _ := path.Match(pat, target); ok {
				return true
			}
		}
	}
	return false
}

// codeRefs are the references in text's inline code spans, outside fenced
// code, with their line numbers. Prose is never read.
func codeRefs(text string) []ref {
	var out []ref
	lines := sections.Lines(text)
	for i, fenced := range sections.Fenced(lines) {
		if fenced || !strings.Contains(lines[i], "`") {
			continue
		}
		parts := strings.Split(lines[i], "`")
		for j := 1; j < len(parts)-1; j += 2 {
			for _, x := range spanRefs(parts[j]) {
				x.line = i + 1
				out = append(out, x)
			}
		}
	}
	return out
}

var (
	taskName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_:.-]*$`)
	testName  = regexp.MustCompile(`^(?:Test[A-Z0-9_][A-Za-z0-9_]*|test_[A-Za-z0-9_]+)$`)
	testWords = regexp.MustCompile(`\b(?:Test[A-Z0-9_][A-Za-z0-9_]*|test_[A-Za-z0-9_]+)\b`)
	notWord   = regexp.MustCompile(`[^A-Za-z0-9_.]+`)
	extension = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,9}$`)
)

// spanRefs are the references in one code span: the tasks of a `mise run`
// in it, every token that reads as a repository path, and every test name
// among the rest. A `cd` moves what follows it to another folder, so the
// tokens after one are left out.
func spanRefs(span string) []ref {
	tokens := strings.Fields(span)
	for i, t := range tokens {
		tokens[i] = strings.Trim(t, `'"(),;`)
	}
	if cd := slices.Index(tokens, "cd"); cd >= 0 {
		tokens = tokens[:cd]
	}
	var out []ref
	used := make([]bool, len(tokens))
	for _, i := range miseRunTasks(tokens) {
		used[i] = true
		out = append(out, ref{kind: refTask, name: tokens[i]})
	}
	for i, t := range tokens {
		if used[i] {
			continue
		}
		head, tests, _ := strings.Cut(t, "::") // pytest's file::test
		if kind, p, ok := repoPath(head); ok {
			if i == 0 || !continuesPath(tokens[i-1]) {
				out = append(out, ref{kind: kind, name: p})
			}
		} else {
			tests = t
		}
		for piece := range strings.SplitSeq(notWord.ReplaceAllString(tests, " "), " ") {
			if testName.MatchString(piece) {
				out = append(out, ref{kind: refTest, name: piece})
			}
		}
	}
	return out
}

// continuesPath reports whether a path token after prev is the rest of a
// path with a space in it: a vault folder's number (`03 Runbooks/x.md`) or
// a path that stops short of a file or a "/" (`~/Documents/Some Folder/`).
func continuesPath(prev string) bool {
	if strings.Trim(prev, "0123456789") == "" {
		return true
	}
	return strings.Contains(prev, "/") && !strings.HasSuffix(prev, "/") && !extension.MatchString(path.Base(prev))
}

// miseRunTasks are the indexes of the task tokens of a `mise run` (or
// `mise r`) among tokens, and of each task after a `:::`. A run with -C or
// --cd names another directory's tasks, so it is left out.
func miseRunTasks(tokens []string) []int {
	var out []int
	for i := 0; i < len(tokens); i++ {
		if tokens[i] != "mise" {
			continue
		}
		j := i + 1
		other := false
		for ; j < len(tokens) && strings.HasPrefix(tokens[j], "-"); j++ {
			other = other || tokens[j] == "-C" || tokens[j] == "--cd" || strings.HasPrefix(tokens[j], "--cd=")
		}
		if other || j >= len(tokens) || tokens[j] != "run" && tokens[j] != "r" {
			continue
		}
		for j++; j < len(tokens); j++ {
			for j < len(tokens) && strings.HasPrefix(tokens[j], "-") {
				j++
			}
			if j < len(tokens) && taskName.MatchString(tokens[j]) {
				out = append(out, j)
			}
			for j < len(tokens) && tokens[j] != ":::" && tokens[j] != "mise" {
				j++
			}
			if j >= len(tokens) || tokens[j] != ":::" {
				break
			}
		}
		i = j - 1
	}
	return out
}

// repoPath reads token as a path inside a repository: relative, holding a
// "/", and naming a file (a last segment with an extension) or a folder
// below the top (a trailing "/", or Go's "/..." package pattern). A lone
// top folder (`images/`) says nothing of where it is. Absolute and home
// paths, URLs, module and host paths, globs, placeholders and lists of
// extensions (`.svg/.ico`) are not repository paths.
func repoPath(token string) (kind, p string, ok bool) {
	p = strings.TrimPrefix(token, "./")
	pkg := strings.HasSuffix(p, "/...")
	p = strings.TrimSuffix(p, "...")
	if !strings.Contains(p, "/") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "..") ||
		strings.ContainsAny(p, "~$*?{}[]<>=@:%\\|#!&^+`'\"") {
		return "", "", false
	}
	segs := strings.Split(strings.TrimSuffix(p, "/"), "/")
	if slices.Contains(segs, "") || slices.Contains(segs, "..") || slices.Contains(segs, ".") ||
		!slices.ContainsFunc(segs, func(s string) bool { return !strings.HasPrefix(s, ".") }) {
		return "", "", false
	}
	if first := segs[0]; strings.Contains(first[1:], ".") {
		return "", "", false // a host or module path: example.com/x
	}
	if strings.HasSuffix(p, "/") {
		if len(segs) < 2 && !pkg {
			return "", "", false
		}
		return refDir, strings.TrimSuffix(p, "/"), true
	}
	if extension.MatchString(segs[len(segs)-1]) {
		return refPath, p, true
	}
	return "", "", false
}

// skipDirs hold no test a repository defines: dependencies, environments,
// build output and caches.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "venv": true, "__pycache__": true,
	"dist": true, "build": true, "target": true, "site-packages": true}

// testNames are the Go and Python test names that appear in root's .go and
// .py files, found by one walk.
func testNames(root string) map[string]bool {
	out := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(name); ext != ".go" && ext != ".py" || !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // a project's checkout, read only
		if err != nil {
			return nil
		}
		for _, m := range testWords.FindAll(b, -1) {
			out[string(m)] = true
		}
		return nil
	})
	return out
}

// miseConfigs are the files mise reads a directory's tasks from.
var miseConfigs = []string{"mise.toml", ".mise.toml", "mise.local.toml", "mise.*.toml", ".mise.*.toml",
	"mise/config.toml", ".mise/config.toml", ".config/mise.toml", ".config/mise/config.toml"}

// miseTaskDirs are mise's file task folders when no task_config.includes
// names others.
var miseTaskDirs = []string{"mise-tasks", ".mise-tasks", "mise/tasks", ".mise/tasks", ".config/mise/tasks"}

// miseTasks are the names and aliases of the tasks mise defines in root:
// [tasks] in its config files, and the file tasks in its task folders (or
// the folders and TOML files task_config.includes names), a file task named
// by its path with ":" for "/" and no extension.
func miseTasks(root string) map[string]bool {
	out := map[string]bool{}
	var includes []string
	for _, pattern := range miseConfigs {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		for _, f := range matches {
			var cfg struct {
				Tasks      map[string]any `toml:"tasks"`
				TaskConfig struct {
					Includes []string `toml:"includes"`
				} `toml:"task_config"`
			}
			if _, err := toml.DecodeFile(f, &cfg); err != nil {
				continue
			}
			addTasks(out, cfg.Tasks)
			includes = append(includes, cfg.TaskConfig.Includes...)
		}
	}
	if len(includes) == 0 {
		includes = miseTaskDirs
	}
	for _, inc := range includes {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(inc)))
		for _, m := range matches {
			if fsx.IsDir(m) {
				fileTasks(out, m)
				continue
			}
			var defs map[string]any
			if _, err := toml.DecodeFile(m, &defs); err == nil {
				addTasks(out, defs)
			}
		}
	}
	return out
}

func addTasks(out map[string]bool, defs map[string]any) {
	for name, def := range defs {
		out[name] = true
		if t, ok := def.(map[string]any); ok {
			for _, a := range vault.CoerceList(t["alias"]) {
				out[a] = true
			}
		}
	}
}

func fileTasks(out map[string]bool, dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		out[strings.ReplaceAll(rel, "/", ":")] = true
		return nil
	})
}
