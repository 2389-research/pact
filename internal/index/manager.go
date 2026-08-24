// ABOUTME: Exposes disposable-index status while keeping source scans under the shared store lock.
// ABOUTME: Models unavailable index values with pointers so command JSON can emit null.
package index

import (
	"context"
	"fmt"
	"path/filepath"

	"pact/internal/ledger"
	"pact/internal/store"
)

const (
	liveIndexName = "pact-v1.sqlite3"
	coverageNone  = "unavailable"
)

// Manager owns disposable SQLite index operations for one initialized store.
type Manager struct{ store *store.Store }

// IndexInfo describes whether the disposable index can answer for the current local source.
type IndexInfo struct {
	State             string
	Coverage          string
	Path              *string
	SchemaVersion     *int
	SourceFingerprint *string
	LogicalDigest     *string
	RebuildRequired   bool
}

// ReplicaInfo describes closure of the scanned local immutable object set.
type ReplicaInfo struct {
	Scope, Completeness, GlobalCompleteness string
	Blockers                                []ledger.Blocker
}

// Counts holds source counts when a bounded canonical scan succeeded.
type Counts struct {
	Objects, Commits, Checkpoints, Events, Edges, CanonicalBytes *uint64
}

// LimitsInfo identifies the fixed resource profile and scan result.
type LimitsInfo struct{ Profile, Status string }

// Status reports source and disposable-index state without repairing either.
type Status struct {
	Index   IndexInfo
	Replica ReplicaInfo
	Counts  Counts
	Limits  LimitsInfo
}

// RebuildResult reports one explicit rebuild publication.
type RebuildResult struct {
	Status
	Created, Replaced bool
}

// New creates an index manager for st.
func New(st *store.Store) *Manager { return &Manager{store: st} }

// Status scans the source and classifies the live index without mutating files.
func (m *Manager) Status(ctx context.Context) (result Status, err error) {
	result = unavailableStatus()
	if m == nil || m.store == nil || ctx == nil {
		return result, fmt.Errorf("index status requires a store and context")
	}
	err = m.store.WithReadLock(func() error {
		scan, scanErr := ledger.Scan(ctx, m.store, ledger.ScanOptions{Limits: ledger.Phase2Limits})
		if scanErr != nil {
			return fmt.Errorf("scan index source: %w", scanErr)
		}
		if !scan.Verification.OK {
			return fmt.Errorf("scan index source: source_invalid")
		}
		result.Replica = replicaInfo(scan)
		result.Counts = sourceCounts(scan.Counts)
		result.Limits = LimitsInfo{Profile: ledger.LimitsProfile, Status: "within_limits"}
		path := filepath.Join(m.store.Root(), ".pact", "index", liveIndexName)
		result.Index, scanErr = validateIndex(ctx, path, scan)
		if scanErr != nil {
			return fmt.Errorf("validate index: %w", scanErr)
		}
		return nil
	})
	return result, err
}

func unavailableStatus() Status {
	return Status{
		Index:   IndexInfo{State: "missing", Coverage: coverageNone, RebuildRequired: true},
		Replica: ReplicaInfo{Scope: "local_object_set", Completeness: "unassessed", GlobalCompleteness: "unknown", Blockers: []ledger.Blocker{}},
		Limits:  LimitsInfo{Profile: ledger.LimitsProfile},
	}
}

func replicaInfo(scan ledger.ScanResult) ReplicaInfo {
	blockers := append([]ledger.Blocker(nil), scan.Completeness.Blockers...)
	return ReplicaInfo{Scope: "local_object_set", Completeness: scan.Completeness.Status, GlobalCompleteness: "unknown", Blockers: blockers}
}

func sourceCounts(counts ledger.ScanCounts) Counts {
	return Counts{
		Objects: new(counts.Objects), Commits: new(counts.Commits), Checkpoints: new(counts.Checkpoints),
		Events: new(counts.Events), Edges: new(counts.Edges), CanonicalBytes: new(counts.CanonicalBytes),
	}
}
