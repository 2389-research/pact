// ABOUTME: Normalizes bounded fixed-field requests and builds parameterized causal selections.
// ABOUTME: Keeps caller data out of SQL text and preserves every filter family for output.
package index

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"pact/internal/ledger"
)

const defaultPageLimit = 100

// Filters contains every fixed log and query filter family.
type Filters struct {
	Namespace  []string `json:"namespace"`
	Type       []string `json:"type"`
	Kind       []string `json:"kind"`
	Subject    []string `json:"subject"`
	Actor      []string `json:"actor"`
	Tag        []string `json:"tag"`
	SchemaRef  []string `json:"schema_ref"`
	EventRef   []string `json:"event_ref"`
	CausedBy   []string `json:"caused_by"`
	Supersedes []string `json:"supersedes"`
}

// LogRequest selects the compact event view through its two allowed filter families.
type LogRequest struct {
	Namespace []string
	Actor     []string
	Limit     int
	Cursor    string
}

// QueryRequest selects the structured event view and requires at least one filter.
type QueryRequest struct {
	Filters Filters
	Limit   int
	Cursor  string
}

// UsageError marks a safe request-validation failure.
type UsageError struct{ message string }

func (err *UsageError) Error() string {
	if err == nil || err.message == "" {
		return "invalid query request"
	}
	return err.message
}

type filterFamily struct {
	name      string
	values    func(*Filters) *[]string
	normalize func(string) (string, error)
	clause    func(int) string
}

var filterRegistry = []filterFamily{
	{name: "namespace", values: func(filters *Filters) *[]string { return &filters.Namespace }, normalize: ledger.NormalizeNamespace, clause: namespaceClause},
	{name: "type", values: func(filters *Filters) *[]string { return &filters.Type }, normalize: ledger.NormalizeEventType, clause: func(count int) string { return "events.event_type IN (" + placeholders(count) + ")" }},
	{name: "kind", values: func(filters *Filters) *[]string { return &filters.Kind }, normalize: ledger.NormalizeEventKind, clause: func(count int) string { return "events.kind IN (" + placeholders(count) + ")" }},
	{name: "subject", values: func(filters *Filters) *[]string { return &filters.Subject }, normalize: ledger.NormalizeSubject, clause: func(count int) string { return "events.subject IN (" + placeholders(count) + ")" }},
	{name: "actor", values: func(filters *Filters) *[]string { return &filters.Actor }, normalize: ledger.NormalizeActorKeyID, clause: func(count int) string { return "objects.actor_key_id IN (" + placeholders(count) + ")" }},
	{name: "tag", values: func(filters *Filters) *[]string { return &filters.Tag }, normalize: ledger.NormalizeTag, clause: func(count int) string {
		return "EXISTS (SELECT 1 FROM event_tags WHERE event_tags.event_ref = events.event_ref AND event_tags.tag IN (" + placeholders(count) + "))"
	}},
	{name: "schema_ref", values: func(filters *Filters) *[]string { return &filters.SchemaRef }, normalize: ledger.NormalizeSchemaRef, clause: func(count int) string { return "events.schema_ref IN (" + placeholders(count) + ")" }},
	{name: "event_ref", values: func(filters *Filters) *[]string { return &filters.EventRef }, normalize: ledger.NormalizeEventRef, clause: func(count int) string { return "events.event_ref IN (" + placeholders(count) + ")" }},
	{name: "caused_by", values: func(filters *Filters) *[]string { return &filters.CausedBy }, normalize: ledger.NormalizeEventRef, clause: eventLinkClause("caused_by")},
	{name: "supersedes", values: func(filters *Filters) *[]string { return &filters.Supersedes }, normalize: ledger.NormalizeEventRef, clause: eventLinkClause("supersedes")},
}

type selectionPosition struct {
	Group string
	Batch *uint64
	Ref   string
}

type selectedRow struct {
	EventRef, CommitID, LocalID, Kind, EventType, Subject, SchemaRef string
	CausalBatch                                                      *uint64
	CausalStatus, Namespace, ActorKeyID, ActorLabel, ObservedAt      string
}

func normalizeLogRequest(ctx context.Context, request LogRequest) (Filters, int, error) {
	return normalizeFilters(ctx, Filters{Namespace: request.Namespace, Actor: request.Actor}, request.Limit, false)
}

func normalizeQueryRequest(ctx context.Context, request QueryRequest) (Filters, int, error) {
	return normalizeFilters(ctx, request.Filters, request.Limit, true)
}

