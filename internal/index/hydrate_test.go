// ABOUTME: Exercises real canonical hydration, parity, causal pages, and shared-lock behavior.
// ABOUTME: Proves indexed output stays bounded, restart-safe, and free of private ledger material.
package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"pact/internal/ledger"
	"pact/internal/store"
)

func TestHydrationQueriesRealRebuildAcrossRestartAndSameSourceRebuild(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := QueryRequest{Filters: Filters{Namespace: []string{"fixture/projection"}}, Limit: 2}
	first, err := manager.Query(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Operation != "query" || first.Index.State != "current" || first.Replica.Completeness != "incomplete" {
		t.Fatalf("query envelope = %#v", first)
	}
	if first.Page.Limit != 2 || first.Page.Returned != 2 || !first.Page.HasMore || first.Page.NextCursor == nil {
		t.Fatalf("first page = %#v", first.Page)
	}
	firstItems := pageItems(first)
	if got := eventRefs(firstItems); !reflect.DeepEqual(got, []string{fixture.presentParent.EventRefs[0], fixture.child.EventRefs[0]}) {
		t.Fatalf("first page refs = %#v", got)
	}
	if len(first.Batches) != 1 || !first.Batches[0].CompleteInPage || len(first.Unresolved) != 1 {
		t.Fatalf("first causal groups = batches %#v, unresolved %#v", first.Batches, first.Unresolved)
	}
	assertCanonicalItem(t, firstItems[0], fixture.scan, true)
	assertCanonicalItem(t, firstItems[1], fixture.scan, true)

	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondRequest := request
	secondRequest.Cursor = *first.Page.NextCursor
	second, err := New(fixture.store).Query(context.Background(), secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventRefs(pageItems(second)); !reflect.DeepEqual(got, []string{fixture.child.EventRefs[1]}) {
		t.Fatalf("second page refs = %#v", got)
	}
	if second.Page.HasMore || second.Page.NextCursor != nil || second.Page.Returned != 1 || len(second.Batches) != 0 || len(second.Unresolved) != 1 {
		t.Fatalf("second page = %#v, groups %#v/%#v", second.Page, second.Batches, second.Unresolved)
	}

	logPage, err := manager.Log(context.Background(), LogRequest{Namespace: []string{"fixture/projection"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logPage.Operation != "log" || logPage.Filters.Type == nil || logPage.Filters.CausedBy == nil {
		t.Fatalf("log page envelope = %#v", logPage)
	}
	for _, item := range pageItems(logPage) {
		if item.LocalID != nil || item.SchemaRef != nil || item.CausedBy != nil || item.Supersedes != nil {
			t.Fatalf("compact log item contains query-only data: %#v", item)
		}
	}

	first.Filters.Namespace[0] = "mutated"
	firstItems[0].Parents = append(firstItems[0].Parents, "mutated")
	again, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{Namespace: []string{"fixture/projection"}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if again.Filters.Namespace[0] != "fixture/projection" || slicesContain(pageItems(again)[0].Parents, "mutated") {
		t.Fatal("returned slices alias request or canonical scan state")
	}
}

func TestPageSplitCausalBatchIsIncompleteOnBothPages(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	wide := commitFixture(t, fixture.store, fixture.key, "fixture/wide", []string{}, time.Date(2026, 8, 24, 13, 0, 0, 0, time.UTC),
		fixtureEvent("a", "action", nil, nil, nil),
		fixtureEvent("b", "action", nil, nil, nil),
		fixtureEvent("c", "action", nil, nil, nil),
	)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := QueryRequest{Filters: Filters{Namespace: []string{"fixture/wide"}}, Limit: 2}
	first, err := manager.Query(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Batches) != 1 || first.Batches[0].CompleteInPage || first.Page.NextCursor == nil {
		t.Fatalf("first split page = %#v", first)
	}
	request.Cursor = *first.Page.NextCursor
	second, err := manager.Query(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Batches) != 1 || second.Batches[0].CompleteInPage || second.Page.HasMore {
		t.Fatalf("second split page = %#v", second)
	}
	got := append(eventRefs(pageItems(first)), eventRefs(pageItems(second))...)
	if !reflect.DeepEqual(got, wide.EventRefs) {
		t.Fatalf("split refs = %#v, want %#v", got, wide.EventRefs)
	}
}

func TestPageKeepsMissingSupersedesOrderedAndMissingCausedByUnresolved(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{Namespace: []string{"fixture"}}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]string{}
	for _, batch := range page.Batches {
		for _, item := range batch.Items {
			groups[item.EventRef] = "ordered"
		}
	}
	for _, item := range page.Unresolved {
		groups[item.EventRef] = "unresolved"
	}
	if groups[fixture.supersedesCommit.EventRefs[0]] != "ordered" || groups[fixture.child.EventRefs[0]] != "unresolved" {
		t.Fatalf("causal groups = %#v", groups)
	}
}

func TestParityRejectsTamperedIndexedScalarTagLinkAndMissingRow(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, signedFixture, string)
	}{
		{name: "scalar", mutate: func(t *testing.T, fixture signedFixture, path string) {
			mutateSQLiteFixture(t, path, "UPDATE events SET subject=? WHERE event_ref=?", "tampered", fixture.child.EventRefs[0])
		}},
		{name: "tag", mutate: func(t *testing.T, fixture signedFixture, path string) {
			mutateSQLiteFixture(t, path, "DELETE FROM event_tags WHERE event_ref=? AND tag=?", fixture.child.EventRefs[0], "alpha")
		}},
		{name: "link", mutate: func(t *testing.T, fixture signedFixture, path string) {
			mutateSQLiteFixture(t, path, "UPDATE event_links SET target_ref=? WHERE source_ref=? AND relation='caused_by'", fixture.missingSupersedes, fixture.child.EventRefs[0])
		}},
		{name: "row", mutate: func(t *testing.T, fixture signedFixture, path string) {
			mutateSQLiteFixture(t, path, "DELETE FROM events WHERE event_ref=?", fixture.chainCommit.EventRefs[0])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := signedPartialScanFixture(t)
			manager := New(fixture.store)
			if _, err := manager.Rebuild(context.Background()); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, fixture, indexPath(fixture.store))
			_, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{Namespace: []string{"fixture"}}})
			assertQueryErrorCode(t, err, "index_corrupt")
		})
	}
}

