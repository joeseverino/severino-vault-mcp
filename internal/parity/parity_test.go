// Package parity runs the Python reference implementation and the Go binary
// side by side over the same fixture vaults and diffs their output: every
// read command's JSON, write results plus the files they leave behind, and
// MCP tool calls over stdio. Opt in by naming the Python command:
//
//	SVMC_PARITY_PY="uv run --quiet --project ../severino-vault-mcp severino-vault-mcp" go test ./internal/parity/ -v
//
// Deliberate differences are listed in known and asserted, not hidden.
package parity

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

var goBin string

func TestMain(m *testing.M) {
	if os.Getenv("SVMC_PARITY_PY") == "" {
		os.Exit(m.Run())
	}
	dir, _ := os.MkdirTemp("", "svmc-parity-")
	goBin = filepath.Join(dir, "severino-vault-mcp")
	build := exec.Command("go", "build", "-o", goBin, "../../cmd/severino-vault-mcp")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build failed:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func pyCmd(t *testing.T) []string {
	t.Helper()
	py := strings.Fields(os.Getenv("SVMC_PARITY_PY"))
	if len(py) == 0 {
		t.Skip("SVMC_PARITY_PY not set")
	}
	return py
}

func envList(env map[string]string) []string {
	out := os.Environ()
	filtered := out[:0]
	for _, kv := range out {
		if !strings.HasPrefix(kv, "SVMC_") && !strings.HasPrefix(kv, "UV_PROJECT_ENVIRONMENT=") {
			filtered = append(filtered, kv)
		}
	}
	for k, v := range env {
		filtered = append(filtered, k+"="+v)
	}
	return filtered
}

func runCmd(argv []string, env map[string]string, stdin string) (string, int) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = envList(env)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return out.String(), code
}

func normalize(t *testing.T, text string) any {
	v, err := jsonx.Decode([]byte(strings.TrimSpace(text)))
	if err != nil {
		return strings.TrimSpace(text)
	}
	b, _ := jsonx.Encode(v, jsonx.Options{SortKeys: true})
	var out any
	_ = jsonx.Decode
	out = string(b)
	return out
}

func fixtureEnv(t *testing.T, root string) map[string]string {
	edu := filepath.Join(root, "edu")
	tk.Write(t, filepath.Join(edu, "GT", "index.md"), "---\ndoc_id: res-gt\ntitle: Georgia Tech\ndoc_type: resource\nstatus: active\n"+
		"institution: Georgia Institute of Technology\nslug: georgia-tech\n---\n\n# GT\n")
	tk.Write(t, filepath.Join(edu, "GT", "CS1", "index.md"), "---\ndoc_id: course-cs1\ntitle: Intro\ndoc_type: course\nstatus: completed\n"+
		"code: CS1000\nterm: Fall 2025\nshort_title: Intro\n---\n\n## Site\n\n- Learned things.\n\n## Scratch\n\nprivate\n")
	eduCfg := filepath.Join(root, "edu.toml")
	tk.Write(t, eduCfg, "[vault]\npath = \""+edu+"\"\nindexed_dirs = [\"GT\"]\n")
	return map[string]string{
		"SVMC_VAULT_PATH":                  root,
		"SVMC_CONFIG":                      filepath.Join(root, "absent.toml"),
		"SVMC_CACHE_SECONDS":               "0",
		"SVMC_EDU_CONFIG":                  eduCfg,
		"SVMC_RESTRICTED_UNLOCK_AUDIT_LOG": filepath.Join(root, ".audit.log"),
		"SVMC_RESTRICTED_UNLOCK_HASH_FILE": filepath.Join(root, ".no-hash"),
	}
}

