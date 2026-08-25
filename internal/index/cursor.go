// ABOUTME: Encodes and strictly validates bounded restart-safe causal query cursors.
// ABOUTME: Binds continuation positions to one fixed view, source snapshot, and index digest.
package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"pact/internal/canonical"
	"pact/internal/ledger"
)

const (
	cursorFormat = "pact/query-cursor/v1"
	cursorDomain = "PACT-QUERY-CURSOR-V1\x00"
)

var beforeCursorBase64Decode = func() {}

// QueryError reports one stable safe index or cursor detail code.
type QueryError struct{ Code string }

func (err *QueryError) Error() string {
	if err == nil {
		return "query failed"
	}
	switch err.Code {
	case "cursor_invalid":
		return "query cursor is invalid"
	case "cursor_query_mismatch":
		return "query cursor does not match this request"
	case "cursor_stale":
		return "query cursor no longer matches the current index"
	case "index_missing", "index_stale", "index_corrupt", "index_incompatible", "index_partial_build":
		return "query requires a current index: " + err.Code
	case "source_invalid":
		return "query source is invalid"
	case "source_changed":
		return "index source changed during the operation"
	case "index_publication_failed":
		return "index publication failed"
	default:
		return "query failed"
	}
}

// OrderInfo describes the fixed causal transport order.
type OrderInfo struct {
	Kind                 string `json:"kind"`
	TieBreaker           string `json:"tie_breaker"`
	TieBreakerIsSemantic bool   `json:"tie_breaker_is_semantic"`
	ObservedAtUsed       bool   `json:"observed_at_used"`
	GlobalCompleteness   string `json:"global_completeness"`
}

type cursorState struct {
	AfterGroup        string
	AfterBatch        *uint64
	AfterRef          string
	Checksum          string
	Format            string
	LogicalDigest     string
	QueryDigest       string
	SchemaVersion     int
	SourceFingerprint string
}

type cursorExpectation struct {
	LogicalDigest, QueryDigest, SourceFingerprint string
	SchemaVersion                                 int
}

func emptyFilters() Filters {
	return Filters{
		Namespace: []string{}, Type: []string{}, Kind: []string{}, Subject: []string{}, Actor: []string{},
		Tag: []string{}, SchemaRef: []string{}, EventRef: []string{}, CausedBy: []string{}, Supersedes: []string{},
	}
}

func fixedOrder() OrderInfo {
	return OrderInfo{
		Kind: "known_causal_batches/v1", TieBreaker: "immutable_reference", TieBreakerIsSemantic: false,
		ObservedAtUsed: false, GlobalCompleteness: "unknown",
	}
}

func queryDigest(ctx context.Context, command string, filters Filters, limit int) (string, error) {
	raw, err := canonical.MarshalContext(ctx, map[string]any{
		"command": command,
		"filters": filtersCanonicalValue(filters),
		"limit":   limit,
		"order":   orderCanonicalValue(fixedOrder()),
	})
	if err != nil {
		return "", err
	}
	return canonical.DigestContext(ctx, raw)
}

