// ABOUTME: Exercises fixed query filters, normalization, bounds, and parameterized SQL selection.
// ABOUTME: Keeps filter semantics independent from cursor and canonical hydration coverage.
package index

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pact/internal/ledger"
)

func TestFilterNormalizationSortsDeduplicatesAndReturnsEveryFamily(t *testing.T) {
	keyID := "ed25519:sha256:" + strings.Repeat("a", 64)
	eventRef := "pact:event:sha256:" + strings.Repeat("b", 64) + "#event"
	filters, limit, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: Filters{
		Namespace:  []string{"org/2389/z", "org/2389/a", "org/2389/z"},
		Type:       []string{"build.z", "build.a", "build.z"},
		Kind:       []string{"decision", "action", "decision"},
		Subject:    []string{"Cafe\u0301", "Caf\u00e9", "alpha"},
		Actor:      []string{keyID, keyID},
		Tag:        []string{"re\u0301sume\u0301", "r\u00e9sum\u00e9", "alpha"},
		SchemaRef:  []string{"pact:core/z/v1", "sha256:" + strings.Repeat("c", 64)},
		EventRef:   []string{eventRef, eventRef},
		CausedBy:   []string{eventRef},
		Supersedes: []string{eventRef},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if limit != 100 {
		t.Fatalf("limit = %d, want default 100", limit)
	}
	want := Filters{
		Namespace: []string{"org/2389/a", "org/2389/z"},
		Type:      []string{"build.a", "build.z"},
		Kind:      []string{"action", "decision"},
		Subject:   []string{"Caf\u00e9", "alpha"},
		Actor:     []string{keyID},
		Tag:       []string{"alpha", "r\u00e9sum\u00e9"},
		SchemaRef: []string{"pact:core/z/v1", "sha256:" + strings.Repeat("c", 64)},
		EventRef:  []string{eventRef}, CausedBy: []string{eventRef}, Supersedes: []string{eventRef},
	}
	if !reflect.DeepEqual(filters, want) {
		t.Fatalf("filters = %#v, want %#v", filters, want)
	}
	if filters.Namespace == nil || filters.Type == nil || filters.Kind == nil || filters.Subject == nil || filters.Actor == nil || filters.Tag == nil || filters.SchemaRef == nil || filters.EventRef == nil || filters.CausedBy == nil || filters.Supersedes == nil {
		t.Fatal("normalized filters contain a nil family")
	}

	logFilters, logLimit, err := normalizeLogRequest(context.Background(), LogRequest{Namespace: []string{"org/z", "org/a"}, Actor: []string{keyID}, Limit: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if logLimit != 1_000 || !reflect.DeepEqual(logFilters.Namespace, []string{"org/a", "org/z"}) || !reflect.DeepEqual(logFilters.Actor, []string{keyID}) {
		t.Fatalf("normalized log = (%#v, %d)", logFilters, logLimit)
	}
	if logFilters.Type == nil || logFilters.Kind == nil || logFilters.Subject == nil || logFilters.Tag == nil || logFilters.SchemaRef == nil || logFilters.EventRef == nil || logFilters.CausedBy == nil || logFilters.Supersedes == nil {
		t.Fatal("log filters omit empty query-only families")
	}
}

func TestFilterValidationUsesLedgerDomainGrammar(t *testing.T) {
	tests := []struct {
		name    string
		filters Filters
	}{
		{name: "namespace", filters: Filters{Namespace: []string{"org/*"}}},
		{name: "type", filters: Filters{Type: []string{"Build.Upper"}}},
		{name: "kind", filters: Filters{Kind: []string{"unknown"}}},
		{name: "subject", filters: Filters{Subject: []string{""}}},
		{name: "actor", filters: Filters{Actor: []string{"sha256:" + strings.Repeat("a", 64)}}},
		{name: "tag", filters: Filters{Tag: []string{""}}},
		{name: "schema", filters: Filters{SchemaRef: []string{"pact:core/no-version"}}},
		{name: "event ref", filters: Filters{EventRef: []string{"sha256:" + strings.Repeat("a", 64)}}},
		{name: "caused by", filters: Filters{CausedBy: []string{"local:event"}}},
		{name: "supersedes", filters: Filters{Supersedes: []string{"pact:event:bad#event"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: test.filters})
			var usage *UsageError
			raw := firstFilterValue(test.filters)
			if !errors.As(err, &usage) || raw != "" && strings.Contains(err.Error(), raw) {
				t.Fatalf("error = %v, want safe typed usage error", err)
			}
		})
	}
}

func TestFilterSecretHazardRunsBeforeGrammarAndNeverEchoesRawValue(t *testing.T) {
	secret := "Bearer abcdefghijklmnopqrstuvwxyz123456"
	_, _, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: Filters{Actor: []string{secret}}})
	if !errors.Is(err, ledger.ErrSecretSafety) {
		t.Fatalf("error = %v, want ledger secret-safety refusal before actor grammar", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret error echoed raw input: %q", err)
	}
}

func TestFilterAndPageLimitsAllowExactBoundsAndRejectFirstExcess(t *testing.T) {
	types := make([]string, 64)
	for index := range types {
		types[index] = "fixture.type." + leftPadDecimal(index)
	}
	if _, limit, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: Filters{Type: append(append([]string(nil), types...), types[0])}, Limit: 1_000}); err != nil || limit != 1_000 {
		t.Fatalf("exact bounds = limit %d, error %v", limit, err)
	}
	_, _, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: Filters{Type: append(append([]string(nil), types...), "fixture.type.overflow")}})
	assertFilterLimitError(t, err, "filter_values_per_family", 64)

	filters := Filters{
		Type:      append([]string(nil), types...),
		Subject:   indexedStrings("subject", 64),
		Tag:       indexedStrings("tag", 64),
		Namespace: indexedStrings("org/fixture", 64),
	}
	if _, _, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: filters}); err != nil {
		t.Fatalf("256 normalized values rejected: %v", err)
	}
	filters.Kind = []string{"action"}
	_, _, err = normalizeQueryRequest(context.Background(), QueryRequest{Filters: filters})
	assertFilterLimitError(t, err, "filter_values_total", 256)

	for _, limit := range []int{-1, 1_001} {
		_, _, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: Filters{Kind: []string{"action"}}, Limit: limit})
		var usage *UsageError
		if !errors.As(err, &usage) {
			t.Fatalf("limit %d error = %v, want UsageError", limit, err)
		}
	}
	if _, _, err := normalizeQueryRequest(context.Background(), QueryRequest{}); err == nil {
		t.Fatal("filterless query succeeded")
	}
}