func richVault(t *testing.T) string {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	os.MkdirAll(filepath.Join(root, "07 Backlog"), 0o755)
	os.MkdirAll(filepath.Join(root, "01 Projects", "cordon", "tasks"), 0o755)
	tk.Write(t, filepath.Join(root, "01 Projects", "cordon", "tasks", "task-wire-it.md"),
		"---\ndoc_id: task-wire-it\ntitle: Wire it\ndoc_type: task\nstatus: open\nrelated_projects:\n  - cordon\neffort: M\npriority: high\ncreated: 2026-06-01\ntags:\n  - backlog\n---\n\n# Wire it\n")
	tk.Write(t, filepath.Join(root, "07 Backlog", "task-cross.md"),
		"---\ndoc_id: task-cross\ntitle: Cross\ndoc_type: task\nstatus: parked\nrelated_projects: []\neffort: S\npriority: low\ncreated: 2026-06-02\ntags:\n  - backlog\n---\n\n# Cross\n")
	return root
}

// known lists the deliberate CLI differences, keyed by command.
var known = map[string]string{
	"describe": "Go adds `serve`, describes its own sources in --fingerprint help, and drops the argparse wording",
}

func TestCLIReadCommandsMatch(t *testing.T) {
	py := pyCmd(t)
	for _, root := range []string{richVault(t), sample(t)} {
		env := fixtureEnv(t, root)
		cases := [][]string{
			{"find", "nginx proxy"}, {"find", "expose a service"}, {"find", "a the of and to"}, {"find", "resolver latency", "--limit", "1"},
			{"find", "generate internal certificate"}, {"find", "offline ca"}, {"find", "troubleshooting", "--pretty"},
			{"read", "rb-add-nginx-proxy-host"}, {"read", "infra-local-pki"}, {"read", "rb-backup-ops", "--section", "troubleshooting"},
			{"read", "rb-backup-ops", "--section", "nope"}, {"read", "rb-nope"}, {"read", "rb-generate-internal-cert"}, {"read", "infra-offline-ca"},
			{"task-list"}, {"task-list", "--all"}, {"task-list", "--project", "cordon"}, {"task-list", "--status", "parked"}, {"task-list", "--stale-only"},
			{"task-projects"}, {"schema"}, {"schema", "--contract"}, {"schema", "--fingerprint"}, {"brief"}, {"brief", "--review-after", "0"},
			{"export", "education"}, {"hq-manifest", root, "--report"}, {"doctor"}, {"doctor", "--propose"}, {"describe"},
		}
		for _, args := range cases {
			pyOut, pyCode := runCmd(append(append([]string{}, py...), args...), env, "")
			goOut, goCode := runCmd(append([]string{goBin}, args...), env, "")
			same := reflect.DeepEqual(normalize(t, pyOut), normalize(t, goOut)) && pyCode == goCode
			if note, ok := known[args[0]]; ok {
				if same {
					t.Errorf("%v: expected a known difference (%s) but outputs match", args, note)
				}
				continue
			}
			if !same {
				t.Errorf("%v differs (exit %d vs %d)\npy: %.600s\ngo: %.600s", args, pyCode, goCode, pyOut, goOut)
			}
		}
	}
}

func sample(t *testing.T) string {
	wd, _ := os.Getwd()
	dst := tk.Dir(t)
	copyDir(t, filepath.Join(wd, "..", "..", "examples", "sample-vault"), dst)
	return dst
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			rel, _ := filepath.Rel(root, p)
			data, _ := os.ReadFile(p)
			out[rel] = string(data)
		}
		return nil
	})
	return out
}

