// ABOUTME: Defines PACT's fixed Phase 2 resource bounds and safe typed errors.
// ABOUTME: Keeps admission failures machine-readable without exposing input data.
package ledger

import "fmt"

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