func encodeCursor(ctx context.Context, state cursorState) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateCursorState(state); err != nil {
		return "", err
	}
	withoutChecksum := cursorCanonicalValue(state, false)
	raw, err := canonical.MarshalContext(ctx, withoutChecksum)
	if err != nil {
		if contextError(err) {
			return "", err
		}
		return "", &QueryError{Code: "cursor_invalid"}
	}
	state.Checksum = cursorChecksum(raw)
	raw, err = canonical.MarshalContext(ctx, cursorCanonicalValue(state, true))
	if err != nil || uint64(len(raw)) > ledger.Phase2Limits.DecodedCursorBytes {
		if contextError(err) {
			return "", err
		}
		return "", &QueryError{Code: "cursor_invalid"}
	}
	encodedLength := base64.RawURLEncoding.EncodedLen(len(raw))
	// #nosec G115 -- the fixed encoded-cursor limit is 4 KiB and fits int on every supported target.
	if uint64(encodedLength) > ledger.Phase2Limits.EncodedCursorBytes {
		return "", &QueryError{Code: "cursor_invalid"}
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(ctx context.Context, token string, expectation cursorExpectation) (cursorState, error) { //nolint:gocyclo // Strict cursor gates stay in contract order.
	invalid := func() (cursorState, error) { return cursorState{}, &QueryError{Code: "cursor_invalid"} }
	if err := ctx.Err(); err != nil {
		return cursorState{}, err
	}
	if token == "" || uint64(len(token)) > ledger.Phase2Limits.EncodedCursorBytes {
		return invalid()
	}
	for _, character := range token {
		if !isRawURLCharacter(character) {
			return invalid()
		}
	}
	decodedLength := base64.RawURLEncoding.DecodedLen(len(token))
	// #nosec G115 -- the fixed decoded-cursor limit is 3 KiB and fits int on every supported target.
	if uint64(decodedLength) > ledger.Phase2Limits.DecodedCursorBytes {
		return invalid()
	}
	beforeCursorBase64Decode()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != decodedLength || uint64(len(raw)) > ledger.Phase2Limits.DecodedCursorBytes {
		return invalid()
	}
	value, err := canonical.ParseContext(ctx, raw)
	if err != nil {
		if contextError(err) {
			return cursorState{}, err
		}
		return invalid()
	}
	reencoded, err := canonical.MarshalContext(ctx, value)
	if err != nil || !bytes.Equal(raw, reencoded) {
		if contextError(err) {
			return cursorState{}, err
		}
		return invalid()
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactCursorKeys(object) {
		return invalid()
	}
	state, ok := cursorStateFromValue(object)
	if !ok || validateDecodedCursorState(state) != nil || !validDigest(state.Checksum) {
		return invalid()
	}
	withoutChecksum, err := canonical.MarshalContext(ctx, cursorCanonicalValue(state, false))
	if err != nil || state.Checksum != cursorChecksum(withoutChecksum) {
		if contextError(err) {
			return cursorState{}, err
		}
		return invalid()
	}
	if state.QueryDigest != expectation.QueryDigest {
		return cursorState{}, &QueryError{Code: "cursor_query_mismatch"}
	}
	if state.SchemaVersion != expectation.SchemaVersion || state.SourceFingerprint != expectation.SourceFingerprint || state.LogicalDigest != expectation.LogicalDigest {
		return cursorState{}, &QueryError{Code: "cursor_stale"}
	}
	return state, nil
}

func validateCursorPosition(ctx context.Context, db *sql.DB, position selectionPosition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var found string
	var err error
	switch {
	case position.Group == "ordered" && position.Batch != nil:
		err = db.QueryRowContext(ctx, "SELECT event_ref FROM events WHERE event_ref=? AND causal_status='ordered' AND causal_batch=?", position.Ref, *position.Batch).Scan(&found)
	case position.Group == "unresolved" && position.Batch == nil:
		err = db.QueryRowContext(ctx, "SELECT event_ref FROM events WHERE event_ref=? AND causal_status='unresolved' AND causal_batch IS NULL", position.Ref).Scan(&found)
	default:
		return &QueryError{Code: "cursor_invalid"}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return &QueryError{Code: "cursor_stale"}
	}
	if err != nil {
		return safeIndexReadError("read cursor position failed", err)
	}
	if found != position.Ref {
		return &QueryError{Code: "cursor_stale"}
	}
	return nil
}

func validateCursorState(state cursorState) error {
	if state.SchemaVersion != SchemaVersion {
		return &QueryError{Code: "cursor_invalid"}
	}
	return validateDecodedCursorState(state)
}

func validateDecodedCursorState(state cursorState) error {
	if state.Format != cursorFormat || state.SchemaVersion < 1 || !validDigest(state.SourceFingerprint) || !validDigest(state.LogicalDigest) || !validDigest(state.QueryDigest) {
		return &QueryError{Code: "cursor_invalid"}
	}
	if _, err := ledger.NormalizeEventRef(state.AfterRef); err != nil {
		return &QueryError{Code: "cursor_invalid"}
	}
	if state.AfterGroup == "ordered" && state.AfterBatch != nil {
		return nil
	}
	if state.AfterGroup == "unresolved" && state.AfterBatch == nil {
		return nil
	}
	return &QueryError{Code: "cursor_invalid"}
}

func cursorCanonicalValue(state cursorState, includeChecksum bool) map[string]any {
	value := map[string]any{
		"after_batch": state.AfterBatch, "after_group": state.AfterGroup, "after_ref": state.AfterRef,
		"format": state.Format, "logical_digest": state.LogicalDigest, "query_digest": state.QueryDigest,
		"schema_version": state.SchemaVersion, "source_fingerprint": state.SourceFingerprint,
	}
	if state.AfterBatch == nil {
		value["after_batch"] = nil
	} else {
		value["after_batch"] = *state.AfterBatch
	}
	if includeChecksum {
		value["checksum"] = state.Checksum
	}
	return value
}

func cursorStateFromValue(value map[string]any) (cursorState, bool) {
	group, groupOK := value["after_group"].(string)
	ref, refOK := value["after_ref"].(string)
	checksum, checksumOK := value["checksum"].(string)
	format, formatOK := value["format"].(string)
	logical, logicalOK := value["logical_digest"].(string)
	query, queryOK := value["query_digest"].(string)
	source, sourceOK := value["source_fingerprint"].(string)
	version, versionOK := value["schema_version"].(int64)
	if !groupOK || !refOK || !checksumOK || !formatOK || !logicalOK || !queryOK || !sourceOK || !versionOK || version < 0 || version > int64(^uint(0)>>1) {
		return cursorState{}, false
	}
	state := cursorState{
		AfterGroup: group, AfterRef: ref, Checksum: checksum, Format: format,
		LogicalDigest: logical, QueryDigest: query, SchemaVersion: int(version), SourceFingerprint: source,
	}
	if value["after_batch"] != nil {
		batch, ok := value["after_batch"].(int64)
		if !ok || batch < 0 {
			return cursorState{}, false
		}
		converted := uint64(batch)
		state.AfterBatch = &converted
	}
	return state, true
}

func hasExactCursorKeys(value map[string]any) bool {
	if len(value) != 9 {
		return false
	}
	for _, key := range []string{"after_batch", "after_group", "after_ref", "checksum", "format", "logical_digest", "query_digest", "schema_version", "source_fingerprint"} {
		if _, found := value[key]; !found {
			return false
		}
	}
	return true
}

func cursorChecksum(raw []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(cursorDomain))
	_, _ = hash.Write(raw)
	return fmt.Sprintf("sha256:%x", hash.Sum(nil))
}

func filtersCanonicalValue(filters Filters) map[string]any {
	return map[string]any{
		"namespace": stringsCanonicalValue(filters.Namespace), "type": stringsCanonicalValue(filters.Type),
		"kind": stringsCanonicalValue(filters.Kind), "subject": stringsCanonicalValue(filters.Subject),
		"actor": stringsCanonicalValue(filters.Actor), "tag": stringsCanonicalValue(filters.Tag),
		"schema_ref": stringsCanonicalValue(filters.SchemaRef), "event_ref": stringsCanonicalValue(filters.EventRef),
		"caused_by": stringsCanonicalValue(filters.CausedBy), "supersedes": stringsCanonicalValue(filters.Supersedes),
	}
}

func orderCanonicalValue(order OrderInfo) map[string]any {
	return map[string]any{
		"kind": order.Kind, "tie_breaker": order.TieBreaker, "tie_breaker_is_semantic": order.TieBreakerIsSemantic,
		"observed_at_used": order.ObservedAtUsed, "global_completeness": order.GlobalCompleteness,
	}
}

func stringsCanonicalValue(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func isRawURLCharacter(character rune) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || strings.ContainsRune("-_", character)
}
