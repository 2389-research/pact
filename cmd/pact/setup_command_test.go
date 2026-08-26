// ABOUTME: Exercises automated setup parsing, rendering, partial errors, and writer failures.
// ABOUTME: Uses the real setup service and files without reading or formatting private key bytes.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/identity"
	setuppkg "pact/internal/setup"
)

var setupTestNow = time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)

type refusedSetupInput struct{}

func (refusedSetupInput) Read([]byte) (int, error) {
	panic("setup read stdin unexpectedly")
}

func TestHelpListsSetupWithEveryFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runWithConfig(nil, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != 0 {
		t.Fatalf("top help exit = %d, stderr=%q", code, stderr.String())
	}
	getStarted := stdout.String()[strings.Index(stdout.String(), "Get started:"):strings.Index(stdout.String(), "Inspect:")]
	if !strings.Contains(getStarted, "setup") {
		t.Fatalf("Get started help lacks setup: %q", getStarted)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithConfig([]string{"setup", "--help"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != 0 {
		t.Fatalf("setup help exit = %d, stderr=%q", code, stderr.String())
	}
	for _, fragment := range []string{"--repo PATH", "--namespace NAMESPACE", "--actor ACTOR", "--key-file PATH", "--json"} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("setup help lacks %q: %q", fragment, stdout.String())
		}
	}
}

func TestSetupJSONUsesWorkingDirectoryWithoutReadingStdin(t *testing.T) {
	workingDir := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{
		"setup", "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile, "--json",
	}, setupRunConfig(workingDir, &stdout, &stderr, true))
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("setup JSON = exit %d, stderr=%q", code, stderr.String())
	}
	result := decodeSetupJSON(t, stdout.Bytes())
	assertSetupResult(t, result, workingDir, keyFile, []setuppkg.ActionStatus{
		setuppkg.ActionCreated, setuppkg.ActionCreated, setuppkg.ActionCreated, setuppkg.ActionValid, setuppkg.ActionCreated,
	})
	if bytes.Contains(stdout.Bytes(), []byte("private_key")) || bytes.Contains(stdout.Bytes(), []byte("public_key")) || bytes.Contains(stdout.Bytes(), []byte("\x1b[")) {
		t.Fatalf("setup JSON exposed forbidden output fields: %q", stdout.String())
	}
}

func TestSetupRedirectedHumanUsesExplicitRepositoryWithoutReadingStdin(t *testing.T) {
	workingDir := t.TempDir()
	repo := filepath.Join(t.TempDir(), "selected-project")
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{
		"setup", "--repo", repo, "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile,
	}, setupRunConfig(workingDir, &stdout, &stderr, false))
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("redirected setup = exit %d, stderr=%q", code, stderr.String())
	}
	for _, fragment := range []string{"PACT setup", "store", "key", "trust", "verify", "index", "ready", keyFile} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("human setup lacks %q: %q", fragment, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("redirected setup used color: %q", stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(workingDir, ".pact")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup ignored explicit repository: working-dir .pact err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".pact", "format.json")); err != nil {
		t.Fatalf("explicit repository was not configured: %v", err)
	}
}

func TestSetupMissingValuesExitUsageBeforeWrites(t *testing.T) {
	for _, test := range []struct {
		name, missing  string
		json, terminal bool
	}{
		{name: "JSON namespace", missing: "namespace", json: true, terminal: true},
		{name: "JSON actor", missing: "actor", json: true, terminal: true},
		{name: "JSON key file", missing: "key-file", json: true, terminal: true},
		{name: "redirected namespace", missing: "namespace"},
		{name: "redirected actor", missing: "actor"},
		{name: "redirected key file", missing: "key-file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := filepath.Join(t.TempDir(), "unwritten-project")
			keyFile := filepath.Join(t.TempDir(), "unwritten.key.json")
			values := map[string]string{"namespace": "org/example/widget", "actor": "Alice", "key-file": keyFile}
			delete(values, test.missing)
			args := []string{"setup", "--repo", repo}
			for _, name := range []string{"namespace", "actor", "key-file"} {
				if value, found := values[name]; found {
					args = append(args, "--"+name, value)
				}
			}
			if test.json {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			config := setupRunConfig(t.TempDir(), &stdout, &stderr, test.terminal)
			if code := runWithConfig(args, config); code != exitUsage {
				t.Fatalf("missing %s exit = %d, stderr=%q", test.missing, code, stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "setup requires --namespace, --actor, and --key-file") {
				t.Fatalf("missing %s output = stdout=%q stderr=%q", test.missing, stdout.String(), stderr.String())
			}
			if _, err := os.Lstat(repo); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing %s wrote repository: %v", test.missing, err)
			}
			if _, err := os.Lstat(keyFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing %s wrote key: %v", test.missing, err)
			}
		})
	}
}

