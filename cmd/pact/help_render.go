// ABOUTME: Renders all PACT help from the command catalog without flag-package usage text.
// ABOUTME: Keeps command discovery, grouped inventory, and nested help deterministic.
package main

import (
	"fmt"
	"io"
	"strings"
)

func renderHelp(writer io.Writer, catalog []commandSpec, path []string) error {
	if len(path) == 0 {
		return renderTopHelp(writer, catalog)
	}
	if spec, _, found := longestCommandPath(catalog, path); found && len(spec.path) == len(path) {
		return renderCommandHelp(writer, spec)
	}
	if len(path) == 1 && path[0] == "index" {
		return renderIndexHelp(writer, catalog)
	}
	return fmt.Errorf("unknown help path: %s", strings.Join(path, " "))
}

func renderTopHelp(writer io.Writer, catalog []commandSpec) error {
	if err := fprintf(writer, "Usage: pact COMMAND [OPTIONS]\n\nCommands:\n"); err != nil {
		return err
	}
	for _, group := range []string{"Get started", "Inspect", "Write", "Maintain"} {
		if err := fprintf(writer, "\n%s:\n", group); err != nil {
			return err
		}
		if err := renderGroupHelp(writer, catalog, group); err != nil {
			return err
		}
	}
	return nil
}

func renderGroupHelp(writer io.Writer, catalog []commandSpec, group string) error {
	for _, spec := range catalog {
		if spec.group == group {
			if err := fprintf(writer, "  %-16s %s\n", strings.Join(spec.path, " "), spec.purpose); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderCommandHelp(writer io.Writer, spec commandSpec) error {
	if err := fprintf(writer, "Usage: %s\n\n%s\n\nCommands:\n  %s\n", spec.usage, spec.purpose, strings.Join(spec.path, " ")); err != nil {
		return err
	}
	if len(spec.examples) == 0 {
		return nil
	}
	if err := fprintf(writer, "\nExamples:\n"); err != nil {
		return err
	}
	for _, example := range spec.examples {
		if err := fprintf(writer, "  %s\n", example); err != nil {
			return err
		}
	}
	return nil
}

func renderIndexHelp(writer io.Writer, catalog []commandSpec) error {
	if err := fprintf(writer, "Usage: pact index COMMAND [OPTIONS]\n\nCommands:\n"); err != nil {
		return err
	}
	for _, spec := range catalog {
		if len(spec.path) == 2 && spec.path[0] == "index" {
			if err := fprintf(writer, "  %-16s %s\n", strings.Join(spec.path, " "), spec.purpose); err != nil {
				return err
			}
		}
	}
	return nil
}

func suggestCommand(name string, catalog []commandSpec) string {
	best, bestDistance := "", 3
	tied := false
	for _, spec := range catalog {
		if len(spec.path) != 1 {
			continue
		}
		distance := editDistance(name, spec.path[0])
		if distance < bestDistance {
			best, bestDistance, tied = spec.path[0], distance, false
		} else if distance == bestDistance {
			tied = true
		}
	}
	if tied || bestDistance > 2 {
		return ""
	}
	return best
}

func editDistance(left, right string) int {
	previous := make([]int, len(right)+1)
	for position := range previous {
		previous[position] = position
	}
	for leftPosition, leftRune := range left {
		current := make([]int, len(right)+1)
		current[0] = leftPosition + 1
		for rightPosition, rightRune := range right {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightPosition+1] = min(current[rightPosition]+1, previous[rightPosition+1]+1, previous[rightPosition]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}
