// Command evals scores the vault retrievers against a case file.
//
// With no flags it runs the committed fixture vault and cases. With
// SVMC_EVAL_VAULT set (or -vault) it runs the operator's real vault,
// read-only, with the cases in evals/local/cases.json (or -cases), which
// stay out of the repository because real doc ids and titles are private.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/evals"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
)

const (
	fixtureVault = "internal/evals/testdata/vault"
	fixtureCases = "internal/evals/testdata/cases.json"
	localCases   = "evals/local/cases.json"
)

func main() {
	vaultPath := flag.String("vault", os.Getenv("SVMC_EVAL_VAULT"), "real vault to score, read-only (default $SVMC_EVAL_VAULT; unset runs the fixture)")
	casesPath := flag.String("cases", "", "case file (default: the fixture's, or "+localCases+" with a real vault)")
	backends := flag.String("backends", strings.Join(evals.BackendNames(), ","), "comma-separated backends to score")
	asJSON := flag.Bool("json", false, "print JSON instead of the table")
	misses := flag.Bool("misses", false, "list every case whose first hit is not a wanted doc")
	flag.Parse()

	var cfg config.Config
	if *vaultPath == "" {
		cfg = evals.FixtureConfig(abs(fixtureVault))
		if *casesPath == "" {
			*casesPath = fixtureCases
		}
	} else {
		cfg = evals.RealConfig(*vaultPath)
		if *casesPath == "" {
			*casesPath = localCases
		}
	}
	cases, err := evals.LoadCases(*casesPath)
	if err != nil {
		fail(err)
	}
	reports, err := evals.RunAll(cfg, cases, strings.Split(*backends, ","))
	if err != nil {
		fail(err)
	}
	if *asJSON {
		fmt.Println(jsonx.Pretty(evals.JSON(reports)))
		return
	}
	fmt.Print(evals.Table(reports, *misses))
}

func abs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		fail(err)
	}
	return a
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "evals:", err)
	os.Exit(1)
}
