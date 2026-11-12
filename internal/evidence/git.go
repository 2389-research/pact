// ABOUTME: Reads immutable local Git blobs with bounded subprocess streams.
// ABOUTME: Blocks inherited Git selectors, lazy fetches, replacement refs, and external rendering.
package evidence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessError distinguishes Git execution failures without disclosing process diagnostics.
type ProcessError struct{ cause error }

func (e *ProcessError) Error() string { return "evidence Git process failed" }
func (e *ProcessError) Unwrap() error { return e.cause }

func readGit(ctx context.Context, repo, revision, name string, sink *digestWriter) (string, error) {
	// Require Git metadata in the explicit scope instead of discovering an ancestor repository.
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return "unavailable", nil //nolint:nilerr // Missing metadata is an evidence state, not a process failure.
	}
	var format bytes.Buffer
	if err := runGit(ctx, repo, "", &boundedWriter{writer: &format, remaining: 128}, "rev-parse", "--show-object-format"); err != nil {
		return "", err
	}
	wantLength := map[string]int{"sha1": 40, "sha256": 64}[strings.TrimSpace(format.String())]
	if wantLength == 0 {
		return "", errors.New("unsupported Git object format")
	}
	if len(revision) != wantLength {
		return "unsupported", nil
	}
	var metadata bytes.Buffer
	err := runGit(ctx, repo, revision+":"+name+"\n", &boundedWriter{writer: &metadata, remaining: 4096}, "cat-file", "--batch-check")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(metadata.String())
	if strings.HasSuffix(metadata.String(), " missing\n") {
		return "unavailable", nil
	}
	if len(fields) != 3 || !fullObjectID(fields[0]) {
		return "", errors.New("invalid Git evidence metadata")
	}
	if fields[1] != "blob" {
		return "unsupported", nil
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return "", errors.New("invalid Git evidence size")
	}
	if size > sink.limit {
		return "", ErrLimit
	}
	if err := runGit(ctx, repo, "", sink, "show", "--no-ext-diff", "--no-textconv", fields[0], "--"); err != nil {
		return "", err
	}
	if sink.bytes != size {
		return "", errors.New("git evidence size changed during read")
	}
	return "", nil
}

func runGit(ctx context.Context, repo, input string, output io.Writer, args ...string) error {
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// #nosec G204 -- Fixed executable/options; object IDs and relative locators are validated, with no shell.
	command := exec.CommandContext(childCtx, "git", append([]string{"--no-pager", "--no-replace-objects", "-C", repo, "-c", "protocol.allow=never"}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	command.Stdin = strings.NewReader(input)
	writer := &cancelWriter{writer: output, cancel: cancel}
	command.Stdout = writer
	command.Stderr = io.Discard
	err := command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if writer.err != nil {
		return safeReadError(writer.err)
	}
	if err != nil {
		return &ProcessError{cause: err}
	}
	return nil
}

type cancelWriter struct {
	writer io.Writer
	cancel context.CancelFunc
	err    error
}

func (w *cancelWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if err != nil {
		w.err = err
		w.cancel()
	}
	return n, err
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, ErrLimit
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	return n, err
}
