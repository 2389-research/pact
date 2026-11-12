// ABOUTME: Checks the development profile's exact artifacts and public scenario contract.
// ABOUTME: Exercises real signing and the reference Python schema oracle without domain claims.
package development

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

func TestProfileArtifactsArePublished(t *testing.T) {
	artifacts := ProfileArtifacts()
	if len(artifacts) != 5 {
		t.Fatalf("artifacts = %d, want four schemas and one policy", len(artifacts))
	}
	var digests map[string]string
	readJSON(t, "schema/digests.json", &digests)
	if len(digests) != len(artifacts) {
		t.Fatal("digest manifest does not cover exact artifact set")
	}
	generated := t.TempDir()
	command := exec.Command("go", "run", "./cmd/profilegen", "-out", generated)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate: %v\n%s", err, output)
	}
	for name, raw := range artifacts {
		for _, dir := range []string{"schema", generated} {
			published, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, published) {
				t.Errorf("%s differs from shared definition", filepath.Join(dir, name))
			}
			if canonical.Digest(published) != digests[name] {
				t.Errorf("digest drift for %s", name)
			}
		}
	}
	// Callers receive fresh definitions and bytes, never mutable global vocabulary.
	artifacts["check.schema.json"][0] = '!'
	definitions := EventDefinitions()
	delete(definitions[CheckExecuted].Fields, "outcome")
	if ProfileArtifacts()["check.schema.json"][0] != '{' || EventDefinitions()[CheckExecuted].Fields["outcome"].Schema == nil {
		t.Fatal("profile state leaked across calls")
	}
}

func TestProfileCorpusAgainstReferenceJSONSchema(t *testing.T) {
	root := filepath.Join("..", "..")
	command := exec.Command("uv", "run", "--with-requirements", filepath.Join(root, "docs/pact-ledger/requirements-dev.txt"), "python", filepath.Join(root, "tests/fixtures/development/oracle.py"), root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("reference oracle: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}

func TestPositiveCorpusSignsAndReadsThroughRawLedger(t *testing.T) {
	var inputs map[string]RecordInput
	readJSON(t, "../../tests/fixtures/development/inputs.json", &inputs)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	initialized, err := store.Init(t.TempDir(), inputs["open-finding"].Namespace, now)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "fixture.key.json"), "Synthetic Fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	st, key := initialized.Store, generated.Key
	if _, err := ledger.AddRoot(st, key, now); err != nil {
		t.Fatal(err)
	}
	priorReview := ""
	for _, name := range []string{"open-finding", "corrected"} {
		input := inputs[name]
		if name == "corrected" {
			input.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = priorReview
		}
		batch, err := ledger.NormalizeEventBatch(envelope(t, input))
		if err != nil {
			t.Fatal(err)
		}
		admitted, err := ledger.Commit(st, key, batch, ledger.CommitOptions{ObservedAt: now.Format(time.RFC3339)})
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range input.Events {
			ref := ledger.EventRef(admitted.ObjectID, event.LocalID)
			if event.LocalID == "review" {
				priorReview = ref
			}
			shown, err := ledger.Show(st, ref)
			if err != nil {
				t.Fatal(err)
			}
			if shown.Integrity != "valid" || shown.Authenticity != "valid" {
				t.Fatalf("unverified event: %#v", shown)
			}
			payload := shown.Event["payload"].(map[string]any)
			if payload["operation_id"] != input.OperationID || payload["source"].(map[string]any)["revision"] != input.Source.Revision {
				t.Fatalf("source/operation lost: %#v", payload)
			}
			got, _ := canonical.Marshal(payload["evidence_indexes"])
			want, _ := canonical.Marshal(event.Payload["evidence_indexes"])
			if !bytes.Equal(got, want) {
				t.Fatal("evidence indexes changed")
			}
		}
	}
	verified, err := ledger.Verify(st, true)
	if err != nil || !verified.OK {
		t.Fatalf("strict verify: %#v %v", verified, err)
	}
}

// envelope is a test adapter, not domain validation or the future recording API.
func envelope(t *testing.T, input RecordInput) map[string]any {
	t.Helper()
	events := make([]any, 0, len(input.Events))
	artifacts, definitions := ProfileArtifacts(), EventDefinitions()
	for _, event := range input.Events {
		definition := definitions[event.Type]
		payload := make(map[string]any, len(event.Payload)+2)
		maps.Copy(payload, event.Payload)
		payload["operation_id"] = input.OperationID
		payload["source"] = map[string]any{"repository": input.Source.Repository, "revision": input.Source.Revision}
		events = append(events, map[string]any{"local_id": event.LocalID, "type": event.Type, "kind": definition.Kind, "subject": input.Subject, "schema_ref": canonical.Digest(artifacts[definition.Artifact]), "payload": payload, "evidence": event.Evidence, "caused_by": event.CausedBy, "supersedes": event.Supersedes, "tags": []string{}})
	}
	// Use JSON's untyped shape, exactly as the existing CLI decoder does.
	raw, err := json.Marshal(map[string]any{"namespace": input.Namespace, "events": events})
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return result.(map[string]any)
}

func readJSON(t *testing.T, path string, dest any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(dest); err != nil {
		t.Fatal(err)
	}
}
