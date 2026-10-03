// Package vaults composes the vaults one server serves. labs is always on;
// edu is on when its config file exists; each provider the operator's config
// declares adds the vault its profile names, and its tools.
package vaults

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/provider"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
)

// Default config paths.
const (
	EduConfigDefault = "~/.config/severino-edu-mcp/config.toml"
	DefaultVault     = "labs"
)

// ProviderTimeout bounds a provider's startup.
var ProviderTimeout = 20 * time.Second

// ScopedEnv drops SVMC_* overrides so labs settings never reach another vault.
func ScopedEnv(env config.Env) config.Env {
	out := config.Env{}
	for k, v := range env {
		if !strings.HasPrefix(k, "SVMC_") {
			out[k] = v
		}
	}
	return out
}

// Labs is the labs vault.
func Labs(env config.Env) *core.Vault {
	return core.NewVault("labs", config.Load("", env), schema.Labs)
}

// EduConfigPath is the edu vault's config file.
func EduConfigPath(env config.Env) string {
	if v, ok := env.Lookup("SVMC_EDU_CONFIG"); ok {
		return config.ExpandUser(v)
	}
	return config.ExpandUser(EduConfigDefault)
}

// Edu is the edu vault.
func Edu(env config.Env) *core.Vault {
	return core.NewVault("edu", config.Load(EduConfigPath(env), ScopedEnv(env)), schema.Education)
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// ProviderSpecs reads the [[providers]] entries from the labs config:
//
//	[[providers]]
//	command = "example"
//	args    = ["mcp-provider"]
//	config  = "~/.config/example/config.toml"
//	env     = { EXAMPLE_HOME = "~/example" }
//
// command and config are required. There are no default providers.
func ProviderSpecs(env config.Env, log io.Writer) []provider.Spec {
	cfgPath := config.ExpandUser(config.DefaultPath)
	if v, ok := env.Lookup("SVMC_CONFIG"); ok {
		cfgPath = config.ExpandUser(v)
	}
	list, _ := config.ReadTOML(cfgPath)["providers"].([]map[string]any)
	var specs []provider.Spec
	for i, entry := range list {
		spec := provider.Spec{Env: map[string]string{}}
		spec.Command, _ = entry["command"].(string)
		if c, ok := entry["config"].(string); ok {
			spec.Config = config.ExpandUser(c)
		}
		if args, ok := entry["args"].([]any); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					spec.Args = append(spec.Args, s)
				}
			}
		}
		if e, ok := entry["env"].(map[string]any); ok {
			for k, v := range e {
				if s, ok := v.(string); ok {
					spec.Env[k] = s
				}
			}
		}
		if spec.Command == "" || spec.Config == "" {
			fmt.Fprintf(log, "severino-vault-mcp: providers[%d] needs command and config; skipped\n", i)
			continue
		}
		specs = append(specs, spec)
	}
	return specs
}

// Registry is the composed set of vaults.
type Registry struct {
	Vaults       []*core.Vault
	Providers    []*provider.Provider
	Instructions []string
}

// Lookup finds a vault by name.
func (r *Registry) Lookup(name string) *core.Vault {
	for _, v := range r.Vaults {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// Names lists the vault names in order.
func (r *Registry) Names() []string {
	var out []string
	for _, v := range r.Vaults {
		out = append(out, v.Name)
	}
	return out
}

// Build composes every available vault, logging what is off to log.
func Build(ctx context.Context, env config.Env, log io.Writer) *Registry {
	r := &Registry{Vaults: []*core.Vault{Labs(env)}}
	if eduPath := EduConfigPath(env); isFile(eduPath) {
		r.Vaults = append(r.Vaults, Edu(env))
	} else {
		fmt.Fprintf(log, "severino-vault-mcp: no edu config at %s; edu vault off\n", eduPath)
	}
	for _, spec := range ProviderSpecs(env, log) {
		p, err := provider.Connect(ctx, spec, env, ProviderTimeout)
		if err != nil {
			fmt.Fprintf(log, "severino-vault-mcp: %v; skipped\n", err)
			continue
		}
		if r.Lookup(p.Vault()) != nil {
			fmt.Fprintf(log, "severino-vault-mcp: provider %s skipped; vault %s already exists\n", spec.Label(), p.Vault())
			_ = p.Close()
			continue
		}
		r.Vaults = append(r.Vaults, core.NewVault(p.Vault(), config.Load(spec.Config, ScopedEnv(env)), p.Profile))
		r.Providers = append(r.Providers, p)
		if p.Instructions != "" {
			r.Instructions = append(r.Instructions, p.Instructions)
		}
	}
	return r
}

// Close ends every provider session.
func (r *Registry) Close() {
	for _, p := range r.Providers {
		_ = p.Close()
	}
}