func TestCLIWritesLeaveTheSameFiles(t *testing.T) {
	py := pyCmd(t)
	steps := [][]string{
		{"task-add", "Ship the Go port", "--project", "cordon", "--effort", "M"},
		{"task-add", "Cross cutting thing", "--related-projects", "cordon", "tools", "--tags", "go", "port"},
		{"task-move", "ship-the-go-port", "done"},
		{"task-move", "task-cross", "open"},
		{"task-reconcile"},
		{"update-frontmatter", "03 Runbooks/Add Nginx Proxy Host.md", "--title", "Add an Nginx Proxy Host", "--set-tags", "nginx", "proxy", "--status", "deprecated"},
		{"update-frontmatter", "03 Runbooks/Add Nginx Proxy Host.md", "--status", "bogus"},
		{"touch-reviewed", "03 Runbooks/Backup Ops.md"},
		{"backfill-aliases"},
		{"task-delete", "task-wire-it"},
		{"daily-write", "--date", "2026-06-25"},
	}
	pyRoot, goRoot := richVault(t), richVault(t)
	pyEnv, goEnv := fixtureEnv(t, pyRoot), fixtureEnv(t, goRoot)
	for _, args := range steps {
		stdin := ""
		if args[0] == "daily-write" {
			stdin = "> brief body\n"
		}
		pyOut, pyCode := runCmd(append(append([]string{}, py...), args...), pyEnv, stdin)
		goOut, goCode := runCmd(append([]string{goBin}, args...), goEnv, stdin)
		// Paths in results name each run's own vault.
		goOut = strings.ReplaceAll(goOut, goRoot, pyRoot)
		if !reflect.DeepEqual(normalize(t, pyOut), normalize(t, goOut)) || pyCode != goCode {
			t.Errorf("%v differs\npy: %.500s\ngo: %.500s", args, pyOut, goOut)
		}
	}
	pySnap, goSnap := snapshot(t, pyRoot), snapshot(t, goRoot)
	for rel, text := range pySnap {
		if strings.HasPrefix(rel, "00 Inbox/Daily Note/2026-06-25") {
			// The created: stamp is the wall clock second of each run.
			text, goSnap[rel] = stripCreated(text), stripCreated(goSnap[rel])
		}
		if goSnap[rel] != text {
			t.Errorf("%s differs\npy:\n%s\ngo:\n%s", rel, text, goSnap[rel])
		}
	}
	if len(pySnap) != len(goSnap) {
		t.Errorf("file sets differ: %v vs %v", keys(pySnap), keys(goSnap))
	}
}