func TestFilterLimitStopsAtFirstUniqueExcess(t *testing.T) {
	values := indexedStrings("fixture.type", 65)
	values = append(values, "Bearer abcdefghijklmnopqrstuvwxyz123456")
	_, _, err := normalizeQueryRequest(context.Background(), QueryRequest{Filters: Filters{Type: values}})
	assertFilterLimitError(t, err, "filter_values_per_family", 64)
}

func TestQueryValidatesManagerAndContextBeforeNormalization(t *testing.T) {
	//nolint:staticcheck // A nil context is the malformed public input under test.
	_, err := New(nil).Query(nil, QueryRequest{Filters: Filters{Kind: []string{"action"}}})
	if err == nil {
		t.Fatal("Query() with nil store and context succeeded")
	}
}

func TestPredicateSQLUsesFixedClausesParametersAndCausalOrder(t *testing.T) {
	injection := "x' OR 1=1 --"
	keyID := "ed25519:sha256:" + strings.Repeat("a", 64)
	eventRef := "pact:event:sha256:" + strings.Repeat("b", 64) + "#event"
	filters := Filters{
		Namespace: []string{"org/2389"}, Type: []string{"build.a", "build.b"}, Kind: []string{"action"}, Subject: []string{injection},
		Actor: []string{keyID}, Tag: []string{injection}, SchemaRef: []string{"pact:core/fixture/v1"}, EventRef: []string{eventRef},
		CausedBy: []string{eventRef}, Supersedes: []string{eventRef},
	}
	statement, arguments := buildSelectionQuery(filters, nil, 11)
	for _, forbidden := range []string{injection, keyID, eventRef, "observed_at ASC", "observed_at DESC"} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("selection SQL contains caller data or forbidden order %q: %s", forbidden, statement)
		}
	}
	for _, clause := range []string{
		"(objects.namespace = ? OR objects.namespace GLOB ?)",
		"events.event_type IN (?,?)", "events.kind IN (?)", "events.subject IN (?)", "objects.actor_key_id IN (?)",
		"EXISTS (SELECT 1 FROM event_tags", "EXISTS (SELECT 1 FROM event_links",
		"ORDER BY CASE events.causal_status WHEN 'ordered' THEN 0 ELSE 1 END, events.causal_batch, events.event_ref LIMIT ?",
	} {
		if !strings.Contains(statement, clause) {
			t.Errorf("selection SQL missing fixed clause %q: %s", clause, statement)
		}
	}
	if !containsArgument(arguments, injection) || !containsArgument(arguments, "org/2389/*") || arguments[len(arguments)-1] != 11 {
		t.Fatalf("selection arguments = %#v", arguments)
	}
}

