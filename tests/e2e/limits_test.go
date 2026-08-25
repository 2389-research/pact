// ABOUTME: Exercises reachable Phase 2 resource, path, concurrency, and interruption boundaries.
// ABOUTME: Runs compiled pact processes over real repositories and sparse or derived files without mocks.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestCLILimitBoundariesAndSafeDiagnostics(t *testing.T) { //nolint:funlen // The CLI boundary matrix is clearest beside its exact values.
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo, _, _ := indexedRepository(t, binary, workspace)

	perFamily := make([]string, 0, 133)
	perFamily = append(perFamily, "query", "--repo", repo)
	for index := range 64 {
		perFamily = append(perFamily, "--type", fmt.Sprintf("limit.type.%03d", index))
	}
	runJSON(t, binary, append(perFamily, "--json")...)
	overFamily := append(append([]string(nil), perFamily...), "--type", "limit.type.overflow", "--json")
	assertResourceLimit(t, runErrorJSON(t, 9, binary, overFamily...), "filter_values_per_family", 64)

	total := make([]string, 0, 517)
	total = append(total, "query", "--repo", repo)
	for index := range 64 {
		total = append(total,
			"--namespace", fmt.Sprintf("org/limit/%03d", index),
			"--type", fmt.Sprintf("limit.total.%03d", index),
			"--subject", fmt.Sprintf("subject/limit/%03d", index),
			"--tag", fmt.Sprintf("tag-%03d", index),
		)
	}
	runJSON(t, binary, append(total, "--json")...)
	overTotal := append(append([]string(nil), total...), "--kind", "observation", "--json")
	assertResourceLimit(t, runErrorJSON(t, 9, binary, overTotal...), "filter_values_total", 256)

	runJSON(t, binary, "query", "--repo", repo, "--subject", "state/original", "--limit", "1000", "--json")
	for _, value := range []string{"1001", "not-an-integer"} {
		failure := runErrorJSON(t, 2, binary, "query", "--repo", repo, "--subject", "state/original", "--limit", value, "--json")
		if strings.Contains(stableJSON(t, failure), "state/original") {
			t.Fatalf("usage diagnostic echoed filters: %#v", failure)
		}
	}

	oversizedCursor := "cursor-secret-marker-" + strings.Repeat("x", 4_097)
	cursorFailure := runErrorJSON(t, 2, binary, "query", "--repo", repo, "--subject", "state/original", "--cursor", oversizedCursor, "--json")
	if cursorFailure["details"].(map[string]any)["code"] != "cursor_invalid" || strings.Contains(stableJSON(t, cursorFailure), oversizedCursor) || strings.Contains(stableJSON(t, cursorFailure), "cursor-secret-marker") {
		t.Fatalf("oversized cursor diagnostic is unsafe: %#v", cursorFailure)
	}

	secretFilter := "Bearer abcdefghijklmnopqrstuvwxyz0123456789"
	secretFailure := runErrorJSON(t, 2, binary, "query", "--repo", repo, "--subject", secretFilter, "--json")
	if strings.Contains(stableJSON(t, secretFailure), secretFilter) || strings.Contains(stableJSON(t, secretFailure), "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret-like filter diagnostic echoed input: %#v", secretFailure)
	}
}

func TestCLISymlinkMalformedAndOversizedPaths(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)

	t.Run("malformed object shard", func(t *testing.T) {
		repo, _, _ := initializeIndexRepository(t, binary, t.TempDir(), "org/example/path")
		badDirectory := filepath.Join(repo, ".pact", "objects", "sha256", "not-a-shard")
		mustMkdir(t, badDirectory)
		if err := os.WriteFile(filepath.Join(badDirectory, "object.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := protectedRepositoryDigest(t, repo)
		failure := runErrorJSON(t, 4, binary, "index", "rebuild", "--repo", repo, "--json")
		if failure["details"].(map[string]any)["code"] != "source_invalid" {
			t.Fatalf("malformed shard rebuild = %#v, want source_invalid", failure)
		}
		if after := protectedRepositoryDigest(t, repo); stableJSON(t, after) != stableJSON(t, before) {
			t.Fatalf("malformed shard rebuild mutated canonical bytes")
		}
	})

	t.Run("symlinked canonical path", func(t *testing.T) {
		repo, _, _ := initializeIndexRepository(t, binary, t.TempDir(), "org/example/path")
		shards := filepath.Join(repo, ".pact", "objects", "sha256")
		if err := os.Remove(shards); err != nil {
			t.Fatal(err)
		}
		target := t.TempDir()
		if err := os.Symlink(target, shards); err != nil {
			t.Fatal(err)
		}
		failure := runErrorJSON(t, 4, binary, "index", "rebuild", "--repo", repo, "--json")
		if failure["details"].(map[string]any)["code"] != "source_invalid" {
			t.Fatalf("symlinked canonical rebuild = %#v, want source_invalid", failure)
		}
	})

	t.Run("symlinked index path", func(t *testing.T) {
		repo, _, _ := initializeIndexRepository(t, binary, t.TempDir(), "org/example/path")
		indexDirectory := filepath.Join(repo, ".pact", "index")
		if err := os.Remove(indexDirectory); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), indexDirectory); err != nil {
			t.Fatal(err)
		}
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "corrupt", "unavailable", true)
		runErrorJSON(t, 10, binary, "index", "rebuild", "--repo", repo, "--json")
	})

	t.Run("sparse oversized SQLite", func(t *testing.T) {
		repo, _, _ := indexedRepository(t, binary, t.TempDir())
		before := protectedRepositoryDigest(t, repo)
		if err := os.Truncate(liveIndexPath(repo), 2_147_483_649); err != nil {
			t.Fatal(err)
		}
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "corrupt", "unavailable", true)
		if after := protectedRepositoryDigest(t, repo); stableJSON(t, after) != stableJSON(t, before) {
			t.Fatal("oversized SQLite status mutated canonical bytes")
		}
	})
}

