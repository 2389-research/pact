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

func runLog(args []string, stderr io.Writer) (index.QueryPage, error) {
	flags := flag.NewFlagSet("log", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	limit := flags.Int("limit", 0, "maximum results")
	cursor := flags.String("cursor", "", "continuation cursor")
	var namespaces, actors repeatFlag
	flags.Var(&namespaces, "namespace", "namespace prefix")
	flags.Var(&actors, "actor", "actor key ID")
	if err := flags.Parse(args); err != nil {
		return index.QueryPage{}, &commandError{code: exitUsage, message: "invalid log arguments"}
	}
	if flags.NArg() != 0 {
		return index.QueryPage{}, &commandError{code: exitUsage, message: "log accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return index.QueryPage{}, &commandError{code: exitStore, message: err.Error()}
	}
	page, err := index.New(st).Log(context.Background(), index.LogRequest{
		Namespace: namespaces, Actor: actors, Limit: *limit, Cursor: *cursor,
	})
	if err != nil {
		return index.QueryPage{}, queryCommandError(err)
	}
	return page, nil
}

func runQuery(args []string, stderr io.Writer) (index.QueryPage, error) { //nolint:funlen // Keeping the fixed flag-to-filter map together makes the CLI contract auditable.
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
		return index.QueryPage{}, &commandError{code: exitUsage, message: "invalid query arguments"}
	}
	if flags.NArg() != 0 {
		return index.QueryPage{}, &commandError{code: exitUsage, message: "query accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return index.QueryPage{}, &commandError{code: exitStore, message: err.Error()}
	}
	page, err := index.New(st).Query(context.Background(), index.QueryRequest{Filters: filters, Limit: *limit, Cursor: *cursor})
	if err != nil {
		return index.QueryPage{}, queryCommandError(err)
	}
	return page, nil
}

func emitQueryResult(writer io.Writer, asJSON bool, page index.QueryPage) error {
	if asJSON {
		return index.WriteQueryPageJSON(context.Background(), writer, page)
	}
	return emitQueryHuman(writer, page)
}

func emitQueryHuman(writer io.Writer, page index.QueryPage) error {
	if err := fprintf(writer, "PACT %s\n", page.Operation); err != nil {
		return err
	}
	if err := fprintf(writer, "index state: %v\ncoverage: %v\n", page.Index.State, page.Index.Coverage); err != nil {
		return err
	}
	if err := fprintf(writer, "local replica completeness: %v (global completeness: %v)\n", page.Replica.Completeness, page.Replica.GlobalCompleteness); err != nil {
		return err
	}
	if err := emitFiltersHuman(writer, page.Filters); err != nil {
		return err
	}
	if err := fprintf(writer, "causal batches use known local dependencies; this is not a total order\n"); err != nil {
		return err
	}
	for _, batch := range page.Batches {
		if err := fprintf(writer, "causal batch %d (complete in page: %v)\n", batch.Batch, batch.CompleteInPage); err != nil {
			return err
		}
		for _, item := range batch.Items {
			if err := emitEventHuman(writer, item); err != nil {
				return err
			}
		}
	}
	if err := fprintf(writer, "unresolved events (separate from ordered causal batches):\n"); err != nil {
		return err
	}
	for _, item := range page.Unresolved {
		if err := emitEventHuman(writer, item); err != nil {
			return err
		}
	}
	if page.Page.NextCursor != nil {
		return fprintf(writer, "continuation: %s\n", *page.Page.NextCursor)
	}
	return nil
}

func emitEventHuman(writer io.Writer, item index.EventItem) error {
	if err := fprintf(writer, "  %s  %s  %s\n", item.EventRef, item.Type, strconv.Quote(item.Subject)); err != nil {
		return err
	}
	return fprintf(writer, "    observed_at (advisory): %s\n", item.ObservedAt)
}

func emitFiltersHuman(writer io.Writer, filters index.Filters) error {
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
			if err := fprintf(writer, "applied filters:\n"); err != nil {
				return err
			}
			wrote = true
		}
		if err := fprintf(writer, "  %s: %s\n", family.name, strings.Join(quoted, ", ")); err != nil {
			return err
		}
	}
	if !wrote {
		return fprintf(writer, "applied filters: none\n")
	}
	return nil
}
