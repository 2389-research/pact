// ABOUTME: Adapts the typed operator status service to PACT's JSON and exit contracts.
// ABOUTME: Keeps health mapping and missing-store actions outside the ledger domain.
package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	"pact/internal/index"
	"pact/internal/ledger"
	statuspkg "pact/internal/status"
	"pact/internal/store"
)

func runStatus(args []string, _ io.Writer, _ runConfig) (commandResult, error) {
	if len(args) != 2 || args[0] != "--repo" {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid status arguments"}
	}
	st, err := store.Open(args[1])
	if err != nil {
		return commandResult{}, missingStatusError(args[1], err)
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

func missingStatusError(repo string, err error) *commandError {
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
	if !errors.Is(err, store.ErrNotInitialized) {
		details["next_action"] = nil
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
