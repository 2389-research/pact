// ABOUTME: Tests bounded evidence reads against real files and immutable Git objects.
// ABOUTME: Exercises local scope, process isolation, cancellation, and byte-exact digests.
package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pact/internal/canonical"
)

func TestResolveWorkingAndHistoricalBytes(t *testing.T) {
	repo, revision := evidenceGitFixture(t)
	original := []byte("original proof\x00\xff\n")
	for _, tc := range []struct {
		ref, status string
		data        []byte
	}{
		{"proof.txt", "changed", []byte("later proof\n")},
		{"git:" + revision + ":proof.txt", "matching", original},
		{"git:" + revision + ":missing.txt", "unavailable", nil},
		{"git:" + strings.Repeat("a", 40) + ":proof.txt", "unavailable", nil},
		{"missing.txt", "unavailable", nil},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			got, err := Resolve(t.Context(), Request{Repo: repo, Ref: tc.ref, Digest: canonical.Digest(original)})
			if err != nil || got.Status != tc.status {
				t.Fatalf("assessment=%+v error=%v", got, err)
			}
			if tc.data != nil && (got.ActualDigest != canonical.Digest(tc.data) || got.Bytes != int64(len(tc.data))) {
				t.Fatalf("wrong exact bytes: %+v", got)
			}
		})
	}
}

func TestResolveRejectsUnsafeLocators(t *testing.T) {
	repo, revision := evidenceGitFixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("synthetic-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"/etc/passwd", "../secret", "dir/../proof.txt", "https://user:synthetic-password@example.test/proof", "file:proof.txt", "C:\\secret", "dir\\secret", "git:--output=secret:proof.txt", "git:HEAD:proof.txt", "git:" + revision + ":../secret", "git:" + revision + ":/secret", "git:" + revision + ":proof.txt\nHEAD", ".", ""} {
		got, err := Resolve(t.Context(), Request{Repo: repo, Ref: ref, Digest: canonical.Digest(nil)})
		if err != nil || got.Status != "unsupported" {
			t.Errorf("%q => %+v, %v", ref, got, err)
		}
		if strings.Contains(got.Ref, "synthetic-password") {
			t.Fatal("credential locator disclosed")
		}
	}
	for _, ref := range []string{"escape", ".git"} {
		got, err := Resolve(t.Context(), Request{Repo: repo, Ref: ref, Digest: canonical.Digest(nil)})
		if err != nil || got.Status != "unsupported" {
			t.Errorf("%s => %+v, %v", ref, got, err)
		}
	}
}

