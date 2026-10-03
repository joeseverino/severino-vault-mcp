package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNormalizesLabels(t *testing.T) {
	for in, want := range map[string]Sensitivity{"": Internal, "PUBLIC": Public, "secret_adjacent": Restricted,
		"credential_adjacent": Restricted, "confidential": Sensitive, "nonsense": Internal} {
		if got := Parse(in); got != want {
			t.Errorf("%q: %s", in, got)
		}
	}
}

func TestVerifyPhrase(t *testing.T) {
	salt := []byte("salt")
	sum := sha256.Sum256(append(append([]byte{}, salt...), "open sesame"...))
	encoded := "sha256:" + hex.EncodeToString(salt) + ":" + hex.EncodeToString(sum[:])
	if !VerifyPhrase("open sesame", encoded) || VerifyPhrase("wrong", encoded) || VerifyPhrase("open sesame", "md5:00:00") {
		t.Fatal("verify")
	}
}

func TestLoadHashPrefersEnvThenFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hash")
	os.WriteFile(file, []byte("sha256:aa:bb\n"), 0o600)
	if LoadHash(" env ", file, "s", "a") != "env" || LoadHash("", file, "s", "a") != "sha256:aa:bb" {
		t.Fatal("load order")
	}
}

func TestAuditNeverWritesPhrasesAndIsPrivate(t *testing.T) {
	log := filepath.Join(t.TempDir(), "state", "audit.log")
	AuditUnlock(log, "infra-x", "released")
	data, _ := os.ReadFile(log)
	info, _ := os.Stat(log)
	if !strings.Contains(string(data), "action=restricted_unlock doc_id=infra-x result=released client=stdio") || info.Mode().Perm() != 0o600 {
		t.Fatalf("%q %v", data, info.Mode())
	}
}

func TestAdvisories(t *testing.T) {
	if !strings.Contains(Advisory(Sensitive, false), "sensitive") || !strings.Contains(Advisory(Restricted, false), "withheld") ||
		!strings.Contains(Advisory(Restricted, true), "released") || Advisory(Public, false) != "" {
		t.Fatal("advisories")
	}
}
