// Package config loads one vault's configuration from TOML plus SVMC_*
// environment overrides. Environment values win over the file; an explicit
// path wins over SVMC_CONFIG, which wins over the default path.
package config

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultPath is the labs vault's config file.
const DefaultPath = "~/.config/severino-vault-mcp/config.toml"

// Env is an injectable process environment.
type Env map[string]string

// OSEnv snapshots the process environment.
func OSEnv() Env {
	env := Env{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			env[k] = v
		}
	}
	return env
}

// Lookup returns a variable and whether it is set.
func (e Env) Lookup(k string) (string, bool) {
	v, ok := e[k]
	return v, ok
}

// Config is one vault's immutable configuration.
type Config struct {
	VaultPath                 string
	IndexedDirs               []string
	ReadOnlyDirs              []string
	DailyNotesDir             string
	AliasesPath               string
	MetadataURL               string
	CacheSeconds              int
	AllowRestrictedUnlock     bool
	RestrictedUnlockHash      string // "" when unset
	RestrictedUnlockHashFile  string
	RestrictedKeychainService string
	RestrictedKeychainAccount string
	RestrictedAuditLog        string
}

// DefaultIndexedDirs are the labs vault's indexed folders: searched, read,
// written and validated against the profile.
var DefaultIndexedDirs = []string{"01 Projects", "02 Infrastructure", "03 Runbooks", "04 Reference", "07 Backlog"}

// DefaultReadOnlyDirs are the labs vault's folders that find and read_doc
// serve but writes and validate leave alone: inbox captures and daily notes
// carry their own minimal frontmatter, and writeups and pages carry the
// site's. They apply only while the indexed dirs are the defaults too.
var DefaultReadOnlyDirs = []string{"00 Inbox", "05 Writeups", "06 Pages"}

// SearchDirs is every folder the index reads: the indexed dirs, then the
// read-only ones.
func (c Config) SearchDirs() []string {
	out := slices.Clone(c.IndexedDirs)
	for _, d := range c.ReadOnlyDirs {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// ReadOnlyDir is the read-only folder holding rel, a vault-relative path, or "".
func (c Config) ReadOnlyDir(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, d := range c.ReadOnlyDirs {
		if slices.Contains(c.IndexedDirs, d) {
			continue
		}
		if strings.HasPrefix(rel, filepath.ToSlash(filepath.Clean(d))+"/") {
			return d
		}
	}
	return ""
}

// ExpandUser expands a leading ~ the way Python's os.path.expanduser does.
func ExpandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home := os.Getenv("HOME")
		if home == "" {
			if h, err := os.UserHomeDir(); err == nil {
				home = h
			}
		}
		return home + p[1:]
	}
	return p
}

func envPath(env Env, name, def string) string {
	if v, ok := env.Lookup(name); ok {
		return ExpandUser(v)
	}
	return ExpandUser(def)
}

func envList(env Env, name string, def []string) []string {
	v, ok := env.Lookup(name)
	if !ok {
		return append([]string(nil), def...)
	}
	return SplitColon(v)
}

