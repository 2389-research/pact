// ABOUTME: Proves setup through compiled nonterminal processes and real pseudo-terminals.
// ABOUTME: Uses real repositories, keys, trust roots, indexes, aliases, and process races.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

const setupCancelledMarker = "cancelled" //nolint:misspell // The setup wire contract uses this spelling.

func TestSetupCompiledLifecycle(t *testing.T) { //nolint:funlen // One built binary drives the independent lifecycle scenarios below.
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)

	t.Run("fresh rerun partial conflicts and refusals", func(t *testing.T) {
		repo := filepath.Join(workspace, "project")
		keyFile := filepath.Join(workspace, "alice.key.json")
		first := runSetupProcess(t, binary, workspace, "setup", "--repo", repo, "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile, "--json")
		assertSetupProcessSuccess(t, first, []string{"created", "created", "created", "valid", "created"})
		before := snapshotSetupFiles(t, repo, keyFile)

		rerun := runSetupProcess(t, binary, workspace, "setup", "--repo", repo, "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile, "--json")
		assertSetupProcessSuccess(t, rerun, []string{"existing", "existing", "existing", "valid", "current"})
		if after := snapshotSetupFiles(t, repo, keyFile); !reflect.DeepEqual(after, before) {
			t.Fatalf("exact setup rerun changed bytes\nbefore=%#v\nafter=%#v", before, after)
		}
		assertSetupOutputSafe(t, first, rerun)
		assertSetupPrivateValueAbsent(t, keyFile, first.Stdout, first.Stderr, rerun.Stdout, rerun.Stderr)
		assertNoPrivateKey(t, repo)

		partialRepo := filepath.Join(workspace, "partial")
		partialKey := filepath.Join(workspace, "partial.key.json")
		runJSON(t, binary, "init", "--repo", partialRepo, "--namespace", "org/example/partial", "--json")
		partial := runSetupProcess(t, binary, workspace, "setup", "--repo", partialRepo, "--namespace", "org/example/partial", "--actor", "Pat", "--key-file", partialKey, "--json")
		assertSetupProcessSuccess(t, partial, []string{"existing", "created", "created", "valid", "created"})

		conflictKey := filepath.Join(workspace, "conflict.key.json")
		conflict := runSetupProcess(t, binary, workspace, "setup", "--repo", repo, "--namespace", "org/example/other", "--actor", "Mallory", "--key-file", conflictKey, "--json")
		if conflict.Code == 0 || conflict.Stdout != "" {
			t.Fatalf("namespace conflict = %#v", conflict)
		}
		if after := snapshotSetupFiles(t, repo, keyFile); !reflect.DeepEqual(after, before) {
			t.Fatalf("namespace conflict changed winner bytes")
		}
		if _, err := os.Lstat(conflictKey); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("namespace conflict wrote losing key: %v", err)
		}

		missingRepo := filepath.Join(workspace, "missing")
		missing := runSetupProcess(t, binary, workspace, "setup", "--repo", missingRepo, "--namespace", "org/example/missing")
		if missing.Code != 2 || missing.Stdout != "" || !strings.Contains(missing.Stderr, "requires --namespace, --actor, and --key-file") {
			t.Fatalf("missing nonterminal input = %#v", missing)
		}
		if _, err := os.Lstat(missingRepo); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing nonterminal input wrote repo: %v", err)
		}

		unsafeRepo := filepath.Join(workspace, "unsafe")
		unsafeKey := filepath.Join(unsafeRepo, "inside.key.json")
		unsafe := runSetupProcess(t, binary, workspace, "setup", "--repo", unsafeRepo, "--namespace", "org/example/unsafe", "--actor", "Unsafe", "--key-file", unsafeKey, "--json")
		if unsafe.Code == 0 || unsafe.Stdout != "" || !strings.Contains(unsafe.Stderr, "safety") {
			t.Fatalf("unsafe key path = %#v", unsafe)
		}
		if _, err := os.Lstat(unsafeRepo); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unsafe setup wrote repo: %v", err)
		}

		corruptRepo := filepath.Join(workspace, "corrupt")
		mustMkdir(t, filepath.Join(corruptRepo, ".pact"))
		formatPath := filepath.Join(corruptRepo, ".pact", "format.json")
		if err := os.WriteFile(formatPath, []byte("corrupt-local-state"), 0o644); err != nil {
			t.Fatal(err)
		}
		corruptKey := filepath.Join(workspace, "corrupt.key.json")
		corrupt := runSetupProcess(t, binary, workspace, "setup", "--repo", corruptRepo, "--namespace", "org/example/corrupt", "--actor", "Corrupt", "--key-file", corruptKey, "--json")
		if corrupt.Code == 0 || corrupt.Stdout != "" {
			t.Fatalf("corrupt local state = %#v", corrupt)
		}
		raw, err := os.ReadFile(formatPath)
		if err != nil || string(raw) != "corrupt-local-state" {
			t.Fatalf("corrupt state changed = %q, %v", raw, err)
		}
		if _, err := os.Lstat(corruptKey); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("corrupt setup wrote key: %v", err)
		}
		assertSetupOutputSafe(t, conflict, missing, unsafe, corrupt)
	})

	t.Run("canonical and symlink process races", func(t *testing.T) {
		t.Run("identical", func(t *testing.T) {
			repo := filepath.Join(workspace, "identical")
			mustMkdir(t, repo)
			alias := filepath.Join(workspace, "identical-link")
			if err := os.Symlink(repo, alias); err != nil {
				t.Fatal(err)
			}
			keyFile := filepath.Join(workspace, "identical.key.json")
			results := runConcurrentSetupProcesses(t, binary, workspace, [][]string{
				{"setup", "--repo", repo, "--namespace", "org/example/race", "--actor", "Race", "--key-file", keyFile, "--json"},
				{"setup", "--repo", alias, "--namespace", "org/example/race", "--actor", "Race", "--key-file", keyFile, "--json"},
			})
			for _, result := range results {
				if result.Code != 0 || result.Stderr != "" {
					t.Fatalf("identical setup race = %#v", results)
				}
			}
			verify := runSetupProcess(t, binary, workspace, "verify", "--repo", repo, "--strict", "--json")
			if verify.Code != 0 || verify.Stderr != "" {
				t.Fatalf("verify after identical setup race = %#v", verify)
			}
			assertNoPrivateKey(t, repo)
			assertSetupOutputSafe(t, results...)
		})

		t.Run("conflicting", func(t *testing.T) {
			repo := filepath.Join(workspace, "conflicting")
			mustMkdir(t, repo)
			alias := filepath.Join(workspace, "conflicting-link")
			if err := os.Symlink(repo, alias); err != nil {
				t.Fatal(err)
			}
			results := runConcurrentSetupProcesses(t, binary, workspace, [][]string{
				{"setup", "--repo", repo, "--namespace", "org/example/one", "--actor", "One", "--key-file", filepath.Join(workspace, "one.key.json"), "--json"},
				{"setup", "--repo", alias, "--namespace", "org/example/two", "--actor", "Two", "--key-file", filepath.Join(workspace, "two.key.json"), "--json"},
			})
			successes := 0
			for _, result := range results {
				if result.Code == 0 {
					successes++
				} else if result.Stdout != "" {
					t.Fatalf("conflicting setup failure wrote stdout: %#v", result)
				}
			}
			if successes != 1 {
				t.Fatalf("conflicting setup successes = %d, results=%#v", successes, results)
			}
			verify := runSetupProcess(t, binary, workspace, "verify", "--repo", repo, "--strict", "--json")
			if verify.Code != 0 || verify.Stderr != "" {
				t.Fatalf("verify after conflicting setup race = %#v", verify)
			}
			assertNoPrivateKey(t, repo)
			assertSetupOutputSafe(t, results...)
		})
	})
}

