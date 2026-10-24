package mcpserver

import (
	"os"
	"path/filepath"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

// executable is the running binary's resolved path; tests point it at a file
// they replace.
var executable = func() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// binaryWatch holds the identity of the binary this process started from. A
// server outlives an install of a newer binary, and the old code reads notes
// in a newer line grammar wrongly and issues ids the new code would not, so a
// replaced binary stops this process writing and marks what it reads.
type binaryWatch struct {
	path string
	info os.FileInfo
}

// watchBinary records the running binary; nil when it cannot be found, which
// leaves every call unchecked.
func watchBinary() *binaryWatch {
	p, err := executable()
	if err != nil {
		return nil
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil
	}
	return &binaryWatch{path: p, info: info}
}

// replaced reports whether the file at the binary's path is no longer the one
// this process started from: another inode, size or mtime, or none at all.
func (w *binaryWatch) replaced() bool {
	if w == nil {
		return false
	}
	now, err := os.Stat(w.path)
	return err != nil || !os.SameFile(w.info, now) || now.Size() != w.info.Size() || !now.ModTime().Equal(w.info.ModTime())
}

// staleText says why a replaced binary's answers can't be trusted and what
// recovers. find and read_doc load with the session (anthropic/alwaysLoad),
// so a reconnect keeps their old schema and only a new session drops it.
func (w *binaryWatch) staleText() string {
	return "the server binary " + w.path + " was replaced after this process started, so it may misread notes " +
		"the new binary writes; restart the Claude Code session to run the new one (a /mcp reconnect is not enough, " +
		"since find and read_doc stay loaded with this process's schema)"
}

// staleFailure refuses a write tool's call on a replaced binary.
func (w *binaryWatch) staleFailure(tool string) *jsonx.Obj {
	return jsonx.Fail("refused: "+w.staleText()).
		Set("next_steps", []string{core.Call(tool) + " with the same arguments, after restarting the session"})
}

// readOnly reports whether a core tool only reads, from its annotations.
func readOnly(tool string) bool {
	a := annotations[tool]
	return a != nil && a.ReadOnlyHint
}
