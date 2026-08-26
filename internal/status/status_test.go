// ABOUTME: Exercises the operator status model over real canonical stores and disposable indexes.
// ABOUTME: Proves healthy, attention, broken, and cancelled inspection without repair side effects.
//
//nolint:misspell // The approved task header uses British spelling.
package status

import (
	"context"
	"testing"
	"time"

	"pact/internal/index"
	"pact/internal/store"
)

func TestInspectClassifiesMissingAndCurrentIndex(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	attention, err := Inspect(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if attention.Health != HealthAttention || attention.Index == nil || attention.Index.Index.State != "missing" {
		t.Fatalf("missing-index status = %#v", attention)
	}
	if attention.NextAction == nil || attention.NextAction.Command != "pact index rebuild" {
		t.Fatalf("missing-index action = %#v", attention.NextAction)
	}

	if _, err := index.New(st).Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	healthy, err := Inspect(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if healthy.Health != HealthHealthy || healthy.Index == nil || healthy.Index.Index.State != "current" || healthy.NextAction != nil {
		t.Fatalf("current-index status = %#v", healthy)
	}
	if healthy.DefaultNamespace != "org/example/widget" || healthy.Verification.Strict != true || healthy.Verification.OK != true {
		t.Fatalf("healthy verification = %#v", healthy)
	}
}

func TestInspectPreservesCancellation(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Inspect(ctx, st); err != context.Canceled { //nolint:errorlint // The status contract preserves the exact context sentinel.
		t.Fatalf("Inspect() error = %v, want context.Canceled", err)
	}
}

func TestInspectChecksCancellationBeforeMetadata(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLocalJSON("format.json", map[string]any{"format": "pact/unknown/v1"}, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Inspect(ctx, st); err != context.Canceled { //nolint:errorlint // The status contract preserves the exact context sentinel.
		t.Fatalf("Inspect() error = %v, want context.Canceled", err)
	}
}

func TestInspectStopsBeforeIndexWhenCanonicalVerificationFails(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"format": "pact/unknown/v1"}); err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if result.Health != HealthBroken || result.Index != nil || result.NextAction != nil {
		t.Fatalf("broken status = %#v", result)
	}
}
