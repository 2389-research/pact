// ABOUTME: Adapts disposable-index status and rebuild operations to stable CLI output.
// ABOUTME: Keeps index validation and publication inside the index domain package.
package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"
)

func runIndexStatus(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("index status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid index status arguments"}
	}
	if flags.NArg() != 0 {
		return nil, &commandError{code: exitUsage, message: "index status accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	status, err := index.New(st).Status(context.Background())
	if err != nil {
		return nil, indexCommandError(err)
	}
	return indexStatusMap("index-status", status), nil
}

func runIndexRebuild(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("index rebuild", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid index rebuild arguments"}
	}
	if flags.NArg() != 0 {
		return nil, &commandError{code: exitUsage, message: "index rebuild accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	rebuilt, err := index.New(st).Rebuild(context.Background())
	if err != nil {
		return nil, indexCommandError(err)
	}
	result := indexStatusMap("index-rebuild", rebuilt.Status)
	result["created"] = rebuilt.Created
	result["replaced"] = rebuilt.Replaced
	return result, nil
}

func indexStatusMap(operation string, status index.Status) map[string]any {
	return map[string]any{
		"operation": operation,
		"index":     indexInfoMap(status.Index),
		"replica":   replicaInfoMap(status.Replica),
		"counts": map[string]any{
			"objects": status.Counts.Objects, "commits": status.Counts.Commits,
			"checkpoints": status.Counts.Checkpoints, "events": status.Counts.Events,
			"edges": status.Counts.Edges, "canonical_bytes": status.Counts.CanonicalBytes,
		},
		"limits": map[string]any{"profile": status.Limits.Profile, "status": status.Limits.Status},
	}
}

func indexInfoMap(info index.IndexInfo) map[string]any {
	return map[string]any{
		"state": info.State, "coverage": info.Coverage, "path": info.Path,
		"schema_version": info.SchemaVersion, "source_fingerprint": info.SourceFingerprint,
		"logical_digest": info.LogicalDigest, "rebuild_required": info.RebuildRequired,
	}
}

func replicaInfoMap(info index.ReplicaInfo) map[string]any {
	blockers := make([]any, len(info.Blockers))
	for position, blocker := range info.Blockers {
		blockers[position] = map[string]any{
			"code": blocker.Code, "source_id": blocker.SourceID,
			"field": blocker.Field, "missing_ref": blocker.MissingRef,
		}
	}
	return map[string]any{
		"scope": info.Scope, "completeness": info.Completeness,
		"global_completeness": info.GlobalCompleteness, "blockers": blockers,
	}
}

func indexCommandError(err error) error {
	if limit, ok := errors.AsType[*ledger.LimitError](err); ok {
		return &commandError{code: exitMissingDependency, message: limit.Error(), details: map[string]any{
			"code": "resource_limit", "resource": limit.Resource,
			"maximum": limit.Maximum, "observed_at_least": limit.ObservedAtLeast,
		}}
	}
	if queryErr, ok := errors.AsType[*index.QueryError](err); ok {
		return queryCommandError(queryErr)
	}
	return &commandError{code: exitUnexpectedError, message: "index operation failed"}
}

func emitIndexHuman(writer io.Writer, result map[string]any) error {
	info, infoOK := result["index"].(map[string]any)
	replica, replicaOK := result["replica"].(map[string]any)
	if !infoOK || !replicaOK {
		return errors.New("index result is malformed")
	}
	if err := fprintf(writer, "PACT %s\n", result["operation"]); err != nil {
		return err
	}
	if err := fprintf(writer, "index state: %v\n", info["state"]); err != nil {
		return err
	}
	if err := fprintf(writer, "coverage: %v\n", info["coverage"]); err != nil {
		return err
	}
	if err := fprintf(writer, "local replica completeness: %v (global completeness: %v)\n", replica["completeness"], replica["global_completeness"]); err != nil {
		return err
	}
	if err := fprintf(writer, "rebuild required: %v\n", info["rebuild_required"]); err != nil {
		return err
	}
	if result["operation"] == "index-rebuild" {
		return fprintf(writer, "created: %v\nreplaced: %v\n", result["created"], result["replaced"])
	}
	return nil
}

func queryCommandError(err error) error {
	if usage, ok := errors.AsType[*index.UsageError](err); ok {
		return &commandError{code: exitUsage, message: usage.Error()}
	}
	if errors.Is(err, ledger.ErrSecretSafety) {
		return &commandError{code: exitUsage, message: "query filter is unsafe"}
	}
	if limit, ok := errors.AsType[*ledger.LimitError](err); ok {
		return &commandError{code: exitMissingDependency, message: limit.Error(), details: map[string]any{
			"code": "resource_limit", "resource": limit.Resource,
			"maximum": limit.Maximum, "observed_at_least": limit.ObservedAtLeast,
		}}
	}
	if errors.Is(err, ledger.ErrMissingDependency) || errors.Is(err, store.ErrMissingDependency) {
		return &commandError{code: exitMissingDependency, message: "indexed query has missing dependencies"}
	}
	if errors.Is(err, ledger.ErrIntegrity) || errors.Is(err, store.ErrIntegrity) {
		return &commandError{code: exitIntegrity, message: "indexed query source is invalid", details: map[string]any{"code": "source_invalid"}}
	}
	queryErr, ok := errors.AsType[*index.QueryError](err)
	if !ok {
		return &commandError{code: exitUnexpectedError, message: "indexed query failed"}
	}
	var code int
	switch queryErr.Code {
	case "cursor_invalid", "cursor_query_mismatch":
		code = exitUsage
	case "source_invalid":
		code = exitIntegrity
	case "index_missing", "index_stale", "index_corrupt", "index_incompatible", "index_partial_build", "source_changed", "resource_limit", "cursor_stale":
		code = exitMissingDependency
	case "index_publication_failed":
		code = exitUnexpectedError
	default:
		return &commandError{code: exitUnexpectedError, message: "indexed query failed"}
	}
	return &commandError{code: code, message: queryErr.Error(), details: map[string]any{"code": queryErr.Code}}
}
