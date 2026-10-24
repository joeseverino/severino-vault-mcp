package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

func TestMoveDocMovesThroughTheCLIAndExitsOneOnARefusal(t *testing.T) {
	root := tk.FakeVault(t)
	before := tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md"))
	code, out, _ := run(t, root, "", "move-doc", "02 Infrastructure/", "--doc-id", "rb-add-nginx-proxy-host", "--dry-run")
	if code != 0 || !strings.Contains(out, `"dry_run":true`) || tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")) != before {
		t.Fatalf("preview: %d %s", code, out)
	}
	code, out, _ = run(t, root, "", "move-doc", "02 Infrastructure/", "--doc-id", "rb-add-nginx-proxy-host")
	if code != 0 || !strings.Contains(out, `"to":"02 Infrastructure/Add Nginx Proxy Host.md"`) {
		t.Fatalf("%d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "02 Infrastructure", "Add Nginx Proxy Host.md")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = run(t, root, "", "move-doc", "03 Runbooks/Local PKI.md", "--relative-path", "02 Infrastructure/Add Nginx Proxy Host.md")
	if code != 1 || !strings.Contains(out, "already names") || !strings.Contains(out, "next_steps") {
		t.Fatalf("%d %s", code, out)
	}
}
