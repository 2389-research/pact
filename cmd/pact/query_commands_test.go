// ABOUTME: Exercises log and query CLI adapters against signed canonical objects and real SQLite.
// ABOUTME: Verifies fixed filters, compact versus structured fields, cursors, and causal wording.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pact/internal/index"
	"pact/internal/ledger"
)

func TestRunLogAndQueryRejectInvalidShapes(t *testing.T) {
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	for _, args := range [][]string{
		{"log", "--repo", repo, "extra", "--json"},
		{"log", "--repo", repo, "--type", "widget.seen", "--json"},
		{"query", "--repo", repo, "--json"},
		{"query", "--repo", repo, "extra", "--type", "widget.seen", "--json"},
		{"query", "--repo", repo, "--type", "widget.seen", "--limit", "1001", "--json"},
	} {
		runErrorJSON(t, args, exitUsage)
	}
}

func TestQueryExitTableUsesTypedSafeErrors(t *testing.T) {
	tests := []struct {
		name, detailCode string
		err              error
		want             int
	}{
		{name: "usage", err: &index.UsageError{}, want: exitUsage},
		{name: "invalid cursor", err: &index.QueryError{Code: "cursor_invalid"}, detailCode: "cursor_invalid", want: exitUsage},
		{name: "query mismatch", err: &index.QueryError{Code: "cursor_query_mismatch"}, detailCode: "cursor_query_mismatch", want: exitUsage},
		{name: "source invalid", err: &index.QueryError{Code: "source_invalid"}, detailCode: "source_invalid", want: exitIntegrity},
		{name: "index missing", err: &index.QueryError{Code: "index_missing"}, detailCode: "index_missing", want: exitMissingDependency},
		{name: "index stale", err: &index.QueryError{Code: "index_stale"}, detailCode: "index_stale", want: exitMissingDependency},
		{name: "index corrupt", err: &index.QueryError{Code: "index_corrupt"}, detailCode: "index_corrupt", want: exitMissingDependency},
		{name: "index incompatible", err: &index.QueryError{Code: "index_incompatible"}, detailCode: "index_incompatible", want: exitMissingDependency},
		{name: "index partial build", err: &index.QueryError{Code: "index_partial_build"}, detailCode: "index_partial_build", want: exitMissingDependency},
		{name: "source changed", err: &index.QueryError{Code: "source_changed"}, detailCode: "source_changed", want: exitMissingDependency},
		{name: "stale cursor", err: &index.QueryError{Code: "cursor_stale"}, detailCode: "cursor_stale", want: exitMissingDependency},
		{name: "missing dependency", err: ledger.ErrMissingDependency, want: exitMissingDependency},
		{name: "resource limit", err: &ledger.LimitError{Resource: "events", Maximum: 1, ObservedAtLeast: 2}, detailCode: "resource_limit", want: exitMissingDependency},
		{name: "publication", err: &index.QueryError{Code: "index_publication_failed"}, detailCode: "index_publication_failed", want: exitUnexpectedError},
		{name: "unexpected", err: errors.New("unsafe SQL and DSN detail"), want: exitUnexpectedError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := queryCommandError(fmt.Errorf("unsafe wrapper: %w", test.err))
			var commandErr *commandError
			if !errors.As(mapped, &commandErr) || commandErr.code != test.want {
				t.Fatalf("mapped error = %#v, want exit %d", mapped, test.want)
			}
			if strings.Contains(commandErr.message, "unsafe") || strings.Contains(strings.ToLower(commandErr.message), "sql") || strings.Contains(strings.ToLower(commandErr.message), "dsn") {
				t.Fatalf("mapped error leaks unsafe prose: %#v", commandErr)
			}
			if test.detailCode == "" {
				return
			}
			if commandErr.details == nil || commandErr.details["code"] != test.detailCode {
				t.Fatalf("mapped details = %#v, want code %q", commandErr.details, test.detailCode)
			}
		})
	}
}

