package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNormalizesLabels(t *testing.T) {
	for in, want := range map[string]Sensitivity{"": Internal, "PUBLIC": Public, "secret_adjacent": Internal,
		"restricted": Restricted, "confidential": Sensitive, "nonsense": Internal} {
		if got := Parse(in); got != want {
			t.Errorf("%q: %s", in, got)
		}
	}
}

// fakeOp puts an op on PATH (and nothing else) that answers like the
// 1Password CLI: it insists on `read --no-newline <ref>` and on an
// environment free of headless credentials.
func fakeOp(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
[ "$1 $2" = "read --no-newline" ] || exit 3
[ -z "${OP_SERVICE_ACCOUNT_TOKEN+x}${OP_CONNECT_HOST+x}${OP_CONNECT_TOKEN+x}" ] || exit 4
case "$3" in
op://Vault/ok/password) printf 'unlock-value' ;;
op://Vault/empty/password) ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "op"), []byte(script), 0o700); err != nil { //nolint:gosec // the fake must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestApproveRunsOpReadWithoutHeadlessCredentials(t *testing.T) {
	fakeOp(t)
	t.Setenv("OP_CONNECT_HOST", "https://connect.example.com")
	t.Setenv("OP_CONNECT_TOKEN", "x")
	if err := Approve("op://Vault/ok/password"); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"op://Vault/empty/password", "op://Vault/missing/password"} {
		err := Approve(ref)
		if err == nil || strings.Contains(err.Error(), "unlock-value") {
			t.Errorf("%s: %v", ref, err)
		}
	}
}

func TestApproveRefusesWithoutOp(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Approve("op://Vault/ok/password"); err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Fatal(err)
	}
}

func TestServiceAccountPresent(t *testing.T) {
	t.Setenv(ServiceAccountToken, "")
	if !ServiceAccountPresent() {
		t.Fatal("an empty token is still present")
	}
	if err := os.Unsetenv(ServiceAccountToken); err != nil {
		t.Fatal(err)
	}
	if ServiceAccountPresent() {
		t.Fatal("absent token")
	}
}

func TestAuditRecordsTheResultAndIsPrivate(t *testing.T) {
	log := filepath.Join(t.TempDir(), "state", "audit.log")
	AuditUnlock(log, "infra-x", "approved")
	data, _ := os.ReadFile(log)
	info, _ := os.Stat(log)
	if !strings.Contains(string(data), "action=restricted_unlock doc_id=infra-x result=approved client=stdio") || info.Mode().Perm() != 0o600 {
		t.Fatalf("%q %v", data, info.Mode())
	}
}

func TestAdvisories(t *testing.T) {
	if !strings.Contains(Advisory(Sensitive, false), "sensitive") || !strings.Contains(Advisory(Restricted, false), "withheld") ||
		!strings.Contains(Advisory(Restricted, true), "released") || Advisory(Public, false) != "" {
		t.Fatal("advisories")
	}
}