func TestSetupTerminalPromptsInOrderPlansOnceAndCancelsWithoutWrites(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "unwritten-project")
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	stdin := strings.NewReader("org/example/widget\nAlice\n" + keyFile + "\nno\n")
	var stdout, stderr bytes.Buffer
	config := setupRunConfig(t.TempDir(), &stdout, &stderr, true)
	config.Stdin = stdin

	if code := runWithConfig([]string{"setup", "--repo", repo}, config); code != 0 {
		t.Fatalf("setup cancel exit = %d, stderr=%q", code, stderr.String())
	}
	for _, fragment := range []string{"Namespace", "Actor", "Key file", "PACT setup plan", "store", "key", "trust", "verify", "index", "Continue? [y/N]"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("interactive setup lacks %q: %q", fragment, stderr.String())
		}
	}
	if strings.Index(stderr.String(), "Namespace") > strings.Index(stderr.String(), "Actor") ||
		strings.Index(stderr.String(), "Actor") > strings.Index(stderr.String(), "Key file") {
		t.Fatalf("prompt order = %q", stderr.String())
	}
	if strings.Count(stderr.String(), "PACT setup plan") != 1 || strings.Count(stderr.String(), "Continue? [y/N]") != 1 {
		t.Fatalf("plan/confirmation count = %q", stderr.String())
	}
	result := setupHumanResult(t, stdout.String())
	if result != setupCancelledStatus {
		t.Fatalf("setup cancel result = %q, stdout=%q", result, stdout.String())
	}
	assertSetupPathsAbsent(t, repo, keyFile)
}

func TestSetupTerminalAcceptsObservedDefaultsAndExplicitYes(t *testing.T) {
	repo := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var initialOut, initialErr bytes.Buffer
	initial := setupRunConfig(repo, &initialOut, &initialErr, false)
	if code := runWithConfig([]string{
		"setup", "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile, "--json",
	}, initial); code != 0 {
		t.Fatalf("initial setup exit = %d, stderr=%q", code, initialErr.String())
	}

	var stdout, stderr bytes.Buffer
	config := setupRunConfig(repo, &stdout, &stderr, true)
	config.Stdin = strings.NewReader("\n\nyEs\n")
	if code := runWithConfig([]string{"setup", "--key-file", keyFile}, config); code != 0 {
		t.Fatalf("defaulted setup exit = %d, stderr=%q", code, stderr.String())
	}
	for _, fragment := range []string{"Namespace [org/example/widget]", "Actor [Alice]", "Continue? [y/N]"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("observed-default setup lacks %q: %q", fragment, stderr.String())
		}
	}
	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("accepted setup output = %q", stdout.String())
	}
}

