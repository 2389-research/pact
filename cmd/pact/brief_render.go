// ABOUTME: Serializes one complete projected state for machine or human readers.
// ABOUTME: Preserves every fact and reference while escaping signed terminal controls.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode"

	"pact/internal/development"
)

func encodeBrief(state development.WorkState, asJSON bool) ([]byte, error) {
	value := struct {
		Operation string `json:"operation"`
		development.WorkState
	}{Operation: "brief", WorkState: state}
	var raw bytes.Buffer
	encoder := json.NewEncoder(&raw)
	if !asJSON {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if asJSON {
		return raw.Bytes(), nil
	}
	var human bytes.Buffer
	human.WriteString("PACT brief · development/v1\n")
	// JSON quotes signed strings. Escape the remaining DEL and C1 controls too.
	for _, character := range raw.String() {
		if unicode.IsControl(character) && character != '\n' {
			fmt.Fprintf(&human, "\\u%04x", character)
		} else {
			human.WriteRune(character)
		}
	}
	return human.Bytes(), nil
}
