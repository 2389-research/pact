// ABOUTME: Defines PACT's fixed Phase 2 resource bounds and safe typed errors.
// ABOUTME: Keeps admission failures machine-readable without exposing input data.
package ledger

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"

	"pact/internal/canonical"
)

// LimitsProfile identifies the fixed Phase 2 resource profile.
const LimitsProfile = "pact/resource-limits/phase2-v1"

// Limits holds the maximum resource use permitted by one Phase 2 operation.
type Limits struct {
	ObjectBytes, Objects, CanonicalBytes, EventsPerCommit, Events uint64
	ParentsPerCommit, CausalDepth, FrontierNodes, GraphEdges      uint64
	PageResults, FilterValuesPerFamily, FilterValuesTotal         uint64
	EncodedCursorBytes, DecodedCursorBytes, JSONResultBytes       uint64
	SQLiteBytes, DiagnosticSamples, DiagnosticTextBytes           uint64
}

// Phase2Limits is the one fixed resource profile for Phase 2.
var Phase2Limits = Limits{
	ObjectBytes: 4_194_304, Objects: 100_000, CanonicalBytes: 1_073_741_824,
	EventsPerCommit: 1_024, Events: 250_000, ParentsPerCommit: 64,
	CausalDepth: 4_096, FrontierNodes: 4_096, GraphEdges: 1_000_000,
	PageResults: 1_000, FilterValuesPerFamily: 64, FilterValuesTotal: 256,
	EncodedCursorBytes: 4_096, DecodedCursorBytes: 3_072, JSONResultBytes: 16_777_216,
	SQLiteBytes: 2_147_483_648, DiagnosticSamples: 100, DiagnosticTextBytes: 512,
}

// LimitError reports the first observed resource bound violation.
type LimitError struct {
	Resource        string
	Maximum         uint64
	ObservedAtLeast uint64
	ObjectID        string
}

func (err *LimitError) Error() string {
	if err == nil {
		return "resource limit exceeded"
	}
	return fmt.Sprintf("resource limit exceeded: %s maximum=%d observed_at_least=%d", err.Resource, err.Maximum, err.ObservedAtLeast)
}

func limitError(resource string, maximum uint64) *LimitError {
	return &LimitError{Resource: resource, Maximum: maximum, ObservedAtLeast: maximum + 1}
}

func checkSignedObjectBytes(format string, body map[string]any, keyID, publicKey string) error {
	bodyRaw, err := canonical.Marshal(body)
	if err != nil {
		return fmt.Errorf("canonicalize signed object body: %w", err)
	}
	prospective := map[string]any{
		"format": format, "body": body, "body_digest": canonical.Digest(bodyRaw),
		"signature": map[string]any{
			"algorithm": "ed25519", "key_id": keyID, "public_key": publicKey,
			"value": strings.Repeat("A", base64.RawURLEncoding.EncodedLen(ed25519.SignatureSize)),
		},
	}
	raw, err := canonical.Marshal(prospective)
	if err != nil {
		return fmt.Errorf("canonicalize prospective signed object: %w", err)
	}
	if uint64(len(raw)) > Phase2Limits.ObjectBytes {
		return limitError("object_bytes", Phase2Limits.ObjectBytes)
	}
	return nil
}
