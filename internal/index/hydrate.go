// ABOUTME: Resolves selected index rows against canonical signed commits under one shared lock.
// ABOUTME: Assembles bounded causal pages without exposing payloads, evidence, or SQLite details.
package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"pact/internal/ledger"
	"pact/internal/store"
)

var (
	beforeQueryHydration   = func() {}
	resolveCanonicalCommit = ledger.ResolveCommit
	queryPageJSONSize      = func(page QueryPage) (uint64, error) {
		raw, err := json.Marshal(page)
		return uint64(len(raw)), err
	}
)

// EventItem is the JSON-ready canonical event projection returned by log and query.
type EventItem struct {
	EventRef     string    `json:"event_ref"`
	CommitID     string    `json:"commit_id"`
	Namespace    string    `json:"namespace"`
	Parents      []string  `json:"parents"`
	ActorKeyID   string    `json:"actor_key_id"`
	ActorLabel   string    `json:"actor_label"`
	ObservedAt   string    `json:"observed_at"`
	CausalBatch  *uint64   `json:"causal_batch"`
	CausalStatus string    `json:"causal_status"`
	Kind         string    `json:"kind"`
	Type         string    `json:"type"`
	Subject      string    `json:"subject"`
	Tags         []string  `json:"tags"`
	LocalID      *string   `json:"local_id,omitempty"`
	SchemaRef    *string   `json:"schema_ref,omitempty"`
	CausedBy     *[]string `json:"caused_by,omitempty"`
	Supersedes   *[]string `json:"supersedes,omitempty"`
}

// Batch groups matching events that share one stored causal batch.
type Batch struct {
	Batch          uint64      `json:"batch"`
	CompleteInPage bool        `json:"complete_in_page"`
	Items          []EventItem `json:"items"`
}

