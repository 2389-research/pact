// ABOUTME: Renders compact human operator status using terminal headings and readable labels.
// ABOUTME: Preserves all copyable values while color only reinforces the written health state.
package main

import (
	"fmt"
	"io"
	"strings"

	statuspkg "pact/internal/status"
)

func emitStatusHuman(writer io.Writer, result statuspkg.Result, color bool, width int) error {
	health := statusHealthLabel(result.Health)
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
	verification := "failed"
	if result.Verification.OK {
		verification = "passed"
	}
	completeness := strings.ReplaceAll(result.Verification.Completeness.Status, "_", " ")
	heads := statusHeadCount(result.Verification.Heads)
	var output strings.Builder
	fmt.Fprintf(&output, "PACT  %s\n%s\nNamespace  %s\n\nLedger\n  Strict verification  %s\n", health, result.Repo, result.DefaultNamespace, verification)
	switch {
	case width >= 120:
		fmt.Fprintf(&output, "  Objects %d  Commits %d  Checkpoints %d  Events %d  Heads %d\n", result.Verification.Counts.Objects, result.Verification.Counts.Commits, result.Verification.Counts.Checkpoints, result.Verification.Counts.Events, heads)
	case width >= 80:
		fmt.Fprintf(&output, "  Objects %d  Commits %d  Checkpoints %d  Events %d\n  Heads %d\n", result.Verification.Counts.Objects, result.Verification.Counts.Commits, result.Verification.Counts.Checkpoints, result.Verification.Counts.Events, heads)
	default:
		fmt.Fprintf(&output, "  Objects %d\n  Commits %d\n  Checkpoints %d\n  Events %d\n  Heads %d\n", result.Verification.Counts.Objects, result.Verification.Counts.Commits, result.Verification.Counts.Checkpoints, result.Verification.Counts.Events, heads)
	}
	fmt.Fprintf(&output, "\nReplica\n  Local completeness   %s\n  Global completeness  unknown\n\n", completeness)
	if result.Index == nil {
		output.WriteString("Index  not inspected\n")
	} else {
		fmt.Fprintf(&output, "Index\n  State     %s\n  Coverage  %s\n", result.Index.Index.State, result.Index.Index.Coverage)
	}
	if result.Health == statuspkg.HealthAttention && result.NextAction != nil {
		fmt.Fprintf(&output, "\nRun  %s\n", result.NextAction.Command)
	}
	_, err := io.WriteString(writer, output.String())
	return err
}

func statusHealthLabel(health statuspkg.Health) string {
	switch health {
	case statuspkg.HealthHealthy:
		return "Healthy"
	case statuspkg.HealthAttention:
		return "Attention"
	case statuspkg.HealthBroken:
		return "Broken"
	default:
		return string(health)
	}
}

func statusHeadCount(heads map[string][]string) int {
	count := 0
	for _, namespaceHeads := range heads {
		count += len(namespaceHeads)
	}
	return count
}
