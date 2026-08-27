// ABOUTME: Parses setup command inputs and adapts the typed setup service to CLI errors.
// ABOUTME: Keeps noninteractive execution deterministic and preserves proven partial actions.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"pact/internal/identity"
	setuppkg "pact/internal/setup"
	"pact/internal/store"
)

const (
	setupInputLimit      = 64 * 1024
	setupCancelledStatus = "cancelled" //nolint:misspell // The approved setup wire contract uses this spelling.
)

func runSetup(args []string, _ io.Writer, config runConfig) (commandResult, error) {
	return runSetupWithApply(args, config, setuppkg.Apply)
}

func runSetupWithApply(
	args []string,
	config runConfig,
	apply func(context.Context, setuppkg.Request) (setuppkg.Result, error),
) (commandResult, error) {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", ".", "project root")
	namespace := flags.String("namespace", "", "default namespace")
	actor := flags.String("actor", "", "actor label")
	keyFile := flags.String("key-file", "", "external signing key file")
	if err := flags.Parse(args); err != nil {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid setup arguments"}
	}
	if flags.NArg() != 0 {
		return commandResult{}, &commandError{code: exitUsage, message: "invalid setup arguments"}
	}
	missing := *namespace == "" || *actor == "" || *keyFile == ""
	if missing && (!config.StdinTerminal || config.JSONOutput) {
		return commandResult{}, &commandError{code: exitUsage, message: "setup requires --namespace, --actor, and --key-file"}
	}
	request := setuppkg.Request{Repo: *repo, Namespace: *namespace, Actor: *actor, KeyFile: *keyFile, Now: config.Now()}
	if missing {
		promptedRequest, canceledResult, promptErr := promptSetupRequest(config, request)
		if promptErr != nil {
			return commandResult{}, promptErr
		}
		if canceledResult != nil {
			return commandResult{setup: canceledResult}, nil
		}
		request = promptedRequest
	}
	result, err := apply(context.Background(), request)
	if err == nil {
		return commandResult{setup: &result}, nil
	}
	var applyErr *setuppkg.ApplyError
	if errors.As(err, &applyErr) {
		commandErr := commandErrorFor(applyErr.Err, exitUnexpectedError)
		partialDetails := setupResultMap(applyErr.Result)
		maps.Copy(partialDetails, commandErr.details)
		commandErr.details = partialDetails
		return commandResult{setup: &applyErr.Result}, commandErr
	}
	return commandResult{}, commandErrorFor(err, exitUsage)
}

func promptSetupRequest(config runConfig, request setuppkg.Request) (setuppkg.Request, *setuppkg.Result, error) {
	reader := bufio.NewReaderSize(config.Stdin, setupInputLimit+3)
	defaults := setupPromptDefaults(request)
	if err := promptMissingSetupValue(reader, config.Stderr, &request.Namespace, "Namespace", "org/example/widget", defaults.namespace); err != nil {
		return request, nil, err
	}
	if err := promptMissingSetupValue(reader, config.Stderr, &request.Actor, "Actor", "Alice", defaults.actor); err != nil {
		return request, nil, err
	}
	if err := promptMissingSetupValue(reader, config.Stderr, &request.KeyFile, "Key file", "../alice.key.json", ""); err != nil {
		return request, nil, err
	}
	plan, err := setuppkg.Inspect(context.Background(), request)
	if err != nil {
		return request, nil, commandErrorFor(err, exitUsage)
	}
	if err := emitSetupPlan(config.Stderr, plan); err != nil {
		return request, nil, &commandError{code: exitUnexpectedError, message: "setup plan output failed"}
	}
	answer, err := readSetupLine(reader, config.Stderr, "Continue? [y/N] ")
	if err != nil {
		return request, nil, err
	}
	if strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes") {
		return request, nil, nil
	}
	if answer = strings.TrimSpace(answer); answer != "" && !strings.EqualFold(answer, "n") && !strings.EqualFold(answer, "no") {
		return request, nil, &commandError{code: exitUsage, message: "setup confirmation requires yes or no"}
	}
	return request, &setuppkg.Result{
		Status: setupCancelledStatus, Repo: plan.Repo, Store: filepath.Join(plan.Repo, ".pact"),
		Namespace: plan.Namespace, Actor: plan.Actor, KeyFile: plan.KeyFile,
	}, nil
}

func promptMissingSetupValue(reader *bufio.Reader, writer io.Writer, target *string, label, example, observed string) error {
	if *target != "" {
		return nil
	}
	value, err := readSetupValue(reader, writer, label, example, observed)
	if err != nil {
		return err
	}
	*target = value
	return nil
}

type setupDefaults struct {
	namespace string
	actor     string
}

func setupPromptDefaults(request setuppkg.Request) setupDefaults {
	var defaults setupDefaults
	if _, err := os.Lstat(request.Repo); err == nil {
		if opened, openErr := store.Open(request.Repo); openErr == nil {
			defaults.namespace, _ = opened.DefaultNamespace()
		}
	}
	if request.KeyFile != "" {
		if key, err := identity.LoadSigningKey(request.KeyFile, request.Repo); err == nil {
			defaults.actor = key.Actor
		}
	}
	return defaults
}

func readSetupValue(reader *bufio.Reader, writer io.Writer, label, example, observed string) (string, error) {
	prompt := fmt.Sprintf("%s (example: %s): ", escapeSetupTerminalText(label), escapeSetupTerminalText(example))
	if observed != "" {
		prompt = fmt.Sprintf("%s [%s]: ", escapeSetupTerminalText(label), escapeSetupTerminalText(observed))
	}
	value, err := readSetupLine(reader, writer, prompt)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value != "" {
		return value, nil
	}
	if observed != "" {
		return observed, nil
	}
	return "", &commandError{code: exitUsage, message: strings.ToLower(label) + " is required"}
}

func readSetupLine(reader *bufio.Reader, writer io.Writer, prompt string) (string, error) {
	if _, err := io.WriteString(writer, prompt); err != nil {
		return "", &commandError{code: exitUnexpectedError, message: "setup prompt output failed"}
	}
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", &commandError{code: exitUsage, message: "setup input exceeds 64 KiB"}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", &commandError{code: exitUnexpectedError, message: "setup input failed"}
	}
	line = bytesTrimLineEnding(line)
	if len(line) > setupInputLimit {
		return "", &commandError{code: exitUsage, message: "setup input exceeds 64 KiB"}
	}
	return string(line), nil
}

func bytesTrimLineEnding(line []byte) []byte {
	line = []byte(strings.TrimSuffix(string(line), "\n"))
	line = []byte(strings.TrimSuffix(string(line), "\r"))
	return line
}