func TestSetupRealPTYPromptContract(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)

	t.Run("cancel", func(t *testing.T) {
		repo := filepath.Join(workspace, "pty-cancel")
		keyFile := filepath.Join(workspace, "pty-cancel.key.json")
		transcript := runSetupPTY(t, binary, workspace, repo, keyFile, "no", setupCancelledMarker)
		assertSetupPTYTranscript(t, transcript)
		if _, err := os.Lstat(repo); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancel PTY wrote repo: %v", err)
		}
		if _, err := os.Lstat(keyFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancel PTY wrote key: %v", err)
		}
	})

	t.Run("accept", func(t *testing.T) {
		repo := filepath.Join(workspace, "pty-accept")
		keyFile := filepath.Join(workspace, "pty-accept.key.json")
		transcript := runSetupPTY(t, binary, workspace, repo, keyFile, "yes", "ready")
		assertSetupPTYTranscript(t, transcript)
		for _, status := range []string{"store   created", "key     created", "trust   created", "verify  valid", "index   created"} {
			if !strings.Contains(transcript, status) {
				t.Fatalf("accepted PTY lacks %q: %q", status, transcript)
			}
		}
		assertNoPrivateKey(t, repo)
		assertSetupPrivateValueAbsent(t, keyFile, transcript)
		if strings.Contains(transcript, "private_key") {
			t.Fatalf("PTY transcript exposed private key field: %q", transcript)
		}
	})
}

func runSetupProcess(t *testing.T, binary, directory string, args ...string) operatorProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("pact %q timed out: %v", args, ctx.Err())
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run pact %q: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return operatorProcessResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

func runConcurrentSetupProcesses(t *testing.T, binary, directory string, commands [][]string) []operatorProcessResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]operatorProcessResult, len(commands))
	var group sync.WaitGroup
	for position := range commands {
		group.Go(func() {
			<-start
			results[position] = runSetupProcess(t, binary, directory, commands[position]...)
		})
	}
	close(start)
	group.Wait()
	return results
}

