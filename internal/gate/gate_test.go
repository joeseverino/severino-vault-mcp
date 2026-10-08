package gate

import (
	"os"
	"path/filepath"
	"slices"
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

var fast = Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashPhraseRoundTrips(t *testing.T) {
	encoded, err := HashPhrase("open sesame", fast)
	if err != nil || !strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatal(encoded, err)
	}
	if !VerifyPhrase("open sesame", encoded) || VerifyPhrase("open sesamE", encoded) || VerifyPhrase("", encoded) {
		t.Fatal("verify")
	}
	again, _ := HashPhrase("open sesame", fast)
	if again == encoded {
		t.Fatal("salt reused")
	}
}

func TestDefaultParamsAreTheDocumentedCost(t *testing.T) {
	encoded, err := HashPhrase("x", DefaultParams)
	if err != nil || !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$") || !VerifyPhrase("x", encoded) {
		t.Fatal(encoded, err)
	}
}

func TestParseHashReadsParameters(t *testing.T) {
	encoded, _ := HashPhrase("x", Params{Memory: 128, Time: 2, Threads: 2, SaltLen: 12, KeyLen: 24})
	p, salt, key, err := ParseHash(encoded)
	if err != nil || p != (Params{Memory: 128, Time: 2, Threads: 2, SaltLen: 12, KeyLen: 24}) || len(salt) != 12 || len(key) != 24 {
		t.Fatalf("%+v %v", p, err)
	}
	// Parameter order is free; the verify uses what the string says.
	parts := strings.Split(encoded, "$")
	parts[3] = "p=2,t=2,m=128"
	if !VerifyPhrase("x", strings.Join(parts, "$")) {
		t.Fatal("reordered parameters")
	}
}

func TestMalformedHashesFailClosed(t *testing.T) {
	good, _ := HashPhrase("x", fast)
	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		c := slices.Clone(parts)
		c[i] = v
		return strings.Join(c, "$")
	}
	for name, encoded := range map[string]string{
		"empty":            "",
		"old sha256":       "sha256:73616c74:00",
		"argon2i":          with(1, "argon2i"),
		"version":          with(2, "v=16"),
		"missing param":    with(3, "m=64,t=1"),
		"duplicate param":  with(3, "m=64,m=64,t=1"),
		"unknown param":    with(3, "m=64,t=1,p=1,x=1"),
		"not a number":     with(3, "m=lots,t=1,p=1"),
		"zero time":        with(3, "m=64,t=0,p=1"),
		"zero threads":     with(3, "m=64,t=1,p=0"),
		"huge memory":      with(3, "m=4294967295,t=1,p=1"),
		"huge time":        with(3, "m=64,t=100,p=1"),
		"threads overflow": with(3, "m=4096,t=1,p=256"),
		"memory too low":   with(3, "m=7,t=1,p=1"),
		"bad salt":         with(4, "!!"),
		"short salt":       with(4, "c2FsdA"),
		"bad hash":         with(5, "!!"),
		"short hash":       with(5, "AAAA"),
		"extra field":      good + "$x",
		"no leading $":     strings.TrimPrefix(good, "$"),
	} {
		if _, _, _, err := ParseHash(encoded); err == nil || VerifyPhrase("x", encoded) {
			t.Errorf("%s accepted: %q", name, encoded)
		}
	}
}

func TestLoadHashPrefersEnvThenFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hash")
	if err := os.WriteFile(file, []byte("$argon2id$v=19$m=64,t=1,p=1$a$b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if LoadHash(" env ", file, "s", "a") != "env" || LoadHash("", file, "s", "a") != "$argon2id$v=19$m=64,t=1,p=1$a$b" {
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

func FuzzParseHash(f *testing.F) {
	encoded, err := HashPhrase("open sesame", fast)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add("$argon2id$v=19$m=64,t=1,p=1$a$b")
	f.Add("$argon2id$v=19$m=1048577,t=17,p=0$$")
	f.Add("$argon2id$v=19$m=64,m=64,p=1$AAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA")
	f.Fuzz(func(t *testing.T, s string) {
		p, salt, key, err := ParseHash(s)
		if err != nil {
			return
		}
		if p.Time == 0 || p.Time > maxTime || p.Memory > maxMemory || p.Threads == 0 ||
			len(salt) < minSaltLen || len(key) < minKeyLen || len(key) > maxKeyLen {
			t.Fatalf("accepted out-of-range hash %q: %+v", s, p)
		}
	})
}