// PageInfo describes one bounded continuation page.
type PageInfo struct {
	Limit      int     `json:"limit"`
	Returned   int     `json:"returned"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

// QueryPage is the fixed JSON-ready result shared by compact log and structured query.
type QueryPage struct {
	Operation  string      `json:"operation"`
	Index      IndexInfo   `json:"index"`
	Replica    ReplicaInfo `json:"replica"`
	Filters    Filters     `json:"filters"`
	Order      OrderInfo   `json:"order"`
	Batches    []Batch     `json:"batches"`
	Unresolved []EventItem `json:"unresolved"`
	Page       PageInfo    `json:"page"`
}

type indexedLink struct {
	Relation, Target string
	Resolved         int64
}

type indexedParent struct {
	ParentID string
	Resolved int64
}

type selectedRelations struct {
	parents map[string][]indexedParent
	tags    map[string][]string
	links   map[string][]indexedLink
}

// Log returns the compact canonical event view in fixed causal order.
func (m *Manager) Log(ctx context.Context, request LogRequest) (QueryPage, error) {
	if err := validateQueryCall(m, ctx); err != nil {
		return QueryPage{}, err
	}
	filters, limit, err := normalizeLogRequest(ctx, request)
	if err != nil {
		return QueryPage{}, err
	}
	return m.queryPage(ctx, "log", filters, limit, request.Cursor)
}

// Query returns the structured canonical event view in fixed causal order.
func (m *Manager) Query(ctx context.Context, request QueryRequest) (QueryPage, error) {
	if err := validateQueryCall(m, ctx); err != nil {
		return QueryPage{}, err
	}
	filters, limit, err := normalizeQueryRequest(ctx, request)
	if err != nil {
		return QueryPage{}, err
	}
	return m.queryPage(ctx, "query", filters, limit, request.Cursor)
}

func validateQueryCall(m *Manager, ctx context.Context) error {
	if m == nil || m.store == nil || ctx == nil {
		return fmt.Errorf("index query requires a store and context")
	}
	return ctx.Err()
}

func (m *Manager) queryPage(ctx context.Context, operation string, filters Filters, limit int, cursor string) (result QueryPage, err error) {
	if m == nil || m.store == nil || ctx == nil {
		return QueryPage{}, fmt.Errorf("index query requires a store and context")
	}
	if err := ctx.Err(); err != nil {
		return QueryPage{}, err
	}
	err = m.store.WithReadLock(func() error {
		var queryErr error
		result, queryErr = m.queryPageLocked(ctx, operation, filters, limit, cursor)
		return queryErr
	})
	return result, err
}

func (m *Manager) queryPageLocked(ctx context.Context, operation string, filters Filters, limit int, token string) (result QueryPage, err error) { //nolint:funlen,gocyclo // The ordered gates keep one source/index snapshot and one handle alive.
	scan, err := m.scanSourceLocked(ctx)
	if err != nil {
		return QueryPage{}, err
	}
	path := filepath.Join(m.store.Root(), ".pact", "index", liveIndexName)
	info, db, err := openValidatedIndex(ctx, path, scan)
	if db != nil {
		defer func() {
			if closeErr := closeIndexReader(db); closeErr != nil && err == nil {
				err = &QueryError{Code: "index_corrupt"}
			}
		}()
	}
	if err != nil {
		return QueryPage{}, err
	}
	if info.State != "current" {
		return QueryPage{}, &QueryError{Code: "index_" + strings.ReplaceAll(info.State, "-", "_")}
	}
	if db == nil || info.SchemaVersion == nil || info.SourceFingerprint == nil || info.LogicalDigest == nil {
		return QueryPage{}, &QueryError{Code: "index_corrupt"}
	}
	digest, err := queryDigest(ctx, operation, filters, limit)
	if err != nil {
		return QueryPage{}, err
	}
	expectation := cursorExpectation{
		SchemaVersion: *info.SchemaVersion, SourceFingerprint: *info.SourceFingerprint,
		LogicalDigest: *info.LogicalDigest, QueryDigest: digest,
	}
	position, hasPosition, err := querySelectionPosition(ctx, db, token, expectation)
	if err != nil {
		return QueryPage{}, err
	}
	var after *selectionPosition
	if hasPosition {
		after = &position
	}
	rows, err := selectIndexRows(ctx, db, filters, after, limit+1)
	if err != nil {
		return QueryPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	beforeQueryHydration()
	items, err := hydrateSelectedRows(ctx, m.store, db, scan, rows, operation == "query")
	if err != nil {
		return QueryPage{}, err
	}
	result = QueryPage{
		Operation: operation, Index: info, Replica: replicaInfo(scan), Filters: cloneFilters(filters), Order: fixedOrder(),
		Batches: []Batch{}, Unresolved: []EventItem{}, Page: PageInfo{Limit: limit, Returned: len(items), HasMore: hasMore},
	}
	if err := assembleCausalGroups(ctx, db, filters, items, &result); err != nil {
		return QueryPage{}, err
	}
	if hasMore {
		last := rows[len(rows)-1]
		state := cursorState{
			AfterGroup: last.CausalStatus, AfterBatch: cloneUint64Pointer(last.CausalBatch), AfterRef: last.EventRef,
			Format: cursorFormat, LogicalDigest: *info.LogicalDigest, QueryDigest: expectation.QueryDigest,
			SchemaVersion: *info.SchemaVersion, SourceFingerprint: *info.SourceFingerprint,
		}
		next, encodeErr := encodeCursor(ctx, state)
		if encodeErr != nil {
			return QueryPage{}, encodeErr
		}
		result.Page.NextCursor = &next
	}
	if err := ctx.Err(); err != nil {
		return QueryPage{}, err
	}
	size, err := queryPageJSONSize(result)
	if err != nil {
		return QueryPage{}, safeIndexReadError("encode query result failed", err)
	}
	if size > ledger.Phase2Limits.JSONResultBytes {
		return QueryPage{}, &ledger.LimitError{Resource: "json_result_bytes", Maximum: ledger.Phase2Limits.JSONResultBytes, ObservedAtLeast: ledger.Phase2Limits.JSONResultBytes + 1}
	}
	if err := ctx.Err(); err != nil {
		return QueryPage{}, err
	}
	return result, nil
}

func querySelectionPosition(ctx context.Context, db *sql.DB, token string, expectation cursorExpectation) (selectionPosition, bool, error) {
	if token == "" {
		return selectionPosition{}, false, nil
	}
	state, err := decodeCursor(ctx, token, expectation)
	if err != nil {
		return selectionPosition{}, false, err
	}
	position := selectionPosition{Group: state.AfterGroup, Batch: cloneUint64Pointer(state.AfterBatch), Ref: state.AfterRef}
	if err := validateCursorPosition(ctx, db, position); err != nil {
		return selectionPosition{}, false, err
	}
	return position, true, nil
}

func selectIndexRows(ctx context.Context, db *sql.DB, filters Filters, after *selectionPosition, fetch int) (result []selectedRow, err error) {
	statement, arguments := buildSelectionQuery(filters, after, fetch)
	rows, err := db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, safeIndexReadError("read indexed events failed", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = safeIndexReadError("close indexed event rows failed", closeErr)
		}
	}()
	result = make([]selectedRow, 0, fetch)
	for rows.Next() {
		row, scanErr := scanSelectedRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, safeIndexReadError("read indexed events failed", err)
	}
	return result, nil
}

func hydrateSelectedRows(ctx context.Context, st *store.Store, db *sql.DB, scan ledger.ScanResult, rows []selectedRow, queryView bool) ([]EventItem, error) {
	unresolved, err := selectedUnresolvedSet(ctx, rows, scan.UnresolvedEvents)
	if err != nil {
		return nil, err
	}
	relations, err := loadSelectedRelations(ctx, db, rows)
	if err != nil {
		return nil, err
	}
	resolved := make(map[string]ledger.CommitRecord)
	items := make([]EventItem, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, found := resolved[row.CommitID]
		if !found {
			commit, err = resolveCanonicalCommit(ctx, st, row.CommitID, ledger.Phase2Limits)
			if err != nil {
				if contextError(err) {
					return nil, err
				}
				return nil, &QueryError{Code: "index_corrupt"}
			}
			resolved[row.CommitID] = commit
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, err := hydrateSelectedRow(row, commit, scan, unresolved, relations, queryView)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, ctx.Err()
}

func hydrateSelectedRow(row selectedRow, resolved ledger.CommitRecord, scan ledger.ScanResult, unresolved map[string]struct{}, relations selectedRelations, queryView bool) (EventItem, error) {
	commit, commitFound := scan.Commits[row.CommitID]
	event, eventFound := scan.Events[row.EventRef]
	if !commitFound || !eventFound || !sameCanonicalCommit(resolved, commit) || event.CommitID != commit.ID || !sameSelectedScalar(row, commit, event) {
		return EventItem{}, &QueryError{Code: "index_corrupt"}
	}
	if !sameIndexedParents(relations.parents[commit.ID], commit.Parents, scan.Commits) || !reflect.DeepEqual(relations.tags[event.Ref], event.Tags) || !sameIndexedLinks(relations.links[event.Ref], event, scan.Events) {
		return EventItem{}, &QueryError{Code: "index_corrupt"}
	}
	batch, ordered := scan.CausalBatches[event.Ref]
	if ordered {
		if row.CausalStatus != "ordered" || row.CausalBatch == nil || *row.CausalBatch != batch {
			return EventItem{}, &QueryError{Code: "index_corrupt"}
		}
	} else if _, found := unresolved[event.Ref]; row.CausalStatus != "unresolved" || row.CausalBatch != nil || !found {
		return EventItem{}, &QueryError{Code: "index_corrupt"}
	}
	item := EventItem{
		EventRef: event.Ref, CommitID: commit.ID, Namespace: commit.Namespace, Parents: cloneStrings(commit.Parents),
		ActorKeyID: commit.ActorID, ActorLabel: commit.ActorLabel, ObservedAt: commit.ObservedAt,
		CausalBatch: cloneUint64Pointer(row.CausalBatch), CausalStatus: row.CausalStatus,
		Kind: event.Kind, Type: event.Type, Subject: event.Subject, Tags: cloneStrings(event.Tags),
	}
	if queryView {
		item.LocalID = new(event.LocalID)
		item.SchemaRef = new(event.SchemaRef)
		causedBy := cloneStrings(event.CausedBy)
		supersedes := cloneStrings(event.Supersedes)
		item.CausedBy = &causedBy
		item.Supersedes = &supersedes
	}
	return item, nil
}

func selectedUnresolvedSet(ctx context.Context, rows []selectedRow, refs []string) (map[string]struct{}, error) {
	needed := false
	for _, row := range rows {
		if row.CausalStatus == "unresolved" {
			needed = true
			break
		}
	}
	if !needed {
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result[ref] = struct{}{}
	}
	return result, ctx.Err()
}

func loadSelectedRelations(ctx context.Context, db *sql.DB, rows []selectedRow) (selectedRelations, error) {
	result := selectedRelations{parents: map[string][]indexedParent{}, tags: map[string][]string{}, links: map[string][]indexedLink{}}
	commitSet := map[string]struct{}{}
	eventSet := map[string]struct{}{}
	for _, row := range rows {
		commitSet[row.CommitID] = struct{}{}
		eventSet[row.EventRef] = struct{}{}
		result.parents[row.CommitID] = []indexedParent{}
		result.tags[row.EventRef] = []string{}
		result.links[row.EventRef] = []indexedLink{}
	}
	commits := sortedSet(commitSet)
	events := sortedSet(eventSet)
	if err := loadParentRows(ctx, db, commits, result.parents); err != nil {
		return selectedRelations{}, err
	}
	if err := loadTagRows(ctx, db, events, result.tags); err != nil {
		return selectedRelations{}, err
	}
	if err := loadLinkRows(ctx, db, events, result.links); err != nil {
		return selectedRelations{}, err
	}
	return result, nil
}

func loadParentRows(ctx context.Context, db *sql.DB, commits []string, destination map[string][]indexedParent) error {
	if len(commits) == 0 {
		return nil
	}
	arguments := stringsToArguments(commits)
	statement := "SELECT child_id,parent_id,resolved FROM parent_edges WHERE child_id IN (" + placeholders(len(commits)) + ") ORDER BY child_id,parent_id"
	return scanRelationRows(ctx, db, statement, arguments, func(rows *sql.Rows) error {
		var child string
		var parent indexedParent
		if err := rows.Scan(&child, &parent.ParentID, &parent.Resolved); err != nil {
			return err
		}
		destination[child] = append(destination[child], parent)
		return nil
	})
}

func loadTagRows(ctx context.Context, db *sql.DB, events []string, destination map[string][]string) error {
	if len(events) == 0 {
		return nil
	}
	arguments := stringsToArguments(events)
	statement := "SELECT event_ref,tag FROM event_tags WHERE event_ref IN (" + placeholders(len(events)) + ") ORDER BY event_ref,tag"
	return scanRelationRows(ctx, db, statement, arguments, func(rows *sql.Rows) error {
		var ref, tag string
		if err := rows.Scan(&ref, &tag); err != nil {
			return err
		}
		destination[ref] = append(destination[ref], tag)
		return nil
	})
}

func loadLinkRows(ctx context.Context, db *sql.DB, events []string, destination map[string][]indexedLink) error {
	if len(events) == 0 {
		return nil
	}
	arguments := stringsToArguments(events)
	statement := "SELECT source_ref,relation,target_ref,resolved FROM event_links WHERE source_ref IN (" + placeholders(len(events)) + ") ORDER BY source_ref,relation,target_ref"
	return scanRelationRows(ctx, db, statement, arguments, func(rows *sql.Rows) error {
		var source string
		var link indexedLink
		if err := rows.Scan(&source, &link.Relation, &link.Target, &link.Resolved); err != nil {
			return err
		}
		destination[source] = append(destination[source], link)
		return nil
	})
}

func scanRelationRows(ctx context.Context, db *sql.DB, statement string, arguments []any, scan func(*sql.Rows) error) (err error) {
	rows, err := db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return safeIndexReadError("read indexed event relations failed", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = safeIndexReadError("close indexed event relation rows failed", closeErr)
		}
	}()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return &QueryError{Code: "index_corrupt"}
		}
	}
	if err := rows.Err(); err != nil {
		return safeIndexReadError("read indexed event relations failed", err)
	}
	return nil
}

func assembleCausalGroups(ctx context.Context, db *sql.DB, filters Filters, items []EventItem, page *QueryPage) error {
	batchIndexes := map[uint64]int{}
	for _, item := range items {
		if item.CausalStatus == "unresolved" {
			page.Unresolved = append(page.Unresolved, item)
			continue
		}
		batch := *item.CausalBatch
		index, found := batchIndexes[batch]
		if !found {
			index = len(page.Batches)
			batchIndexes[batch] = index
			page.Batches = append(page.Batches, Batch{Batch: batch, Items: []EventItem{}})
		}
		page.Batches[index].Items = append(page.Batches[index].Items, item)
	}
	for index := range page.Batches {
		count, err := countMatchingBatch(ctx, db, filters, page.Batches[index].Batch)
		if err != nil {
			return err
		}
		page.Batches[index].CompleteInPage = count == uint64(len(page.Batches[index].Items))
	}
	return nil
}

func countMatchingBatch(ctx context.Context, db *sql.DB, filters Filters, batch uint64) (uint64, error) {
	clauses, arguments := filterPredicates(filters)
	clauses = append(clauses, "events.causal_status='ordered'", "events.causal_batch=?")
	arguments = append(arguments, batch)
	statement := "SELECT count(*) FROM events JOIN objects ON objects.object_id=events.commit_id WHERE " + strings.Join(clauses, " AND ")
	var count int64
	if err := db.QueryRowContext(ctx, statement, arguments...).Scan(&count); err != nil {
		return 0, safeIndexReadError("count matching causal batch failed", err)
	}
	if count < 0 {
		return 0, &QueryError{Code: "index_corrupt"}
	}
	return uint64(count), nil
}

func sameCanonicalCommit(left, right ledger.CommitRecord) bool {
	return left.ID == right.ID && left.Namespace == right.Namespace && left.ActorID == right.ActorID && left.ActorLabel == right.ActorLabel &&
		left.ObservedAt == right.ObservedAt && left.BodyDigest == right.BodyDigest && reflect.DeepEqual(left.Parents, right.Parents) &&
		reflect.DeepEqual(left.EventRefs, right.EventRefs) && left.Integrity == right.Integrity && left.Structure == right.Structure && left.Authenticity == right.Authenticity
}

func sameSelectedScalar(row selectedRow, commit ledger.CommitRecord, event ledger.EventRecord) bool {
	return row.CommitID == commit.ID && row.Namespace == commit.Namespace && row.ActorKeyID == commit.ActorID && row.ActorLabel == commit.ActorLabel &&
		row.ObservedAt == commit.ObservedAt && row.EventRef == event.Ref && row.LocalID == event.LocalID && row.Kind == event.Kind &&
		row.EventType == event.Type && row.Subject == event.Subject && row.SchemaRef == event.SchemaRef
}

func sameIndexedLinks(actual []indexedLink, event ledger.EventRecord, events map[string]ledger.EventRecord) bool {
	expected := make([]indexedLink, 0, len(event.CausedBy)+len(event.Supersedes))
	for _, target := range event.CausedBy {
		expected = append(expected, indexedLink{Relation: "caused_by", Target: target, Resolved: resolvedReference(events, target)})
	}
	for _, target := range event.Supersedes {
		expected = append(expected, indexedLink{Relation: "supersedes", Target: target, Resolved: resolvedReference(events, target)})
	}
	sort.Slice(expected, func(left, right int) bool {
		if expected[left].Relation != expected[right].Relation {
			return expected[left].Relation < expected[right].Relation
		}
		return expected[left].Target < expected[right].Target
	})
	return reflect.DeepEqual(actual, expected)
}

func sameIndexedParents(actual []indexedParent, parents []string, commits map[string]ledger.CommitRecord) bool {
	expected := make([]indexedParent, 0, len(parents))
	for _, parent := range parents {
		expected = append(expected, indexedParent{ParentID: parent, Resolved: resolvedReference(commits, parent)})
	}
	sort.Slice(expected, func(left, right int) bool { return expected[left].ParentID < expected[right].ParentID })
	return reflect.DeepEqual(actual, expected)
}

func resolvedReference[Value any](values map[string]Value, ref string) int64 {
	if _, found := values[ref]; found {
		return 1
	}
	return 0
}

func cloneFilters(filters Filters) Filters {
	return Filters{
		Namespace: cloneStrings(filters.Namespace), Type: cloneStrings(filters.Type), Kind: cloneStrings(filters.Kind),
		Subject: cloneStrings(filters.Subject), Actor: cloneStrings(filters.Actor), Tag: cloneStrings(filters.Tag),
		SchemaRef: cloneStrings(filters.SchemaRef), EventRef: cloneStrings(filters.EventRef), CausedBy: cloneStrings(filters.CausedBy),
		Supersedes: cloneStrings(filters.Supersedes),
	}
}

func cloneStrings(values []string) []string { return append([]string{}, values...) }

func cloneUint64Pointer(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	return new(*value)
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stringsToArguments(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func contextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func safeIndexReadError(message string, err error) error {
	if contextError(err) {
		return err
	}
	return errors.New(message)
}