func TestSetupTerminalEscapesObservedDefaultsAndPlanWithoutChangingRequest(t *testing.T) {
	repo := t.TempDir()
	actor := "Alice\nFORGED ACTOR\x1b[31m"
	keyFile := filepath.Join(t.TempDir(), "key\nFORGED PATH\x1b[2J.json")
	var initialOut, initialErr bytes.Buffer
	initial := setupRunConfig(repo, &initialOut, &initialErr, false)
	if code := runWithConfig([]string{
		"setup", "--namespace", "org/example/widget", "--actor", actor, "--key-file", keyFile, "--json",
	}, initial); code != 0 {
		t.Fatalf("hostile fixture setup exit = %d, stderr=%q", code, initialErr.String())
	}

	var stderr bytes.Buffer
	config := setupRunConfig(repo, io.Discard, &stderr, true)
	config.Stdin = strings.NewReader("\n\nyes\n")
	output, err := runSetup([]string{"--repo", repo, "--key-file", keyFile}, io.Discard, config)
	if err != nil || output.setup == nil {
		t.Fatalf("escaped prompted setup = (%#v, %v), stderr=%q", output.setup, err, stderr.String())
	}
	if output.setup.Actor != actor || output.setup.KeyFile != keyFile {
		t.Fatalf("setup changed raw request = actor %q key %q", output.setup.Actor, output.setup.KeyFile)
	}
	for _, raw := range []string{"\x1b", "\nFORGED ACTOR", "\nFORGED PATH"} {
		if strings.Contains(stderr.String(), raw) {
			t.Fatalf("setup terminal output contains raw control sequence %q: %q", raw, stderr.String())
		}
	}
	for _, escaped := range []string{escapeSetupTerminalText(actor), escapeSetupTerminalText(keyFile)} {
		if !strings.Contains(stderr.String(), escaped) {
			t.Fatalf("setup terminal output lacks escaped value %q: %q", escaped, stderr.String())
		}
	}
}