func normalizeFilters(ctx context.Context, filters Filters, requestedLimit int, requireFilter bool) (Filters, int, error) {
	limit, err := normalizePageLimit(requestedLimit)
	if err != nil {
		return Filters{}, 0, err
	}
	total := uint64(0)
	for _, family := range filterRegistry {
		raw := *family.values(&filters)
		normalized := make(map[string]struct{})
		for _, value := range raw {
			if err := ledger.CheckFilterValueSafety(ctx, value); err != nil {
				return Filters{}, 0, err
			}
			value, err = family.normalize(value)
			if err != nil {
				return Filters{}, 0, &UsageError{message: "invalid " + family.name + " filter"}
			}
			if _, found := normalized[value]; found {
				continue
			}
			normalized[value] = struct{}{}
			if uint64(len(normalized)) > ledger.Phase2Limits.FilterValuesPerFamily {
				return Filters{}, 0, &ledger.LimitError{Resource: "filter_values_per_family", Maximum: ledger.Phase2Limits.FilterValuesPerFamily, ObservedAtLeast: ledger.Phase2Limits.FilterValuesPerFamily + 1}
			}
			if total+uint64(len(normalized)) > ledger.Phase2Limits.FilterValuesTotal {
				return Filters{}, 0, &ledger.LimitError{Resource: "filter_values_total", Maximum: ledger.Phase2Limits.FilterValuesTotal, ObservedAtLeast: ledger.Phase2Limits.FilterValuesTotal + 1}
			}
		}
		values := make([]string, 0, len(normalized))
		for value := range normalized {
			values = append(values, value)
		}
		sort.Strings(values)
		total += uint64(len(values))
		*family.values(&filters) = values
	}
	if requireFilter && total == 0 {
		return Filters{}, 0, &UsageError{message: "query requires at least one filter"}
	}
	return filters, limit, nil
}

func normalizePageLimit(requested int) (int, error) {
	if requested == 0 {
		return defaultPageLimit, nil
	}
	if requested < 1 || uint64(requested) > ledger.Phase2Limits.PageResults {
		return 0, &UsageError{message: "query limit must be between 1 and 1000"}
	}
	return requested, nil
}

func buildSelectionQuery(filters Filters, after *selectionPosition, fetch int) (string, []any) {
	const columns = "SELECT events.event_ref,events.commit_id,events.local_id,events.kind,events.event_type,events.subject,events.schema_ref,events.causal_batch,events.causal_status,objects.namespace,objects.actor_key_id,objects.actor_label,objects.observed_at FROM events JOIN objects ON objects.object_id = events.commit_id"
	clauses, arguments := filterPredicates(filters)
	if after != nil {
		if after.Group == "ordered" {
			clauses = append(clauses, "((events.causal_status = 'ordered' AND (events.causal_batch > ? OR (events.causal_batch = ? AND events.event_ref > ?))) OR events.causal_status = 'unresolved')")
			arguments = append(arguments, *after.Batch, *after.Batch, after.Ref)
		} else {
			clauses = append(clauses, "(events.causal_status = 'unresolved' AND events.event_ref > ?)")
			arguments = append(arguments, after.Ref)
		}
	}
	statement := columns
	if len(clauses) != 0 {
		statement += " WHERE " + strings.Join(clauses, " AND ")
	}
	statement += " ORDER BY CASE events.causal_status WHEN 'ordered' THEN 0 ELSE 1 END, events.causal_batch, events.event_ref LIMIT ?"
	arguments = append(arguments, fetch)
	return statement, arguments
}

func filterPredicates(filters Filters) ([]string, []any) {
	clauses := make([]string, 0, len(filterRegistry))
	arguments := make([]any, 0)
	for _, family := range filterRegistry {
		values := *family.values(&filters)
		if len(values) == 0 {
			continue
		}
		clauses = append(clauses, family.clause(len(values)))
		if family.name == "namespace" {
			for _, value := range values {
				arguments = append(arguments, value, value+"/*")
			}
			continue
		}
		for _, value := range values {
			arguments = append(arguments, value)
		}
	}
	return clauses, arguments
}

func namespaceClause(count int) string {
	clauses := make([]string, count)
	for index := range clauses {
		clauses[index] = "(objects.namespace = ? OR objects.namespace GLOB ?)"
	}
	return "(" + strings.Join(clauses, " OR ") + ")"
}

func eventLinkClause(relation string) func(int) string {
	return func(count int) string {
		return "EXISTS (SELECT 1 FROM event_links WHERE event_links.source_ref = events.event_ref AND event_links.relation = '" + relation + "' AND event_links.target_ref IN (" + placeholders(count) + "))"
	}
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func scanSelectedRow(rows *sql.Rows) (selectedRow, error) {
	var row selectedRow
	var batch sql.NullInt64
	err := rows.Scan(
		&row.EventRef, &row.CommitID, &row.LocalID, &row.Kind, &row.EventType, &row.Subject, &row.SchemaRef,
		&batch, &row.CausalStatus, &row.Namespace, &row.ActorKeyID, &row.ActorLabel, &row.ObservedAt,
	)
	if err != nil {
		return selectedRow{}, &QueryError{Code: "index_corrupt"}
	}
	if batch.Valid {
		if batch.Int64 < 0 {
			return selectedRow{}, &QueryError{Code: "index_corrupt"}
		}
		value := uint64(batch.Int64)
		row.CausalBatch = &value
	}
	return row, nil
}
