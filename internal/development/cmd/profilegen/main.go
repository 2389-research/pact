// ABOUTME: Writes bundled development/v1 artifacts from the shared profile definitions.
// ABOUTME: Run go generate ./internal/development after a deliberate contract change.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"pact/internal/canonical"
	"pact/internal/development"
)

func main() {
	out := flag.String("out", "schema", "directory for generated development/v1 schemas and policy")
	flag.Parse()
	if err := generate(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(out string) error {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	digests := map[string]string{}
	for name, raw := range development.ProfileArtifacts() {
		digests[name] = canonical.Digest(raw)
		if err := os.WriteFile(filepath.Join(out, name), raw, 0o600); err != nil {
			return fmt.Errorf("write profile artifact %s: %w", name, err)
		}
	}
	raw, err := json.MarshalIndent(digests, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile digests: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, "digests.json"), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write profile digests: %w", err)
	}
	return nil
}
