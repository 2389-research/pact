// ABOUTME: Runs the real development projector as a compiled acceptance-test process.
// ABOUTME: Emits canonical state bytes from a selected signed store and explicit request.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"pact/internal/canonical"
	"pact/internal/development"
	"pact/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: projectdriver REPO REQUEST_JSON")
	}
	var request development.ProjectRequest
	if err := json.Unmarshal([]byte(os.Args[2]), &request); err != nil {
		return err
	}
	st, err := store.Open(os.Args[1])
	if err != nil {
		return err
	}
	state, err := development.Project(context.Background(), st, request)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	value, err := canonical.Parse(raw)
	if err != nil {
		return err
	}
	raw, err = canonical.Marshal(value)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(raw, '\n'))
	return err
}
