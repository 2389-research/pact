// ABOUTME: Adapts PACT commit, inspection, and verification APIs to the command line.
// ABOUTME: Parses event input strictly and preserves layered verification results in JSON.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

func runCommit(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("commit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	keyPath := flags.String("key-file", "", "external key file")
	eventsPath := flags.String("events", "", "event batch JSON")
	namespace := flags.String("namespace", "", "namespace")
	observedAt := flags.String("observed-at", "", "advisory observed time")
	correlationID := flags.String("correlation-id", "", "correlation ID")
	parents := repeatFlag{}
	flags.Var(&parents, "parent", "parent commit ID")
	delegation := flags.String("delegation-ref", "", "unsupported in this phase")
	epoch := flags.String("epoch", "", "unsupported in this phase")
	lease := flags.String("lease-ref", "", "unsupported in this phase")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid commit arguments"}
	}
	if flags.NArg() != 0 || *keyPath == "" || *eventsPath == "" {
		return nil, &commandError{code: exitUsage, message: "commit requires --key-file and --events"}
	}
	if *delegation != "" || *epoch != "" || *lease != "" {
		return nil, &commandError{code: exitUsage, message: "delegation, epoch, and lease authority hints are not supported in this phase"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	key, err := identity.LoadKeyFile(*keyPath, true)
	if err != nil {
		return nil, &commandError{code: exitUsage, message: err.Error()}
	}
	raw, err := os.ReadFile(*eventsPath)
	if err != nil {
		return nil, &commandError{code: exitUsage, message: fmt.Sprintf("file not found: %s", *eventsPath)}
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		return nil, &commandError{code: exitUsage, message: "event batch is malformed: " + err.Error()}
	}
	batchObject, ok := parsed.(map[string]any)
	if !ok {
		return nil, &commandError{code: exitUsage, message: "event batch must be a JSON object"}
	}
	batch, err := ledger.NormalizeEventBatch(batchObject)
	if err != nil {
		return nil, ledgerCommandError(err)
	}
	result, err := ledger.Commit(st, key, batch, ledger.CommitOptions{Namespace: *namespace, Parents: parents, ObservedAt: *observedAt, CorrelationID: *correlationID})
	if err != nil {
		return nil, ledgerCommandError(err)
	}
	return map[string]any{"operation": "commit", "object_id": result.ObjectID, "created": result.Created, "namespace": result.Namespace, "parents": result.Parents, "event_refs": result.EventRefs, "integrity": result.Integrity, "authenticity": result.Authenticity, "authorization": result.Authorization, "authorization_reasons": result.AuthorizationReasons, "lease_status": result.LeaseStatus, "path": result.Path}, nil
}

func runHeads(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("heads", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	namespace := flags.String("namespace", "", "namespace prefix")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid heads arguments"}
	}
	if flags.NArg() != 0 {
		return nil, &commandError{code: exitUsage, message: "heads accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	heads, err := ledger.Heads(st, *namespace)
	if err != nil {
		return nil, ledgerCommandError(err)
	}
	return map[string]any{"operation": "heads", "repo": *repo, "scope": *namespace, "heads": heads, "note": "heads describe this local replica; they are not a global completeness claim"}, nil
}
func runShow(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid show arguments"}
	}
	if flags.NArg() != 1 {
		return nil, &commandError{code: exitUsage, message: "show requires one object ID or event reference"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	shown, err := ledger.Show(st, flags.Arg(0))
	if err != nil {
		return nil, ledgerCommandError(err)
	}
	result := map[string]any{"operation": "show", "identifier": shown.Identifier, "kind": shown.Kind, "integrity": shown.Integrity, "authenticity": shown.Authenticity, "errors": shown.Errors}
	if shown.Kind == "event" {
		result["commit_id"] = shown.CommitID
		result["namespace"] = shown.Namespace
		result["actor"] = shown.Actor
		result["observed_at"] = shown.ObservedAt
		result["event"] = shown.Event
	} else {
		result["object"] = shown.Object
	}
	return result, nil
}
func runVerify(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	strict := flags.Bool("strict", false, "treat missing references as errors")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid verify arguments"}
	}
	if flags.NArg() != 0 {
		return nil, &commandError{code: exitUsage, message: "verify accepts no positional arguments"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	verified, err := ledger.Verify(st, *strict)
	if err != nil {
		return nil, ledgerCommandError(err)
	}
	result := verifyMap(verified)
	if !verified.OK {
		return nil, &commandError{code: exitIntegrity, message: "PACT verification failed", details: result}
	}
	return result, nil
}
func runCheckpoint(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	keyPath := flags.String("key-file", "", "external key file")
	scope := flags.String("scope", "", "namespace prefix")
	policyRef := flags.String("policy-ref", "", "policy object ID")
	authorityEpoch := flags.String("authority-epoch", "", "authority epoch")
	schemaRefs := repeatFlag{}
	flags.Var(&schemaRefs, "schema-ref", "schema object ID")
	previous := flags.String("previous", "", "previous checkpoint ID")
	purpose := flags.String("purpose", "", "checkpoint purpose")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid checkpoint arguments"}
	}
	if flags.NArg() != 0 || *keyPath == "" || *scope == "" || *policyRef == "" || *authorityEpoch == "" {
		return nil, &commandError{code: exitUsage, message: "checkpoint requires --key-file, --scope, --policy-ref, and --authority-epoch"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	key, err := identity.LoadKeyFile(*keyPath, true)
	if err != nil {
		return nil, &commandError{code: exitUsage, message: err.Error()}
	}
	checkpoint, err := ledger.Checkpoint(st, key, ledger.CheckpointOptions{
		Scope: *scope, PolicyRef: *policyRef, AuthorityEpoch: *authorityEpoch,
		SchemaRefs: schemaRefs, PreviousCheckpoint: *previous, Purpose: *purpose,
	})
	if err != nil {
		return nil, ledgerCommandError(err)
	}
	frontier := make([]any, len(checkpoint.Frontier))
	for index, entry := range checkpoint.Frontier {
		frontier[index] = map[string]any{"namespace": entry.Namespace, "heads": entry.Heads}
	}
	var previousValue any
	if checkpoint.PreviousCheckpoint != "" {
		previousValue = checkpoint.PreviousCheckpoint
	}
	return map[string]any{
		"operation": "checkpoint", "object_id": checkpoint.ObjectID, "created": checkpoint.Created,
		"scope": checkpoint.Scope, "frontier": frontier, "policy_ref": checkpoint.PolicyRef,
		"schema_refs": checkpoint.SchemaRefs, "authority_epoch": checkpoint.AuthorityEpoch,
		"previous_checkpoint": previousValue, "integrity": checkpoint.Integrity,
		"authenticity": checkpoint.Authenticity, "authorization": checkpoint.Authorization,
		"authorization_reasons": checkpoint.AuthorizationReasons, "path": checkpoint.Path,
	}, nil
}
func verifyMap(result ledger.VerifyResult) map[string]any {
	objects := map[string]any{}
	for id, object := range result.Objects {
		objects[id] = map[string]any{"type": object.Type, "namespace": object.Namespace, "integrity": object.Integrity, "structure": object.Structure, "authenticity": object.Authenticity, "errors": object.Errors, "warnings": object.Warnings, "path": object.Path}
	}
	return map[string]any{"operation": "verify", "repo": result.Repo, "store": result.Store, "strict": result.Strict, "ok": result.OK, "counts": map[string]any{"objects": result.Counts.Objects, "commits": result.Counts.Commits, "checkpoints": result.Counts.Checkpoints, "events": result.Counts.Events, "integrity": result.Counts.Integrity, "structure": result.Counts.Structure, "authenticity": result.Counts.Authenticity, "dag": result.Counts.DAG, "references": result.Counts.References, "authorized": result.Counts.Authorized, "unauthorized": result.Counts.Unauthorized, "indeterminate": result.Counts.Indeterminate}, "heads": result.Heads, "index_status": result.IndexStatus, "integrity": result.Integrity, "structure": result.Structure, "authenticity": result.Authenticity, "dag": result.DAG, "references": result.References, "errors": result.Errors, "warnings": result.Warnings, "authorization": result.Authorization, "objects": objects}
}
func ledgerCommandError(err error) error {
	var verificationError *ledger.CheckpointVerificationError
	if errors.As(err, &verificationError) {
		return &commandError{code: exitIntegrity, message: err.Error(), details: verifyMap(verificationError.Result)}
	}
	if errors.Is(err, ledger.ErrCheckpointAuthorization) {
		return &commandError{code: exitAuthorization, message: err.Error()}
	}
	if strings.Contains(err.Error(), "secret-like") {
		return &commandError{code: exitSecretSafety, message: err.Error()}
	}
	if strings.Contains(err.Error(), "unavailable") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no commit heads") {
		return &commandError{code: 9, message: err.Error()}
	}
	if errors.Is(err, ledger.ErrIntegrity) {
		return &commandError{code: exitIntegrity, message: err.Error()}
	}
	return &commandError{code: exitUsage, message: err.Error()}
}

type repeatFlag []string

func (values *repeatFlag) String() string         { return fmt.Sprint([]string(*values)) }
func (values *repeatFlag) Set(value string) error { *values = append(*values, value); return nil }
