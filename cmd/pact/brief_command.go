// ABOUTME: Adapts bounded development reads to the existing projector and index owners.
// ABOUTME: Captures complete output before writing and refuses oversized successful views.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"pact/internal/development"
	"pact/internal/evidence"
	"pact/internal/index"
	"pact/internal/store"
)

func runBrief(args []string, stderr io.Writer, config runConfig) (commandResult, error) {
	flags := flag.NewFlagSet("brief", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	profile := flags.String("profile", "", "fixed development profile")
	namespace := flags.String("namespace", "", "exact namespace")
	subject := flags.String("subject", "", "exact subject")
	revision := flags.String("source-revision", "", "full source revision")
	maximum := flags.Int("max-bytes", 16384, "maximum encoded output bytes")
	var frontier repeatFlag
	flags.Var(&frontier, "frontier", "frontier commit ID")
	if err := flags.Parse(args); err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid brief arguments"}
	}
	if flags.NArg() != 0 || *profile != development.Profile || *namespace == "" || *subject == "" || !regexp.MustCompile(development.RevisionPattern).MatchString(*revision) {
		return commandResult{}, &commandError{code: exitUsage, message: "brief requires --profile development/v1, --namespace, --subject, and a full lowercase --source-revision"}
	}
	if *maximum < 1024 || *maximum > 65536 {
		return commandResult{}, &commandError{code: exitUsage, message: "brief --max-bytes must be between 1024 and 65536"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return commandResult{}, &commandError{code: exitStore, message: err.Error()}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	status, err := index.New(st).Status(ctx)
	if err != nil {
		return commandResult{}, indexCommandError(err)
	}
	if status.Index.State != "current" {
		failure := commandErrorFor(&index.QueryError{Code: "index_" + strings.ReplaceAll(status.Index.State, "-", "_")}, exitMissingDependency)
		failure.message += "; run pact index rebuild for this repository"
		return commandResult{}, failure
	}
	// Status is a separate readiness observation. Project owns the later frozen cut.
	state, err := development.Project(ctx, st, development.ProjectRequest{Namespace: *namespace, Subject: *subject, SourceRevision: *revision, Frontier: frontier})
	if err != nil {
		return commandResult{}, briefProjectionError(err)
	}
	raw, err := encodeBrief(state, config.JSONOutput)
	if err != nil {
		return commandResult{}, err
	}
	if len(raw) > *maximum {
		return commandResult{}, &commandError{code: exitUsage, message: fmt.Sprintf("brief requires %d bytes; requested maximum is %d; narrow --namespace, --subject, --source-revision or --frontier, or increase --max-bytes (maximum 65536)", len(raw), *maximum), details: map[string]any{"code": "brief_output_limit", "requested_bytes": *maximum, "required_bytes": len(raw)}}
	}
	return commandResult{document: string(raw)}, nil
}

func briefProjectionError(err error) *commandError {
	code := exitUnexpectedError
	if _, invalid := errors.AsType[*development.ProjectRequestError](err); invalid || errors.Is(err, evidence.ErrLimit) {
		code = exitUsage
	}
	return commandErrorFor(err, code)
}
