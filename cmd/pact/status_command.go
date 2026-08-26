// ABOUTME: Adapts the typed operator status service to PACT's JSON and exit contracts.
// ABOUTME: Keeps health mapping and missing-store actions outside the ledger domain.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pact/internal/index"
	"pact/internal/ledger"
	statuspkg "pact/internal/status"
	"pact/internal/store"
)

var errStatusStoreMissing = errors.New("PACT store is missing")

func runStatus(args []string, _ io.Writer, _ runConfig) (commandResult, error) {
	repo, found := statusRepository(args)
	if !found {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid status arguments"}
	}
	if err := preflightStatusRepository(repo); err != nil {
		if errors.Is(err, errStatusStoreMissing) {
			return commandResult{}, missingStatusError(repo)
		}
		return commandResult{}, commandErrorFor(err, exitStore)
	}
	st, err := store.Open(repo)
	if err != nil {
		return commandResult{}, commandErrorFor(err, exitStore)
	}
	result, err := statuspkg.Inspect(context.Background(), st)
	if err != nil {
		return commandResult{}, commandErrorFor(err, exitUnexpectedError)
	}
	value := statusMap(result)
	switch result.Health {
	case statuspkg.HealthHealthy:
		return commandResult{result: value, status: &result}, nil
	case statuspkg.HealthAttention:
		return commandResult{result: value, status: &result}, nil
	default:
		return commandResult{result: value, status: &result}, nil
	}
}

func statusRepository(args []string) (string, bool) {
	if len(args) == 2 && args[0] == "--repo" && args[1] != "" {
		return args[1], true
	}
	if len(args) == 1 {
		if repo, found := strings.CutPrefix(args[0], "--repo="); found && repo != "" {
			return repo, true
		}
	}
	return "", false
}

func preflightStatusRepository(repo string) error {
	// #nosec G703 -- the adapter normalizes the explicit --repo path before this read-only preflight.
	info, err := os.Stat(repo)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errStatusStoreMissing
		}
		return err
	}
	if !info.IsDir() {
		return errors.New("repository is not a directory")
	}
	// #nosec G703 -- .pact is a fixed entry beneath the normalized repository selected above.
	_, err = os.Lstat(filepath.Join(repo, ".pact"))
	if errors.Is(err, os.ErrNotExist) {
		return errStatusStoreMissing
	}
	return err
}

func missingStatusError(repo string) *commandError {
	resolved, resolveErr := filepath.Abs(repo)
	if resolveErr != nil {
		resolved = repo
	}
	details := map[string]any{
		"operation": "status", "ok": false, "health": string(statuspkg.HealthBroken),
		"repo": resolved, "store": filepath.Join(resolved, ".pact"), "default_namespace": "",
		"verification": nil, "index": nil, "replica": nil, "counts": nil, "heads": nil,
		"next_action": map[string]any{"reason": "no PACT store was found", "command": "pact setup"},
	}
	return &commandError{code: exitStore, message: "no PACT store found; run: pact setup", details: details}
}

func statusMap(result statuspkg.Result) map[string]any {
	return map[string]any{
		"operation": "status", "ok": result.Health == statuspkg.HealthHealthy, "health": string(result.Health),
		"repo": result.Repo, "store": result.Store, "default_namespace": result.DefaultNamespace,
		"verification": compactVerificationMap(result.Verification), "index": compactIndexValue(result.Index),
		"replica": compactReplicaValue(result.Index, result.Verification), "counts": verifyCountsMap(result.Verification.Counts),
		"heads": result.Verification.Heads, "next_action": result.NextAction,
	}
}

func compactVerificationMap(result ledger.VerifyResult) map[string]any {
	return map[string]any{
		"strict": result.Strict, "ok": result.OK,
		"integrity":    map[string]any{"errors": result.Integrity.Errors, "warnings": result.Integrity.Warnings},
		"structure":    map[string]any{"errors": result.Structure.Errors, "warnings": result.Structure.Warnings},
		"authenticity": map[string]any{"errors": result.Authenticity.Errors, "warnings": result.Authenticity.Warnings},
		"dag":          map[string]any{"errors": result.DAG.Errors, "warnings": result.DAG.Warnings},
		"references":   map[string]any{"errors": result.References.Errors, "warnings": result.References.Warnings},
	}
}

func compactIndexValue(result *index.Status) any {
	if result == nil {
		return nil
	}
	return indexInfoMap(result.Index)
}

func compactReplicaValue(indexStatus *index.Status, result ledger.VerifyResult) any {
	if indexStatus != nil {
		return replicaInfoMap(indexStatus.Replica)
	}
	return map[string]any{"scope": result.Completeness.Scope, "completeness": result.Completeness.Status, "global_completeness": "unknown", "blockers": result.Completeness.Blockers}
}

func verifyCountsMap(counts ledger.VerifyCounts) map[string]any {
	return map[string]any{"objects": counts.Objects, "commits": counts.Commits, "checkpoints": counts.Checkpoints,
		"events": counts.Events, "integrity": counts.Integrity, "structure": counts.Structure,
		"authenticity": counts.Authenticity, "dag": counts.DAG, "references": counts.References,
		"authorized": counts.Authorized, "unauthorized": counts.Unauthorized, "indeterminate": counts.Indeterminate}
}
