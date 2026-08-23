// ABOUTME: Checks the public PACT canonical JSON vectors through the Go package API.
// ABOUTME: Keeps frozen reference bytes and SHA-256 IDs interoperable across languages.
package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pact/internal/canonical"
)

func TestCanonicalizationVectors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "pact-ledger", "examples", "canonicalization-vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vectors struct {
		Valid []struct {
			Name          string `json:"name"`
			InputJSON     string `json:"input_json"`
			CanonicalUTF8 string `json:"canonical_utf8"`
			SHA256        string `json:"sha256"`
		} `json:"valid"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}

	for _, vector := range vectors.Valid {
		t.Run(vector.Name, func(t *testing.T) {
			value, err := canonical.Parse([]byte(vector.InputJSON))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			got, err := canonical.Marshal(value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != vector.CanonicalUTF8 {
				t.Errorf("Marshal() = %q, want %q", got, vector.CanonicalUTF8)
			}
			if canonical.Digest(got) != vector.SHA256 {
				t.Errorf("Digest() = %q, want %q", canonical.Digest(got), vector.SHA256)
			}
		})
	}
}
