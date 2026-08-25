// ABOUTME: Pins strict restart-safe query cursor vectors, grammar, bounds, and state checks.
// ABOUTME: Uses canonical bytes and a real SQLite continuation row without treating cursors as auth.
package index

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"pact/internal/canonical"
)

const (
	fixedQueryDigest = "sha256:cf26419be1d19ee78f39cb4fec7ee28b822d4c48f83c15febac9ef63a1706e2d"
	fixedChecksum    = "sha256:9292788b9db6e3a0930b32315909fa9cb29c5733dec459f44e5c88a880ce97f0"
)

const fixedCursorVector = "eyJhZnRlcl9iYXRjaCI6MTIsImFmdGVyX2dyb3VwIjoib3JkZXJlZCIsImFmdGVyX3JlZiI6InBhY3Q6ZXZlbnQ6c2hhMjU2OmJi" +
	"YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmIjZXZlbnQiLCJjaGVj" +
	"a3N1bSI6InNoYTI1Njo5MjkyNzg4YjlkYjZlM2EwOTMwYjMyMzE1OTA5ZmE5Y2IyOWM1NzMzZGVjNDU5ZjQ0ZTVjODhhODgwY2U5" +
	"N2YwIiwiZm9ybWF0IjoicGFjdC9xdWVyeS1jdXJzb3IvdjEiLCJsb2dpY2FsX2RpZ2VzdCI6InNoYTI1NjpkZGRkZGRkZGRkZGRk" +
	"ZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkIiwicXVlcnlfZGlnZXN0Ijoic2hhMjU2" +
	"OmNmMjY0MTliZTFkMTllZTc4ZjM5Y2I0ZmVjN2VlMjhiODIyZDRjNDhmODNjMTVmZWJhYzllZjYzYTE3MDZlMmQiLCJzY2hlbWFf" +
	"dmVyc2lvbiI6MSwic291cmNlX2ZpbmdlcnByaW50Ijoic2hhMjU2OmNjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2Nj" +
	"Y2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2MifQ"

