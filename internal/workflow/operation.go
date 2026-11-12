// ABOUTME: Serializes cooperative operation keys and reconciles signed canonical claims.
// ABOUTME: Keeps retry identity independent of mutable indexes, evidence, and parent heads.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"pact/internal/canonical"
	"pact/internal/development"
	"pact/internal/ledger"
	"pact/internal/store"
)

// ErrConflict means signed history already contains incompatible or multiple claims.
var ErrConflict = errors.New("operation key conflicts with signed history")

func batchFacts(batch ledger.EventBatch) map[string]any {
	events := make([]any, len(batch.Events))
	for i, event := range batch.Events {
		events[i] = map[string]any{"local_id": event.LocalID, "kind": event.Kind, "type": event.Type, "subject": event.Subject, "schema_ref": event.SchemaRef, "payload": event.Payload, "evidence": sliceValues(event.Evidence), "caused_by": sliceValues(event.CausedBy), "supersedes": sliceValues(event.Supersedes), "tags": sliceValues(event.Tags)}
	}
	return map[string]any{"profile": development.Profile, "namespace": batch.Namespace, "events": events}
}

func digestValue(ctx context.Context, value any) (string, error) {
	raw, err := canonical.MarshalContext(ctx, value)
	if err != nil {
		return "", err
	}
	return canonical.DigestContext(ctx, raw)
}

func operationLock(ctx context.Context, st *store.Store, namespace, signer, operation string) (*os.File, error) {
	digest, err := digestValue(ctx, []any{namespace, signer, development.Profile, operation})
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(st.Dir(), "tmp")
	if err := prepareLockDirectory(directory); err != nil {
		return nil, err
	}
	name := filepath.Join(directory, "record-"+strings.TrimPrefix(digest, "sha256:")+".lock")
	// #nosec G304,G703 -- name is a digest beneath the checked store tmp directory; O_NOFOLLOW refuses symlinks.
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open operation lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("operation lock must be a regular file")
	}
	if err := waitOperationLock(ctx, file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func prepareLockDirectory(directory string) error {
	// #nosec G703 -- directory is the fixed tmp child of the already opened store.
	info, err := os.Lstat(directory)
	if errors.Is(err, fs.ErrNotExist) {
		// #nosec G703 -- directory is the fixed tmp child of the already opened store.
		if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("operation lock directory must be a real directory")
	}
	return nil
}

func waitOperationLock(ctx context.Context, file *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("lock operation: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func reconcile(ctx context.Context, st *store.Store, signer, operation, digest string, result *RecordResult, namespace string) error {
	return st.WithReadLock(func() error {
		scan, err := ledger.Scan(ctx, st, ledger.ScanOptions{Limits: ledger.Phase2Limits})
		if err != nil {
			return err
		}
		result.SourceFingerprint = scan.SourceFingerprint
		if !scan.Verification.OK {
			return fmt.Errorf("%w: canonical history failed verification", ledger.ErrIntegrity)
		}
		candidates := []string{}
		mismatch := false
		for id, commit := range scan.Commits {
			if commit.Namespace != namespace || commit.ActorID != signer {
				continue
			}
			matches, exact, err := inspectClaim(ctx, st, id, operation, digest)
			if err != nil {
				return err
			}
			if !matches {
				continue
			}
			candidates = append(candidates, id)
			mismatch = mismatch || !exact
		}
		sort.Strings(candidates)
		if len(candidates) > 1 || mismatch {
			result.Admission, result.ConflictingObjectIDs = "conflict", candidates
			result.NextAction = "Use a new operation_id for different facts; inspect all conflicting objects."
			return ErrConflict
		}
		if len(candidates) == 1 {
			id := candidates[0]
			commit := scan.Commits[id]
			result.Admission, result.ObjectID, result.EventRefs = "existing", id, append([]string{}, commit.EventRefs...)
			result.Integrity, result.Authenticity = commit.Integrity, commit.Authenticity
			result.Authorization = scan.Verification.Authorization[id].Status
		}
		return nil
	})
}

func inspectClaim(ctx context.Context, st *store.Store, id, operation, digest string) (bool, bool, error) {
	raw, err := st.GetBoundedContext(ctx, id, ledger.Phase2Limits.ObjectBytes)
	if err != nil {
		return false, false, err
	}
	value, err := canonical.ParseContext(ctx, raw)
	if err != nil {
		return false, false, err
	}
	object, _ := value.(map[string]any)
	body, _ := object["body"].(map[string]any)
	metadata, _ := body["metadata"].(map[string]any)
	claim, _ := metadata["workflow"].(map[string]any)
	if claim["profile"] != development.Profile || claim["operation_id"] != operation {
		return false, false, nil
	}
	actual, err := digestValue(ctx, map[string]any{"profile": development.Profile, "namespace": body["namespace"], "events": body["events"]})
	if err != nil {
		return true, false, err
	}
	return true, claim["request_digest"] == digest && actual == digest, nil
}

func sliceValues[T any](values []T) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}
