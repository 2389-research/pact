// ABOUTME: Proves a compiled pact binary can redirect its embedded skill to a standalone file.
// ABOUTME: Compares exact canonical bytes and current Go CLI instructions without a repository.
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuickstartCompiledBinaryWritesCanonicalSkill(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	outputPath := filepath.Join(workspace, "SKILL.md")
	output, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "quickstart")
	command.Dir = workspace
	command.Stdout = output
	var stderr bytes.Buffer
	command.Stderr = &stderr
	runErr := command.Run()
	closeErr := output.Close()
	if runErr != nil || closeErr != nil || stderr.Len() != 0 {
		t.Fatalf("compiled quickstart = run %v, close %v, stderr=%q", runErr, closeErr, stderr.String())
	}

	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "docs", "pact-ledger", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("compiled quickstart output differs from canonical SKILL.md")
	}
	if strings.Contains(string(got), "python3") || !strings.Contains(string(got), "pact setup") {
		t.Fatal("compiled quickstart emitted a legacy skill")
	}
}
