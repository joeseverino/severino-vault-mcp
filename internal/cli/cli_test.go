package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func run(t *testing.T, root string, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(args, &Ctx{Env: tk.Env(root), Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func goldenDir() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..", "tests", "golden")
}

func TestSchemaMatchesTheCommittedHQContract(t *testing.T) {
	_, out, _ := run(t, tk.Dir(t), "", "schema", "--json")
	golden, _ := os.ReadFile(filepath.Join(goldenDir(), "schema.json"))
	if out != string(golden) {
		t.Fatalf("schema drifted:\n%s", out)
	}
}

func TestSchemaContractAndFingerprint(t *testing.T) {
	_, out, _ := run(t, tk.Dir(t), "", "schema", "--contract")
	if strings.TrimSpace(out) != jsonx.Canonical(schema.Labs.ContractDict()) {
		t.Fatal(out)
	}
	_, out, _ = run(t, tk.Dir(t), "", "schema", "--fingerprint")
	if strings.TrimSpace(out) != schema.Labs.Fingerprint() {
		t.Fatal(out)
	}
}

func TestDescribeMatchesTheCommittedContract(t *testing.T) {
	_, out, _ := run(t, tk.Dir(t), "", "describe")
	golden, _ := os.ReadFile(filepath.Join(goldenDir(), "cli-describe.json"))
	if strings.TrimSpace(out) != strings.TrimSpace(string(golden)) {
		t.Fatalf("describe drifted from tests/golden/cli-describe.json; regenerate it deliberately")
	}
}

func TestDescribeIsTheCommandTable(t *testing.T) {
	d := Describe()
	cmds := tk.Hits(d, "commands")
	var names []string
	for _, c := range cmds {
		names = append(names, c.Str("name"))
	}
	for _, want := range []string{"find", "read", "describe", "schema", "export", "serve"} {
		if !slices.Contains(names, want) {
			t.Fatalf("missing %s", want)
		}
	}
	seen := map[string]bool{}
	for _, cmd := range commands {
		if seen[cmd.Name] || cmd.Run == nil {
			t.Fatalf("command %s duplicated or unrunnable", cmd.Name)
		}
		seen[cmd.Name] = true
	}
	byName := map[string]*jsonx.Obj{}
	for _, c := range cmds {
		byName[c.Str("name")] = c
	}
	if byName["touch-reviewed"].Str("effect") != "vault_write" || byName["find"].Str("effect") != "read" {
		t.Fatal("effects")
	}
	args := tk.Hits(byName["find"], "args")
	if args[0].Str("name") != "query" || !args[0].Bool("positional") || !args[1].Bool("takes_value") || args[2].Bool("takes_value") {
		t.Fatal(jsonx.Compact(args))
	}
}

func TestFindAndReadEmitTheMenu(t *testing.T) {
	root := tk.FakeVault(t)
	code, out, _ := run(t, root, "", "find", "nginx proxy")
	var payload map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &payload) != nil || !payload["ok"].(bool) {
		t.Fatal(out)
	}
	hit := payload["hits"].([]any)[0].(map[string]any)
	if hit["doc_id"] != "rb-add-nginx-proxy-host" {
		t.Fatal(out)
	}
	code, out, _ = run(t, root, "", "read", "rb-add-nginx-proxy-host", "--section", hit["section"].(string))
	if code != 0 || !strings.Contains(out, `"body_scope":"section"`) || !strings.Contains(out, "Expose an internal service") {
		t.Fatal(out)
	}
}

func TestReadExitsOneWhenNotOK(t *testing.T) {
	if code, _, _ := run(t, tk.FakeVault(t), "", "read", "rb-nope"); code != 1 {
		t.Fatal(code)
	}
}

func TestArgumentErrorsExitTwo(t *testing.T) {
	root := tk.Dir(t)
	for _, args := range [][]string{{"find"}, {"find", "q", "--limit", "x"}, {"export", "fitness"}, {"nope"}, {"find", "q", "--bogus"}} {
		if code, _, errOut := run(t, root, "", args...); code != 2 || !strings.Contains(errOut, "error:") {
			t.Errorf("%v: %d %s", args, code, errOut)
		}
	}
}

func TestLongOptionPrefixesResolve(t *testing.T) {
	if code, out, _ := run(t, tk.FakeVault(t), "", "find", "nginx", "--lim", "1", "--pre"); code != 0 || !strings.Contains(out, "\n  \"ok\": true") {
		t.Fatal(out)
	}
}

func TestTaskAddAndMoveRoundTrip(t *testing.T) {
	root := tk.Dir(t)
	os.MkdirAll(filepath.Join(root, "07 Backlog"), 0o755)
	code, out, _ := run(t, root, "", "task-add", "Ship the port", "--tags", "go", "port")
	if code != 0 || !strings.Contains(out, `"doc_id":"task-ship-the-port"`) {
		t.Fatal(out)
	}
	if code, out, _ := run(t, root, "", "task-move", "ship-the-port", "done"); code != 0 || !strings.Contains(out, `"status":"done"`) {
		t.Fatal(out)
	}
}

func TestUpdateFrontmatterEmptySetClears(t *testing.T) {
	root := tk.FakeVault(t)
	code, out, _ := run(t, root, "", "update-frontmatter", "03 Runbooks/Add Nginx Proxy Host.md", "--set-tags")
	if code != 0 || !strings.Contains(out, `"changed_fields":["tags"]`) {
		t.Fatal(out)
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")), "tags: []") {
		t.Fatal("tags not cleared")
	}
}

func TestDailyWriteReadsStdin(t *testing.T) {
	root := tk.Dir(t)
	code, out, _ := run(t, root, "brief body", "daily-write", "--date", "2026-06-25")
	if code != 0 || !strings.Contains(out, `"created":true`) {
		t.Fatal(out)
	}
}

func TestExportEducationUsesTheEduConfig(t *testing.T) {
	root := tk.Dir(t)
	edu := filepath.Join(root, "edu")
	tk.Write(t, filepath.Join(edu, "GT", "index.md"), "---\ndoc_id: res-gt\ntitle: GT\ndoc_type: resource\nstatus: active\ninstitution: GT\nslug: gt\n---\n")
	cfg := filepath.Join(root, "edu.toml")
	tk.Write(t, cfg, "[vault]\npath = \""+edu+"\"\nindexed_dirs = [\"GT\"]\n")
	var out, errb bytes.Buffer
	env := tk.Env(root, "SVMC_EDU_CONFIG", cfg, "SVMC_VAULT_PATH", filepath.Join(root, "labs-elsewhere"))
	code := Main([]string{"export", "education"}, &Ctx{Env: env, Stdout: &out, Stderr: &errb})
	if code != 0 || !strings.Contains(out.String(), `"slug":"gt"`) {
		t.Fatal(out.String(), errb.String())
	}
}

func TestFingerprintIsStable(t *testing.T) {
	_, a, _ := run(t, tk.Dir(t), "", "--fingerprint")
	_, b, _ := run(t, tk.Dir(t), "", "--fingerprint")
	if a != b || len(strings.TrimSpace(a)) != 16 {
		t.Fatal(a, b)
	}
}

func TestHQManifestPrintsEntries(t *testing.T) {
	root := tk.FakeVault(t)
	code, out, errOut := run(t, root, "", "hq-manifest", root, "03 Runbooks")
	if code != 0 || !strings.Contains(out, "rb-add-nginx-proxy-host") || !strings.Contains(errOut, "ok: 2 entries") {
		t.Fatal(out, errOut)
	}
}

func unlockHash(t *testing.T, tty bool, phrases ...string) (int, string, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	origTTY, origRead := isTerminal, readPassword
	t.Cleanup(func() { isTerminal, readPassword = origTTY, origRead })
	isTerminal = func(int) bool { return tty }
	readPassword = func(int) ([]byte, error) {
		if len(phrases) == 0 {
			return nil, io.EOF
		}
		p := phrases[0]
		phrases = phrases[1:]
		return []byte(p), nil
	}
	var out, errb bytes.Buffer
	code := Main([]string{"unlock-hash"}, &Ctx{Env: tk.Env(tk.Dir(t)), Stdin: r, Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func TestUnlockHashPrintsAVerifiablePHCString(t *testing.T) {
	code, out, errOut := unlockHash(t, true, "open sesame", "open sesame")
	hash := strings.TrimSpace(out)
	if code != 0 || !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") || !gate.VerifyPhrase("open sesame", hash) ||
		strings.Contains(out+errOut, "open sesame") {
		t.Fatal(code, out, errOut)
	}
}

func TestUnlockHashRefusesMismatchEmptyAndNonTTY(t *testing.T) {
	if code, out, errOut := unlockHash(t, true, "one", "two"); code != 1 || out != "" || !strings.Contains(errOut, "don't match") {
		t.Fatal(code, out, errOut)
	}
	if code, out, errOut := unlockHash(t, true, ""); code != 1 || out != "" || !strings.Contains(errOut, "empty phrase") {
		t.Fatal(code, out, errOut)
	}
	if code, out, errOut := unlockHash(t, false, "x", "x"); code != 2 || out != "" || !strings.Contains(errOut, "not a terminal") {
		t.Fatal(code, out, errOut)
	}
	// A reader that isn't a file is never a terminal.
	if code, _, errOut := run(t, tk.Dir(t), "x\nx\n", "unlock-hash"); code != 2 || !strings.Contains(errOut, "not a terminal") {
		t.Fatal(code, errOut)
	}
}
