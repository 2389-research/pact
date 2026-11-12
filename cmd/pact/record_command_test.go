// ABOUTME: Checks record diagnostics and typed JSON fields through the CLI adapter.
// ABOUTME: Ensures malformed objects fail before signing and humans see recovery identity.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pact/internal/workflow"
)

func TestRecordRejectsArrayWithObjectDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runErrorJSON(t, []string{"record", "--repo", t.TempDir(), "--profile", "development/v1", "--input", path, "--key-file", "missing", "--json"}, exitUsage)
	if !strings.Contains(result["error"].(string), "expected one record object") {
		t.Fatalf("%+v", result)
	}
}

func TestRecordJSONCollectionsAndHumanAdmission(t *testing.T) {
	result := workflow.RecordResult{Admission: "existing", ObjectID: "sha256:" + strings.Repeat("a", 64), EventRefs: []string{}, IndexState: "pending", NextAction: "Retry exact input."}
	details := recordResultMap(result)
	if details["admission"] != "existing" || details["object_id"] != result.ObjectID {
		t.Fatal(details)
	}
	var out bytes.Buffer
	if err := emitRecordHuman(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), result.ObjectID) || !strings.Contains(out.String(), "pending") || !strings.Contains(out.String(), result.NextAction) {
		t.Fatal(out.String())
	}
}