func assertSetupProcessSuccess(t *testing.T, result operatorProcessResult, statuses []string) {
	t.Helper()
	if result.Code != 0 || result.Stderr != "" {
		t.Fatalf("setup process = %#v", result)
	}
	decoded := decodeSingleOperatorJSON(t, result.Stdout)
	if decoded["operation"] != "setup" || decoded["ok"] != true || decoded["status"] != "ready" {
		t.Fatalf("setup JSON = %#v", decoded)
	}
	actions := decoded["actions"].([]any)
	if len(actions) != len(statuses) {
		t.Fatalf("setup actions = %#v", actions)
	}
	for position, status := range statuses {
		if actions[position].(map[string]any)["status"] != status {
			t.Fatalf("setup action %d = %#v, want %q", position, actions[position], status)
		}
	}
}

func snapshotSetupFiles(t *testing.T, repo, keyFile string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	for _, root := range []string{repo, keyFile} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] = raw
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func assertSetupOutputSafe(t *testing.T, results ...operatorProcessResult) {
	t.Helper()
	for _, result := range results {
		for _, output := range []string{result.Stdout, result.Stderr} {
			if strings.Contains(output, "private_key") {
				t.Fatalf("setup output exposed private key field: %q", output)
			}
		}
	}
}

func assertSetupPrivateValueAbsent(t *testing.T, keyFile string, outputs ...string) {
	t.Helper()
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	privateValue, ok := key["private_key"].(string)
	if !ok || privateValue == "" {
		t.Fatal("generated signing key lacks private material")
	}
	for _, output := range outputs {
		if strings.Contains(output, privateValue) {
			t.Fatal("setup output exposed generated private key bytes")
		}
	}
}

func runSetupPTY(t *testing.T, binary, directory, repo, keyFile, confirmation, finalMarker string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "setup", "--repo", repo)
	command.Dir = directory
	terminal, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Close() })
	var transcript bytes.Buffer
	for _, interaction := range []struct{ marker, input string }{
		{"Namespace", "org/example/pty\n"},
		{"Actor", "PTY Operator\n"},
		{"Key file", keyFile + "\n"},
		{"Continue? [y/N]", confirmation + "\n"},
	} {
		readSetupPTYUntil(t, terminal, &transcript, interaction.marker)
		if _, err := io.WriteString(terminal, interaction.input); err != nil {
			t.Fatal(err)
		}
	}
	readSetupPTYUntil(t, terminal, &transcript, finalMarker)
	if err := command.Wait(); err != nil {
		t.Fatalf("PTY setup wait: %v; transcript=%q", err, transcript.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("PTY setup timed out: %v; transcript=%q", ctx.Err(), transcript.String())
	}
	return strings.ReplaceAll(transcript.String(), "\r", "")
}

func readSetupPTYUntil(t *testing.T, terminal *os.File, transcript *bytes.Buffer, marker string) {
	t.Helper()
	buffer := make([]byte, 4096)
	for !strings.Contains(transcript.String(), marker) {
		count, err := terminal.Read(buffer)
		if count != 0 {
			_, _ = transcript.Write(buffer[:count])
		}
		if err != nil {
			t.Fatalf("read PTY through %q: %v; transcript=%q", marker, err, transcript.String())
		}
	}
}

func assertSetupPTYTranscript(t *testing.T, transcript string) {
	t.Helper()
	positions := make([]int, 0, 4)
	for _, marker := range []string{"Namespace", "Actor", "Key file", "Continue? [y/N]"} {
		position := strings.Index(transcript, marker)
		if position < 0 {
			t.Fatalf("PTY transcript lacks %q: %q", marker, transcript)
		}
		positions = append(positions, position)
	}
	for position := 1; position < len(positions); position++ {
		if positions[position-1] >= positions[position] {
			t.Fatalf("PTY prompt order = %q", transcript)
		}
	}
	if strings.Count(transcript, "PACT setup plan") != 1 || strings.Count(transcript, "Continue? [y/N]") != 1 {
		t.Fatalf("PTY transcript plan/confirmation count = %q", transcript)
	}
	planPosition := strings.Index(transcript, "PACT setup plan")
	confirmationPosition := strings.Index(transcript, "Continue? [y/N]")
	if planPosition < 0 || planPosition >= confirmationPosition {
		t.Fatalf("PTY plan does not precede confirmation: %q", transcript)
	}
	plan := transcript[planPosition:confirmationPosition]
	for _, action := range []string{"store   planned", "key     planned", "trust   planned", "verify  planned", "index   planned"} {
		if !strings.Contains(plan, action) {
			t.Fatalf("PTY plan before confirmation lacks %q: %q", action, transcript)
		}
	}
}
