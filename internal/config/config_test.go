package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileSetsValuesAndEnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	a, b := filepath.Join(tmp, "vault-a"), filepath.Join(tmp, "vault-b")
	cfg := filepath.Join(tmp, "config.toml")
	write(t, cfg, "[vault]\npath = \""+a+"\"\nindexed_dirs = [\"Docs\"]\n\n[cache]\nseconds = 12\n\n[metadata]\nurl = \"https://metadata.example.test\"\n")
	c := Load("", Env{"SVMC_CONFIG": cfg})
	if c.VaultPath != a || !slices.Equal(c.IndexedDirs, []string{"Docs"}) || c.DailyNotesDir != "00 Inbox/Daily Note" ||
		c.CacheSeconds != 12 || c.MetadataURL != "https://metadata.example.test" {
		t.Fatalf("%+v", c)
	}
	c = Load("", Env{"SVMC_CONFIG": cfg, "SVMC_VAULT_PATH": b, "SVMC_INDEXED_DIRS": "Ops:Runbooks", "SVMC_CACHE_SECONDS": "0",
		"SVMC_METADATA_URL": "https://override.example.test"})
	if c.VaultPath != b || !slices.Equal(c.IndexedDirs, []string{"Ops", "Runbooks"}) || c.CacheSeconds != 0 || c.MetadataURL != "https://override.example.test" {
		t.Fatalf("%+v", c)
	}
}

func TestExplicitPathAndEnvironmentMapping(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "instance.toml")
	write(t, cfg, "[vault]\npath = \""+filepath.Join(tmp, "file-vault")+"\"\nindexed_dirs = [\"Docs\"]\n[cache]\nseconds = 20\n")
	c := Load(cfg, Env{"SVMC_VAULT_PATH": filepath.Join(tmp, "env-vault"), "SVMC_CACHE_SECONDS": "7"})
	if c.VaultPath != filepath.Join(tmp, "env-vault") || !slices.Equal(c.IndexedDirs, []string{"Docs"}) || c.CacheSeconds != 7 {
		t.Fatalf("%+v", c)
	}
}

func TestExplicitPathWinsOverEnvConfigPath(t *testing.T) {
	tmp := t.TempDir()
	chosen, ignored := filepath.Join(tmp, "chosen.toml"), filepath.Join(tmp, "ignored.toml")
	write(t, chosen, "[vault]\npath = \""+filepath.Join(tmp, "chosen")+"\"\n")
	write(t, ignored, "[vault]\npath = \""+filepath.Join(tmp, "ignored")+"\"\n")
	if c := Load(chosen, Env{"SVMC_CONFIG": ignored}); c.VaultPath != filepath.Join(tmp, "chosen") {
		t.Fatal(c.VaultPath)
	}
}

func TestDefaultsAndUnlockSettings(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	c := Load("/nonexistent.toml", Env{"SVMC_ALLOW_RESTRICTED_UNLOCK": "yes"})
	if c.VaultPath != "/home/x/Documents/vault" || !slices.Equal(c.IndexedDirs, DefaultIndexedDirs) || c.CacheSeconds != 30 || !c.AllowRestrictedUnlock ||
		c.AliasesPath != "/home/x/Documents/vault/.svmc/aliases.toml" || c.RestrictedAuditLog != "/home/x/.local/state/severino-vault-mcp/audit.log" {
		t.Fatalf("%+v", c)
	}
}
