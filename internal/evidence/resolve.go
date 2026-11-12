// ABOUTME: Resolves selected repository evidence with byte and scope bounds.
// ABOUTME: Separates observed digests from claims without changing ledger state.
package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode"
)

// MaxItemBytes and MaxOperationBytes bound exact evidence reads.
const MaxItemBytes int64 = 16 << 20
const MaxOperationBytes int64 = 64 << 20

// ErrLimit means no complete digest can be reported within the allowed budget.
var ErrLimit = errors.New("evidence byte limit exceeded")

// Request selects bytes within one explicit repository. Zero MaxBytes uses the item limit.
type Request struct {
	Repo     string
	Ref      string
	Digest   string
	MaxBytes int64
}

// Assessment records a local observation, independently of signature or policy verification.
// Unsafe locators are withheld to avoid reflecting credentials into output.
type Assessment struct {
	Ref          string `json:"ref"`
	Digest       string `json:"digest"`
	ActualDigest string `json:"actual_digest"`
	Status       string `json:"status"`
	Bytes        int64  `json:"bytes"`
}

// Resolve hashes exact selected bytes; cancellation and process failures remain errors.
func Resolve(ctx context.Context, request Request) (Assessment, error) {
	limit := request.MaxBytes
	if limit == 0 {
		limit = MaxItemBytes
	}
	return resolve(ctx, request, limit)
}

// ResolveAll checks selected requests in order under one 64 MiB byte budget.
// It returns no partial results on error; repeated selections each consume their bytes.
func ResolveAll(ctx context.Context, requests []Request) ([]Assessment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]Assessment, 0, len(requests))
	remaining := MaxOperationBytes
	for _, request := range requests {
		limit := request.MaxBytes
		if limit == 0 {
			limit = MaxItemBytes
		}
		if limit < 0 || limit > MaxItemBytes {
			return nil, ErrLimit
		}
		if limit > remaining {
			limit = remaining
		}
		result, err := resolve(ctx, request, limit)
		if err != nil {
			return nil, err
		}
		remaining -= result.Bytes
		results = append(results, result)
	}
	return results, nil
}

func resolve(ctx context.Context, request Request, limit int64) (Assessment, error) {
	result := Assessment{Digest: request.Digest, Status: "unchecked"}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if limit < 0 || limit > MaxItemBytes {
		return result, ErrLimit
	}
	revision, name, valid := parseLocator(request.Ref)
	if !valid {
		result.Status = "unsupported"
		return result, nil
	}
	result.Ref = request.Ref
	sink := &digestWriter{ctx: ctx, hash: sha256.New(), limit: limit}
	var status string
	var err error
	if revision != "" {
		status, err = readGit(ctx, request.Repo, revision, name, sink)
	} else {
		status, err = readFile(request.Repo, name, sink)
	}
	if err != nil {
		return result, err
	}
	if status != "" {
		result.Status = status
		return result, nil
	}
	result.Bytes = sink.bytes
	result.ActualDigest = fmt.Sprintf("sha256:%x", sink.hash.Sum(nil))
	if request.Digest != "" {
		result.Status = "changed"
		if result.ActualDigest == request.Digest {
			result.Status = "matching"
		}
	}
	return result, nil
}

func parseLocator(ref string) (revision, name string, valid bool) {
	name = ref
	if strings.HasPrefix(ref, "git:") {
		var found bool
		revision, name, found = strings.Cut(strings.TrimPrefix(ref, "git:"), ":")
		if !found || !fullObjectID(revision) {
			return "", "", false
		}
	} else if strings.Contains(ref, ":") {
		return "", "", false
	}
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\@?#") || strings.ContainsFunc(name, unicode.IsControl) {
		return "", "", false
	}
	return revision, name, true
}

func fullObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func readFile(repo, name string, sink *digestWriter) (string, error) {
	root, err := os.OpenRoot(repo)
	if err != nil {
		return "unavailable", nil
	}
	defer func() { _ = root.Close() }()
	// Refuse all symlink components, including links that currently point inside.
	parts := strings.Split(name, "/")
	for i := range parts {
		info, statErr := root.Lstat(strings.Join(parts[:i+1], "/"))
		if statErr != nil {
			return "unavailable", nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "unsupported", nil
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return "unsupported", nil
		}
	}
	file, err := root.Open(name)
	if err != nil {
		return "unavailable", nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", errors.New("evidence file inspection failed")
	}
	if !info.Mode().IsRegular() {
		return "unsupported", nil
	}
	if info.Size() > sink.limit {
		return "", ErrLimit
	}
	if _, err := io.Copy(sink, file); err != nil {
		return "", safeReadError(err)
	}
	return "", nil
}

func safeReadError(err error) error {
	if errors.Is(err, ErrLimit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("evidence byte read failed")
}

type digestWriter struct {
	ctx          context.Context
	hash         hash.Hash
	limit, bytes int64
}

func (w *digestWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(data)) > w.limit-w.bytes {
		return 0, ErrLimit
	}
	n, err := w.hash.Write(data)
	w.bytes += int64(n)
	return n, err
}
