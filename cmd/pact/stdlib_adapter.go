// ABOUTME: Routes operator commands through the standard-library argument adapter.
// ABOUTME: Applies catalog dispatch, help, presentation flags, and writer-safe diagnostics.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	statuspkg "pact/internal/status"
)

type runConfig struct {
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	WorkingDir     string
	StdoutTerminal bool
	StderrTerminal bool
	Width          int
	Environment    map[string]string
}

type presentation struct {
	asJSON    bool
	colorMode string
}

func run(args []string, stdout, stderr io.Writer) int {
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}
	return runWithConfig(args, runConfig{
		Stdin: os.Stdin, Stdout: stdout, Stderr: stderr, WorkingDir: workingDir,
		Width: 80, Environment: environmentMap(os.Environ()),
	})
}

func runWithConfig(args []string, config runConfig) int { //nolint:funlen // The adapter keeps all process-boundary decisions visible in one place.
	config = normalizedRunConfig(config)
	args, display, err := parsePresentation(args)
	if err != nil {
		return writeCommandError(config.Stderr, display.asJSON, &commandError{code: exitUsage, message: err.Error()})
	}
	catalog := commandCatalog()
	if isHelpRequest(args) {
		if err := renderHelp(config.Stdout, catalog, helpPath(args)); err != nil {
			var pathErr *helpPathError
			if errors.As(err, &pathErr) {
				return writeCommandError(config.Stderr, display.asJSON, &commandError{code: exitUsage, message: pathErr.Error()})
			}
			return writeFailure(config.Stderr, display.asJSON, "help output failed")
		}
		return 0
	}
	spec, consumed, found := longestCommandPath(catalog, args)
	if !found {
		return writeUnknownCommand(config, display, args, catalog)
	}
	commandArgs, err := normalizeRepositoryArgs(args[consumed:], spec.repository, config.WorkingDir)
	if err != nil {
		return writeCommandError(config.Stderr, display.asJSON, &commandError{code: exitUsage, message: err.Error()})
	}
	output, err := spec.handler(commandArgs, io.Discard, config)
	if err != nil {
		var commandErr *commandError
		if errors.As(err, &commandErr) {
			return writeCommandError(config.Stderr, display.asJSON, commandErr)
		}
		return writeCommandError(config.Stderr, display.asJSON, &commandError{code: exitUnexpectedError, message: err.Error()})
	}
	if output.page != nil {
		if err := emitQueryResult(config.Stdout, display.asJSON, *output.page); err != nil {
			return writeFailure(config.Stderr, display.asJSON, "query output failed")
		}
		return 0
	}
	if output.status != nil {
		return writeStatus(config, display, *output.status)
	}
	if err := emitResult(config.Stdout, display.asJSON, output.result); err != nil {
		return writeFailure(config.Stderr, display.asJSON, "command output failed")
	}
	return 0
}

func writeStatus(config runConfig, display presentation, result statuspkg.Result) int {
	value := statusMap(result)
	var code int
	message := ""
	writer := config.Stdout
	switch result.Health {
	case statuspkg.HealthHealthy:
		code = 0
	case statuspkg.HealthAttention:
		code, message, writer = exitMissingDependency, "indexed reads are not ready", config.Stderr
	default:
		code, message, writer = verificationFailureExitCode(result.Verification), "PACT verification failed", config.Stderr
	}
	if display.asJSON {
		if code == 0 {
			if err := emitResult(writer, true, value); err != nil {
				return writeFailure(config.Stderr, true, "status output failed")
			}
			return 0
		}
		return writeCommandError(writer, true, &commandError{code: code, message: message, details: value})
	}
	terminal := config.StdoutTerminal
	if code != 0 {
		terminal = config.StderrTerminal
	}
	if err := emitStatusHuman(writer, result, colorEnabled(display, config, terminal), config.Width); err != nil {
		return writeFailure(config.Stderr, false, "status output failed")
	}
	return code
}

