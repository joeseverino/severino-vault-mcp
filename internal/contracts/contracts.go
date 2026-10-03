// Package contracts holds the deterministic receipts governed writes return.
package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

// Fingerprint is the SHA-256 of a value's sorted, compact JSON (non-ASCII
// kept as UTF-8).
func Fingerprint(v any) string {
	b, _ := jsonx.Encode(v, jsonx.Options{SortKeys: true})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Receipt reports a completed governed mutation. Evidence, never state.
type Receipt struct {
	Operation           string
	EntityType          string
	EntityID            string
	ChangedFields       []string
	BeforeFingerprint   string
	AfterFingerprint    string
	AffectedProjections []string
	Metadata            *jsonx.Obj
}

// AsDict renders the receipt.
func (r Receipt) AsDict() *jsonx.Obj {
	changed := slices.Sorted(slices.Values(r.ChangedFields))
	projections := slices.Sorted(slices.Values(r.AffectedProjections))
	if changed == nil {
		changed = []string{}
	}
	if projections == nil {
		projections = []string{}
	}
	var after any
	if r.AfterFingerprint != "" {
		after = r.AfterFingerprint
	}
	key := Fingerprint(jsonx.New(
		"operation", r.Operation,
		"entity_type", r.EntityType,
		"entity_id", r.EntityID,
		"after_fingerprint", after,
		"changed_fields", changed,
	))
	meta := r.Metadata
	if meta == nil {
		meta = jsonx.New()
	}
	o := jsonx.New(
		"receipt_version", 1,
		"operation", r.Operation,
		"entity", jsonx.New("type", r.EntityType, "id", r.EntityID),
		"changed_fields", changed,
		"affected_projections", projections,
		"idempotency_key", key,
		"metadata", meta,
	)
	if r.BeforeFingerprint != "" {
		o.Set("before_fingerprint", r.BeforeFingerprint)
	}
	if r.AfterFingerprint != "" {
		o.Set("after_fingerprint", r.AfterFingerprint)
	}
	return o
}
