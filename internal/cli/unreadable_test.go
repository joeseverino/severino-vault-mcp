package cli

import (
	"os"
	"strings"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestAnUnreadableVaultFailsDoctorValidateAndFind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	root := tk.FakeVault(t)
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	for _, args := range [][]string{
		{"doctor"},
		{"validate", "--json", root + "/03 Runbooks/Add Nginx Proxy Host.md"},
		{"validate", "03 Runbooks/Add Nginx Proxy Host.md"},
		{"find", "runbook"},
		{"update-frontmatter", "03 Runbooks/Add Nginx Proxy Host.md", "--status", "draft"},
		{"touch-reviewed", "03 Runbooks/Add Nginx Proxy Host.md"},
		{"backfill-aliases"},
		{"read", "rb-add-nginx-proxy-host"},
		{"write-doc", "--relative-path", "03 Runbooks/New.md", "--doc-id", "rb-new"},
		{"move-folder", "01 Projects/example-wiki", "01 Projects/other"},
		{"move-doc", "--doc-id", "rb-add-nginx-proxy-host", "04 Reference/"},
		{"log-entries", "list", "infra-example-log"},
		{"queue", "list"},
	} {
		code, out, _ := run(t, root, "", args...)
		if code != 1 || !strings.Contains(out, "unreadable") || !strings.Contains(out, root) {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
}
