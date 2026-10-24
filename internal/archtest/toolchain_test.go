package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// Go's toolchain directive also lets advisory scanners identify the standard
// library release. mise installs that same release, rather than letting Go
// fetch a different one behind the tool lock.
func TestToolchainPinsAgree(t *testing.T) {
	root := filepath.Join("..", "..")
	var config struct {
		Tools struct{ Go string }
		Env   struct {
			GoToolchain string `toml:"GOTOOLCHAIN"`
		}
		Settings struct{ Locked bool }
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "mise.toml"), &config); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	language := regexp.MustCompile(`(?m)^go (\S+)$`).FindSubmatch(mod)
	toolchain := regexp.MustCompile(`(?m)^toolchain go(\S+)$`).FindSubmatch(mod)
	if len(language) != 2 || len(toolchain) != 2 {
		t.Fatal("go.mod must declare its language and toolchain")
	}
	if config.Tools.Go != string(toolchain[1]) || !strings.HasPrefix(config.Tools.Go, string(language[1])+".") {
		t.Fatalf("mise Go %s, go.mod language %s and toolchain %s disagree", config.Tools.Go, language[1], toolchain[1])
	}
	if !config.Settings.Locked || config.Env.GoToolchain != "local" {
		t.Fatal("mise must enforce its lock and GOTOOLCHAIN=local")
	}
}
