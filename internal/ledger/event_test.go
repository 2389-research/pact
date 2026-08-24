// ABOUTME: Tests strict, normalized PACT semantic event-batch admission data.
// ABOUTME: Covers immutable-secret safety without ever asserting raw secret bytes.
package ledger

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeEventBatchAdmitsExactEventLimitAndRejectsFirstExcess(t *testing.T) {
	for _, count := range []int{1_024, 1_025} {
		t.Run(fmt.Sprintf("%d events", count), func(t *testing.T) {
			events := make([]any, count)
			for index := range events {
				events[index] = eventInput(fmt.Sprintf("event-%04d", index), []any{}, []any{})
			}
			batch, err := NormalizeEventBatch(map[string]any{"events": events})
			if count == 1_024 {
				if err != nil || len(batch.Events) != count {
					t.Fatalf("NormalizeEventBatch() = (%#v, %v), want %d events", batch, err, count)
				}
				return
			}
			assertLimitError(t, err, "events_per_commit", Phase2Limits.EventsPerCommit)
			if containsAny(err.Error(), "$.events", "payload", "event-1024") {
				t.Fatalf("event limit error leaks input detail: %q", err)
			}
		})
	}
}

func TestNormalizeEventBatchCanonicalizesAndSorts(t *testing.T) {
	batch, err := NormalizeEventBatch(map[string]any{
		"namespace":   "org/example/widget",
		"observed_at": "not-a-date-but-advisory",
		"metadata":    map[string]any{"note": "cafe\u0301"},
		"events": []any{
			eventInput("z", []any{"blue", "blue", "a\u0308"}, []any{"local:a", "local:a"}),
			eventInput("a", []any{"green"}, []any{}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Events[0].LocalID != "a" || batch.Events[1].LocalID != "z" {
		t.Fatalf("event order = %#v", batch.Events)
	}
	if got, want := batch.Events[1].Tags, []string{"blue", "ä"}; !equalStrings(got, want) {
		t.Fatalf("tags = %q, want %q", got, want)
	}
	if got, want := batch.Events[1].CausedBy, []string{"local:a"}; !equalStrings(got, want) {
		t.Fatalf("caused_by = %q, want %q", got, want)
	}
	if got := batch.Metadata["note"]; got != "café" {
		t.Fatalf("metadata note = %#v", got)
	}
}

func TestNormalizeEventBatchRejectsInvalidInputAndSecretHazards(t *testing.T) {
	for name, batch := range map[string]map[string]any{
		"unsupported field":   {"events": []any{eventInput("a", []any{}, []any{})}, "extra": true},
		"duplicate local id":  {"events": []any{eventInput("a", []any{}, []any{}), eventInput("a", []any{}, []any{})}},
		"missing local cause": {"events": []any{eventInput("a", []any{}, []any{"local:gone"})}},
		"self cause":          {"events": []any{eventInput("a", []any{}, []any{"local:a"})}},
		"bad namespace":       {"namespace": "not a namespace", "events": []any{eventInput("a", []any{}, []any{})}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeEventBatch(batch); err == nil {
				t.Fatal("NormalizeEventBatch() error = nil")
			}
		})
	}
	batch := map[string]any{"events": []any{eventInput("a", []any{}, []any{})}}
	batch["events"].([]any)[0].(map[string]any)["payload"] = map[string]any{"api_key": "super-secret-material"}
	if _, err := NormalizeEventBatch(batch); err == nil || !strings.Contains(err.Error(), "$.events[0].payload.api_key: secret-like field value") || strings.Contains(err.Error(), "super-secret-material") {
		t.Fatalf("secret refusal = %v", err)
	}
}

func TestNormalizeEventBatchReportsCredentialBearingURLWithoutValue(t *testing.T) {
	batch := map[string]any{"events": []any{eventInput("a", []any{}, []any{})}}
	batch["events"].([]any)[0].(map[string]any)["payload"] = map[string]any{"endpoint": "https://user:raw-token@example.test/a"}
	if _, err := NormalizeEventBatch(batch); err == nil || !strings.Contains(err.Error(), "$.events[0].payload.endpoint: credential-bearing URL userinfo") || strings.Contains(err.Error(), "raw-token") {
		t.Fatalf("URL secret refusal = %v", err)
	}
}

func TestNormalizeEventBatchScansRawReferencesBeforeValidationWithoutLeakingValues(t *testing.T) {
	for _, field := range []string{"caused_by", "supersedes"} {
		for name, secret := range map[string]string{
			"PEM":   "-----BEGIN PRIVATE KEY-----\nrejected-material\n-----END PRIVATE KEY-----",
			"token": "ghp_abcdefghijklmnopqrstuvwxyz123456",
		} {
			t.Run(field+"/"+name, func(t *testing.T) {
				event := eventInput("a", []any{}, []any{})
				event[field] = []any{secret}
				_, err := NormalizeEventBatch(map[string]any{"events": []any{event}})
				if !errors.Is(err, ErrSecretSafety) {
					t.Fatalf("NormalizeEventBatch() error = %v, want typed secret refusal", err)
				}
				if !strings.Contains(err.Error(), "$.events[0]."+field) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "rejected-material") {
					t.Fatalf("unsafe secret diagnostic = %q", err)
				}
			})
		}
	}
}

func TestNormalizeEventBatchInvalidReferencesReportOnlyPathAndClass(t *testing.T) {
	for _, field := range []string{"caused_by", "supersedes"} {
		event := eventInput("a", []any{}, []any{})
		rejected := "invalid-reference-value"
		event[field] = []any{rejected}
		_, err := NormalizeEventBatch(map[string]any{"events": []any{event}})
		if err == nil || !strings.Contains(err.Error(), "$.events[0]."+field) || strings.Contains(err.Error(), rejected) {
			t.Fatalf("%s error = %v, want value-free path diagnostic", field, err)
		}
	}
}

func TestNormalizeEventBatchSecretEnvironmentPlaceholdersMatchOracle(t *testing.T) {
	for _, value := range []string{"_", "A", strings.Repeat("A", 129)} {
		batch := map[string]any{"events": []any{eventInput("a", []any{}, []any{})}}
		batch["events"].([]any)[0].(map[string]any)["payload"] = map[string]any{"api_key": value}
		if _, err := NormalizeEventBatch(batch); err == nil || !strings.Contains(err.Error(), "secret-like field value") {
			t.Fatalf("api_key %q error = %v", value, err)
		}
	}
	for _, value := range []string{"PACT_API_KEY", "$PACT_API_KEY", "${PACT_API_KEY}"} {
		batch := map[string]any{"events": []any{eventInput("a", []any{}, []any{})}}
		batch["events"].([]any)[0].(map[string]any)["payload"] = map[string]any{"api_key": value}
		if _, err := NormalizeEventBatch(batch); err != nil {
			t.Fatalf("api_key placeholder %q error = %v", value, err)
		}
	}
}

func TestNormalizeEventBatchUsesRuneLimits(t *testing.T) {
	batch := eventInput("a", []any{strings.Repeat("界", 128)}, []any{})
	batch["subject"] = strings.Repeat("界", 512)
	if _, err := NormalizeEventBatch(map[string]any{"observed_at": strings.Repeat("界", 64), "correlation_id": strings.Repeat("界", 255), "events": []any{batch}}); err != nil {
		t.Fatalf("rune boundaries error = %v", err)
	}
}

func TestNormalizeEventBatchExactKeyErrorsAreSortedAndStable(t *testing.T) {
	tests := []struct {
		name  string
		event func() map[string]any
		want  string
	}{
		{name: "missing", event: func() map[string]any {
			event := eventInput("a", []any{}, []any{})
			delete(event, "local_id")
			delete(event, "kind")
			return event
		}, want: "$.events[0]: missing required fields: kind, local_id"},
		{name: "extra", event: func() map[string]any {
			event := eventInput("a", []any{}, []any{})
			event["zeta"] = true
			event["alpha"] = true
			return event
		}, want: "$.events[0]: unsupported fields: alpha, zeta"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for iteration := range 100 {
				_, err := NormalizeEventBatch(map[string]any{"events": []any{test.event()}})
				if err == nil || err.Error() != test.want {
					t.Fatalf("iteration %d error = %v, want %q", iteration, err, test.want)
				}
			}
		})
	}
}

func eventInput(localID string, tags, causedBy []any) map[string]any {
	return map[string]any{
		"local_id": localID, "kind": "observation", "type": "widget.seen", "subject": "widget-1",
		"schema_ref": "pact:core/widget/v1", "payload": map[string]any{"count": 1}, "evidence": []any{},
		"caused_by": causedBy, "supersedes": []any{}, "tags": tags,
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
