// Package gate is the sensitivity policy: public, internal and sensitive
// bodies are released (sensitive with an advisory); restricted bodies are
// withheld unless the caller asks and a 1Password read approved with
// Touch ID succeeds.
package gate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
			"include_restricted=True; the local MCP will ask for Touch ID " +
			"through 1Password."
	case s == Restricted && overrideUsed:
		return "Body released after explicit request plus a Touch ID unlock. " +
			"This doc is labeled restricted — be deliberate about what you " +
			"do with the content."
	}
	return ""
}

// Approve reads the unlock item at ref through the 1Password CLI, whose
// desktop app integration asks for Touch ID. A nil error is the approval;
// the value read only proves presence and is dropped here. Tests replace it.
var Approve = func(ref string) error {
	if _, err := exec.LookPath("op"); err != nil {
		return fmt.Errorf("the 1Password CLI (op) is not on PATH")
	}
	out, err := runTimeout(60*time.Second, "op", "read", "--no-newline", ref)
	if err != nil {
		return fmt.Errorf("op read failed: %w", err)
	}
	if out == "" {
		return fmt.Errorf("op read returned an empty value")
	}
	return nil
}

// ServiceAccountToken is the variable that makes op act as a 1Password
// service account, which would approve an unlock without a human.
const ServiceAccountToken = "OP_SERVICE_ACCOUNT_TOKEN" //nolint:gosec // a variable name, not a credential

// ServiceAccountPresent reports whether this process carries a service
// account token, in which case no unlock may run.
func ServiceAccountPresent() bool {
	_, ok := os.LookupEnv(ServiceAccountToken)
	return ok
}

// headlessVars authenticate op without the desktop app; the child never
// gets them, so the only path to a value is the Touch ID prompt.
var headlessVars = []string{ServiceAccountToken, "OP_CONNECT_HOST", "OP_CONNECT_TOKEN"}

func runTimeout(d time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // only called with fixed program names
	cmd.WaitDelay = time.Second
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(headlessVars, k)
	})
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timeout")
		}
		return "", err
	}
	return string(out), nil
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

// AuditUnlock records one unlock attempt and its result. Never body
// content or the value read.
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
