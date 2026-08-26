// ABOUTME: Renders typed setup results for people and stable JSON automation.
// ABOUTME: Shows only public setup facts and propagates every output failure.
package main

import (
	"fmt"
	"io"
	"strings"

	setuppkg "pact/internal/setup"
)

func setupResultMap(result setuppkg.Result) map[string]any {
	actions := make([]map[string]any, len(result.Actions))
	for position, action := range result.Actions {
		actions[position] = map[string]any{"name": string(action.Name), "status": string(action.Status)}
	}
	return map[string]any{
		"operation": "setup", "ok": result.Status == "ready" || result.Status == setupCancelledStatus, "status": result.Status,
		"repo": result.Repo, "store": result.Store, "namespace": result.Namespace, "actor": result.Actor,
		"key_file": result.KeyFile, "key_id": result.KeyID, "actions": actions,
	}
}

func writeSetup(config runConfig, display presentation, result setuppkg.Result) int {
	var err error
	if display.asJSON {
		err = emitResult(config.Stdout, true, setupResultMap(result))
	} else {
		err = emitSetupHuman(config.Stdout, result, colorEnabled(display, config, config.StdoutTerminal), config.Width)
	}
	if err != nil {
		return writeFailure(config.Stderr, display.asJSON, "setup output failed")
	}
	return 0
}

func writeSetupError(config runConfig, display presentation, result setuppkg.Result, commandErr *commandError) int {
	if display.asJSON {
		return writeCommandError(config.Stderr, true, commandErr)
	}
	if err := emitSetupErrorHuman(config.Stderr, result, commandErr, colorEnabled(display, config, config.StderrTerminal)); err != nil {
		return exitUnexpectedError
	}
	return commandErr.code
}

func emitSetupHuman(writer io.Writer, result setuppkg.Result, color bool, _ int) error {
	var output strings.Builder
	fmt.Fprintf(&output, "PACT setup\nRepo      %s\nStore     %s\nKey file  %s\n", result.Repo, result.Store, result.KeyFile)
	if result.KeyID != "" {
		fmt.Fprintf(&output, "Key ID    %s\n", result.KeyID)
	}
	if result.Status == setupCancelledStatus {
		fmt.Fprintf(&output, "\n%s\n", setupCancelledStatus)
		_, err := io.WriteString(writer, output.String())
		return err
	}
	output.WriteString("\nSetup\n")
	writeSetupActions(&output, result.Actions, color)
	ready := "ready"
	if color {
		ready = "\x1b[32m" + ready + "\x1b[0m"
	}
	fmt.Fprintf(&output, "\n%s\n", ready)
	_, err := io.WriteString(writer, output.String())
	return err
}

func emitSetupPlan(writer io.Writer, plan setuppkg.Plan) error {
	var output strings.Builder
	fmt.Fprintf(&output, "\nPACT setup plan\nRepo       %s\nNamespace  %s\nActor      %s\nKey file   %s\n\nPlan\n", plan.Repo, plan.Namespace, plan.Actor, plan.KeyFile)
	writeSetupActions(&output, plan.Actions, false)
	output.WriteByte('\n')
	_, err := io.WriteString(writer, output.String())
	return err
}

func emitSetupErrorHuman(writer io.Writer, result setuppkg.Result, commandErr *commandError, color bool) error {
	var output strings.Builder
	fmt.Fprintf(&output, "PACT error: %s\n", commandErr.message)
	writeSetupFact(&output, "Repo", result.Repo)
	writeSetupFact(&output, "Store", result.Store)
	writeSetupFact(&output, "Namespace", result.Namespace)
	writeSetupFact(&output, "Actor", result.Actor)
	writeSetupFact(&output, "Key file", result.KeyFile)
	writeSetupFact(&output, "Key ID", result.KeyID)
	if len(result.Actions) != 0 {
		output.WriteString("\nCompleted setup actions\n")
		writeSetupActions(&output, result.Actions, color)
	}
	_, err := io.WriteString(writer, output.String())
	return err
}

func writeSetupFact(output *strings.Builder, label, value string) {
	if value != "" {
		fmt.Fprintf(output, "%-9s %s\n", label, value)
	}
}

func writeSetupActions(output *strings.Builder, actions []setuppkg.Action, color bool) {
	for _, action := range actions {
		status := string(action.Status)
		if color {
			status = "\x1b[32m" + status + "\x1b[0m"
		}
		fmt.Fprintf(output, "  %-7s %s\n", action.Name, status)
	}
}
