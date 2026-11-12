// ABOUTME: Exercises strict development input validation before signing or canonical mutation.
// ABOUTME: Covers the public corpus, bounded diagnostics, and real signed storage round trips.
package development

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

func validationCorpus(t *testing.T) map[string]RecordInput {
	t.Helper()
	var inputs map[string]RecordInput
	readJSON(t, "../../tests/fixtures/development/inputs.json", &inputs)
	return inputs
}

func TestValidateCorpusBeforeAdmission(t *testing.T) {
	inputs := validationCorpus(t)
	var expected map[string]struct {
		DomainValid bool `json:"domain_valid"`
	}
	readJSON(t, "../../tests/fixtures/development/expected.json", &expected)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	initialized, err := store.Init(t.TempDir(), inputs["open-finding"].Namespace, now)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "test.key.json"), "Validation Fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AddRoot(initialized.Store, generated.Key, now); err != nil {
		t.Fatal(err)
	}
	beforeCancellation := protectedFiles(t, initialized.Store.Root())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateInput(ctx, inputs["open-finding"]); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if !reflect.DeepEqual(beforeCancellation, protectedFiles(t, initialized.Store.Root())) {
		t.Fatal("canceled validation mutated store/trust bytes")
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			beforeInput, _ := json.Marshal(input)
			before := protectedFiles(t, initialized.Store.Root())
			batch, err := ValidateInput(context.Background(), input)
			if (err == nil) != expected[name].DomainValid {
				t.Fatalf("validation error = %v, valid = %v", err, expected[name].DomainValid)
			}
			if !reflect.DeepEqual(before, protectedFiles(t, initialized.Store.Root())) {
				t.Fatal("validation mutated store/trust bytes")
			}
			afterInput, _ := json.Marshal(input)
			if !bytes.Equal(beforeInput, afterInput) {
				t.Fatal("validation mutated caller input")
			}
			if err != nil {
				assertValidationError(t, err)
				return
			}
			admitted, err := ledger.Commit(initialized.Store, generated.Key, batch, ledger.CommitOptions{ObservedAt: now.Format(time.RFC3339)})
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range batch.Events {
				shown, err := ledger.Show(initialized.Store, ledger.EventRef(admitted.ObjectID, event.LocalID))
				if err != nil || shown.Integrity != "valid" || shown.Authenticity != "valid" {
					t.Fatalf("readback: %v %v", shown, err)
				}
				if event.SchemaRef != canonical.Digest(ProfileArtifacts()[EventDefinitions()[event.Type].Artifact]) || event.Subject != input.Subject || event.Payload["operation_id"] != input.OperationID {
					t.Fatal("profile facts lost")
				}
			}
		})
	}
}

func protectedFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, ".pact"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = canonical.Digest(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertValidationError(t *testing.T, err error) {
	t.Helper()
	var diagnostic *ValidationError
	if !errors.As(err, &diagnostic) || diagnostic.Path == "" || diagnostic.Category == "" || len(err.Error()) > 512 {
		t.Fatalf("unbounded/unstructured diagnostic: %v", err)
	}
}

func TestValidateRejectsDomainContradictions(t *testing.T) {
	cases := map[string]func(*RecordInput){
		"invalid-utf8":          func(v *RecordInput) { v.Events[0].Payload["summary"] = string([]byte{0xff}) },
		"floating-whole-number": func(v *RecordInput) { v.Events[0].Payload["exit_code"] = float64(0) },
		"unknown-type":          func(v *RecordInput) { v.Events[0].Type = "dev.future.executed" },
		"empty-operation":       func(v *RecordInput) { v.OperationID = "" },
		"empty-subject":         func(v *RecordInput) { v.Subject = "" },
		"bad-namespace":         func(v *RecordInput) { v.Namespace = "../bad" },
		"short-source":          func(v *RecordInput) { v.Source.Revision = "1234" },
		"upper-source":          func(v *RecordInput) { v.Source.Revision = strings.Repeat("A", 40) },
		"empty-events":          func(v *RecordInput) { v.Events = []InputEvent{} },
		"operation-override":    func(v *RecordInput) { v.Events[0].Payload["operation_id"] = "other" },
		"source-override":       func(v *RecordInput) { v.Events[0].Payload["source"] = v.Source },
		"unknown-field":         func(v *RecordInput) { v.Events[0].Payload[strings.Repeat("sensitive", 1000)] = "private-marker" },
		"missing-name":          func(v *RecordInput) { delete(v.Events[0].Payload, "name") },
		"bad-command":           func(v *RecordInput) { v.Events[0].Payload["command"] = []any{} },
		"fractional-exit":       func(v *RecordInput) { v.Events[0].Payload["exit_code"] = 0.5 },
		"passed-no-exit":        func(v *RecordInput) { delete(v.Events[0].Payload, "exit_code") },
		"passed-signal":         func(v *RecordInput) { v.Events[0].Payload["signal"] = "SIGTERM" },
		"failed-negative-exit":  func(v *RecordInput) { v.Events[0].Payload["outcome"] = "failed"; v.Events[0].Payload["exit_code"] = -1 },
		"interrupted-without-observation": func(v *RecordInput) {
			v.Events[0].Payload["outcome"] = "interrupted"
			v.Events[0].Payload["exit_code"] = 0
		},
		"unknown-with-exit": func(v *RecordInput) { v.Events[0].Payload["outcome"] = "unknown" },
		"not-run-with-signal": func(v *RecordInput) {
			v.Events[0].Payload["outcome"] = "not_run"
			delete(v.Events[0].Payload, "exit_code")
			v.Events[0].Payload["signal"] = "SIGTERM"
		},
		"coverage-over-total": func(v *RecordInput) { v.Events[0].Payload["coverage"] = map[string]any{"completed": 2, "total": 1} },
		"coverage-negative":   func(v *RecordInput) { v.Events[0].Payload["coverage"] = map[string]any{"completed": -1, "total": 1} },
		"index-out-of-bounds": func(v *RecordInput) { v.Events[0].Payload["evidence_indexes"] = []any{1} },
		"index-duplicate":     func(v *RecordInput) { v.Events[0].Payload["evidence_indexes"] = []any{0, 0} },
		"finding-index": func(v *RecordInput) {
			v.Events[1].Payload["findings"].([]any)[0].(map[string]any)["evidence_indexes"] = []any{-1}
		},
		"severity":         func(v *RecordInput) { v.Events[1].Payload["findings"].([]any)[0].(map[string]any)["severity"] = "high" },
		"empty-findings":   func(v *RecordInput) { v.Events[1].Payload["findings"] = []any{} },
		"blank-finding-id": func(v *RecordInput) { v.Events[1].Payload["findings"].([]any)[0].(map[string]any)["id"] = "" },
		"secret":           func(v *RecordInput) { v.Events[0].Payload["summary"] = "Bearer abcdefghijklmnopqrstuvwxyz" },
		"envelope-ref":     func(v *RecordInput) { v.Events[0].CausedBy = []string{"local:missing"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := validationCorpus(t)["open-finding"]
			mutate(&input)
			_, err := ValidateInput(context.Background(), input)
			if err == nil {
				t.Fatal("invalid input accepted")
			}
			assertValidationError(t, err)
			if strings.Contains(err.Error(), "private-marker") || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "abcdefghijklmnopqrstuvwxyz") {
				t.Fatal("diagnostic disclosed caller content")
			}
			if name == "secret" && !errors.Is(err, ledger.ErrSecretSafety) {
				t.Fatalf("core secret error lost: %v", err)
			}
		})
	}
}

