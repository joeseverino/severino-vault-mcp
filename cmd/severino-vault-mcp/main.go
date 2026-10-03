// Command severino-vault-mcp serves Joe's Obsidian vaults over MCP and runs
// one governed vault call per subcommand.
package main

import (
	"os"

	"github.com/joeseverino/severino-vault-mcp/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], &cli.Ctx{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