// SplitColon splits a colon list, dropping empty parts.
func SplitColon(v string) []string {
	out := []string{}
	for part := range strings.SplitSeq(v, ":") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envBool(env Env, name string, def bool) bool {
	v, ok := env.Lookup(name)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ReadTOML parses a TOML file into a map; missing or invalid files are empty.
func ReadTOML(path string) map[string]any {
	data := map[string]any{}
	raw, err := os.ReadFile(path) //nolint:gosec // the config path is operator input
	if err != nil {
		return data
	}
	if _, err := toml.Decode(string(raw), &data); err != nil {
		return map[string]any{}
	}
	return data
}

// Section returns a TOML table, or an empty map.
func Section(data map[string]any, name string) map[string]any {
	if v, ok := data[name].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func strValue(section map[string]any, key, def string) string {
	v, ok := section[key]
	if !ok || v == nil {
		return def
	}
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return def
}

// tomlList reads a TOML array or colon list of folders, and whether it was set.
func tomlList(v any, def []string) ([]string, bool) {
	switch x := v.(type) {
	case []any:
		out := []string{}
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		return SplitColon(x), true
	}
	return slices.Clone(def), false
}

// Load builds a Config. path "" means SVMC_CONFIG, then DefaultPath.
func Load(path string, env Env) Config {
	if env == nil {
		env = OSEnv()
	}
	resolved := ExpandUser(path)
	if path == "" {
		resolved = envPath(env, "SVMC_CONFIG", DefaultPath)
	}
	data := ReadTOML(resolved)
	vault := Section(data, "vault")
	metadata := Section(data, "metadata")
	cache := Section(data, "cache")
	unlock := Section(data, "restricted")
	if len(unlock) == 0 {
		unlock = Section(data, "secret_adjacent")
	}

	indexed, indexedSet := tomlList(vault["indexed_dirs"], DefaultIndexedDirs)
	_, indexedEnv := env.Lookup("SVMC_INDEXED_DIRS")
	readOnlyDefault := DefaultReadOnlyDirs
	if indexedSet || indexedEnv {
		readOnlyDefault = nil
	}
	readOnly, _ := tomlList(vault["read_only_dirs"], readOnlyDefault)

	vaultPath := envPath(env, "SVMC_VAULT_PATH", strValue(vault, "path", "~/Documents/vault"))
	aliases := Section(data, "aliases")
	aliasesDefault := filepath.Join(vaultPath, ".svmc", "aliases.toml")

	dailyDir := strValue(vault, "daily_notes_dir", "00 Inbox/Daily Note")
	if v, ok := env.Lookup("SVMC_DAILY_NOTES_DIR"); ok {
		dailyDir = v
	}
	metadataURL := strValue(metadata, "url", "")
	if v, ok := env.Lookup("SVMC_METADATA_URL"); ok {
		metadataURL = v
	}
	cacheRaw := strValue(cache, "seconds", "30")
	if v, ok := env.Lookup("SVMC_CACHE_SECONDS"); ok {
		cacheRaw = v
	}
	cacheSeconds, err := strconv.Atoi(strings.TrimSpace(cacheRaw))
	if err != nil {
		cacheSeconds = 30
	}

	allowDefault := false
	if v, ok := unlock["allow_unlock"].(bool); ok {
		allowDefault = v
	}
	allow := envBool(env, "SVMC_ALLOW_RESTRICTED_UNLOCK",
		envBool(env, "SVMC_ALLOW_SECRET_ADJACENT_UNLOCK", allowDefault))

	hash := strValue(unlock, "hash", "")
	if v, ok := env.Lookup("SVMC_SECRET_ADJACENT_UNLOCK_HASH"); ok {
		hash = v
	}
	if v, ok := env.Lookup("SVMC_RESTRICTED_UNLOCK_HASH"); ok {
		hash = v
	}

	legacy := func(name, def string) string {
		if v, ok := env.Lookup(name); ok {
			return v
		}
		return def
	}

	return Config{
		VaultPath:     vaultPath,
		IndexedDirs:   envList(env, "SVMC_INDEXED_DIRS", indexed),
		ReadOnlyDirs:  envList(env, "SVMC_READ_ONLY_DIRS", readOnly),
		DailyNotesDir: dailyDir,
		AliasesPath:   envPath(env, "SVMC_ALIASES_PATH", strValue(aliases, "path", aliasesDefault)),
		MetadataURL:   metadataURL,
		CacheSeconds:  cacheSeconds,

		AllowRestrictedUnlock: allow,
		RestrictedUnlockHash:  hash,
		RestrictedUnlockHashFile: envPath(env, "SVMC_RESTRICTED_UNLOCK_HASH_FILE",
			legacy("SVMC_SECRET_ADJACENT_UNLOCK_HASH_FILE",
				strValue(unlock, "hash_file", "~/.config/severino-vault-mcp/restricted-unlock.phc"))),
		RestrictedKeychainService: legacy("SVMC_RESTRICTED_UNLOCK_KEYCHAIN_SERVICE",
			legacy("SVMC_SECRET_ADJACENT_UNLOCK_KEYCHAIN_SERVICE",
				strValue(unlock, "keychain_service", "severino-vault-mcp"))),
		RestrictedKeychainAccount: legacy("SVMC_RESTRICTED_UNLOCK_KEYCHAIN_ACCOUNT",
			legacy("SVMC_SECRET_ADJACENT_UNLOCK_KEYCHAIN_ACCOUNT",
				strValue(unlock, "keychain_account", "restricted-unlock"))),
		RestrictedAuditLog: envPath(env, "SVMC_RESTRICTED_UNLOCK_AUDIT_LOG",
			legacy("SVMC_SECRET_ADJACENT_UNLOCK_AUDIT_LOG",
				strValue(unlock, "audit_log", "~/.local/state/severino-vault-mcp/audit.log"))),
	}
}
