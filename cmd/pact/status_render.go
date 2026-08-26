// ABOUTME: Renders compact human operator status using terminal headings and readable labels.
// ABOUTME: Preserves all copyable values while color only reinforces the written health state.
package main

import (
	"fmt"
	"io"
	"strings"

	statuspkg "pact/internal/status"
)

func emitStatusHuman(writer io.Writer, result statuspkg.Result, color bool) error {
	health := string(result.Health)
	if result.Health == statuspkg.HealthHealthy {
		health = "Healthy"
	}
	if color {
		colorCode := "32"
		switch result.Health {
		case statuspkg.HealthAttention:
			colorCode = "33"
		case statuspkg.HealthBroken:
			colorCode = "31"
		}
		health = "\x1b[" + colorCode + "m" + health + "\x1b[0m"
	}
	completeness := strings.ReplaceAll(result.Verification.Completeness.Status, "_", " ")
	if _, err := fmt.Fprintf(writer, "%s\n\nLedger\n  Namespace: %s\n  Strict verification: %v\n\nReplica\n  Local completeness: %s\n  Global completeness: unknown\n\nIndex\n", health, result.DefaultNamespace, result.Verification.OK, completeness); err != nil {
		return err
	}
	if result.Index == nil {
		if _, err := fmt.Fprintln(writer, "  not inspected"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(writer, "  State: %s\n  Coverage: %s\n", result.Index.Index.State, result.Index.Index.Coverage); err != nil {
		return err
	}
	if result.Health == statuspkg.HealthAttention && result.NextAction != nil {
		_, err := fmt.Fprintf(writer, "\nRun: %s\n", result.NextAction.Command)
		return err
	}
	return nil
}
