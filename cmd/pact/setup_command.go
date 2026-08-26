// ABOUTME: Parses setup command inputs and adapts the typed setup service to CLI errors.
// ABOUTME: Keeps noninteractive execution deterministic and preserves proven partial actions.
package main

import (
	"context"
	"errors"
	"flag"
	"io"

	setuppkg "pact/internal/setup"
)

func runSetup(args []string, _ io.Writer, config runConfig) (commandResult, error) {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", ".", "project root")
	namespace := flags.String("namespace", "", "default namespace")
	actor := flags.String("actor", "", "actor label")
	keyFile := flags.String("key-file", "", "external signing key file")
	if err := flags.Parse(args); err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid setup arguments"}
	}
	if flags.NArg() != 0 || *namespace == "" || *actor == "" || *keyFile == "" {
		return commandResult{}, &commandError{code: exitUsage, message: "setup requires --namespace, --actor, and --key-file"}
	}
	result, err := setuppkg.Apply(context.Background(), setuppkg.Request{
		Repo: *repo, Namespace: *namespace, Actor: *actor, KeyFile: *keyFile, Now: config.Now(),
	})
	if err == nil {
		return commandResult{setup: &result}, nil
	}
	var applyErr *setuppkg.ApplyError
	if errors.As(err, &applyErr) {
		commandErr := commandErrorFor(applyErr.Err, exitUnexpectedError)
		commandErr.details = setupResultMap(applyErr.Result)
		return commandResult{setup: &applyErr.Result}, commandErr
	}
	return commandResult{}, commandErrorFor(err, exitUsage)
}