func normalizedRunConfig(config runConfig) runConfig {
	if config.Stdout == nil {
		config.Stdout = io.Discard
	}
	if config.Stderr == nil {
		config.Stderr = io.Discard
	}
	if config.WorkingDir == "" {
		config.WorkingDir = "."
	}
	if config.Width <= 0 {
		config.Width = 80
	}
	if config.Environment == nil {
		config.Environment = map[string]string{}
	}
	return config
}

func parsePresentation(args []string) ([]string, presentation, error) {
	result := make([]string, 0, len(args))
	display := presentation{colorMode: "auto"}
	commandArgs := argumentsBeforeSentinel(args)
	for position := 0; position < len(commandArgs); position++ {
		argument := commandArgs[position]
		switch {
		case argument == "--json":
			display.asJSON = true
		case argument == "--color":
			if position+1 == len(commandArgs) {
				return nil, display, errors.New("--color requires auto, always, or never")
			}
			position++
			display.colorMode = commandArgs[position]
		case strings.HasPrefix(argument, "--color="):
			display.colorMode = strings.TrimPrefix(argument, "--color=")
		default:
			result = append(result, argument)
		}
	}
	if display.colorMode != "auto" && display.colorMode != "always" && display.colorMode != "never" {
		return nil, display, errors.New("--color requires auto, always, or never")
	}
	result = append(result, args[len(commandArgs):]...)
	return result, display, nil
}

func argumentsBeforeSentinel(args []string) []string {
	for position, argument := range args {
		if argument == "--" {
			return args[:position]
		}
	}
	return args
}

func colorEnabled(display presentation, config runConfig, terminal bool) bool {
	if display.asJSON {
		return false
	}
	switch display.colorMode {
	case "always":
		return true
	case "never":
		return false
	default:
		_, noColor := config.Environment["NO_COLOR"]
		return terminal && !noColor && config.Environment["TERM"] != "dumb"
	}
}

func isHelpRequest(args []string) bool {
	commandArgs := argumentsBeforeSentinel(args)
	if len(commandArgs) == 0 || commandArgs[0] == "help" {
		return true
	}
	for _, argument := range commandArgs {
		if argument == "--help" || argument == "-h" {
			return true
		}
	}
	return false
}

func helpPath(args []string) []string {
	commandArgs := argumentsBeforeSentinel(args)
	if len(commandArgs) > 0 && commandArgs[0] == "help" {
		return commandArgs[1:]
	}
	result := make([]string, 0, len(commandArgs))
	for _, argument := range commandArgs {
		if argument != "--help" && argument != "-h" {
			result = append(result, argument)
		}
	}
	return result
}

func longestCommandPath(catalog []commandSpec, args []string) (commandSpec, int, bool) {
	var best commandSpec
	bestLength := 0
	for _, spec := range catalog {
		if len(spec.path) > len(args) || len(spec.path) <= bestLength {
			continue
		}
		matched := true
		for position, part := range spec.path {
			if args[position] != part {
				matched = false
				break
			}
		}
		if matched {
			best, bestLength = spec, len(spec.path)
		}
	}
	return best, bestLength, bestLength > 0
}

func writeUnknownCommand(config runConfig, display presentation, args []string, catalog []commandSpec) int {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	message := fmt.Sprintf("unknown command: %s", name)
	if suggestion := suggestCommand(name, catalog); suggestion != "" {
		message += fmt.Sprintf("; did you mean %q?", suggestion)
	}
	return writeCommandError(config.Stderr, display.asJSON, &commandError{code: exitUsage, message: message})
}

func writeCommandError(writer io.Writer, asJSON bool, commandErr *commandError) int {
	if err := emitError(writer, asJSON, commandErr); err != nil {
		return exitUnexpectedError
	}
	return commandErr.code
}

func writeFailure(stderr io.Writer, asJSON bool, message string) int {
	if err := emitError(stderr, asJSON, &commandError{code: exitUnexpectedError, message: message}); err != nil {
		return exitUnexpectedError
	}
	return exitUnexpectedError
}

func environmentMap(entries []string) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		if found {
			result[key] = value
		}
	}
	return result
}
