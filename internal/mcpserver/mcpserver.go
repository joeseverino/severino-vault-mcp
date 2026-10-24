// Package mcpserver is the MCP face: one server for every vault. Shared tools
// register once with a `vault` argument; each provider's tools are
// re-exported as they are.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vaults"
)

// Instructions is the server's guidance to the model.
const Instructions = `Joe's Obsidian vaults: labs (homelab infrastructure, runbooks, projects),
edu (Georgia Tech coursework and certifications), and any vault a provider
adds below. Every shared tool takes ` + "`vault`" + ` (default labs).
Load the vault skill for how to use these tools.
`

// version is a build's own label, set with
// -ldflags "-X github.com/joeseverino/severino-vault-mcp/v3/internal/mcpserver.version=<label>".
var version string

// Version is the build's label, else the module version from build info.
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// Serve composes the vaults and serves MCP over stdio until ctx ends. A
// process started by a replaced binary's handoff (binary.go) resumes that
// session instead of waiting for initialize.
func Serve(ctx context.Context, env config.Env, log io.Writer) error {
	carried, _ := env.Lookup(handoffEnv)
	delete(env, handoffEnv)
	if err := os.Unsetenv(handoffEnv); err != nil {
		return err
	}
	if carried == handoffProbe {
		_, err := fmt.Fprintln(os.Stdout, handoffReady)
		return err
	}
	var resumed *handoff
	if carried != "" {
		resumed = &handoff{}
		if err := json.Unmarshal([]byte(carried), resumed); err != nil {
			return fmt.Errorf("the session handoff: %w", err)
		}
	}
	reg := vaults.Build(ctx, env, log)
	defer reg.Close()
	bin := watchBinary()
	f := newFront(os.Stdin, os.Stdout, bin, log)
	f.takeOver = func(h handoff) error { return bin.takeOver(h, reg.Close) }
	opts := &mcp.ServerSessionOptions{}
	if resumed != nil {
		f.resume(*resumed)
		opts.State = &resumed.State
	}
	ss, err := newServer(reg, log, bin).Connect(ctx, f.transport(), opts)
	if err != nil {
		return err
	}
	if resumed != nil {
		fmt.Fprintf(log, "severino-vault-mcp: build %s took over the session from the binary it replaced\n", Version())
		if err := f.toolsChanged(); err != nil {
			return err
		}
	}
	done := make(chan error, 1)
	go func() { done <- ss.Wait() }()
	select {
	case <-ctx.Done():
		_ = ss.Close()
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// New builds the server for a registry.
func New(reg *vaults.Registry, log io.Writer) *mcp.Server { return newServer(reg, log, nil) }

// newServer builds the server; bin, when set, is the binary whose failed
// handoff the answers report.
func newServer(reg *vaults.Registry, log io.Writer, bin *binaryWatch) *mcp.Server {
	instructions := strings.Join(append([]string{Instructions}, reg.Instructions...), "\n")
	s := mcp.NewServer(&mcp.Implementation{Name: "severino-vault-mcp", Version: Version()},
		&mcp.ServerOptions{Instructions: instructions, Capabilities: &mcp.ServerCapabilities{}})
	h := &handlers{reg: reg, bin: bin}
	taken := h.registerCore(s)
	for _, p := range reg.Providers {
		for _, tool := range p.Tools {
			if taken[tool.Name] {
				fmt.Fprintf(log, "severino-vault-mcp: provider %s tool %s skipped; the name is taken\n", p.Vault(), tool.Name)
				continue
			}
			taken[tool.Name] = true
			p := p
			t := *tool
			addTool(s, &t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return p.Call(ctx, t.Name, req.Params.Arguments)
			})
		}
	}
	return s
}

// registerCore adds the shared tools and the vault resources, and returns
// the tool names it took.
func (h *handlers) registerCore(s *mcp.Server) map[string]bool {
	fields := h.fields()
	taken := map[string]bool{}
	for _, t := range []coreTool{
		h.find(),
		h.readDoc(),
		h.setFrontmatter(fields),
		h.writeDoc(fields),
		h.moveDoc(),
		h.moveFolder(),
		h.logEntries(),
		h.queue(),
	} {
		h.add(s, t)
		taken[t.name] = true
	}
	h.addResources(s)
	return taken
}

// coreTool is one shared tool: its name, description, input schema, and the
// handler that reads its arguments and runs it.
type coreTool struct {
	name, desc string
	input      json.RawMessage
	run        func(p *params) (*jsonx.Obj, error)
}

type handlers struct {
	reg *vaults.Registry
	bin *binaryWatch
}

// fields are the frontmatter fields every vault's profile declares.
func (h *handlers) fields() []schema.Field {
	var profiles []*schema.Profile
	for _, v := range h.reg.Vaults {
		if v.Profile != nil {
			profiles = append(profiles, v.Profile)
		}
	}
	return schema.Union(profiles...)
}

// vault is the vault a call names, or nil with the refusal recorded on p.
func (h *handlers) vault(p *params) *core.Vault {
	name := p.strOr("vault", vaults.DefaultVault)
	if p.err != nil {
		return nil
	}
	v := h.reg.Lookup(name)
	if v == nil {
		p.fail(argError{msg: fmt.Sprintf("unknown vault %q; one of %s", name, pystr.Quoted(h.reg.Names())), arg: "vault"})
	}
	return v
}

func (h *handlers) add(s *mcp.Server, t coreTool) {
	tool := &mcp.Tool{Name: t.name, Description: t.desc, InputSchema: t.input, Annotations: annotations[t.name],
		OutputSchema: outputSchemas[t.name], Meta: toolMeta[t.name]}
	addTool(s, tool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := parseParams(req.Params.Arguments)
		if err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		o, err := t.run(p)
		if err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		if w := h.bin.takeWarning(); w != "" {
			o.Set("warning", w)
		}
		return result(o), nil
	})
}

// result is a tool's answer. A failure envelope is also an MCP tool error
// (isError), the standard signal a client acts on without reading ok.
func result(o *jsonx.Obj) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: core.Text(o)}}, StructuredContent: o, IsError: o.Failed()}
}

func (h *handlers) addResources(s *mcp.Server) {
	read := func(uri, docID string, vaultName string) (*mcp.ReadResourceResult, error) {
		v := h.reg.Lookup(vaultName)
		if v == nil {
			return nil, fmt.Errorf("unknown vault %q; one of %s", vaultName, pystr.Quoted(h.reg.Names()))
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: core.RenderDoc(v, docID)}}}, nil
	}
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vault://{vault}/quick-index", Name: "quick-index", Title: "Vault Quick Index",
		Description: "A vault's navigation hub for broad 'how do I' or 'where do I look' questions.", MIMEType: "text/markdown",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		rest, _ := strings.CutPrefix(uri, "vault://")
		name, _ := strings.CutSuffix(rest, "/quick-index")
		return read(uri, core.QuickIndexDocID, name)
	})
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vault://{vault}/doc/{doc_id}", Name: "vault-doc", Title: "Vault doc by doc_id",
		Description: "One indexed doc's markdown body, or an advisory plus metadata when restricted.", MIMEType: "text/markdown",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		rest, _ := strings.CutPrefix(uri, "vault://")
		name, docID, ok := strings.Cut(rest, "/doc/")
		if !ok {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return read(uri, docID, name)
	})
}
