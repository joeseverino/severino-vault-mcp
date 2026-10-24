package mcpserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vaults"
)

// skillProblems lists what keeps a skill file from carrying the server's
// procedure: frontmatter naming the skill and saying when to load it, and a
// mention of every tool the server registers.
func skillProblems(text string, toolNames []string) []string {
	rest, opened := strings.CutPrefix(text, "---\n")
	fm, _, closed := strings.Cut(rest, "\n---\n")
	if !opened || !closed {
		return []string{"no frontmatter"}
	}
	var problems []string
	lines := strings.Split(fm, "\n")
	if !slices.Contains(lines, "name: vault") {
		problems = append(problems, "frontmatter does not set name: vault")
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return len(l) > len("description: ") && strings.HasPrefix(l, "description: ") }) {
		problems = append(problems, "frontmatter has no description")
	}
	for _, name := range toolNames {
		if !strings.Contains(text, "`"+name+"`") {
			problems = append(problems, "skill does not name "+name)
		}
	}
	return problems
}

// The procedure lives in the skill the server's instructions point to, so the
// skill names every tool the server registers and cannot drift from it.
func TestTheVaultSkillNamesEveryRegisteredTool(t *testing.T) {
	root := tk.Dir(t)
	env := tk.Env(root, "SVMC_EDU_CONFIG", eduConfig(t, root))
	s := connect(t, vaults.Build(context.Background(), env, io.Discard))
	registered := names(tools(t, s))
	if !strings.Contains(s.InitializeResult().Instructions, "vault skill") {
		t.Fatal("instructions do not point to the vault skill")
	}
	wd, _ := os.Getwd()
	raw, err := os.ReadFile(filepath.Join(wd, "..", "..", "skills", "vault", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if problems := skillProblems(text, registered); len(problems) > 0 {
		t.Fatal(problems)
	}
	if got := skillProblems(strings.ReplaceAll(text, "`log_entries`", "the log tool"), registered); !slices.Equal(got, []string{"skill does not name log_entries"}) {
		t.Fatalf("a skill missing a tool must fail: %v", got)
	}
}

// The skill and the tool contract state read_doc's bound as a number; it is
// core.BoundedBodyChars, so a change to the constant fails here until both
// say the new one.
func TestTheDocsStateTheReadBound(t *testing.T) {
	n := core.BoundedBodyChars
	want := fmt.Sprintf("%d,%03d", n/1000, n%1000)
	wd, _ := os.Getwd()
	for _, rel := range []string{"skills/vault/SKILL.md", "docs/ai-tool-contract.md"} {
		raw, err := os.ReadFile(filepath.Join(wd, "..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), want+" characters") && !strings.Contains(string(raw), "("+want+") characters") {
			t.Errorf("%s does not state read_doc's bound, %s characters", rel, want)
		}
	}
}
