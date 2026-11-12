// ABOUTME: Checks explicitly selected event evidence without appending or rebuilding state.
// ABOUTME: Reports local observation time and assessments separately from signed claims.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"pact/internal/evidence"
	"pact/internal/ledger"
	"pact/internal/store"
)

func runEvidenceCheck(args []string, stderr io.Writer, config runConfig) (commandResult, error) {
	flags := flag.NewFlagSet("evidence check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	eventRef := flags.String("event-ref", "", "selected event reference")
	if err := flags.Parse(args); err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid evidence check arguments"}
	}
	if flags.NArg() != 0 {
		return commandResult{}, &commandError{code: exitUsage, message: "evidence check accepts no positional arguments"}
	}
	ref, err := ledger.NormalizeEventRef(*eventRef)
	if err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "evidence check requires a valid --event-ref"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return commandResult{}, &commandError{code: exitStore, message: "evidence check could not open repository"}
	}
	shown, err := ledger.Show(st, ref)
	if err != nil {
		return commandResult{}, ledgerCommandError(err)
	}
	requests, err := evidenceRequests(*repo, shown.Event)
	if err != nil {
		return commandResult{}, err
	}
	assessments, err := evidence.ResolveAll(context.Background(), requests)
	if err != nil {
		code := exitUnexpectedError
		if errors.Is(err, evidence.ErrLimit) {
			code = exitUsage
		}
		return commandResult{}, &commandError{code: code, message: err.Error()}
	}
	checkedAt := config.Now().UTC().Format(time.RFC3339Nano)
	result := map[string]any{"operation": "evidence-check", "event_ref": ref, "checked_at": checkedAt, "assessments": assessments}
	if config.JSONOutput {
		return commandResult{result: result}, nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "PACT evidence check\nchecked at: %s\nevent: %s\n", checkedAt, ref)
	for i, assessment := range assessments {
		fmt.Fprintf(&out, "  %d  %s  %q (%d bytes)\n", i, assessment.Status, assessment.Ref, assessment.Bytes)
	}
	out.WriteString("Local byte observations; signature verification and acceptance are separate.\n")
	return commandResult{document: out.String()}, nil
}

func evidenceRequests(repo string, event map[string]any) ([]evidence.Request, error) {
	entries, ok := event["evidence"].([]any)
	if !ok {
		return nil, &commandError{code: exitIntegrity, message: "invalid selected event evidence"}
	}
	requests := make([]evidence.Request, 0, len(entries))
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil, &commandError{code: exitIntegrity, message: "invalid selected event evidence"}
		}
		ref, refOK := item["ref"].(string)
		digest, digestOK := item["digest"].(string)
		if !refOK || !digestOK {
			return nil, &commandError{code: exitIntegrity, message: "invalid selected event evidence"}
		}
		requests = append(requests, evidence.Request{Repo: repo, Ref: ref, Digest: digest})
	}
	return requests, nil
}