func TestCanonicalHydrationRejectsSelectedCommitChangedAfterValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(string) error
	}{
		{name: "removed", change: os.Remove},
		{name: "mutated", change: func(path string) error { return os.WriteFile(path, []byte("{}"), 0o644) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := signedPartialScanFixture(t)
			manager := New(fixture.store)
			if _, err := manager.Rebuild(context.Background()); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(fixture.presentParent.Path)
			if err != nil {
				t.Fatal(err)
			}
			original := beforeQueryHydration
			beforeQueryHydration = func() { _ = test.change(fixture.presentParent.Path) }
			t.Cleanup(func() {
				beforeQueryHydration = original
				_ = os.WriteFile(fixture.presentParent.Path, raw, 0o644)
			})
			_, err = manager.Query(context.Background(), QueryRequest{Filters: Filters{EventRef: []string{fixture.presentParent.EventRefs[0]}}})
			assertQueryErrorCode(t, err, "index_corrupt")
		})
	}
}

func TestHydrationResolvesEachDistinctCommitExactlyOnce(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	original := resolveCanonicalCommit
	counts := map[string]int{}
	resolveCanonicalCommit = func(ctx context.Context, st *store.Store, id string, limits ledger.Limits) (ledger.CommitRecord, error) {
		counts[id]++
		return original(ctx, st, id, limits)
	}
	t.Cleanup(func() { resolveCanonicalCommit = original })
	page, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{Namespace: []string{"fixture"}}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, item := range pageItems(page) {
		want[item.CommitID] = true
	}
	if len(counts) != len(want) {
		t.Fatalf("resolve counts = %#v, want commits %#v", counts, want)
	}
	for id := range want {
		if counts[id] != 1 {
			t.Fatalf("commit %s resolved %d times", id, counts[id])
		}
	}
}

func TestCanonicalWriterWaitsBehindReadLockThroughHydration(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	original := beforeQueryHydration
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	beforeQueryHydration = func() {
		once.Do(func() { close(reached) })
		<-release
	}
	t.Cleanup(func() { beforeQueryHydration = original })
	queryDone := make(chan error, 1)
	go func() {
		_, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{EventRef: []string{fixture.presentParent.EventRefs[0]}}})
		queryDone <- err
	}()
	<-reached
	writerDone := make(chan error, 1)
	go func() {
		_, _, err := fixture.store.PutCanonical(map[string]any{"probe": "writer"})
		writerDone <- err
	}()
	select {
	case err := <-writerDone:
		t.Fatalf("canonical writer completed during hydration: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-queryDone; err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if err := <-writerDone; err != nil {
		t.Fatalf("writer error = %v", err)
	}
}