func TestCLISignedLocalCausedByCycleIsCanonicalFailure(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo, keyPath, _ := initializeIndexRepository(t, binary, workspace, "org/example/cycle")

	eventA := phase2Event("a", "cycle.local", "cycle/a", "observation")
	eventA["caused_by"] = []any{"local:b"}
	eventB := phase2Event("b", "cycle.local", "cycle/b", "observation")
	eventB["caused_by"] = []any{"local:a"}
	committed := commitEvents(t, binary, workspace, repo, keyPath, "cycle.json", "org/example/cycle", "2026-08-24T15:00:00Z", nil, eventA, eventB)
	if len(committed["event_refs"].([]any)) != 2 {
		t.Fatalf("cycle commit admission = %#v", committed)
	}

	protectedBefore := protectedRepositoryDigest(t, repo)
	verified := runErrorJSON(t, 4, binary, "verify", "--repo", repo, "--strict", "--json")
	if !strings.Contains(stableJSON(t, verified), "caused_by cycle") {
		t.Fatalf("strict cycle verification = %#v", verified)
	}
	rebuild := runErrorJSON(t, 4, binary, "index", "rebuild", "--repo", repo, "--json")
	if rebuild["details"].(map[string]any)["code"] != "source_invalid" {
		t.Fatalf("cycle rebuild = %#v, want source_invalid", rebuild)
	}
	if _, err := os.Stat(liveIndexPath(repo)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cycle rebuild published a usable index: %v", err)
	}
	if protectedAfter := protectedRepositoryDigest(t, repo); !reflect.DeepEqual(protectedAfter, protectedBefore) {
		t.Fatalf("cycle refusal mutated protected bytes\nbefore=%#v\nafter=%#v", protectedBefore, protectedAfter)
	}
}