func TestPredicateFamiliesUseORWithinAndANDAcrossAndRespectNamespaceBoundary(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	path := indexPath(fixture.store)
	writeSnapshotFixture(t, path, Project(fixture.scan))
	db, err := openIndexReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keyID := fixture.key.KeyID
	tests := []struct {
		name    string
		filters Filters
		want    []string
	}{
		{name: "or within", filters: Filters{Kind: []string{"action", "decision"}}, want: []string{fixture.child.EventRefs[0], fixture.child.EventRefs[1], fixture.supersedesCommit.EventRefs[0]}},
		{name: "and across", filters: Filters{Kind: []string{"action", "decision"}, Namespace: []string{"fixture/projection"}, Actor: []string{keyID}}, want: []string{fixture.child.EventRefs[0], fixture.child.EventRefs[1]}},
		{name: "exact namespace", filters: Filters{Namespace: []string{"fixture/projection"}}, want: []string{fixture.presentParent.EventRefs[0], fixture.child.EventRefs[0], fixture.child.EventRefs[1]}},
		{name: "not lexical prefix", filters: Filters{Namespace: []string{"fixture/project"}}, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement, arguments := buildSelectionQuery(test.filters, nil, 1_001)
			rows, queryErr := db.QueryContext(context.Background(), statement, arguments...)
			if queryErr != nil {
				t.Fatal(queryErr)
			}
			var got []string
			for rows.Next() {
				row, scanErr := scanSelectedRow(rows)
				if scanErr != nil {
					rows.Close()
					t.Fatal(scanErr)
				}
				got = append(got, row.EventRef)
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			want := append([]string(nil), test.want...)
			slices.Sort(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("refs = %#v, want %#v", got, want)
			}
		})
	}
}

func TestSelectedRowRejectsNegativeCausalBatchAsIndexCorrupt(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.QueryContext(context.Background(), `SELECT
		'ref','commit','local','action','fixture.type','subject','pact:core/fixture/v1',-1,'ordered',
		'fixture','ed25519:sha256:key','actor','2026-08-24T00:00:00Z'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("selected row missing: %v", rows.Err())
	}
	_, err = scanSelectedRow(rows)
	var queryErr *QueryError
	if !errors.As(err, &queryErr) || queryErr.Code != "index_corrupt" {
		t.Fatalf("error = %#v, want typed index_corrupt", err)
	}
}

func firstFilterValue(filters Filters) string {
	for _, family := range [][]string{filters.Namespace, filters.Type, filters.Kind, filters.Subject, filters.Actor, filters.Tag, filters.SchemaRef, filters.EventRef, filters.CausedBy, filters.Supersedes} {
		if len(family) != 0 {
			return family[0]
		}
	}
	return ""
}

func assertFilterLimitError(t *testing.T, err error, resource string, maximum uint64) {
	t.Helper()
	var limit *ledger.LimitError
	if !errors.As(err, &limit) || limit.Resource != resource || limit.Maximum != maximum || limit.ObservedAtLeast != maximum+1 {
		t.Fatalf("error = %#v, want %s limit %d", err, resource, maximum)
	}
}

func indexedStrings(prefix string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = prefix + "." + leftPadDecimal(index)
	}
	return values
}

func containsArgument(arguments []any, want string) bool {
	for _, argument := range arguments {
		if argument == want {
			return true
		}
	}
	return false
}

func leftPadDecimal(value int) string {
	if value < 10 {
		return "00" + string(rune('0'+value))
	}
	return "0" + string(rune('0'+value/10)) + string(rune('0'+value%10))
}