func TestValidatePayloadPinsSchemaIdentity(t *testing.T) {
	input := validationCorpus(t)["open-finding"]
	batch := envelope(t, input)
	event := batch["events"].([]any)[0].(map[string]any)
	payload := event["payload"].(map[string]any)
	digest := event["schema_ref"].(string)
	for _, schema := range []string{"pact:core/generic-object/v1", "sha256:" + strings.Repeat("0", 64), canonical.Digest(ProfileArtifacts()["review.schema.json"])} {
		if err := ValidatePayload(context.Background(), CheckExecuted, schema, payload, 1); err == nil {
			t.Fatal("unsupported schema accepted")
		} else {
			assertValidationError(t, err)
		}
	}
	if err := ValidatePayload(context.Background(), "external.event", digest, payload, 1); err == nil {
		t.Fatal("unknown domain checked as development")
	}
	if err := ValidatePayload(context.Background(), CheckExecuted, digest, payload, 1); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeInputRejectsNonObjectAndExtraFields(t *testing.T) {
	input := validationCorpus(t)["open-finding"]
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(raw)
	for _, raw := range []string{"[]", "null", valid + valid, `{"secret": "private-marker"}`, strings.Replace(valid, `"operation_id":`, `"profile":"other", "operation_id":`, 1), strings.Replace(valid, `"type":`, `"schema_ref":"sha256:bad", "type":`, 1), strings.Replace(valid, `"revision":`, `"extra":"private-marker", "revision":`, 1), strings.Replace(valid, `"operation_id":`, `"operation_id":"duplicate", "operation_id":`, 1)} {
		_, err := DecodeInput(context.Background(), []byte(raw))
		if err == nil {
			t.Fatal("invalid shape accepted")
		}
		assertValidationError(t, err)
		if strings.Contains(err.Error(), "private-marker") {
			t.Fatal("diagnostic disclosed input")
		}
	}
	decoded, err := DecodeInput(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateInput(context.Background(), decoded); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeArrayExplainsRecordObject(t *testing.T) {
	_, err := DecodeInput(context.Background(), []byte(`[]`))
	for _, field := range []string{"operation_id", "namespace", "subject", "source", "events"} {
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("missing object-shape guidance for %s: %v", field, err)
		}
	}
}

func TestValidationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := validationCorpus(t)["open-finding"]
	if _, err := ValidateInput(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := DecodeInput(ctx, []byte(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("decode cancellation: %v", err)
	}
	if err := ValidatePayload(ctx, CheckExecuted, "bad", nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("payload cancellation: %v", err)
	}
}

func TestInconclusiveFixtureHasNoDanglingFinding(t *testing.T) {
	input := validationCorpus(t)["inconclusive"]
	for _, event := range input.Events {
		if event.Type == NextActionRecorded && (len(event.Payload["blocking_findings"].([]any)) != 0 || strings.Contains(event.Payload["action"].(string), "finding-1")) {
			t.Fatal("inconclusive action refers to absent finding-1")
		}
	}
}

func TestValidatePayloadRequiredFieldsAndClosedNestedObjects(t *testing.T) {
	for _, name := range []string{"open-finding", "corrected"} {
		batch := envelope(t, validationCorpus(t)[name])
		for _, raw := range batch["events"].([]any) {
			event := raw.(map[string]any)
			eventType, digest := event["type"].(string), event["schema_ref"].(string)
			original := event["payload"].(map[string]any)
			for field, definition := range EventDefinitions()[eventType].Fields {
				if !definition.Required {
					continue
				}
				t.Run(eventType+"/missing-"+field, func(t *testing.T) {
					payload := make(map[string]any)
					maps.Copy(payload, original)
					delete(payload, field)
					if err := ValidatePayload(context.Background(), eventType, digest, payload, 1); err == nil {
						t.Fatal("missing required field accepted")
					}
				})
			}
		}
	}
	cases := map[string]func(*RecordInput){
		"review-verdict": func(v *RecordInput) { v.Events[1].Payload["verdict"] = "partial" },
		"finding-extra":  func(v *RecordInput) { v.Events[1].Payload["findings"].([]any)[0].(map[string]any)["extra"] = true },
		"finding-ref-extra": func(v *RecordInput) {
			v.Events[2].Payload["blocking_findings"].([]any)[0].(map[string]any)["extra"] = true
		},
		"finding-ref-malformed": func(v *RecordInput) {
			v.Events[2].Payload["blocking_findings"].([]any)[0].(map[string]any)["review_event_ref"] = "local:bad\n"
		},
		"evidence-envelope-extra": func(v *RecordInput) { v.Events[0].Evidence[0]["extra"] = true },
		"duplicate-local-id":      func(v *RecordInput) { v.Events[1].LocalID = v.Events[0].LocalID },
		"null-causes":             func(v *RecordInput) { v.Events[0].CausedBy = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := validationCorpus(t)["open-finding"]
			mutate(&input)
			if _, err := ValidateInput(context.Background(), input); err == nil {
				t.Fatal("invalid nested contract accepted")
			}
		})
	}
	for _, mutate := range []func(*RecordInput){
		func(v *RecordInput) {
			v.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["evidence_indexes"] = []any{}
		},
		func(v *RecordInput) {
			v.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["correction_source"].(map[string]any)["revision"] = "short"
		},
		func(v *RecordInput) { v.Events[2].Payload["check_refs"] = []any{} },
		func(v *RecordInput) { v.Events[2].Payload["review_refs"] = []any{"bad"} },
		func(v *RecordInput) { v.Events[2].Payload["policy_ref"] = "external-policy" },
		func(v *RecordInput) { v.Events[2].Payload["decision"] = "maybe" },
	} {
		input := validationCorpus(t)["corrected"]
		mutate(&input)
		if _, err := ValidateInput(context.Background(), input); err == nil {
			t.Fatal("invalid resolution/acceptance accepted")
		}
	}
}

func TestValidateCheckObservationFormsAndNormalization(t *testing.T) {
	for _, observation := range []map[string]any{
		{"outcome": "interrupted", "exit_code": -1},
		{"outcome": "interrupted", "exit_code": 0, "signal": "SIGTERM"},
		{"outcome": "interrupted", "interruption": "process terminated"},
		{"outcome": "unknown"}, {"outcome": "not_run"},
	} {
		input := validationCorpus(t)["open-finding"]
		input.Source.Revision = strings.Repeat("a", 64)
		payload := input.Events[0].Payload
		delete(payload, "exit_code")
		maps.Copy(payload, observation)
		payload["summary"] = "cafe\u0301"
		batch, err := ValidateInput(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if event.LocalID == "check" && event.Payload["summary"] != "café" {
				t.Fatal("core normalization lost")
			}
		}
	}
}