func TestRunQueryCarriesAllRepeatedFiltersAndStructuredFields(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	args := []string{"query", "--repo", fixture.repo,
		"--namespace", "org/example", "--namespace", "org/example/widget",
		"--type", "widget.changed", "--kind", "action", "--subject", "widget-2",
		"--actor", fixture.actor, "--tag", "beta", "--schema-ref", "pact:core/widget/v1",
		"--event-ref", fixture.eventRef, "--caused-by", fixture.missingCause,
		"--supersedes", fixture.missingSupersedes, "--limit", "1", "--json"}
	result := runJSON(t, args)
	assertExactKeys(t, result, "operation", "index", "replica", "filters", "order", "batches", "unresolved", "page")
	filters := result["filters"].(map[string]any)
	assertExactKeys(t, filters, "namespace", "type", "kind", "subject", "actor", "tag", "schema_ref", "event_ref", "caused_by", "supersedes")
	if len(filters["namespace"].([]any)) != 2 || filters["actor"].([]any)[0] != fixture.actor {
		t.Fatalf("query filters = %#v", filters)
	}
	unresolved := result["unresolved"].([]any)
	if len(unresolved) != 1 {
		t.Fatalf("query unresolved = %#v", unresolved)
	}
	item := unresolved[0].(map[string]any)
	for _, field := range []string{"local_id", "schema_ref", "caused_by", "supersedes"} {
		if _, found := item[field]; !found {
			t.Fatalf("query item lacks %q: %#v", field, item)
		}
	}
}

func TestRunLogIsCompactAndSupportsCursorContinuation(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	first := runJSON(t, []string{"log", "--repo", fixture.repo, "--namespace", "org/example", "--actor", fixture.actor, "--limit", "1", "--json"})
	assertExactKeys(t, first, "operation", "index", "replica", "filters", "order", "batches", "unresolved", "page")
	page := first["page"].(map[string]any)
	if page["returned"] != float64(1) || page["has_more"] != true || page["next_cursor"] == nil {
		t.Fatalf("first log page = %#v", page)
	}
	item := first["batches"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	for _, field := range []string{"local_id", "schema_ref", "caused_by", "supersedes", "payload", "evidence"} {
		if _, found := item[field]; found {
			t.Fatalf("compact log item contains %q: %#v", field, item)
		}
	}
	second := runJSON(t, []string{"log", "--repo", fixture.repo, "--namespace", "org/example", "--actor", fixture.actor, "--limit", "1", "--cursor", page["next_cursor"].(string), "--json"})
	if second["page"].(map[string]any)["returned"] != float64(1) {
		t.Fatalf("second log page = %#v", second)
	}
}

func TestRunQueryEmptyResultHasNoCursor(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	result := runJSON(t, []string{"query", "--repo", fixture.repo, "--subject", "not-present", "--json"})
	page := result["page"].(map[string]any)
	if page["limit"] != float64(100) || page["returned"] != float64(0) || page["has_more"] != false || page["next_cursor"] != nil {
		t.Fatalf("empty query page = %#v", page)
	}
}

func TestRunQueryRejectsSecretLikeFilterWithoutEcho(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	secret := "Bearer abcdefghijklmnopqrstuvwxyz123456"
	result := runErrorJSON(t, []string{"query", "--repo", fixture.repo, "--subject", secret, "--json"}, exitUsage)
	if strings.Contains(result["error"].(string), secret) {
		t.Fatalf("query error echoed secret-like filter: %#v", result)
	}
}

func TestRunQueryMismatchCursorIsSafeUsageError(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	first := runJSON(t, []string{"log", "--repo", fixture.repo, "--limit", "1", "--json"})
	token := first["page"].(map[string]any)["next_cursor"].(string)
	result := runErrorJSON(t, []string{"log", "--repo", fixture.repo, "--actor", fixture.actor, "--limit", "1", "--cursor", token, "--json"}, exitUsage)
	if result["details"].(map[string]any)["code"] != "cursor_query_mismatch" || strings.Contains(result["error"].(string), token) {
		t.Fatalf("query-mismatch cursor error = %#v", result)
	}
}

func TestRunLogAndQueryHumanOutputStatesCausalLimits(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	for _, args := range [][]string{
		{"log", "--repo", fixture.repo},
		{"query", "--repo", fixture.repo, "--subject", "widget-2"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%q) exit = %d, stderr=%q", args, code, stderr.String())
		}
		lower := strings.ToLower(stdout.String())
		for _, required := range []string{
			"index state", "coverage", "local replica completeness", "global completeness", "applied filters",
			"causal batch", "known local dependencies", "not a total order", "advisory", "unresolved",
		} {
			if !strings.Contains(lower, required) {
				t.Fatalf("human output %q lacks %q", stdout.String(), required)
			}
		}
		if args[0] == "log" && !strings.Contains(lower, "applied filters: none") {
			t.Fatalf("unfiltered log output = %q, want explicit empty filters", stdout.String())
		}
		if args[0] == "query" && !strings.Contains(stdout.String(), `subject: "widget-2"`) {
			t.Fatalf("query output = %q, want normalized subject filter", stdout.String())
		}
	}
}

