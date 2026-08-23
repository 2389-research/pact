// ABOUTME: Tests PACT's strict JSON parser, normalizer, and canonical encoder.
// ABOUTME: Locks the pact-json-v1 wire bytes and error behavior.
package canonical

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVectors(t *testing.T) {
	t.Parallel()

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
		Invalid []struct {
			Name          string `json:"name"`
			InputJSON     string `json:"input_json"`
			InputHex      string `json:"input_hex"`
			ErrorContains string `json:"error_contains"`
		} `json:"invalid"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}

	for _, vector := range vectors.Valid {
		t.Run(vector.Name, func(t *testing.T) {
			value, err := Parse([]byte(vector.InputJSON))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			canonical, err := Marshal(value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if got := string(canonical); got != vector.CanonicalUTF8 {
				t.Errorf("Marshal() = %q, want %q", got, vector.CanonicalUTF8)
			}
			if got := Digest(canonical); got != vector.SHA256 {
				t.Errorf("Digest() = %q, want %q", got, vector.SHA256)
			}
		})
	}

	for _, vector := range vectors.Invalid {
		t.Run(vector.Name, func(t *testing.T) {
			raw := []byte(vector.InputJSON)
			if vector.InputHex != "" {
				var err error
				raw, err = hex.DecodeString(vector.InputHex)
				if err != nil {
					t.Fatalf("decode input hex: %v", err)
				}
			}
			_, err := Parse(raw)
			if err == nil || !strings.Contains(err.Error(), vector.ErrorContains) {
				t.Errorf("Parse() error = %v, want substring %q", err, vector.ErrorContains)
			}
		})
	}
}

func TestParseRejectsTrailingJSONAndInvalidUTF8(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"trailing JSON": []byte(`{} {}`),
		"invalid UTF-8": {0xff},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatal("Parse() error = nil, want rejection")
			}
		})
	}
}

func TestParseValidatesUnicodeSurrogateEscapes(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]struct {
		raw          string
		want         string
		errorContain string
	}{
		"unpaired high surrogate": {raw: `{"x":"\uD800"}`, errorContain: "unpaired high surrogate escape"},
		"unpaired low surrogate":  {raw: `{"x":"\uDC00"}`, errorContain: "unpaired low surrogate escape"},
		"surrogate pair":          {raw: `{"x":"\uD83D\uDE00"}`, want: `{"x":"😀"}`},
		"escaped backslash":       {raw: `{"x":"\\uD800"}`, want: `{"x":"\\uD800"}`},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := Parse([]byte(input.raw))
			if input.errorContain != "" {
				if err == nil || !strings.Contains(err.Error(), input.errorContain) {
					t.Errorf("Parse() error = %v, want substring %q", err, input.errorContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			got, err := Marshal(value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != input.want {
				t.Errorf("Marshal() = %q, want %q", got, input.want)
			}
		})
	}
}

func TestMarshalNormalizesNestedStringsAndEmptyValues(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"empty":  map[string]any{"array": []any{}, "object": map[string]any{}, "string": ""},
		"nested": []any{map[string]any{"e\u0301": "Cafe\u0301"}},
	}
	got, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"empty":{"array":[],"object":{},"string":""},"nested":[{"é":"Café"}]}`
	if string(got) != want {
		t.Errorf("Marshal() = %q, want %q", got, want)
	}
}

func TestSafeIntegerBoundaries(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"minimum": `-9007199254740991`,
		"maximum": `9007199254740991`,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := Parse([]byte(raw))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			got, err := Marshal(value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != raw {
				t.Errorf("Marshal() = %q, want %q", got, raw)
			}
		})
	}
}
