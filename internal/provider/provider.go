// Package provider connects to a vault provider: a separate stdio MCP server
// that owns one vault's domain tools and schema profile. The host knows a
// provider only by the command in the operator's config; everything else
// arrives at runtime, so a private domain never lives in this module.
//
// The contract a provider implements:
//   - resource ProfileURI: the vault's profile contract as JSON
//     (schema.Profile.ContractDict's shape). Its name is the vault's name.
//   - tools: listed and called as usual; the host re-exports them unchanged.
//   - instructions: optional; appended to the host's server instructions.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
)

// ProfileURI is where a provider serves its vault's profile contract.
const ProfileURI = "vault-provider://profile"

const handshakeVersion = "2025-11-25"

// terminateGrace is how long a provider gets to exit after SIGTERM before it
// is killed.
const terminateGrace = time.Second

// Spec is one [[providers]] entry.
type Spec struct {
	Command string            // the stdio MCP server
	Args    []string          // its arguments
	Env     map[string]string // extra environment for the provider
	Config  string            // the vault's config file ([vault] path, indexed dirs); the provider gets it as SVMC_CONFIG
}

// Label names the spec in logs before its vault name is known.
func (s Spec) Label() string { return strings.Join(append([]string{s.Command}, s.Args...), " ") }

// Provider is a connected provider.
type Provider struct {
	Spec         Spec
	session      *mcp.ClientSession
	Profile      *schema.Profile
	Tools        []*mcp.Tool
	Instructions string
}

// Vault is the vault the provider serves, named by its profile.
func (p *Provider) Vault() string { return p.Profile.Name }

// Connect starts the provider, reads its profile, and lists its tools.
func Connect(ctx context.Context, spec Spec, env config.Env, timeout time.Duration) (*Provider, error) {
	label := spec.Label()
	if spec.Command == "" {
		return nil, fmt.Errorf("provider: no command")
	}
	bin, err := exec.LookPath(config.ExpandUser(spec.Command))
	if err != nil {
		return nil, fmt.Errorf("provider %s: %s not found", label, spec.Command)
	}
	cmd := exec.CommandContext(ctx, bin, spec.Args...) //nolint:gosec // the provider command is operator config
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = terminateGrace
	cmd.Env = providerEnv(env, spec)
	cmd.Stderr = os.Stderr

	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "severino-vault-mcp", Version: "host"}, nil)
	// The initialize handshake, not the stateless server/discover probe:
	// providers on older SDKs reject the probe.
	session, err := client.Connect(connectCtx, &mcp.CommandTransport{Command: cmd, TerminateDuration: terminateGrace}, &mcp.ClientSessionOptions{ProtocolVersion: handshakeVersion})
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", label, err)
	}
	p := &Provider{Spec: spec, session: session}
	if init := session.InitializeResult(); init != nil {
		p.Instructions = init.Instructions
	}
	res, err := session.ReadResource(connectCtx, &mcp.ReadResourceParams{URI: ProfileURI})
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("provider %s: reading %s: %w", label, ProfileURI, err)
	}
	if len(res.Contents) == 0 {
		_ = session.Close()
		return nil, fmt.Errorf("provider %s: empty profile", label)
	}
	decoded, err := jsonx.Decode([]byte(res.Contents[0].Text))
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("provider %s: profile is not JSON: %w", label, err)
	}
	obj, ok := decoded.(*jsonx.Obj)
	if !ok {
		_ = session.Close()
		return nil, fmt.Errorf("provider %s: profile is not an object", label)
	}
	if p.Profile, err = schema.FromContract(obj); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("provider %s: %w", label, err)
	}
	if p.Profile.Name == "" {
		_ = session.Close()
		return nil, fmt.Errorf("provider %s: profile has no name", label)
	}
	for tool, err := range session.Tools(connectCtx, nil) {
		if err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("provider %s: listing tools: %w", label, err)
		}
		p.Tools = append(p.Tools, tool)
	}
	return p, nil
}

func providerEnv(env config.Env, spec Spec) []string {
	var out []string
	for k, v := range env {
		// The provider resolves its own vault; labs overrides must not leak in.
		if strings.HasPrefix(k, "SVMC_") {
			continue
		}
		out = append(out, k+"="+v)
	}
	if spec.Config != "" {
		out = append(out, "SVMC_CONFIG="+spec.Config)
	}
	for k, v := range spec.Env {
		out = append(out, k+"="+v)
	}
	return out
}

// Call forwards a tool call.
func (p *Provider) Call(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
	var arguments any = map[string]any{}
	if len(args) > 0 {
		arguments = args
	}
	return p.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
}

// Close ends the provider session.
func (p *Provider) Close() error { return p.session.Close() }
