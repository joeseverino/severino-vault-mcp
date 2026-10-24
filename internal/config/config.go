// Package config loads one vault's configuration from TOML plus SVMC_*
// environment overrides. Environment values win over the file; an explicit
// path wins over SVMC_CONFIG, which wins over the default path.
package config

import (
	"maps"
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
	VaultPath           string
	IndexedDirs         []string
	ReadOnlyDirs        []string
	ArchiveDir          string // "" when the vault has no archive
	DailyNotesDir       string
	AliasesPath         string
	MetadataURL         string
	CacheSeconds        int
	RestrictedUnlockRef string // op:// reference to the unlock item; "" keeps restricted bodies withheld
	RestrictedAuditLog  string
}

// DefaultIndexedDirs are the labs vault's indexed folders: searched, read,
// written and validated against the profile.
var DefaultIndexedDirs = []string{"01 Projects", "02 Infrastructure", "03 Runbooks", "04 Reference", "07 Backlog"}

// DefaultReadOnlyDirs are the labs vault's folders that find and read_doc
// serve but writes and validate leave alone: inbox captures and daily notes
// carry their own minimal frontmatter, and writeups and pages carry the
// site's. They apply only while the indexed dirs are the defaults too.
var DefaultReadOnlyDirs = []string{"00 Inbox", "05 Writeups", "06 Pages"}

// DefaultArchiveDir is the labs vault's archive, where retired docs and
// projects go with status: archived. The index reads it apart from the rest,
// so find leaves its docs out unless asked, and a write there keeps the doc
// archived (schema.Profile.CheckArchived). It applies only while the indexed
// dirs are the defaults too.
const DefaultArchiveDir = "99 Archive"

// Archived reports whether rel, a vault-relative path, is in the archive.
func (c Config) Archived(rel string) bool {
	if c.ArchiveDir == "" {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	return strings.HasPrefix(rel, c.ArchiveDir+"/")
}

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

// UnlockRefVar overrides [restricted].unlock_ref.
const UnlockRefVar = "SVMC_RESTRICTED_UNLOCK_REF"

// Locked returns env with restricted unlock unconfigured, whatever the
// config file says, so nothing loaded from it can release a restricted body.
func Locked(env Env) Env {
	out := maps.Clone(env)
	if out == nil {
		out = Env{}
	}
	out[UnlockRefVar] = ""
	return out
}

// Hermetic is a locked environment over the vault at root: no config file,
// so nothing on this machine leaks into what it loads.
func Hermetic(root string) Env {
	return Locked(Env{
		"HOME":            os.Getenv("HOME"),
		"SVMC_CONFIG":     filepath.Join(root, "absent.toml"),
		"SVMC_VAULT_PATH": root,
	})
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

	indexed, indexedSet := tomlList(vault["indexed_dirs"], DefaultIndexedDirs)
	_, indexedEnv := env.Lookup("SVMC_INDEXED_DIRS")
	readOnlyDefault := DefaultReadOnlyDirs
	if indexedSet || indexedEnv {
		readOnlyDefault = nil
	}
	readOnly, _ := tomlList(vault["read_only_dirs"], readOnlyDefault)
	archiveDefault := DefaultArchiveDir
	if indexedSet || indexedEnv {
		archiveDefault = ""
	}
	archive := strValue(vault, "archive_dir", archiveDefault)
	if v, ok := env.Lookup("SVMC_ARCHIVE_DIR"); ok {
		archive = v
	}

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

	unlockRef := strValue(unlock, "unlock_ref", "")
	if v, ok := env.Lookup(UnlockRefVar); ok {
		unlockRef = v
	}

	cfg := Config{
		VaultPath:     vaultPath,
		IndexedDirs:   envList(env, "SVMC_INDEXED_DIRS", indexed),
		ReadOnlyDirs:  envList(env, "SVMC_READ_ONLY_DIRS", readOnly),
		DailyNotesDir: dailyDir,
		AliasesPath:   envPath(env, "SVMC_ALIASES_PATH", strValue(aliases, "path", aliasesDefault)),
		MetadataURL:   metadataURL,
		CacheSeconds:  cacheSeconds,

		RestrictedUnlockRef: strings.TrimSpace(unlockRef),
		RestrictedAuditLog: envPath(env, "SVMC_RESTRICTED_UNLOCK_AUDIT_LOG",
			strValue(unlock, "audit_log", "~/.local/state/severino-vault-mcp/audit.log")),
	}
	// A folder the index already reads with the rest is no archive apart.
	archive = strings.Trim(filepath.ToSlash(filepath.Clean(archive)), "/")
	if archive != "" && archive != "." && !slices.Contains(cfg.SearchDirs(), archive) {
		cfg.ArchiveDir = archive
	}
	return cfg
}