func TestResolveBoundsAndUnchecked(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "proof"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(t.Context(), Request{Repo: repo, Ref: "proof", MaxBytes: 4})
	if !errors.Is(err, ErrLimit) || got.ActualDigest != "" {
		t.Fatalf("overflow: %+v %v", got, err)
	}
	got, err = Resolve(t.Context(), Request{Repo: repo, Ref: "proof", MaxBytes: 5})
	if err != nil || got.Status != "unchecked" || got.ActualDigest != canonical.Digest([]byte("12345")) {
		t.Fatalf("unchecked: %+v %v", got, err)
	}
	for _, limit := range []int64{-1, MaxItemBytes + 1} {
		if _, err := Resolve(t.Context(), Request{Repo: repo, Ref: "proof", MaxBytes: limit}); !errors.Is(err, ErrLimit) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Resolve(ctx, Request{Repo: repo, Ref: "proof"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "large"), bytes.Repeat([]byte("x"), int(MaxItemBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{Repo: repo, Ref: "large", Digest: canonical.Digest(bytes.Repeat([]byte("x"), int(MaxItemBytes)))}
	requests := []Request{request, request, request, request}
	if got, err := ResolveAll(t.Context(), requests); err != nil || len(got) != 4 {
		t.Fatalf("exact aggregate bound: %d %v", len(got), err)
	}
	if got, err := ResolveAll(t.Context(), append(requests, Request{Repo: repo, Ref: "proof"})); !errors.Is(err, ErrLimit) || got != nil {
		t.Fatalf("aggregate overflow: %+v %v", got, err)
	}
}

func TestResolveGitIgnoresInheritedSelectorsAndReplacement(t *testing.T) {
	repo, revision := evidenceGitFixture(t)
	outside, _ := evidenceGitFixture(t)
	runFixtureGit(t, repo, nil, "replace", revision, "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
	t.Setenv("GIT_WORK_TREE", outside)
	t.Setenv("GIT_COMMON_DIR", filepath.Join(outside, ".git"))
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(outside, "missing"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.pager")
	t.Setenv("GIT_CONFIG_VALUE_0", "nonexistent-pager")
	got, err := Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":proof.txt", Digest: canonical.Digest([]byte("original proof\x00\xff\n"))})
	if err != nil || got.Status != "matching" {
		t.Fatalf("Git environment or replace escaped scope: %+v %v", got, err)
	}
}

func TestResolveGitLiteralPathsAndLimits(t *testing.T) {
	repo, revision := evidenceGitFixture(t)
	for _, name := range []string{"-option", "a:b", "[literal]", "space name"} {
		got, err := Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":" + name, Digest: canonical.Digest([]byte(name))})
		if err != nil || got.Status != "matching" {
			t.Fatalf("literal path %q: %+v %v", name, got, err)
		}
	}
	got, err := Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":proof.txt", MaxBytes: 1})
	if !errors.Is(err, ErrLimit) || got.ActualDigest != "" {
		t.Fatalf("Git overflow: %+v %v", got, err)
	}
	// A directory is a tree object, never byte evidence.
	got, err = Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":dir"})
	if err != nil || got.Status != "unsupported" {
		t.Fatalf("tree evidence: %+v %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":proof.txt"}); err == nil {
		t.Fatal("missing Git executable treated as missing evidence")
	}
}

func evidenceGitFixture(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	if err := os.Mkdir(filepath.Join(repo, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"proof.txt", "-option", "a:b", "[literal]", "space name", "dir/item"} {
		content := []byte(name)
		if name == "proof.txt" {
			content = []byte("original proof\x00\xff\n")
		}
		if err := os.WriteFile(filepath.Join(repo, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runFixtureGit(t, repo, nil, "add", "--all")
	runFixtureGit(t, repo, nil, "-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "original")
	revision := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "HEAD")))
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), []byte("later proof\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "add", "proof.txt")
	runFixtureGit(t, repo, nil, "-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "later")
	return repo, revision
}

func runFixtureGit(t *testing.T, repo string, stdin []byte, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Stdin = bytes.NewReader(stdin)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v\n%s", args, err, output)
	}
	return output
}

func TestResolveGitRequiresFullRepositoryObjectID(t *testing.T) {
	repo := t.TempDir()
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha256")
	runFixtureGit(t, repo, []byte("blob bytes"), "hash-object", "-w", "--stdin")
	if err := os.WriteFile(filepath.Join(repo, "proof"), []byte("proof"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "add", "proof")
	runFixtureGit(t, repo, nil, "-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "proof")
	revision := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "HEAD")))
	got, err := Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":proof", Digest: canonical.Digest([]byte("proof"))})
	if err != nil || got.Status != "matching" {
		t.Fatalf("full SHA256 identity: %+v %v", got, err)
	}
	got, err = Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision[:40] + ":proof"})
	if err != nil || got.Status != "unsupported" {
		t.Fatalf("abbreviated SHA256 accepted: %+v %v", got, err)
	}
}

