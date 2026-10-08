package gate

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are argon2id cost parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	SaltLen int
	KeyLen  int
}

// DefaultParams is the cost unlock-hash uses.
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 4, SaltLen: 16, KeyLen: 32}

// Ceilings on parameters read from a stored hash, so a malformed or hostile
// string can't make a verify allocate or spin without bound.
const (
	maxMemory  = 1 << 20 // KiB (1 GiB)
	maxTime    = 16
	minSaltLen = 8
	minKeyLen  = 16
	maxKeyLen  = 64
)

var b64 = base64.RawStdEncoding

// keyLen is KeyLen as argon2 takes it; ParseHash bounds it to maxKeyLen.
func (p Params) keyLen() uint32 {
	return uint32(min(max(p.KeyLen, 0), maxKeyLen)) //nolint:gosec // clamped to [0, maxKeyLen]
}

// HashPhrase returns the PHC string for phrase:
// $argon2id$v=19$m=<KiB>,t=<passes>,p=<threads>$<salt>$<hash>.
func HashPhrase(phrase string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(phrase), salt, p.Time, p.Memory, p.Threads, p.keyLen())
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// ParseHash splits a PHC argon2id string into its parameters, salt and key.
func ParseHash(encoded string) (Params, []byte, []byte, error) {
	var p Params
	parts := strings.Split(strings.TrimSpace(encoded), "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, fmt.Errorf("not an argon2id PHC string")
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, nil, nil, fmt.Errorf("unsupported argon2 version %q", parts[2])
	}
	seen := map[string]bool{}
	for kv := range strings.SplitSeq(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || seen[k] {
			return p, nil, nil, fmt.Errorf("bad parameter %q", kv)
		}
		seen[k] = true
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return p, nil, nil, fmt.Errorf("bad parameter %q", kv)
		}
		switch k {
		case "m":
			p.Memory = uint32(n)
		case "t":
			p.Time = uint32(n)
		case "p":
			if n > 255 {
				return p, nil, nil, fmt.Errorf("bad parameter %q", kv)
			}
			p.Threads = uint8(n)
		default:
			return p, nil, nil, fmt.Errorf("unknown parameter %q", k)
		}
	}
	if len(seen) != 3 {
		return p, nil, nil, fmt.Errorf("need m, t and p")
	}
	if p.Threads == 0 || p.Time == 0 || p.Time > maxTime || p.Memory < 8*uint32(p.Threads) || p.Memory > maxMemory {
		return p, nil, nil, fmt.Errorf("parameters out of range")
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < minSaltLen {
		return p, nil, nil, fmt.Errorf("bad salt")
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) < minKeyLen || len(key) > maxKeyLen {
		return p, nil, nil, fmt.Errorf("bad hash")
	}
	p.SaltLen, p.KeyLen = len(salt), len(key)
	return p, salt, key, nil
}

// VerifyPhrase checks a phrase against a PHC argon2id string in constant time.
// Anything malformed fails closed.
func VerifyPhrase(phrase, encoded string) bool {
	p, salt, key, err := ParseHash(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(phrase), salt, p.Time, p.Memory, p.Threads, p.keyLen())
	return subtle.ConstantTimeCompare(got, key) == 1
}