func TestCLIConcurrentAndInterruptedIndexOperations(t *testing.T) { //nolint:funlen // Real process races and termination share one compiled binary.
	if runtime.GOOS == "windows" {
		t.Skip("Windows process termination does not provide the required pre-rename signal semantics")
	}
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)

	t.Run("query and rebuild serialize safely", func(t *testing.T) {
		repo, _, _ := indexedRepository(t, binary, t.TempDir())
		commands := [][]string{
			{"query", "--repo", repo, "--subject", "state/original", "--json"},
			{"index", "rebuild", "--repo", repo, "--json"},
		}
		results := runProcessesTogether(t, binary, commands)
		for index, result := range results {
			if result.exitCode != 0 || len(result.stderr) != 0 {
				t.Fatalf("concurrent command %v = exit %d stdout=%q stderr=%q", commands[index], result.exitCode, result.stdout, result.stderr)
			}
			decodeOneJSON(t, result.stdout)
		}
	})

	t.Run("commit rebuild and query return contract states", func(t *testing.T) {
		fixture := t.TempDir()
		repo, keyPath, _ := indexedRepository(t, binary, fixture)
		batch := writePhase2Batch(t, fixture, "race-commit.json", phase2Event("race", "state.race", "state/race", "action"))
		commands := [][]string{
			{"commit", "--repo", repo, "--key-file", keyPath, "--events", batch, "--namespace", "org/example/state", "--json"},
			{"index", "rebuild", "--repo", repo, "--json"},
			{"query", "--repo", repo, "--subject", "state/original", "--json"},
		}
		results := runProcessesTogether(t, binary, commands)
		for index, result := range results {
			if result.exitCode == 0 {
				if len(result.stderr) != 0 {
					t.Fatalf("successful concurrent command %v wrote stderr %q", commands[index], result.stderr)
				}
				decodeOneJSON(t, result.stdout)
				continue
			}
			if index != 2 || result.exitCode != 9 || len(result.stdout) != 0 {
				t.Fatalf("concurrent command %v = exit %d stdout=%q stderr=%q", commands[index], result.exitCode, result.stdout, result.stderr)
			}
			diagnostic := decodeOneJSON(t, result.stderr)
			if diagnostic["details"].(map[string]any)["code"] != "index_stale" {
				t.Fatalf("concurrent query refusal = %#v", diagnostic)
			}
		}
		runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
		if verify := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json"); verify["ok"] != true || verify["index_status"] != "current" {
			t.Fatalf("final race verify = %#v", verify)
		}
	})

	t.Run("interrupted rebuild preserves prior live index", func(t *testing.T) {
		fixture := t.TempDir()
		repo, keyPath, _ := indexedRepository(t, binary, fixture)
		liveBefore, err := os.ReadFile(liveIndexPath(repo))
		if err != nil {
			t.Fatal(err)
		}
		for batchIndex := range 4 {
			events := make([]map[string]any, 1_024)
			for eventIndex := range events {
				events[eventIndex] = phase2Event(fmt.Sprintf("bulk-%d-%04d", batchIndex, eventIndex), "state.bulk", fmt.Sprintf("state/bulk/%d/%04d", batchIndex, eventIndex), "observation")
				events[eventIndex]["tags"] = []any{}
			}
			commitEvents(t, binary, fixture, repo, keyPath, fmt.Sprintf("bulk-%d.json", batchIndex), "org/example/state", fmt.Sprintf("2026-08-24T14:%02d:00Z", batchIndex), nil, events...)
		}
		command := exec.Command(binary, "index", "rebuild", "--repo", repo, "--json")
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		completion := make(chan error, 1)
		go func() { completion <- command.Wait() }()
		buildPath := waitForBuildFile(t, filepath.Join(repo, ".pact", "index"), command, completion)
		if err := command.Process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatalf("stop rebuild before publication: %v", err)
		}
		if err := command.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := <-completion; err == nil {
			t.Fatal("interrupted rebuild unexpectedly succeeded")
		}
		if !strings.HasPrefix(filepath.Base(buildPath), ".build-") || !strings.HasSuffix(buildPath, ".sqlite3") {
			t.Fatalf("observed unsafe build path %q", buildPath)
		}
		liveAfter, err := os.ReadFile(liveIndexPath(repo))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(liveBefore, liveAfter) {
			t.Fatal("interrupted rebuild replaced prior live bytes")
		}
		status := runJSON(t, binary, "index", "status", "--repo", repo, "--json")
		assertIndexState(t, status, "stale", "unavailable", true)
		runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "current", "complete", false)
	})
}

type processResult struct {
	stdout, stderr []byte
	exitCode       int
}

func runProcessesTogether(t *testing.T, binary string, arguments [][]string) []processResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]processResult, len(arguments))
	var group sync.WaitGroup
	for index := range arguments {
		group.Go(func() {
			<-start
			command := exec.Command(binary, arguments[index]...)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			exitCode := 0
			if err != nil {
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) {
					exitCode = -1
				} else {
					exitCode = exitError.ExitCode()
				}
			}
			results[index] = processResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
		})
	}
	close(start)
	group.Wait()
	return results
}

func waitForBuildFile(t *testing.T, directory string, command *exec.Cmd, completion <-chan error) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-completion:
			t.Fatalf("rebuild exited before a build file became observable: %v", err)
		default:
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".build-") && strings.HasSuffix(entry.Name(), ".sqlite3") && !entry.IsDir() {
				return filepath.Join(directory, entry.Name())
			}
		}
		time.Sleep(time.Millisecond)
	}
	_ = command.Process.Kill()
	<-completion
	t.Fatal("timed out waiting for exact index build file")
	return ""
}

func writePhase2Batch(t *testing.T, directory, name string, events ...map[string]any) string {
	t.Helper()
	items := make([]any, len(events))
	for index := range events {
		items[index] = events[index]
	}
	raw, err := json.Marshal(map[string]any{"events": items})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertResourceLimit(t *testing.T, failure map[string]any, resource string, maximum float64) {
	t.Helper()
	details := failure["details"].(map[string]any)
	if details["code"] != "resource_limit" || details["resource"] != resource || details["maximum"] != maximum || details["observed_at_least"] != maximum+1 {
		t.Fatalf("resource limit = %#v, want %s maximum %.0f", failure, resource, maximum)
	}
}

func decodeOneJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("multiple JSON values in %q", raw)
	}
	return value
}
