// ABOUTME: Tests PACT's fixed Phase 2 resource profile and safe limit diagnostics.
// ABOUTME: Pins every published maximum so callers share one bounded contract.
package ledger

import (
	"errors"
	"strings"
	"testing"
)

func TestPhase2LimitsMatchApprovedProfile(t *testing.T) {
	want := Limits{
		ObjectBytes: 4_194_304, Objects: 100_000, CanonicalBytes: 1_073_741_824,
		EventsPerCommit: 1_024, Events: 250_000, ParentsPerCommit: 64,
		CausalDepth: 4_096, FrontierNodes: 4_096, GraphEdges: 1_000_000,
		PageResults: 1_000, FilterValuesPerFamily: 64, FilterValuesTotal: 256,
		EncodedCursorBytes: 4_096, DecodedCursorBytes: 3_072, JSONResultBytes: 16_777_216,
		SQLiteBytes: 2_147_483_648, DiagnosticSamples: 100, DiagnosticTextBytes: 512,
	}
	if LimitsProfile != "pact/resource-limits/phase2-v1" {
		t.Fatalf("LimitsProfile = %q", LimitsProfile)
	}
	if Phase2Limits != want {
		t.Fatalf("Phase2Limits = %#v, want %#v", Phase2Limits, want)
	}
}

func TestLimitErrorDoesNotExposeUntrustedDetails(t *testing.T) {
	err := &LimitError{Resource: "events_per_commit", Maximum: 1_024, ObservedAtLeast: 1_025, ObjectID: "not-an-object-id"}
	if got := err.Error(); got == "" || containsAny(got, "not-an-object-id", "$.events", "payload") {
		t.Fatalf("LimitError.Error() = %q, want fixed value-free diagnostic", got)
	}
}

func containsAny(value string, rejected ...string) bool {
	for _, item := range rejected {
		if len(item) != 0 && strings.Contains(value, item) {
			return true
		}
	}
	return false
}

func assertLimitError(t *testing.T, err error, resource string, maximum uint64) {
	t.Helper()
	var limit *LimitError
	if !errors.As(err, &limit) {
		t.Fatalf("error = %v, want LimitError", err)
	}
	if limit.Resource != resource || limit.Maximum != maximum || limit.ObservedAtLeast != maximum+1 {
		t.Fatalf("LimitError = %#v, want resource=%q maximum=%d observed_at_least=%d", limit, resource, maximum, maximum+1)
	}
}
