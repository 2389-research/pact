// ABOUTME: Exercises the validation library in a compiled process before real signing and admission.
// ABOUTME: Proves invalid corpus inputs preserve canonical and trust bytes without loading a key.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"pact/internal/canonical"
	"pact/internal/development"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

func TestDevelopmentValidationPipeline(t *testing.T) {
	root, workspace := projectRoot(t), t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo, key := filepath.Join(workspace, "repo"), filepath.Join(workspace, "key.json")
	mustMkdir(t, repo)
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/development", "--actor", "Validation Fixture", "--key-file", key, "--json")
	var inputs map[string]json.RawMessage
	var expected map[string]struct {
		DomainValid bool `json:"domain_valid"`
	}
	for name, target := range map[string]any{"inputs.json": &inputs, "expected.json": &expected} {
		raw, err := os.ReadFile(filepath.Join(root, "tests/fixtures/development", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	// This executable is a compiled Go test driver; it calls the real library and store.
	driver, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range inputs {
		t.Run(name, func(t *testing.T) {
			before := validationStoreSnapshot(t, repo)
			inputPath, outputPath := filepath.Join(workspace, name+".json"), filepath.Join(workspace, name+"-result.json")
			if err := os.WriteFile(inputPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			keyPath := key
			if !expected[name].DomainValid {
				keyPath = filepath.Join(workspace, "key-must-never-be-opened")
			}
			command := exec.Command(driver, "-test.run=^TestDevelopmentValidationDriver$")
			command.Env = append(os.Environ(), "PACT_VALIDATION_DRIVER=1", "PACT_VALIDATION_INPUT="+inputPath, "PACT_VALIDATION_REPO="+repo, "PACT_VALIDATION_KEY="+keyPath, "PACT_VALIDATION_OUTPUT="+outputPath)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("compiled pipeline: %v\n%s", err, output)
			}
			resultRaw, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(resultRaw, &result); err != nil {
				t.Fatal(err)
			}
			if !expected[name].DomainValid {
				if result["validation"] != "rejected" {
					t.Fatalf("result = %s", resultRaw)
				}
				if !reflect.DeepEqual(before, validationStoreSnapshot(t, repo)) {
					t.Fatal("rejected input changed canonical or trust bytes")
				}
				return
			}
			if result["validation"] != "valid" || result["integrity"] != "valid" || result["authenticity"] != "valid" {
				t.Fatalf("result = %s", resultRaw)
			}
			runJSON(t, binary, "show", "--repo", repo, result["event_ref"].(string), "--json")
		})
	}
	runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
}

func TestDevelopmentValidationDriver(t *testing.T) {
	if os.Getenv("PACT_VALIDATION_DRIVER") != "1" {
		return
	}
	raw, err := os.ReadFile(os.Getenv("PACT_VALIDATION_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := development.DecodeInput(context.Background(), raw)
	var batch ledger.EventBatch
	if err == nil {
		batch, err = development.ValidateInput(context.Background(), input)
	}
	result := map[string]any{}
	if err != nil {
		var diagnostic *development.ValidationError
		if !errors.As(err, &diagnostic) {
			t.Fatal(err)
		}
		result["validation"], result["category"], result["path"] = "rejected", diagnostic.Category, diagnostic.Path
	} else {
		result = admitValidationBatch(t, batch)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("PACT_VALIDATION_OUTPUT"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func admitValidationBatch(t *testing.T, batch ledger.EventBatch) map[string]any {
	t.Helper()
	// No key or store handle is opened until domain validation has succeeded.
	repo := os.Getenv("PACT_VALIDATION_REPO")
	key, err := identity.LoadSigningKey(os.Getenv("PACT_VALIDATION_KEY"), repo)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := ledger.Commit(st, key, batch, ledger.CommitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ref := ledger.EventRef(admitted.ObjectID, batch.Events[0].LocalID)
	shown, err := ledger.Show(st, ref)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"validation": "valid", "event_ref": ref, "integrity": shown.Integrity, "authenticity": shown.Authenticity}
}

func validationStoreSnapshot(t *testing.T, repo string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(filepath.Join(repo, ".pact"), func(path string, entry fs.DirEntry, err error) error {
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