func TestCursorFixedQueryDigestChecksumAndTokenVectors(t *testing.T) {
	filters := emptyFilters()
	filters.Namespace = []string{"org/2389"}
	got, err := queryDigest(context.Background(), "query", filters, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != fixedQueryDigest {
		t.Fatalf("query digest = %q, want %q", got, fixedQueryDigest)
	}
	batch := uint64(12)
	state := cursorState{
		AfterGroup: "ordered", AfterBatch: &batch,
		AfterRef:          "pact:event:sha256:" + strings.Repeat("b", 64) + "#event",
		Format:            cursorFormat,
		LogicalDigest:     "sha256:" + strings.Repeat("d", 64),
		QueryDigest:       fixedQueryDigest,
		SchemaVersion:     1,
		SourceFingerprint: "sha256:" + strings.Repeat("c", 64),
	}
	token, err := encodeCursor(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if state.Checksum != "" {
		t.Fatal("encodeCursor mutated caller state")
	}
	if token != fixedCursorVector {
		t.Fatalf("cursor = %q, want fixed vector %q", token, fixedCursorVector)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{"after_batch":12,"after_group":"ordered","after_ref":"pact:event:sha256:` + strings.Repeat("b", 64) + `#event","checksum":"` + fixedChecksum + `","format":"pact/query-cursor/v1","logical_digest":"sha256:` + strings.Repeat("d", 64) + `","query_digest":"` + fixedQueryDigest + `","schema_version":1,"source_fingerprint":"sha256:` + strings.Repeat("c", 64) + `"}`
	if string(raw) != wantJSON {
		t.Fatalf("cursor JSON = %s, want %s", raw, wantJSON)
	}
}

func TestCursorDecodeStrictCanonicalShapeAndSafeErrors(t *testing.T) {
	expectation := fixedCursorExpectation()
	state, err := decodeCursor(context.Background(), fixedCursorVector, expectation)
	if err != nil || state.AfterGroup != "ordered" || state.AfterBatch == nil || *state.AfterBatch != 12 {
		t.Fatalf("decode fixed cursor = (%#v, %v)", state, err)
	}

	fixedRaw, err := base64.RawURLEncoding.DecodeString(fixedCursorVector)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		token string
		code  string
	}{
		{name: "padding", token: fixedCursorVector + "=", code: "cursor_invalid"},
		{name: "alphabet", token: fixedCursorVector[:10] + "+" + fixedCursorVector[11:], code: "cursor_invalid"},
		{name: "noncanonical whitespace", token: base64.RawURLEncoding.EncodeToString(append([]byte(" "), fixedRaw...)), code: "cursor_invalid"},
		{name: "extra key", token: cursorTokenFromRaw(strings.Replace(string(fixedRaw), `{"after_batch":12,`, `{"added":true,"after_batch":12,`, 1)), code: "cursor_invalid"},
		{name: "missing key", token: cursorTokenFromRaw(strings.Replace(string(fixedRaw), `"format":"pact/query-cursor/v1",`, "", 1)), code: "cursor_invalid"},
		{name: "wrong type", token: cursorTokenFromRaw(strings.Replace(string(fixedRaw), `"schema_version":1`, `"schema_version":"1"`, 1)), code: "cursor_invalid"},
		{name: "damaged checksum", token: cursorTokenFromRaw(strings.Replace(string(fixedRaw), fixedChecksum, "sha256:"+strings.Repeat("0", 64), 1)), code: "cursor_invalid"},
		{name: "bad event ref", token: cursorTokenFromRaw(strings.Replace(string(fixedRaw), "pact:event:sha256:", "pact:event:bad:", 1)), code: "cursor_invalid"},
		{name: "unsupported format", token: cursorTokenFromRaw(strings.Replace(string(fixedRaw), cursorFormat, "pact/query-cursor/v2", 1)), code: "cursor_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeCursor(context.Background(), test.token, expectation)
			assertQueryErrorCode(t, err, test.code)
			if strings.Contains(err.Error(), test.token) {
				t.Fatal("cursor error echoed raw token")
			}
		})
	}
}

func TestCursorRejectsNonCanonicalRawURLTrailingBits(t *testing.T) {
	fixedRaw, err := base64.RawURLEncoding.DecodeString(fixedCursorVector)
	if err != nil {
		t.Fatal(err)
	}
	for _, final := range "RST" {
		token := fixedCursorVector[:len(fixedCursorVector)-1] + string(final)
		raw, decodeErr := base64.RawURLEncoding.DecodeString(token)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if !bytes.Equal(raw, fixedRaw) {
			t.Fatalf("final %q no longer demonstrates identical decoded bytes", final)
		}
		_, decodeErr = decodeCursor(context.Background(), token, fixedCursorExpectation())
		assertQueryErrorCode(t, decodeErr, "cursor_invalid")
	}
}

func TestCursorRejectsBoundsBeforeBase64Decode(t *testing.T) {
	original := beforeCursorBase64Decode
	decoded := false
	beforeCursorBase64Decode = func() { decoded = true }
	t.Cleanup(func() { beforeCursorBase64Decode = original })
	_, err := decodeCursor(context.Background(), strings.Repeat("A", 4_096), fixedCursorExpectation())
	assertQueryErrorCode(t, err, "cursor_invalid")
	if !decoded {
		t.Fatal("exact encoded and decoded cursor bounds were rejected before base64 decode")
	}
	decoded = false
	_, err = decodeCursor(context.Background(), strings.Repeat("A", 4_097), fixedCursorExpectation())
	assertQueryErrorCode(t, err, "cursor_invalid")
	if decoded {
		t.Fatal("oversized cursor reached base64 decode")
	}
}

func TestCursorClassifiesQueryMismatchAndStaleDimensions(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*cursorExpectation)
		code   string
	}{
		{name: "view filter or limit digest", modify: func(value *cursorExpectation) { value.QueryDigest = "sha256:" + strings.Repeat("e", 64) }, code: "cursor_query_mismatch"},
		{name: "schema", modify: func(value *cursorExpectation) { value.SchemaVersion = 2 }, code: "cursor_stale"},
		{name: "source", modify: func(value *cursorExpectation) { value.SourceFingerprint = "sha256:" + strings.Repeat("e", 64) }, code: "cursor_stale"},
		{name: "logical", modify: func(value *cursorExpectation) { value.LogicalDigest = "sha256:" + strings.Repeat("e", 64) }, code: "cursor_stale"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expectation := fixedCursorExpectation()
			test.modify(&expectation)
			_, err := decodeCursor(context.Background(), fixedCursorVector, expectation)
			assertQueryErrorCode(t, err, test.code)
		})
	}
}

func TestCursorClassifiesValidDifferentSchemaAsStale(t *testing.T) {
	batch := uint64(12)
	state := cursorState{
		AfterGroup: "ordered", AfterBatch: &batch, AfterRef: validCursorEventRef(), Format: cursorFormat,
		LogicalDigest: "sha256:" + strings.Repeat("d", 64), QueryDigest: fixedQueryDigest,
		SchemaVersion: 2, SourceFingerprint: "sha256:" + strings.Repeat("c", 64),
	}
	token := uncheckedCursorToken(t, state)
	_, err := decodeCursor(context.Background(), token, fixedCursorExpectation())
	assertQueryErrorCode(t, err, "cursor_stale")
}

func TestCursorRequiresValidGroupBatchPair(t *testing.T) {
	tests := []cursorState{
		{AfterGroup: "ordered", AfterRef: validCursorEventRef()},
		{AfterGroup: "unresolved", AfterBatch: new(uint64), AfterRef: validCursorEventRef()},
		{AfterGroup: "other", AfterRef: validCursorEventRef()},
	}
	for _, state := range tests {
		state.Format = cursorFormat
		state.LogicalDigest = "sha256:" + strings.Repeat("d", 64)
		state.QueryDigest = fixedQueryDigest
		state.SchemaVersion = 1
		state.SourceFingerprint = "sha256:" + strings.Repeat("c", 64)
		if _, err := encodeCursor(context.Background(), state); err == nil {
			t.Fatalf("encodeCursor(%#v) succeeded", state)
		}
	}
}

func TestCursorContinuationMustExistInValidatedIndex(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	path := indexPath(fixture.store)
	writeSnapshotFixture(t, path, mustProject(t, fixture.scan))
	db, err := openIndexReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	orderedRef := fixture.presentParent.EventRefs[0]
	orderedBatch := fixture.scan.CausalBatches[orderedRef]
	if err := validateCursorPosition(context.Background(), db, selectionPosition{Group: "ordered", Batch: &orderedBatch, Ref: orderedRef}); err != nil {
		t.Fatalf("ordered continuation rejected: %v", err)
	}
	unresolvedRef := fixture.child.EventRefs[0]
	if err := validateCursorPosition(context.Background(), db, selectionPosition{Group: "unresolved", Ref: unresolvedRef}); err != nil {
		t.Fatalf("unresolved continuation rejected: %v", err)
	}
	missing := "pact:event:sha256:" + strings.Repeat("f", 64) + "#missing"
	err = validateCursorPosition(context.Background(), db, selectionPosition{Group: "unresolved", Ref: missing})
	assertQueryErrorCode(t, err, "cursor_stale")
	wrongBatch := orderedBatch + 1
	err = validateCursorPosition(context.Background(), db, selectionPosition{Group: "ordered", Batch: &wrongBatch, Ref: orderedRef})
	assertQueryErrorCode(t, err, "cursor_stale")
}

func fixedCursorExpectation() cursorExpectation {
	return cursorExpectation{
		QueryDigest: fixedQueryDigest, SchemaVersion: 1,
		SourceFingerprint: "sha256:" + strings.Repeat("c", 64),
		LogicalDigest:     "sha256:" + strings.Repeat("d", 64),
	}
}

func validCursorEventRef() string {
	return "pact:event:sha256:" + strings.Repeat("b", 64) + "#event"
}

func cursorTokenFromRaw(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func uncheckedCursorToken(t *testing.T, state cursorState) string {
	t.Helper()
	raw, err := canonical.Marshal(cursorCanonicalValue(state, false))
	if err != nil {
		t.Fatal(err)
	}
	state.Checksum = cursorChecksum(raw)
	raw, err = canonical.Marshal(cursorCanonicalValue(state, true))
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func assertQueryErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var queryErr *QueryError
	if !errors.As(err, &queryErr) || queryErr.Code != code {
		t.Fatalf("error = %#v, want QueryError code %q", err, code)
	}
}