func stripCreated(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(line, "created: ") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func session(t *testing.T, argv []string, env map[string]string) *mcp.ClientSession {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = envList(env)
	cmd.Stderr = &bytes.Buffer{}
	s, err := mcp.NewClient(&mcp.Implementation{Name: "parity", Version: "1"}, nil).Connect(context.Background(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func callText(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "protocol error: " + err.Error(), true
	}
	if len(res.Content) == 0 {
		return "", res.IsError
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestMCPToolCallsMatch(t *testing.T) {
	py := pyCmd(t)
	pyRoot, goRoot := richVault(t), richVault(t)
	pyS := session(t, py, fixtureEnv(t, pyRoot))
	goS := session(t, []string{goBin}, fixtureEnv(t, goRoot))

	names := func(s *mcp.ClientSession) []string {
		var out []string
		for tool, err := range s.Tools(context.Background(), nil) {
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, tool.Name)
		}
		sort.Strings(out)
		return out
	}
	if p, g := names(pyS), names(goS); !reflect.DeepEqual(p, g) {
		t.Fatalf("tool names differ: %v vs %v", p, g)
	}

	calls := []struct {
		name string
		args map[string]any
	}{
		{"find", map[string]any{"query": "nginx proxy"}},
		{"find", map[string]any{"query": "nginx", "by": "system"}},
		{"find", map[string]any{"query": "cordon", "by": "project"}},
		{"find", map[string]any{"query": "HTTPS via NPM", "by": "text"}},
		{"find", map[string]any{"query": "CA private key", "by": "text"}},
		{"find", map[string]any{"query": "resolver latency", "limit": 1}},
		{"read_doc", map[string]any{"doc_id": "rb-add-nginx-proxy-host"}},
		{"read_doc", map[string]any{"doc_id": "https proxy"}},
		{"read_doc", map[string]any{"doc_id": "infra-local-pki"}},
		{"read_doc", map[string]any{"doc_id": "infra-local-pki", "include_restricted": true}},
		{"read_doc", map[string]any{"doc_id": "rb-backup-ops", "section": "troubleshooting"}},
		{"read_doc", map[string]any{"doc_id": "rb-backup-ops", "section": "nope"}},
		{"read_doc", map[string]any{"doc_id": "not a doc"}},
		{"daily_progress", map[string]any{"query": "what did i do on friday", "today": "2026-06-20"}},
		{"daily_progress", map[string]any{"query": "yesterday", "today": "2026-06-19"}},
		{"task_board", map[string]any{}},
		{"task_board", map[string]any{"include_all": true}},
		{"task_board", map[string]any{"project": "cordon"}},
		{"education_dataset", map[string]any{}},
		{"set_frontmatter", map[string]any{"relative_path": "01 Projects/untagged.md", "doc_id": "project-untagged", "title": "Untagged",
			"doc_type": "architecture_note", "system": "Untagged", "add_tags": []any{"one"}}},
		{"set_frontmatter", map[string]any{"relative_path": "03 Runbooks/Add Nginx Proxy Host.md", "add_tags": []any{"proxy"}, "status": "deprecated"}},
		{"set_frontmatter", map[string]any{"relative_path": "03 Runbooks/Add Nginx Proxy Host.md", "doc_id": "rb-other"}},
		{"set_frontmatter", map[string]any{"relative_path": "01 Projects/nope.md", "title": "x"}},
		{"task_write", map[string]any{"action": "add", "title": "Port the tests", "project": "cordon", "problem": "p", "fix": "f"}},
		{"task_write", map[string]any{"action": "status", "doc_id": "port-the-tests", "status": "done"}},
		{"task_write", map[string]any{"action": "add"}},
		{"task_write", map[string]any{"action": "delete", "doc_id": "task-cross"}},
		{"find", map[string]any{"query": "untagged", "by": "system"}},
	}
	for _, c := range calls {
		pText, pErr := callText(t, pyS, c.name, c.args)
		gText, gErr := callText(t, goS, c.name, c.args)
		gText = strings.ReplaceAll(gText, goRoot, pyRoot)
		if pErr != gErr || !reflect.DeepEqual(normalize(t, pText), normalize(t, gText)) {
			t.Errorf("%s %v differs\npy: %.700s\ngo: %.700s", c.name, c.args, pText, gText)
		}
	}
	pySnap, goSnap := snapshot(t, pyRoot), snapshot(t, goRoot)
	for rel, text := range pySnap {
		if goSnap[rel] != text {
			t.Errorf("%s differs after MCP writes\npy:\n%s\ngo:\n%s", rel, text, goSnap[rel])
		}
	}
}

// update_link is a deliberate difference: Python rebuilt the file from a
// character slice sized by a line number and destroyed the frontmatter.
func TestUpdateLinkKnownDifference(t *testing.T) {
	py := pyCmd(t)
	mk := func() string {
		root := tk.Dir(t)
		tk.Write(t, filepath.Join(root, "02 Infrastructure", "doc.md"), "---\ndoc_id: report-link-test\ntitle: Link Test\n"+
			"doc_type: architecture_note\nsystem: test\nenvironment: other\nstatus: active\nsensitivity: internal\n---\n\nOpen [D](https://old.example).\n")
		return root
	}
	pyRoot, goRoot := mk(), mk()
	args := []string{"update-doc-link", "report-link-test", "D", "https://old.example", "https://new.example"}
	runCmd(append(append([]string{}, py...), args...), fixtureEnv(t, pyRoot), "")
	runCmd(append([]string{goBin}, args...), fixtureEnv(t, goRoot), "")
	pyText := tk.Read(t, filepath.Join(pyRoot, "02 Infrastructure", "doc.md"))
	goText := tk.Read(t, filepath.Join(goRoot, "02 Infrastructure", "doc.md"))
	if strings.Contains(pyText, "title: Link Test") {
		t.Log("the Python reference no longer corrupts the frontmatter; drop this known difference")
	}
	if !strings.Contains(goText, "title: Link Test") || !strings.Contains(goText, "[D](https://new.example)") {
		t.Fatalf("go output:\n%s", goText)
	}
}
