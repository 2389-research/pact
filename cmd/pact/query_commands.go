// ABOUTME: Adapts fixed log and query filters to canonical index manager requests.
// ABOUTME: Renders causal results without payload, evidence, cursor internals, or SQL details.
package main

import (
	"context"
	"flag"
	"io"
	"strconv"
	"strings"

	"pact/internal/index"
	"pact/internal/store"
)

func runLog(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("log", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	limit := flags.Int("limit", 0, "maximum results")
	cursor := flags.String("cursor", "", "continuation cursor")
	var namespaces, actors repeatFlag
	flags.Var(&namespaces, "namespace", "namespace prefix")
	flags.Var(&actors, "actor", "actor key ID")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid log arguments"}
	}
	if flags.NArg() != 0 {
		return nil, &commandError{code: exitUsage, message: "log accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	page, err := index.New(st).Log(context.Background(), index.LogRequest{
		Namespace: namespaces, Actor: actors, Limit: *limit, Cursor: *cursor,
	})
	if err != nil {
		return nil, queryCommandError(err)
	}
	return queryPageMap(page), nil
}

func runQuery(args []string, stderr io.Writer) (map[string]any, error) { //nolint:funlen // Keeping the fixed flag-to-filter map together makes the CLI contract auditable.
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	limit := flags.Int("limit", 0, "maximum results")
	cursor := flags.String("cursor", "", "continuation cursor")
	filters := index.Filters{}
	flags.Var((*repeatFlag)(&filters.Namespace), "namespace", "namespace prefix")
	flags.Var((*repeatFlag)(&filters.Type), "type", "event type")
	flags.Var((*repeatFlag)(&filters.Kind), "kind", "event kind")
	flags.Var((*repeatFlag)(&filters.Subject), "subject", "event subject")
	flags.Var((*repeatFlag)(&filters.Actor), "actor", "actor key ID")
	flags.Var((*repeatFlag)(&filters.Tag), "tag", "event tag")
	flags.Var((*repeatFlag)(&filters.SchemaRef), "schema-ref", "schema reference")
	flags.Var((*repeatFlag)(&filters.EventRef), "event-ref", "event reference")
	flags.Var((*repeatFlag)(&filters.CausedBy), "caused-by", "causal event reference")
	flags.Var((*repeatFlag)(&filters.Supersedes), "supersedes", "superseded event reference")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid query arguments"}
	}
	if flags.NArg() != 0 {
		return nil, &commandError{code: exitUsage, message: "query accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	page, err := index.New(st).Query(context.Background(), index.QueryRequest{Filters: filters, Limit: *limit, Cursor: *cursor})
	if err != nil {
		return nil, queryCommandError(err)
	}
	return queryPageMap(page), nil
}

func queryPageMap(page index.QueryPage) map[string]any {
	return map[string]any{
		"operation": page.Operation, "index": indexInfoMap(page.Index),
		"replica": replicaInfoMap(page.Replica), "filters": page.Filters,
		"order": page.Order, "batches": page.Batches,
		"unresolved": page.Unresolved, "page": page.Page,
	}
}

func emitQueryHuman(writer io.Writer, result map[string]any) {
	info, infoOK := result["index"].(map[string]any)
	replica, replicaOK := result["replica"].(map[string]any)
	filters, filtersOK := result["filters"].(index.Filters)
	batches, batchesOK := result["batches"].([]index.Batch)
	unresolved, unresolvedOK := result["unresolved"].([]index.EventItem)
	page, pageOK := result["page"].(index.PageInfo)
	if !infoOK || !replicaOK || !filtersOK || !batchesOK || !unresolvedOK || !pageOK {
		return
	}
	fprintf(writer, "PACT %s\n", result["operation"])
	fprintf(writer, "index state: %v\ncoverage: %v\n", info["state"], info["coverage"])
	fprintf(writer, "local replica completeness: %v (global completeness: %v)\n", replica["completeness"], replica["global_completeness"])
	emitFiltersHuman(writer, filters)
	fprintf(writer, "causal batches use known local dependencies; this is not a total order\n")
	for _, batch := range batches {
		fprintf(writer, "causal batch %d (complete in page: %v)\n", batch.Batch, batch.CompleteInPage)
		for _, item := range batch.Items {
			emitEventHuman(writer, item)
		}
	}
	fprintf(writer, "unresolved events (separate from ordered causal batches):\n")
	for _, item := range unresolved {
		emitEventHuman(writer, item)
	}
	if page.NextCursor != nil {
		fprintf(writer, "continuation: %s\n", *page.NextCursor)
	}
}

func emitEventHuman(writer io.Writer, item index.EventItem) {
	fprintf(writer, "  %s  %s  %s\n", item.EventRef, item.Type, strconv.Quote(item.Subject))
	fprintf(writer, "    observed_at (advisory): %s\n", item.ObservedAt)
}

func emitFiltersHuman(writer io.Writer, filters index.Filters) {
	families := []struct {
		name   string
		values []string
	}{
		{name: "namespace", values: filters.Namespace}, {name: "type", values: filters.Type},
		{name: "kind", values: filters.Kind}, {name: "subject", values: filters.Subject},
		{name: "actor", values: filters.Actor}, {name: "tag", values: filters.Tag},
		{name: "schema_ref", values: filters.SchemaRef}, {name: "event_ref", values: filters.EventRef},
		{name: "caused_by", values: filters.CausedBy}, {name: "supersedes", values: filters.Supersedes},
	}
	wrote := false
	for _, family := range families {
		if len(family.values) == 0 {
			continue
		}
		quoted := make([]string, len(family.values))
		for position, value := range family.values {
			quoted[position] = strconv.Quote(value)
		}
		if !wrote {
			fprintf(writer, "applied filters:\n")
			wrote = true
		}
		fprintf(writer, "  %s: %s\n", family.name, strings.Join(quoted, ", "))
	}
	if !wrote {
		fprintf(writer, "applied filters: none\n")
	}
}