func TestPageJSONOmitsForbiddenFieldsAndEnforcesEncodedLimit(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{EventRef: []string{fixture.child.EventRefs[0]}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"payload", "evidence", "signature", "public_key", "private", "trust", "canonical_path", "dsn", "sql"} {
		if strings.Contains(strings.ToLower(string(raw)), `"`+forbidden+`"`) {
			t.Fatalf("query JSON contains forbidden field %q: %s", forbidden, raw)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"operation", "index", "replica", "filters", "order", "batches", "unresolved", "page"} {
		if _, found := decoded[key]; !found {
			t.Errorf("query JSON omits top-level key %q: %s", key, raw)
		}
	}
	filterObject, ok := decoded["filters"].(map[string]any)
	if !ok || len(filterObject) != 10 {
		t.Fatalf("query JSON filters = %#v, want ten fixed arrays", decoded["filters"])
	}
	for _, key := range []string{"namespace", "type", "kind", "subject", "actor", "tag", "schema_ref", "event_ref", "caused_by", "supersedes"} {
		if _, found := filterObject[key]; !found {
			t.Errorf("query JSON filters omit %q", key)
		}
	}

	original := queryPageJSONSize
	queryPageJSONSize = func(QueryPage) (uint64, error) { return ledger.Phase2Limits.JSONResultBytes + 1, nil }
	t.Cleanup(func() { queryPageJSONSize = original })
	_, err = manager.Query(context.Background(), QueryRequest{Filters: Filters{EventRef: []string{fixture.child.EventRefs[0]}}})
	var limit *ledger.LimitError
	if !errors.As(err, &limit) || limit.Resource != "json_result_bytes" || limit.Maximum != ledger.Phase2Limits.JSONResultBytes {
		t.Fatalf("encoded page error = %#v", err)
	}
}

func TestQueryRefusesNonCurrentIndexAndPropagatesCancellation(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	_, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{Namespace: []string{"fixture"}}})
	assertQueryErrorCode(t, err, "index_missing")
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.Query(ctx, QueryRequest{Filters: Filters{Namespace: []string{"fixture"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Query() error = %v", err)
	}
}

func TestQueryCloseReaderFailureIsSafeIndexCorruption(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	resetValidationSeams(t)
	unsafe := "close file:/private/index.sqlite?dsn=secret after SELECT payload"
	closeIndexReader = func(db *sql.DB) error { return errors.Join(db.Close(), errors.New(unsafe)) }
	_, err := manager.Query(context.Background(), QueryRequest{Filters: Filters{EventRef: []string{fixture.presentParent.EventRefs[0]}}})
	assertQueryErrorCode(t, err, "index_corrupt")
	if strings.Contains(err.Error(), unsafe) || strings.Contains(strings.ToLower(err.Error()), "select") || strings.Contains(strings.ToLower(err.Error()), "dsn") {
		t.Fatalf("query close error leaked SQLite detail: %q", err)
	}
}

func TestIndexReadErrorsHideDriverDetailsAndPreserveContext(t *testing.T) {
	unsafe := errors.New("SELECT payload FROM events using dsn=file:/private/index.sqlite")
	err := safeIndexReadError("read indexed events failed", unsafe)
	if err.Error() != "read indexed events failed" || strings.Contains(strings.ToLower(err.Error()), "select") || strings.Contains(strings.ToLower(err.Error()), "dsn") {
		t.Fatalf("safe index read error = %q", err)
	}
	if err := safeIndexReadError("read indexed events failed", context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v, want context canceled", err)
	}
}

func pageItems(page QueryPage) []EventItem {
	items := make([]EventItem, 0, page.Page.Returned)
	for _, batch := range page.Batches {
		items = append(items, batch.Items...)
	}
	return append(items, page.Unresolved...)
}

func eventRefs(items []EventItem) []string {
	refs := make([]string, len(items))
	for index, item := range items {
		refs[index] = item.EventRef
	}
	return refs
}

func assertCanonicalItem(t *testing.T, item EventItem, scan ledger.ScanResult, queryView bool) {
	t.Helper()
	event := scan.Events[item.EventRef]
	commit := scan.Commits[item.CommitID]
	if item.Namespace != event.Namespace || item.ActorKeyID != commit.ActorID || item.ActorLabel != commit.ActorLabel || item.ObservedAt != commit.ObservedAt || !reflect.DeepEqual(item.Parents, commit.Parents) || item.Kind != event.Kind || item.Type != event.Type || item.Subject != event.Subject || !reflect.DeepEqual(item.Tags, event.Tags) {
		t.Fatalf("item does not match canonical scan: %#v, event %#v, commit %#v", item, event, commit)
	}
	if queryView && (item.LocalID == nil || *item.LocalID != event.LocalID || item.SchemaRef == nil || *item.SchemaRef != event.SchemaRef || item.CausedBy == nil || !reflect.DeepEqual(*item.CausedBy, event.CausedBy) || item.Supersedes == nil || !reflect.DeepEqual(*item.Supersedes, event.Supersedes)) {
		t.Fatalf("query-only item does not match canonical event: %#v, event %#v", item, event)
	}
}

func slicesContain(values []string, want string) bool {
	return slices.Contains(values, want)
}
