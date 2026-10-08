// Package gate is the sensitivity policy: public, internal and sensitive
// bodies are released (sensitive with an advisory); restricted bodies are
// withheld unless the caller asks and a local interactive unlock succeeds.
package gate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Sensitivity is a doc's label.
type Sensitivity string

// The labels.
const (
	Public     Sensitivity = "public"
	Internal   Sensitivity = "internal"
	Sensitive  Sensitivity = "sensitive"
	Restricted Sensitivity = "restricted"
)

// Parse normalizes a label; missing or unknown labels are internal.
func Parse(value string) Sensitivity {
	if value == "" {
		return Internal
	}
	switch v := strings.ToLower(strings.TrimSpace(value)); v {
	case "secret_adjacent", "credential_adjacent":
		return Restricted
	case "confidential":
		return Sensitive
	case "public", "internal", "sensitive", "restricted":
		return Sensitivity(v)
	}
	return Internal
}

// Releasable reports whether a body may be returned without an unlock.
func Releasable(s Sensitivity) bool { return s != Restricted }

// Advisory is the free-text note that accompanies a read.
func Advisory(s Sensitivity, overrideUsed bool) string {
	switch {
	case s == Sensitive:
		return "Doc is labeled `sensitive`. Body returned because the MCP runs " +
			"locally, but treat this content as private — don't paste it into " +
			"untrusted contexts."
	case s == Restricted && !overrideUsed:
		return "Body withheld: sensitivity=restricted. This doc is near " +
			"credentials, keys, or recovery paths. To request release, rerun read_doc with " +
			"include_restricted=True; the local MCP will require an " +
			"interactive unlock on the Mac."
	case s == Restricted && overrideUsed:
		return "Body released after explicit request plus local interactive unlock. " +
			"This doc is labeled restricted — be deliberate about what you " +
			"do with the content."
	}
	return ""
}

// LoadHash reads the unlock hash from the environment value, a local
// file, or the macOS Keychain, in that order.
func LoadHash(envHash, hashFile, service, account string) string {
	if envHash != "" {
		return strings.TrimSpace(envHash)
	}
	if info, err := os.Stat(hashFile); err == nil && info.Mode().IsRegular() {
		raw, err := os.ReadFile(hashFile) //nolint:gosec // the unlock-hash file is operator config
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(raw))
	}
	if runtime.GOOS != "darwin" {
		return ""
	}
	if _, err := exec.LookPath("security"); err != nil {
		return ""
	}
	out, err := runTimeout(5*time.Second, "security", "find-generic-password", "-s", service, "-a", account, "-w")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// PromptPhrase asks for the unlock phrase through a macOS hidden-input
// dialog. Tests replace it.
var PromptPhrase = func(docID, title string) (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		return "", false
	}
	label := appleScriptString(fmt.Sprintf("Unlock restricted doc?\n\n%s\n%s", title, docID))
	script := "display dialog " + label + ` default answer "" with hidden answer ` +
		`buttons {"Cancel", "Unlock"} default button "Unlock"`
	out, err := runTimeout(60*time.Second, "osascript", "-e", script, "-e", "text returned of result")
	if err != nil {
		return "", false
	}
	return strings.TrimRight(out, "\n"), true
}

func runTimeout(d time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // only called with fixed program names
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timeout")
		}
		return "", err
	}
	return string(out), nil
}

func appleScriptString(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

// isoNow is Python's datetime.now(UTC).isoformat().
func isoNow() string {
	now := time.Now().UTC()
	base := now.Format("2006-01-02T15:04:05")
	if us := now.Nanosecond() / 1000; us != 0 {
		base += fmt.Sprintf(".%06d", us)
	}
	return base + "+00:00"
}

func appendAudit(path, line string) {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // audit log directories keep the standard directory mode
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // the audit log path is operator config
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
	_ = os.Chmod(path, 0o600)
}

// AuditUnlock records one unlock attempt. Never body content or phrases.
func AuditUnlock(path, docID, result string) {
	appendAudit(path, fmt.Sprintf("%s action=restricted_unlock doc_id=%s result=%s client=stdio", isoNow(), docID, result))
}

// AuditEvent records a non-unlock local action.
func AuditEvent(path, action, detail string) {
	suffix := ""
	if detail != "" {
		suffix = " " + detail
	}
	appendAudit(path, fmt.Sprintf("%s action=%s%s client=stdio", isoNow(), action, suffix))
}
