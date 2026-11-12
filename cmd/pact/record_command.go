// ABOUTME: Adapts one noninteractive development record object to the recording workflow.
// ABOUTME: Preserves admission identity in error details when derived index refresh fails.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"pact/internal/development"
	"pact/internal/evidence"
	"pact/internal/ledger"
	"pact/internal/workflow"
)

func runRecord(args []string, stderr io.Writer, config runConfig) (commandResult, error) {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	profile := flags.String("profile", "", "fixed development profile")
	input := flags.String("input", "", "one record JSON object")
	key := flags.String("key-file", "", "existing external signing key")
	if err := flags.Parse(args); err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid record arguments"}
	}
	if flags.NArg() != 0 || *profile != development.Profile || *input == "" || *key == "" {
		return commandResult{}, &commandError{code: exitUsage, message: "record requires --profile development/v1, --input, and --key-file"}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	raw, err := readRecordInput(*input)
	if err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "record input could not be read within the object byte limit"}
	}
	decoded, err := development.DecodeInput(ctx, raw)
	if err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: err.Error()}
	}
	result, err := workflow.Record(ctx, workflow.RecordRequest{Repo: *repo, KeyFile: *key, Input: decoded})
	details := recordResultMap(result)
	if err != nil {
		code := exitUnexpectedError
		var validation *development.ValidationError
		if errors.As(err, &validation) || errors.Is(err, workflow.ErrConflict) || errors.Is(err, workflow.ErrEvidence) || errors.Is(err, evidence.ErrLimit) {
			code = exitUsage
		}
		commandErr := commandErrorFor(err, code)
		commandErr.details = details
		if !config.JSONOutput && result.ObjectID != "" {
			commandErr.message = fmt.Sprintf("%s; admission: %s; object: %s; index: %s; %s", commandErr.message, result.Admission, result.ObjectID, result.IndexState, result.NextAction)
		}
		return commandResult{}, commandErr
	}
	if !config.JSONOutput {
		var out strings.Builder
		if err := emitRecordHuman(&out, result); err != nil {
			return commandResult{}, err
		}
		return commandResult{document: out.String()}, nil
	}
	return commandResult{result: details}, nil
}

func readRecordInput(path string) ([]byte, error) {
	// #nosec G304,G703 -- the operator explicitly selects the input file.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	maximum := ledger.Phase2Limits.ObjectBytes
	if maximum >= math.MaxInt64 {
		return nil, errors.New("invalid record byte limit")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) > ledger.Phase2Limits.ObjectBytes {
		return nil, errors.New("record input exceeds object byte limit")
	}
	return raw, nil
}

func recordResultMap(result workflow.RecordResult) map[string]any {
	return map[string]any{"operation": "record", "admission": result.Admission, "object_id": result.ObjectID, "event_refs": result.EventRefs, "schema_validation": result.SchemaValidation, "integrity": result.Integrity, "authenticity": result.Authenticity, "authorization": result.Authorization, "index_state": result.IndexState, "source_fingerprint": result.SourceFingerprint, "next_action": result.NextAction, "evidence": result.Evidence, "evidence_state": result.EvidenceState, "conflicting_object_ids": result.ConflictingObjectIDs}
}

func emitRecordHuman(writer io.Writer, result workflow.RecordResult) error {
	_, err := fmt.Fprintf(writer, "PACT record\nadmission: %s\nobject: %s\nschema: %s\nintegrity: %s\nauthenticity: %s\nauthorization: %s\nevidence: %s\nindex: %s\n%s\nReported source and execution facts; evidence checks compare selected bytes.\n", result.Admission, result.ObjectID, result.SchemaValidation, result.Integrity, result.Authenticity, result.Authorization, result.EvidenceState, result.IndexState, result.NextAction)
	return err
}
