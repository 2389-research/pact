// ABOUTME: Implements the repository-free quickstart command for agent setup.
// ABOUTME: Returns the embedded canonical skill as Markdown or a JSON envelope.
package main

import (
	"io"

	pactledger "pact/docs/pact-ledger"
)

func runQuickstart(args []string, _ io.Writer, config runConfig) (commandResult, error) {
	if len(args) != 0 {
		return commandResult{}, &commandError{code: exitUsage, message: "quickstart accepts no arguments"}
	}
	if config.JSONOutput {
		return commandResult{result: map[string]any{
			"operation": "quickstart",
			"format":    "skill.md",
			"skill":     pactledger.Skill(),
		}}, nil
	}
	return commandResult{document: pactledger.Skill()}, nil
}