func TestHumanQueryEscapesSubjectControls(t *testing.T) {
	var output bytes.Buffer
	emitEventHuman(&output, index.EventItem{EventRef: "pact:event:fixture#e1", Type: "fixture.event", Subject: "safe\nforged", ObservedAt: "2026-08-24T12:00:00Z"})
	if strings.Contains(output.String(), "safe\nforged") || !strings.Contains(output.String(), `"safe\nforged"`) {
		t.Fatalf("human event output did not escape controls: %q", output.String())
	}
}

func TestRunQueryMissingIndexAndInvalidCursorUseStableExitCodes(t *testing.T) {
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	missing := runErrorJSON(t, []string{"query", "--repo", repo, "--subject", "widget-1", "--json"}, exitMissingDependency)
	if missing["details"].(map[string]any)["code"] != "index_missing" {
		t.Fatalf("missing index error = %#v", missing)
	}
	fixture := newCLIQueryFixture(t)
	invalid := runErrorJSON(t, []string{"query", "--repo", fixture.repo, "--subject", "widget-1", "--cursor", "raw-secret-cursor", "--json"}, exitUsage)
	if invalid["details"].(map[string]any)["code"] != "cursor_invalid" || strings.Contains(invalid["error"].(string), "raw-secret-cursor") {
		t.Fatalf("invalid cursor error = %#v", invalid)
	}
}

func TestRunVerifyCompletenessPreservesKeysAndReportsRealIndexStatus(t *testing.T) {
	fixture := newCLIQueryFixtureWithoutRebuild(t)
	missing := runJSON(t, []string{"verify", "--repo", fixture.repo, "--json"})
	if missing["index_status"] != "missing" || missing["completeness"] == nil || missing["limits"] == nil || missing["counts"] == nil || missing["objects"] == nil {
		t.Fatalf("missing-index verify = %#v", missing)
	}
	runJSON(t, []string{"index", "rebuild", "--repo", fixture.repo, "--json"})
	current := runJSON(t, []string{"verify", "--repo", fixture.repo, "--json"})
	if current["index_status"] != "current" {
		t.Fatalf("current-index verify = %#v", current["index_status"])
	}
	writeCLIEvent(t, fixture.repo, fixture.keyPath, "e3", "widget-3", "widget.changed", nil, nil)
	stale := runJSON(t, []string{"verify", "--repo", fixture.repo, "--json"})
	if stale["index_status"] != "stale" {
		t.Fatalf("stale-index verify = %#v", stale["index_status"])
	}
}

type cliQueryFixture struct {
	repo, keyPath, actor, eventRef, missingCause, missingSupersedes string
}

func newCLIQueryFixture(t *testing.T) cliQueryFixture {
	t.Helper()
	fixture := newCLIQueryFixtureWithoutRebuild(t)
	runJSON(t, []string{"index", "rebuild", "--repo", fixture.repo, "--json"})
	return fixture
}

func newCLIQueryFixtureWithoutRebuild(t *testing.T) cliQueryFixture {
	t.Helper()
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	key := runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	first := writeCLIEvent(t, repo, keyPath, "e1", "widget-1", "widget.seen", nil, nil)
	missingCause := "pact:event:sha256:" + strings.Repeat("d", 64) + "#gone"
	missingSupersedes := "pact:event:sha256:" + strings.Repeat("e", 64) + "#old"
	second := writeCLIEvent(t, repo, keyPath, "e2", "widget-2", "widget.changed", []string{missingCause}, []string{missingSupersedes})
	_ = first
	return cliQueryFixture{repo: repo, keyPath: keyPath, actor: key["key_id"].(string), eventRef: second, missingCause: missingCause, missingSupersedes: missingSupersedes}
}

func writeCLIEvent(t *testing.T, repo, keyPath, localID, subject, eventType string, causedBy, supersedes []string) string {
	t.Helper()
	causes := jsonStringArray(causedBy)
	superseded := jsonStringArray(supersedes)
	raw := []byte(`{"events":[{"local_id":"` + localID + `","kind":"action","type":"` + eventType + `","subject":"` + subject + `","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":` + causes + `,"supersedes":` + superseded + `,"tags":["beta"]}]}`)
	path := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	commit := runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", path, "--json"})
	return commit["event_refs"].([]any)[0].(string)
}

func jsonStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return `["` + strings.Join(values, `","`) + `"]`
}
