package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/provider"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vaults"
)

var coreTools = []string{"find", "log_entries", "move_doc", "move_folder", "queue", "read_doc", "set_frontmatter", "write_doc"}

// TestMain doubles as a fake vault provider when re-executed with
// FAKE_PROVIDER set to the vault name it should serve, so the provider seam
// is tested over a real process.
func TestMain(m *testing.M) {
	if name := os.Getenv("FAKE_PROVIDER"); name != "" {
		runFakeProvider(name)
		return
	}
	os.Exit(m.Run())
}

func runFakeProvider(name string) {
	s := mcp.NewServer(&mcp.Implementation{Name: "fake-provider", Version: "1"}, &mcp.ServerOptions{Instructions: "Example: fake provider instructions."})
	profile := &schema.Profile{
		Name: name, DocTypes: []string{"renewal", "task"}, Environments: []string{"other"}, Statuses: []string{"active"},
		Sensitivities: []string{"internal"}, DocIDPrefixes: []string{"renewal-", "task-"}, RequiredFields: []string{"doc_id", "title", "doc_type"},
		TaskStatuses: []string{"open", "done"}, TaskRequiredFields: []string{"doc_id", "title", "doc_type", "status"}, TaskFields: []string{"status"},
		DocumentSchemas: map[string]schema.DocumentSchema{"renewal": {Fields: map[string]schema.Field{"renews": {Required: true, Kind: "date"}}}},
	}
	s.AddResource(&mcp.Resource{URI: provider.ProfileURI, Name: "profile", MIMEType: "application/json"},
		func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: provider.ProfileURI, Text: jsonx.Compact(profile.ContractDict())}}}, nil
		})
	s.AddTool(&mcp.Tool{Name: "example_lookup", Description: "fake lookup", InputSchema: json.RawMessage(`{"type":"object","properties":{"days":{"type":"integer"}}}`)},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"ok":true,"args":` + string(req.Params.Arguments) + `}`}}}, nil
		})
	s.AddTool(&mcp.Tool{Name: "find", Description: "collides", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	_ = s.Run(context.Background(), &mcp.StdioTransport{})
}

func connect(t *testing.T, reg *vaults.Registry) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := New(reg, io.Discard)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func tools(t *testing.T, s *mcp.ClientSession) map[string]*mcp.Tool {
	out := map[string]*mcp.Tool{}
	for tool, err := range s.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		out[tool.Name] = tool
	}
	return out
}

func names(m map[string]*mcp.Tool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func vaultProp(t *testing.T, tool *mcp.Tool) map[string]any {
	schema := tool.InputSchema.(map[string]any)
	return schema["properties"].(map[string]any)["vault"].(map[string]any)
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (*jsonx.Obj, bool) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	v, err := jsonx.Decode([]byte(text))
	if err != nil {
		t.Fatalf("%v: %s", err, text)
	}
	return v.(*jsonx.Obj), res.IsError
}

func labsOnly(t *testing.T) (*vaults.Registry, string) {
	root := tk.FakeVault(t)
	env := tk.Env(root)
	return vaults.Build(context.Background(), env, io.Discard), root
}

func TestLabsOnlyWhenEduAndProvidersAreAbsent(t *testing.T) {
	reg, _ := labsOnly(t)
	got := tools(t, connect(t, reg))
	if !slices.Equal(names(got), coreTools) {
		t.Fatal(names(got))
	}
	if vp := vaultProp(t, got["find"]); vp["const"] != "labs" || vp["default"] != "labs" {
		t.Fatal(vp)
	}
}

func eduConfig(t *testing.T, root string) string {
	vault := filepath.Join(root, "edu-vault")
	os.MkdirAll(vault, 0o755)
	cfg := filepath.Join(root, "edu.toml")
	tk.Write(t, cfg, "[vault]\npath = \""+vault+"\"\n")
	return cfg
}

// The edu vault is reached through the shared tools; it registers none of
// its own.
func TestEduVaultRegistersWhenItsConfigExists(t *testing.T) {
	root := tk.FakeVault(t)
	env := tk.Env(root, "SVMC_EDU_CONFIG", eduConfig(t, root))
	got := tools(t, connect(t, vaults.Build(context.Background(), env, io.Discard)))
	if !slices.Equal(names(got), coreTools) {
		t.Fatal(names(got))
	}
	if vp := vaultProp(t, got["find"]); !slices.Equal(toStrings(vp["enum"]), []string{"labs", "edu"}) || vp["default"] != "labs" {
		t.Fatal(vp)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestLabsOverridesNeverReachTheEduVault(t *testing.T) {
	root := tk.Dir(t)
	env := tk.Env(root, "SVMC_EDU_CONFIG", eduConfig(t, root), "SVMC_VAULT_PATH", filepath.Join(root, "labs-vault"))
	if got := vaults.Edu(env).Config.VaultPath; got != filepath.Join(root, "edu-vault") {
		t.Fatal(got)
	}
	if vaults.Edu(env).Profile.Name != "education" {
		t.Fatal("edu profile")
	}
}

func TestToolCallsRouteAndValidate(t *testing.T) {
	reg, _ := labsOnly(t)
	s := connect(t, reg)
	r, isErr := call(t, s, "find", map[string]any{"query": "nginx proxy"})
	if isErr || tk.Get(r, "hits.0.doc_id") != "rb-add-nginx-proxy-host" {
		t.Fatal(jsonx.Compact(r))
	}
	if r, isErr := call(t, s, "find", map[string]any{"query": "x", "vault": "example"}); !isErr || !strings.Contains(r.Str("error"), "unknown vault \"example\"") {
		t.Fatal(jsonx.Compact(r))
	}
	for _, by := range []string{"fuzzy", "system", "project"} {
		if r, isErr := call(t, s, "find", map[string]any{"query": "x", "by": by}); !isErr || !strings.Contains(r.Str("error"), "by: input should be \"relevance\" or \"text\"") {
			t.Fatal(jsonx.Compact(r))
		}
	}
	if r, isErr := call(t, s, "find", map[string]any{"query": "nginx proxy", "system": "no such system"}); isErr || len(tk.Hits(r, "hits")) != 0 ||
		tk.Get(r, "filters.system") != "no such system" {
		t.Fatal(jsonx.Compact(r))
	}
	if r, isErr := call(t, s, "find", map[string]any{}); !isErr || !strings.Contains(r.Str("error"), "query: field required") {
		t.Fatal(jsonx.Compact(r))
	}
	if r, _ := call(t, s, "read_doc", map[string]any{"doc_id": "infra-local-pki"}); r.Bool("body_released") || r.Has("body") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestResourcesResolvePerVault(t *testing.T) {
	reg, _ := labsOnly(t)
	s := connect(t, reg)
	res, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vault://labs/doc/rb-add-nginx-proxy-host"})
	if err != nil || !strings.Contains(res.Contents[0].Text, "Expose an internal service") {
		t.Fatal(err)
	}
	res, err = s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vault://labs/quick-index"})
	if err != nil || !strings.Contains(res.Contents[0].Text, "Quick Index") {
		t.Fatal(err)
	}
	if _, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vault://nope/quick-index"}); err == nil {
		t.Fatal("unknown vault resolved")
	}
}

// providerFixture writes a labs config declaring the fake provider (this test
// binary) for a vault named by the profile it serves.
func providerFixture(t *testing.T, root string, extra string) config.Env {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exampleVault := filepath.Join(root, "example-vault")
	os.MkdirAll(filepath.Join(exampleVault, "Renewals"), 0o755)
	tk.Write(t, filepath.Join(exampleVault, "Renewals", "untagged.md"), "# Untagged\n")
	exampleCfg := filepath.Join(root, "example.toml")
	tk.Write(t, exampleCfg, "[vault]\npath = \""+exampleVault+"\"\nindexed_dirs = [\"Renewals\"]\n")
	labsCfg := filepath.Join(root, "labs.toml")
	tk.Write(t, labsCfg, "[[providers]]\ncommand = \""+self+"\"\nconfig = \""+exampleCfg+"\"\nenv = { FAKE_PROVIDER = \"example\" }\n"+extra)
	vaults.ProviderTimeout = 30 * time.Second
	return tk.Env(root, "SVMC_CONFIG", labsCfg)
}

func TestProviderToolsProfileAndInstructionsAreReexported(t *testing.T) {
	root := tk.FakeVault(t)
	reg := vaults.Build(context.Background(), providerFixture(t, root, ""), io.Discard)
	t.Cleanup(reg.Close)
	if reg.Lookup("example") == nil || reg.Lookup("example").Profile.Name != "example" {
		t.Fatal(reg.Names())
	}
	s := connect(t, reg)
	got := tools(t, s)
	if _, ok := got["example_lookup"]; !ok {
		t.Fatal(names(got))
	}
	if got["find"].Description == "collides" {
		t.Fatal("provider overrode a core tool")
	}
	if vp := vaultProp(t, got["find"]); !slices.Equal(toStrings(vp["enum"]), []string{"labs", "example"}) {
		t.Fatal(vp)
	}
	r, isErr := call(t, s, "example_lookup", map[string]any{"days": 2})
	if isErr || jsonx.Compact(r) != `{"ok":true,"args":{"days":2}}` {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.Contains(s.InitializeResult().Instructions, "fake provider instructions") {
		t.Fatal("instructions not composed")
	}
	// The provider's profile governs writes in its vault.
	bad, _ := call(t, s, "set_frontmatter", map[string]any{"relative_path": "Renewals/untagged.md", "vault": "example",
		"doc_id": "rb-x", "title": "X", "doc_type": "runbook", "system": "x"})
	if bad.Bool("ok") || !strings.Contains(bad.Str("error"), "doc_type \"runbook\" not in") {
		t.Fatal(jsonx.Compact(bad))
	}
}

// Every registered tool, core or provider, refuses an argument its schema
// doesn't declare, naming it and the valid ones, so no tool can ship open.
func TestEveryToolRejectsUnknownArguments(t *testing.T) {
	root := tk.FakeVault(t)
	env := providerFixture(t, root, "")
	env["SVMC_EDU_CONFIG"] = eduConfig(t, root)
	reg := vaults.Build(context.Background(), env, io.Discard)
	t.Cleanup(reg.Close)
	s := connect(t, reg)
	all := tools(t, s)
	for _, want := range []string{"set_frontmatter", "example_lookup"} {
		if _, ok := all[want]; !ok {
			t.Fatalf("%s not registered: %v", want, names(all))
		}
	}
	for name, tool := range all {
		r, isErr := call(t, s, name, map[string]any{"not_an_argument": 1})
		if !isErr || !strings.Contains(r.Str("error"), "unknown arguments \"not_an_argument\"") {
			t.Errorf("%s accepted an unknown argument: %s", name, jsonx.Compact(r))
		}
		props, _ := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		for prop := range props {
			if !strings.Contains(r.Str("error"), strconv.Quote(prop)) {
				t.Errorf("%s: error doesn't list %s: %s", name, prop, r.Str("error"))
			}
		}
	}
}

func TestSetFrontmatterRefusesAFieldsObject(t *testing.T) {
	reg, root := labsOnly(t)
	before := tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md"))
	r, isErr := call(t, connect(t, reg), "set_frontmatter", map[string]any{
		"relative_path": "03 Runbooks/Add Nginx Proxy Host.md", "fields": map[string]any{"status": "draft"}})
	if !isErr || !strings.Contains(r.Str("error"), "unknown arguments \"fields\"; valid arguments are \"") {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")) != before {
		t.Fatal("refused call wrote")
	}
}

func TestWriteDocCreatesThenEditsASection(t *testing.T) {
	reg, root := labsOnly(t)
	s := connect(t, reg)
	rel := "03 Runbooks/Rotate Example Token.md"
	create := map[string]any{"relative_path": rel, "doc_id": "rb-rotate-example-token", "title": "Rotate Example Token",
		"doc_type": "runbook", "system": "homelab", "body": "## Steps\n\n1. Run `x`.\n"}
	if r, isErr := call(t, s, "write_doc", create); isErr || !r.Bool("ok") || r.Str("relative_path") != rel {
		t.Fatal(jsonx.Compact(r))
	}
	r, isErr := call(t, s, "write_doc", map[string]any{"mode": "replace_section", "doc_id": "rb-rotate-example-token",
		"heading": "Steps", "body": "1. Run `y`.\n"})
	if isErr || !r.Bool("ok") || !strings.HasSuffix(tk.Read(t, filepath.Join(root, rel)), "## Steps\n\n1. Run `y`.\n") {
		t.Fatal(jsonx.Compact(r))
	}
	if r, isErr := call(t, s, "write_doc", map[string]any{"mode": "rewrite", "body": "x"}); !isErr || !strings.Contains(r.Str("error"), "mode: input should be") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestProvidersAreOnlyWhatTheConfigDeclares(t *testing.T) {
	root := tk.FakeVault(t)
	// A second entry serving the same vault name, and one missing its config.
	self, _ := os.Executable()
	extra := "\n[[providers]]\ncommand = \"" + self + "\"\nconfig = \"" + filepath.Join(root, "example.toml") + "\"\nenv = { FAKE_PROVIDER = \"example\" }\n" +
		"\n[[providers]]\ncommand = \"" + self + "\"\n"
	var log strings.Builder
	reg := vaults.Build(context.Background(), providerFixture(t, root, extra), &log)
	t.Cleanup(reg.Close)
	if !slices.Equal(reg.Names(), []string{"labs", "example"}) || len(reg.Providers) != 1 {
		t.Fatal(reg.Names())
	}
	if !strings.Contains(log.String(), "vault example already exists") || !strings.Contains(log.String(), "providers[2] needs command and config") {
		t.Fatal(log.String())
	}
	// No config, no providers.
	if reg := vaults.Build(context.Background(), tk.Env(tk.Dir(t)), io.Discard); len(reg.Providers) != 0 {
		t.Fatal(reg.Names())
	}
}

func TestProviderSpecsFromConfig(t *testing.T) {
	root := tk.Dir(t)
	cfg := filepath.Join(root, "config.toml")
	tk.Write(t, cfg, "[[providers]]\ncommand = \"example-mcp\"\nargs = [\"serve\"]\nconfig = \"~/example.toml\"\nenv = { EXAMPLE_HOME = \"/x\" }\n")
	specs := vaults.ProviderSpecs(config.Env{"SVMC_CONFIG": cfg, "HOME": os.Getenv("HOME")}, io.Discard)
	if len(specs) != 1 || specs[0].Command != "example-mcp" || !slices.Equal(specs[0].Args, []string{"serve"}) ||
		!strings.HasSuffix(specs[0].Config, "/example.toml") || specs[0].Env["EXAMPLE_HOME"] != "/x" {
		t.Fatalf("%+v", specs)
	}
}

// The registered tool names are a contract Claude Code binds to:
// tests/golden/mcp-tools.txt, composed as labs plus an empty edu vault.
func TestToolNamesMatchTheGolden(t *testing.T) {
	root := tk.Dir(t)
	env := tk.Env(root, "SVMC_EDU_CONFIG", eduConfig(t, root))
	got := strings.Join(names(tools(t, connect(t, vaults.Build(context.Background(), env, io.Discard)))), "\n") + "\n"
	wd, _ := os.Getwd()
	golden, err := os.ReadFile(filepath.Join(wd, "..", "..", "tests", "golden", "mcp-tools.txt"))
	if err != nil || got != string(golden) {
		t.Fatalf("tool names drifted:\n%s", got)
	}
}
