// ABOUTME: Tests strict, normalized PACT semantic event-batch admission data.
// ABOUTME: Covers immutable-secret safety without ever asserting raw secret bytes.
package ledger

import (
	"strings"
	"testing"
)

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
