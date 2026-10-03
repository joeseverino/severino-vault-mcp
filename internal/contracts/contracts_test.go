package contracts

import (
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

func TestFingerprintIsOrderIndependent(t *testing.T) {
	if Fingerprint(jsonx.New("b", 2, "a", 1)) != Fingerprint(jsonx.New("a", 1, "b", 2)) {
		t.Fatal("order changed the fingerprint")
	}
}

func TestReceiptIsStableSortedAndBodyFree(t *testing.T) {
	r := Receipt{
		Operation: "task.create", EntityType: "task", EntityID: "task-example",
		ChangedFields:       []string{"title", "status"},
		AfterFingerprint:    Fingerprint(jsonx.New("status", "open", "title", "Example")),
		AffectedProjections: []string{"task_board", "brief"},
		Metadata:            jsonx.New("relative_path", "07 Backlog/task-example.md"),
	}.AsDict()
	if jsonx.Compact(func() any { v, _ := r.Get("entity"); return v }()) != `{"type":"task","id":"task-example"}` ||
		jsonx.Compact(func() any { v, _ := r.Get("changed_fields"); return v }()) != `["status","title"]` ||
		len(r.Str("idempotency_key")) != 64 || strings.Contains(strings.ToLower(jsonx.Compact(r)), "body") {
		t.Fatal(jsonx.Compact(r))
	}
}