func TestResolveGitNeverFetchesMissingPromisorBlob(t *testing.T) {
	source, _ := evidenceGitFixture(t)
	runFixtureGit(t, source, nil, "config", "uploadpack.allowFilter", "true")
	clone := filepath.Join(t.TempDir(), "partial")
	runFixtureGit(t, source, nil, "clone", "--quiet", "--no-checkout", "--filter=blob:none", "file://"+source, clone)
	revision := strings.TrimSpace(string(runFixtureGit(t, source, nil, "rev-parse", "HEAD")))
	before, err := os.ReadDir(filepath.Join(clone, ".git", "objects", "pack"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(t.Context(), Request{Repo: clone, Ref: "git:" + revision + ":proof.txt"})
	if err != nil || got.Status != "unavailable" {
		t.Fatalf("missing promisor blob: %+v %v", got, err)
	}
	after, err := os.ReadDir(filepath.Join(clone, ".git", "objects", "pack"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("evidence read fetched missing objects")
	}
}

func TestResolveGitSymlinkHashesLinkTextWithoutFollowing(t *testing.T) {
	repo, _ := evidenceGitFixture(t)
	target := filepath.Join(t.TempDir(), "outside")
	secret := []byte("synthetic outside content")
	if err := os.WriteFile(target, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "add", "link")
	runFixtureGit(t, repo, nil, "-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "link blob")
	revision := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "HEAD")))
	got, err := Resolve(t.Context(), Request{Repo: repo, Ref: "git:" + revision + ":link", Digest: canonical.Digest([]byte(target))})
	if err != nil || got.Status != "matching" || got.Bytes != int64(len(target)) || got.ActualDigest == canonical.Digest(secret) {
		t.Fatalf("link blob dereferenced: %+v %v", got, err)
	}
	got, err = Resolve(t.Context(), Request{Repo: repo, Ref: "link"})
	if err != nil || got.Status != "unsupported" {
		t.Fatalf("working symlink accepted: %+v %v", got, err)
	}
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = Resolve(t.Context(), Request{Repo: nested, Ref: "git:" + revision + ":proof.txt"})
	if err != nil || got.Status != "unavailable" {
		t.Fatalf("searched ancestor scope: %+v %v", got, err)
	}
}

func TestResolveFailureDoesNotReflectCredentialLocator(t *testing.T) {
	ref := "https://user:synthetic-password@example.test/proof"
	got, err := Resolve(t.Context(), Request{Repo: t.TempDir(), Ref: ref, MaxBytes: -1})
	if !errors.Is(err, ErrLimit) || got.Ref != "" {
		t.Errorf("limit refusal reflected locator: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err = Resolve(ctx, Request{Repo: t.TempDir(), Ref: ref})
	if !errors.Is(err, context.Canceled) || got.Ref != "" {
		t.Errorf("cancellation reflected locator: %+v %v", got, err)
	}
	if _, err := ResolveAll(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("empty canceled operation: %v", err)
	}
}

func TestGitStreamLimitAndLinkedWorktree(t *testing.T) {
	repo, revision := evidenceGitFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	runFixtureGit(t, repo, nil, "worktree", "add", "--quiet", "--detach", linked, revision)
	got, err := Resolve(t.Context(), Request{Repo: linked, Ref: "git:" + revision + ":proof.txt", Digest: canonical.Digest([]byte("original proof\x00\xff\n"))})
	if err != nil || got.Status != "matching" {
		t.Fatalf("linked worktree: %+v %v", got, err)
	}
	// Exercise the streaming boundary independently of the early metadata size refusal.
	sink := &digestWriter{ctx: t.Context(), hash: sha256.New(), limit: 1}
	err = runGit(t.Context(), linked, "", sink, "show", "--no-ext-diff", "--no-textconv", revision+":proof.txt", "--")
	if !errors.Is(err, ErrLimit) || sink.bytes > 1 {
		t.Fatalf("stream overflow: %d %v", sink.bytes, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sink = &digestWriter{ctx: ctx, hash: sha256.New(), limit: MaxItemBytes}
	if err := runGit(ctx, linked, "", sink, "show", revision+":proof.txt", "--"); !errors.Is(err, context.Canceled) {
		t.Fatalf("process cancellation: %v", err)
	}
}