func TestSetupTerminalFreshExamplesAreNotDefaults(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "unwritten-project")
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var stdout, stderr bytes.Buffer
	config := setupRunConfig(t.TempDir(), &stdout, &stderr, true)
	config.Stdin = strings.NewReader("\n")

	if code := runWithConfig([]string{"setup", "--repo", repo, "--actor", "Alice", "--key-file", keyFile}, config); code != exitUsage {
		t.Fatalf("empty fresh namespace exit = %d, stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "example: org/example/widget") || !strings.Contains(stderr.String(), "namespace is required") {
		t.Fatalf("empty fresh namespace output = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertSetupPathsAbsent(t, repo, keyFile)
}

func TestSetupTerminalRejectsOverlongLineBeforeWrites(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "unwritten-project")
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var stdout, stderr bytes.Buffer
	config := setupRunConfig(t.TempDir(), &stdout, &stderr, true)
	config.Stdin = strings.NewReader(strings.Repeat("n", 64*1024+1) + "\n")

	if code := runWithConfig([]string{"setup", "--repo", repo, "--actor", "Alice", "--key-file", keyFile}, config); code != exitUsage {
		t.Fatalf("overlong line exit = %d, stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "setup input exceeds 64 KiB") {
		t.Fatalf("overlong line output = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertSetupPathsAbsent(t, repo, keyFile)
}

func TestSetupInputAllowsExact64KiBWithCRLF(t *testing.T) {
	want := strings.Repeat("n", setupInputLimit)
	reader := bufio.NewReaderSize(strings.NewReader(want+"\r\n"), setupInputLimit+3)
	got, err := readSetupLine(reader, io.Discard, "")
	if err != nil || got != want {
		t.Fatalf("exact 64 KiB CRLF input = (%d bytes, %v), want %d bytes", len(got), err, len(want))
	}
}

func TestSetupTerminalTextEscapesControlsAndPreservesPrintableUnicode(t *testing.T) {
	input := "Björk/東京\\A\nB\rC\tD\x00E\x07F\x1b[31mG\x7fH\u009bI" + string([]byte{0xff}) + "J\u200bK"
	want := `Björk/東京\\A\nB\rC\tD\x00E\x07F\x1b[31mG\x7fH\x9bI\xffJ\u200bK`
	got := escapeSetupTerminalText(input)
	if got != want {
		t.Fatalf("escaped terminal text = %q, want %q", got, want)
	}
	if bytes.Contains([]byte(got), []byte{0xff}) || strings.ContainsRune(got, '\u200b') {
		t.Fatalf("escaped terminal text retained raw invalid or non-printable bytes: %q", got)
	}
}

func TestSetupTerminalWriterFailuresPrecedeAllMutation(t *testing.T) {
	for _, failCall := range []int{1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("stderr write %d", failCall), func(t *testing.T) {
			repo := filepath.Join(t.TempDir(), "unwritten-project")
			keyFile := filepath.Join(t.TempDir(), "alice.key.json")
			stdin := strings.NewReader("org/example/widget\nAlice\n" + keyFile + "\nyes\n")
			var stdout bytes.Buffer
			stderr := &setupCallFailWriter{failCall: failCall}
			config := setupRunConfig(t.TempDir(), &stdout, stderr, true)
			config.Stdin = stdin

			if code := runWithConfig([]string{"setup", "--repo", repo}, config); code != exitUnexpectedError {
				t.Fatalf("writer failure %d exit = %d", failCall, code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("writer failure %d fell back to stdout: %q", failCall, stdout.String())
			}
			assertSetupPathsAbsent(t, repo, keyFile)
		})
	}
}

func TestSetupHumanColorAndNarrowMeaning(t *testing.T) {
	repo := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var stdout, stderr bytes.Buffer
	config := setupRunConfig(repo, &stdout, &stderr, true)
	config.StdoutTerminal = true
	config.Width = 60
	code := runWithConfig([]string{
		"setup", "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile, "--color", "always",
	}, config)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("terminal setup = exit %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("terminal setup lacks selected color: %q", stdout.String())
	}
	plain := stripSetupANSI(stdout.String())
	for _, fragment := range []string{"PACT setup", "Repo", "Key file", "store   created", "key     created", "trust   created", "verify  valid", "index   created", "ready"} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("narrow setup lacks %q: %q", fragment, plain)
		}
	}
}

func TestSetupHumanRenderersEscapeFactsErrorsAndActions(t *testing.T) {
	hostile := "safe\\text\nFORGED\x1b[31m"
	result := setuppkg.Result{
		Status: setupCancelledStatus, Repo: hostile + "/repo", Store: hostile + "/store",
		Namespace: hostile + "/namespace", Actor: hostile + "/actor", KeyFile: hostile + "/key", KeyID: hostile + "/id",
		Actions: []setuppkg.Action{{Name: setuppkg.ActionName(hostile), Status: setuppkg.ActionStatus(hostile)}},
	}
	var success, failure bytes.Buffer
	ready := result
	ready.Status = "ready"
	if err := emitSetupHuman(&success, ready, false, 80); err != nil {
		t.Fatal(err)
	}
	if err := emitSetupErrorHuman(&failure, result, &commandError{code: exitUsage, message: hostile + "/error"}, false); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"success": success.String(), "failure": failure.String()} {
		if strings.Contains(output, "\x1b") || strings.Contains(output, "\nFORGED") {
			t.Fatalf("%s setup output contains raw terminal controls: %q", name, output)
		}
		if !strings.Contains(output, escapeSetupTerminalText(hostile)) {
			t.Fatalf("%s setup output lacks escaped terminal text: %q", name, output)
		}
	}
}

func TestSetupJSONKeepsControlCharactersAsStructuredValues(t *testing.T) {
	hostile := "safe\\text\nvalue\x1b[31m"
	raw, err := json.Marshal(setupResultMap(setuppkg.Result{
		Status: "ready", Repo: hostile, Store: hostile, Namespace: hostile,
		Actor: hostile, KeyFile: hostile, KeyID: hostile,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte{'\n'}) || bytes.Contains(raw, []byte{'\x1b'}) {
		t.Fatalf("setup JSON contains raw terminal controls: %q", raw)
	}
	decoded := decodeSetupJSON(t, raw)
	for _, name := range []string{"repo", "store", "namespace", "actor", "key_file", "key_id"} {
		if decoded[name] != hostile {
			t.Fatalf("setup JSON %s = %q, want raw structured value %q", name, decoded[name], hostile)
		}
	}
}

func TestSetupPreApplyErrorEscapesHumanAndPreservesJSONError(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo\nFORGED REPO\x1b[31m")
	keyFile := filepath.Join(repo, "key\nFORGED KEY\x1b[2J.json")
	_, cause := identity.ValidateSigningKeyPath(keyFile, repo)
	if cause == nil {
		t.Fatal("unsafe setup fixture unexpectedly passed key validation")
	}
	args := []string{
		"setup", "--repo", repo, "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile,
	}

	var humanOut, humanErr bytes.Buffer
	if code := runWithConfig(args, setupRunConfig(t.TempDir(), &humanOut, &humanErr, false)); code != exitSecretSafety {
		t.Fatalf("unsafe human setup exit = %d, stderr=%q", code, humanErr.String())
	}
	if humanOut.Len() != 0 || bytes.Contains(humanErr.Bytes(), []byte{'\x1b'}) || strings.Contains(humanErr.String(), "\nFORGED") {
		t.Fatalf("unsafe human setup exposed terminal controls: stdout=%q stderr=%q", humanOut.String(), humanErr.String())
	}
	if !strings.Contains(humanErr.String(), escapeSetupTerminalText(cause.Error())) {
		t.Fatalf("unsafe human setup lacks escaped error %q: %q", escapeSetupTerminalText(cause.Error()), humanErr.String())
	}

	var jsonOut, jsonErr bytes.Buffer
	jsonArgs := append(append([]string(nil), args...), "--json")
	if code := runWithConfig(jsonArgs, setupRunConfig(t.TempDir(), &jsonOut, &jsonErr, false)); code != exitSecretSafety {
		t.Fatalf("unsafe JSON setup exit = %d, stderr=%q", code, jsonErr.String())
	}
	if jsonOut.Len() != 0 || bytes.Contains(jsonErr.Bytes(), []byte{'\x1b'}) || strings.Contains(jsonErr.String(), "\nFORGED") {
		t.Fatalf("unsafe JSON setup exposed literal terminal controls: stdout=%q stderr=%q", jsonOut.String(), jsonErr.String())
	}
	diagnostic := decodeSetupJSON(t, jsonErr.Bytes())
	if diagnostic["error"] != cause.Error() || diagnostic["exit_code"] != float64(exitSecretSafety) {
		t.Fatalf("unsafe setup JSON diagnostic = %#v, want raw error %q", diagnostic, cause.Error())
	}
}

func TestSetupApplyErrorRendersOnlyProvenPartialActions(t *testing.T) {
	repo := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	var stdout, stderr bytes.Buffer
	config := setupRunConfig(repo, &stdout, &stderr, false)
	args := []string{"setup", "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile, "--json"}
	if code := runWithConfig(args, config); code != 0 {
		t.Fatalf("initial setup exit = %d, stderr=%q", code, stderr.String())
	}
	initial := decodeSetupJSON(t, stdout.Bytes())
	indexFile := filepath.Join(repo, ".pact", "index", "pact-v1.sqlite3")
	if err := os.Remove(indexFile); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "unsafe-index-target")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, indexFile); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithConfig(args, config); code != exitUnexpectedError || stdout.Len() != 0 {
		t.Fatalf("partial JSON setup = exit %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	diagnostic := decodeSetupJSON(t, stderr.Bytes())
	details, ok := diagnostic["details"].(map[string]any)
	if !ok {
		t.Fatalf("partial setup diagnostic lacks details: %#v", diagnostic)
	}
	actions := details["actions"].([]any)
	if len(actions) != 4 {
		t.Fatalf("partial setup actions = %#v, want four proven actions", actions)
	}
	if actions[3].(map[string]any)["name"] != "verify" || strings.Contains(stderr.String(), "private_key") || strings.Contains(stderr.String(), "public_key") {
		t.Fatalf("unsafe partial setup diagnostic: %q", stderr.String())
	}

	args = args[:len(args)-1]
	stderr.Reset()
	if code := runWithConfig(args, config); code != exitUnexpectedError || stdout.Len() != 0 {
		t.Fatalf("partial human setup = exit %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, fragment := range []string{
		"PACT error:", "Store     " + initial["store"].(string), "Key ID    " + initial["key_id"].(string),
		"Completed setup actions", "  store", "  key", "  trust", "  verify",
	} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("partial human setup lacks %q: %q", fragment, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "  index") {
		t.Fatalf("partial human setup claimed failed index action: %q", stderr.String())
	}
}

func TestSetupZeroActionApplyErrorRendersSafeResolvedFacts(t *testing.T) {
	repo := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	if err := os.WriteFile(filepath.Join(repo, ".pact"), []byte("unsafe store entry"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	config := setupRunConfig(repo, &stdout, &stderr, false)
	code := runWithConfig([]string{
		"setup", "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile,
	}, config)
	if code != exitStore || stdout.Len() != 0 {
		t.Fatalf("zero-action setup = exit %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	resolvedRepo, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	resolvedKey, err := filepath.Abs(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"PACT error:", "Repo      " + resolvedRepo, "Namespace org/example/widget", "Actor     Alice", "Key file  " + resolvedKey,
	} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("zero-action setup lacks %q: %q", fragment, stderr.String())
		}
	}
	for _, forbidden := range []string{
		"Store     ", "Key ID    ", "Completed setup actions", "\n  store", "\n  key", "\n  trust", "\n  verify", "\n  index",
		"private_key", "public_key", "\x1b[",
	} {
		if strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("zero-action setup contains %q: %q", forbidden, stderr.String())
		}
	}
	if _, err := os.Lstat(keyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zero-action failure wrote key: %v", err)
	}
}

func TestNormalizedRunConfigSuppliesClock(t *testing.T) {
	if normalizedRunConfig(runConfig{}).Now == nil {
		t.Fatal("normalized run config lacks a clock")
	}
}

func TestSetupWriterFailuresReturnUnexpectedAfterDurableApply(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		name := "human"
		if asJSON {
			name = "JSON"
		}
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			keyFile := filepath.Join(t.TempDir(), "alice.key.json")
			var stderr bytes.Buffer
			config := setupRunConfig(repo, io.Discard, &stderr, false)
			output, err := runSetup([]string{
				"--repo", repo, "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile,
			}, io.Discard, config)
			if err != nil || output.setup == nil {
				t.Fatalf("direct setup apply = (%#v, %v)", output.setup, err)
			}
			created := []setuppkg.ActionStatus{
				setuppkg.ActionCreated, setuppkg.ActionCreated, setuppkg.ActionCreated, setuppkg.ActionValid, setuppkg.ActionCreated,
			}
			assertTypedSetupActions(t, *output.setup, created)

			stdout := &setupPartialWriter{limit: 24}
			config.Stdout = stdout
			if code := writeSetup(config, presentation{asJSON: asJSON}, *output.setup); code != exitUnexpectedError {
				t.Fatalf("setup writer failure exit = %d, stderr=%q", code, stderr.String())
			}
			if stdout.accepted == 0 || !strings.Contains(stderr.String(), "setup output failed") {
				t.Fatalf("setup writer failure output = accepted %d, stderr=%q", stdout.accepted, stderr.String())
			}
			assertTypedSetupActions(t, *output.setup, created)

			var sink bytes.Buffer
			rerun, err := runSetup([]string{"--repo", repo, "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile}, io.Discard, config)
			if err != nil || rerun.setup == nil {
				t.Fatalf("direct setup rerun = (%#v, %v)", rerun.setup, err)
			}
			if err := json.NewEncoder(&sink).Encode(setupResultMap(*rerun.setup)); err != nil {
				t.Fatal(err)
			}
			assertSetupActions(t, decodeSetupJSON(t, sink.Bytes()), []setuppkg.ActionStatus{
				setuppkg.ActionExisting, setuppkg.ActionExisting, setuppkg.ActionExisting, setuppkg.ActionValid, setuppkg.ActionCurrent,
			})
		})
	}
}

func TestSetupStderrFailureWhileReportingOutputFailureReturnsUnexpected(t *testing.T) {
	repo := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "alice.key.json")
	stdout := &setupPartialWriter{limit: 24}
	stderr := &setupRejectingWriter{}
	config := setupRunConfig(repo, stdout, stderr, false)
	code := runWithConfig([]string{
		"setup", "--namespace", "org/example/widget", "--actor", "Alice", "--key-file", keyFile,
	}, config)
	if code != exitUnexpectedError || stdout.accepted == 0 || stderr.calls != 1 || stderr.accepted != 0 {
		t.Fatalf("double writer failure = exit %d, stdout accepted %d, stderr calls %d accepted %d", code, stdout.accepted, stderr.calls, stderr.accepted)
	}
	if _, err := os.Stat(filepath.Join(repo, ".pact", "format.json")); err != nil {
		t.Fatalf("double writer failure lost durable setup: %v", err)
	}
}

type setupPartialWriter struct {
	limit, accepted int
}

func (writer *setupPartialWriter) Write(value []byte) (int, error) {
	remaining := writer.limit - writer.accepted
	if remaining <= 0 {
		return 0, errors.New("closed setup output")
	}
	if len(value) > remaining {
		writer.accepted += remaining
		return remaining, errors.New("closed setup output")
	}
	writer.accepted += len(value)
	return len(value), nil
}

type setupRejectingWriter struct{ calls, accepted int }

func (writer *setupRejectingWriter) Write([]byte) (int, error) {
	writer.calls++
	return 0, errors.New("closed setup diagnostic")
}

type setupCallFailWriter struct {
	calls, failCall int
	output          bytes.Buffer
}

func (writer *setupCallFailWriter) Write(value []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failCall {
		return 0, errors.New("closed interactive diagnostic")
	}
	return writer.output.Write(value)
}

func setupRunConfig(workingDir string, stdout, stderr io.Writer, stdinTerminal bool) runConfig {
	return runConfig{
		Stdin: refusedSetupInput{}, Stdout: stdout, Stderr: stderr, WorkingDir: workingDir,
		StdinTerminal: stdinTerminal, Width: 80, Now: func() time.Time { return setupTestNow },
	}
}

func setupHumanResult(t *testing.T, output string) string {
	t.Helper()
	if strings.Contains(output, setupCancelledStatus) {
		return setupCancelledStatus
	}
	if strings.Contains(output, "ready") {
		return "ready"
	}
	return ""
}

func assertSetupPathsAbsent(t *testing.T, repo, keyFile string) {
	t.Helper()
	if _, err := os.Lstat(repo); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup wrote repository: %v", err)
	}
	if _, err := os.Lstat(keyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup wrote key: %v", err)
	}
}

func decodeSetupJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode setup JSON %q: %v", raw, err)
	}
	return result
}

func assertSetupResult(t *testing.T, result map[string]any, repo, keyFile string, statuses []setuppkg.ActionStatus) {
	t.Helper()
	assertExactKeys(t, result, "operation", "ok", "status", "repo", "store", "namespace", "actor", "key_file", "key_id", "actions")
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	resolvedKey, err := filepath.Abs(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if result["operation"] != "setup" || result["ok"] != true || result["status"] != "ready" ||
		result["repo"] != resolvedRepo || result["store"] != filepath.Join(resolvedRepo, ".pact") ||
		result["namespace"] != "org/example/widget" || result["actor"] != "Alice" || result["key_file"] != resolvedKey ||
		!strings.HasPrefix(result["key_id"].(string), "ed25519:") {
		t.Fatalf("setup result fields = %#v", result)
	}
	assertSetupActions(t, result, statuses)
}

func assertSetupActions(t *testing.T, result map[string]any, statuses []setuppkg.ActionStatus) {
	t.Helper()
	actions, ok := result["actions"].([]any)
	if !ok || len(actions) != 5 || len(statuses) != 5 {
		t.Fatalf("setup actions = %#v", result["actions"])
	}
	names := []setuppkg.ActionName{setuppkg.ActionStore, setuppkg.ActionKey, setuppkg.ActionTrust, setuppkg.ActionVerify, setuppkg.ActionIndex}
	for position, value := range actions {
		action := value.(map[string]any)
		assertExactKeys(t, action, "name", "status")
		if action["name"] != string(names[position]) || action["status"] != string(statuses[position]) {
			t.Fatalf("setup action %d = %#v, want (%s, %s)", position, action, names[position], statuses[position])
		}
	}
}

func assertTypedSetupActions(t *testing.T, result setuppkg.Result, statuses []setuppkg.ActionStatus) {
	t.Helper()
	if len(result.Actions) != len(statuses) {
		t.Fatalf("setup actions = %#v, want statuses %#v", result.Actions, statuses)
	}
	for position, action := range result.Actions {
		if action.Status != statuses[position] {
			t.Fatalf("setup action %d = %#v, want status %s", position, action, statuses[position])
		}
	}
}

func stripSetupANSI(value string) string {
	for _, code := range []string{"\x1b[32m", "\x1b[0m"} {
		value = strings.ReplaceAll(value, code, "")
	}
	return value
}
