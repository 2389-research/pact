// ABOUTME: Proves the compiled Phase 2 index lifecycle against signed canonical repositories.
// ABOUTME: Uses real processes, Ed25519 keys, immutable objects, and disposable SQLite files.
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"
)

func TestCLIIndexLifecycleAndImmutability(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo, keyPath, key := initializeIndexRepository(t, binary, workspace, "org/example/widget")

	const payloadMarker = "payload-marker-phase2-lifecycle-7cf1"
	const evidenceMarker = "evidence-marker-phase2-lifecycle-9a42"
	rootEvent := phase2Event("root", "widget.root", "widget/root", "observation")
	rootEvent["payload"] = map[string]any{"marker": payloadMarker}
	rootEvent["evidence"] = []any{map[string]any{
		"ref": "urn:pact:test:" + evidenceMarker, "digest": "sha256:" + strings.Repeat("1", 64),
		"media_type": "application/json", "role": "supporting", "description": evidenceMarker,
	}}
	first := commitEvents(t, binary, workspace, repo, keyPath, "root.json", "org/example/widget", "2030-01-01T00:00:00Z", nil, rootEvent)
	rootID, rootRef := first["object_id"].(string), first["event_refs"].([]any)[0].(string)

	left := commitEvents(t, binary, workspace, repo, keyPath, "left.json", "org/example/widget", "2029-01-01T00:00:00Z", []string{rootID}, phase2Event("left", "widget.branch", "widget/left", "action"))
	right := commitEvents(t, binary, workspace, repo, keyPath, "right.json", "org/example/widget", "2028-01-01T00:00:00Z", []string{rootID}, phase2Event("right", "widget.branch", "widget/right", "decision"))
	merge := commitEvents(t, binary, workspace, repo, keyPath, "merge.json", "org/example/widget", "2027-01-01T00:00:00Z", []string{left["object_id"].(string), right["object_id"].(string)}, phase2Event("merge", "widget.merge", "widget/merge", "control"))

	checkpoint := runJSON(t, binary, "checkpoint", "--repo", repo, "--key-file", keyPath,
		"--scope", "org/example", "--policy-ref", "sha256:"+strings.Repeat("a", 64),
		"--authority-epoch", "phase2-e2e", "--schema-ref", "sha256:"+strings.Repeat("b", 64),
		"--purpose", "phase 2 lifecycle fixture", "--json")
	if checkpoint["created"] != true {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}

	protectedBefore := protectedRepositoryDigest(t, repo)
	headsBefore := runJSON(t, binary, "heads", "--repo", repo, "--namespace", "org/example", "--json")
	missing := runJSON(t, binary, "index", "status", "--repo", repo, "--json")
	assertIndexState(t, missing, "missing", "unavailable", true)

	rebuilt := runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	assertIndexState(t, rebuilt, "current", "complete", false)
	if rebuilt["created"] != true || rebuilt["replaced"] != false {
		t.Fatalf("first rebuild flags = %#v", rebuilt)
	}
	status := runJSON(t, binary, "index", "status", "--repo", repo, "--json")
	assertIndexState(t, status, "current", "complete", false)

	logResult := runJSON(t, binary, "log", "--repo", repo, "--namespace", "org/example", "--json")
	queryResult := runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example", "--json")
	show := runJSON(t, binary, "show", "--repo", repo, rootRef, "--json")
	verified := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
	if verified["ok"] != true || verified["index_status"] != "current" {
		t.Fatalf("strict verify = %#v", verified)
	}
	if !strings.Contains(stableJSON(t, show), payloadMarker) || !strings.Contains(stableJSON(t, show), evidenceMarker) {
		t.Fatalf("show did not return canonical payload and evidence: %#v", show)
	}

	ordered := orderedEventRefs(t, queryResult)
	leftRef := left["event_refs"].([]any)[0].(string)
	rightRef := right["event_refs"].([]any)[0].(string)
	mergeRef := merge["event_refs"].([]any)[0].(string)
	if position(ordered, rootRef) >= position(ordered, leftRef) || position(ordered, rootRef) >= position(ordered, rightRef) || position(ordered, leftRef) >= position(ordered, mergeRef) || position(ordered, rightRef) >= position(ordered, mergeRef) {
		t.Fatalf("causal order follows advisory timestamps or lost fork/merge: %v", ordered)
	}
	if queryResult["order"].(map[string]any)["observed_at_used"] != false {
		t.Fatalf("query order = %#v", queryResult["order"])
	}

	privateMaterial := privateKeyMaterial(t, keyPath)
	indexPath := filepath.Join(repo, ".pact", "index", "pact-v1.sqlite3")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"log": []byte(stableJSON(t, logResult)), "query": []byte(stableJSON(t, queryResult)), "index": indexBytes,
	} {
		for _, forbidden := range []string{payloadMarker, evidenceMarker, privateMaterial, `"private_key"`} {
			if forbidden != "" && bytes.Contains(raw, []byte(forbidden)) {
				t.Fatalf("%s contains forbidden marker or private key material", name)
			}
		}
	}

	logicalDigest := status["index"].(map[string]any)["logical_digest"]
	logJSON, queryJSON := stableJSON(t, logResult), stableJSON(t, queryResult)
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "missing", "unavailable", true)
	if missingShow := runJSON(t, binary, "show", "--repo", repo, rootRef, "--json"); missingShow["kind"] != "event" {
		t.Fatalf("show without index = %#v", missingShow)
	}
	if missingVerify := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json"); missingVerify["ok"] != true || missingVerify["index_status"] != "missing" {
		t.Fatalf("verify without index = %#v", missingVerify)
	}
	rebuilt = runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	assertIndexState(t, rebuilt, "current", "complete", false)
	if rebuilt["index"].(map[string]any)["logical_digest"] != logicalDigest {
		t.Fatalf("logical digest changed: before=%v after=%v", logicalDigest, rebuilt["index"].(map[string]any)["logical_digest"])
	}
	if got := stableJSON(t, runJSON(t, binary, "log", "--repo", repo, "--namespace", "org/example", "--json")); got != logJSON {
		t.Fatalf("log changed after rebuild\nbefore=%s\nafter=%s", logJSON, got)
	}
	if got := stableJSON(t, runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example", "--json")); got != queryJSON {
		t.Fatalf("query changed after rebuild\nbefore=%s\nafter=%s", queryJSON, got)
	}
	if after := protectedRepositoryDigest(t, repo); !reflect.DeepEqual(after, protectedBefore) {
		t.Fatalf("index lifecycle mutated protected files\nbefore=%#v\nafter=%#v", protectedBefore, after)
	}
	if headsAfter := runJSON(t, binary, "heads", "--repo", repo, "--namespace", "org/example", "--json"); stableJSON(t, headsAfter) != stableJSON(t, headsBefore) {
		t.Fatalf("heads changed after index lifecycle: before=%#v after=%#v", headsBefore, headsAfter)
	}
	if key["key_id"] == "" {
		t.Fatal("fixture key ID is empty")
	}
}

func TestCLIQueryFiltersPaginationRestartAndPartialReplica(t *testing.T) { //nolint:funlen // One compiled scenario owns the full restart and partial-replica contract.
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo, keyPath, _ := initializeIndexRepository(t, binary, workspace, "org/example/source")

	target := phase2Event("target", "audit.target", "subject/target", "assertion")
	target["tags"] = []any{"alpha", "common"}
	targetCommit := commitEvents(t, binary, workspace, repo, keyPath, "filter-target.json", "org/example/source", "2026-08-24T12:00:00Z", nil, target)
	targetRef := targetCommit["event_refs"].([]any)[0].(string)

	dependentKeyPath := filepath.Join(workspace, "dependent.key.json")
	dependentKey := runJSON(t, binary, "keygen", "--actor", "Dependent Actor", "--out", dependentKeyPath, "--json")
	runJSON(t, binary, "trust-add", "--repo", repo, "--key-file", dependentKeyPath, "--json")
	dependent := phase2Event("dependent", "audit.dependent", "subject/dependent", "action")
	dependent["tags"] = []any{"beta", "common"}
	dependent["caused_by"] = []any{targetRef}
	dependent["supersedes"] = []any{targetRef}
	dependent["schema_ref"] = "sha256:" + strings.Repeat("c", 64)
	dependentCommit := commitEvents(t, binary, workspace, repo, dependentKeyPath, "filter-dependent.json", "org/example/child", "2025-08-24T12:00:00Z", nil, dependent)
	dependentRef := dependentCommit["event_refs"].([]any)[0].(string)

	wideA := phase2Event("wide-a", "audit.wide", "subject/wide-a", "observation")
	wideB := phase2Event("wide-b", "audit.wide", "subject/wide-b", "observation")
	commitEvents(t, binary, workspace, repo, keyPath, "filter-wide.json", "org/example/wide/grand", "2026-08-24T12:01:00Z", nil, wideA, wideB)
	boundary := commitEvents(t, binary, workspace, repo, keyPath, "filter-boundary.json", "org/example2", "2026-08-24T12:02:00Z", nil, phase2Event("boundary", "audit.boundary", "subject/boundary", "control"))
	boundaryRef := boundary["event_refs"].([]any)[0].(string)

	missingCause := "pact:event:sha256:" + strings.Repeat("d", 64) + "#missing-cause"
	missingSupersedes := "pact:event:sha256:" + strings.Repeat("e", 64) + "#missing-supersedes"
	unresolved := phase2Event("unresolved", "audit.partial", "subject/unresolved", "decision")
	unresolved["caused_by"] = []any{missingCause}
	supersedesOnly := phase2Event("supersedes-only", "audit.partial", "subject/supersedes-only", "decision")
	supersedesOnly["supersedes"] = []any{missingSupersedes}
	partialCommit := commitEvents(t, binary, workspace, repo, keyPath, "filter-partial.json", "org/example/partial", "2026-08-24T12:03:00Z", nil, unresolved, supersedesOnly)
	unresolvedRef := ledger.EventRef(partialCommit["object_id"].(string), "unresolved")
	supersedesOnlyRef := ledger.EventRef(partialCommit["object_id"].(string), "supersedes-only")

	rebuilt := runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	assertIndexState(t, rebuilt, "current", "partial", false)
	replica := rebuilt["replica"].(map[string]any)
	if replica["completeness"] != "incomplete" || replica["global_completeness"] != "unknown" {
		t.Fatalf("partial replica = %#v", replica)
	}
	blockers := replica["blockers"].([]any)
	blockerKeys := make([]string, len(blockers))
	for position, raw := range blockers {
		blocker := raw.(map[string]any)
		blockerKeys[position] = fmt.Sprint(blocker["source_id"], "|", blocker["code"], "|", blocker["field"], "|", blocker["missing_ref"])
	}
	if len(blockers) != 2 || !sort.StringsAreSorted(blockerKeys) {
		t.Fatalf("blockers are not stable and sorted: %#v", blockers)
	}

	actor := dependentKey["key_id"].(string)
	filterCases := []struct {
		name string
		args []string
	}{
		{name: "namespace", args: []string{"--namespace", "org/example/child"}},
		{name: "type", args: []string{"--type", "audit.dependent"}},
		{name: "kind", args: []string{"--kind", "action"}},
		{name: "subject", args: []string{"--subject", "subject/dependent"}},
		{name: "actor", args: []string{"--actor", actor}},
		{name: "tag", args: []string{"--tag", "beta"}},
		{name: "schema ref", args: []string{"--schema-ref", "sha256:" + strings.Repeat("c", 64)}},
		{name: "event ref", args: []string{"--event-ref", dependentRef}},
		{name: "caused by", args: []string{"--caused-by", targetRef}},
		{name: "supersedes", args: []string{"--supersedes", targetRef}},
	}
	filterJSON := make(map[string]string, len(filterCases))
	for _, test := range filterCases {
		t.Run(test.name, func(t *testing.T) {
			result := runFilterQuery(t, binary, repo, test.args)
			if refs := eventRefsInResult(result); !reflect.DeepEqual(refs, []string{dependentRef}) || queryReturned(result) != 1 {
				t.Fatalf("filter %s result = %v, want only %s: %#v", test.name, refs, dependentRef, result)
			}
			filterJSON[test.name] = stableJSON(t, result)
		})
	}

	orResult := runJSON(t, binary, "query", "--repo", repo, "--type", "audit.dependent", "--type", "audit.no-match", "--json")
	if !containsEvent(orResult, dependentRef) {
		t.Fatalf("OR within type family = %#v", orResult)
	}
	andResult := runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example/child", "--type", "audit.dependent", "--tag", "beta", "--json")
	if queryReturned(andResult) != 1 || !containsEvent(andResult, dependentRef) {
		t.Fatalf("AND across filter families = %#v", andResult)
	}
	empty := runJSON(t, binary, "query", "--repo", repo, "--type", "audit.empty", "--json")
	if queryReturned(empty) != 0 || empty["page"].(map[string]any)["next_cursor"] != nil {
		t.Fatalf("empty query = %#v", empty)
	}
	namespaceResult := runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example", "--json")
	if containsEvent(namespaceResult, boundaryRef) || !containsEvent(namespaceResult, dependentRef) {
		t.Fatalf("namespace descendant boundary = %#v", namespaceResult)
	}
	ordered := orderedEventRefs(t, namespaceResult)
	if position(ordered, targetRef) >= position(ordered, dependentRef) {
		t.Fatalf("resolved cross-namespace caused_by is not causal: %v", ordered)
	}
	if !containsString(ordered, supersedesOnlyRef) || containsString(ordered, unresolvedRef) {
		t.Fatalf("supersedes-only event should stay ordered and caused-by gap unresolved: ordered=%v result=%#v", ordered, namespaceResult["unresolved"])
	}

	objectsBeforeRefusal := protectedRepositoryDigest(t, repo)
	strictFailure := runErrorJSON(t, 9, binary, "verify", "--repo", repo, "--strict", "--json")
	if !strings.Contains(stableJSON(t, strictFailure), "missing_event_reference") {
		t.Fatalf("strict partial verification = %#v", strictFailure)
	}
	runErrorJSON(t, 9, binary, "checkpoint", "--repo", repo, "--key-file", keyPath,
		"--scope", "org/example", "--policy-ref", "sha256:"+strings.Repeat("a", 64),
		"--authority-epoch", "partial-refusal", "--json")
	if after := protectedRepositoryDigest(t, repo); !reflect.DeepEqual(after, objectsBeforeRefusal) {
		t.Fatalf("checkpoint refusal persisted canonical bytes: before=%#v after=%#v", objectsBeforeRefusal, after)
	}

	firstPage := runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example", "--limit", "1", "--json")
	firstCursor := firstPage["page"].(map[string]any)["next_cursor"].(string)
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	for _, test := range filterCases {
		if got := stableJSON(t, runFilterQuery(t, binary, repo, test.args)); got != filterJSON[test.name] {
			t.Fatalf("filter %s changed across same-source rebuild\nbefore=%s\nafter=%s", test.name, filterJSON[test.name], got)
		}
	}
	page := runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example", "--limit", "1", "--cursor", firstCursor, "--json")
	seen := eventRefsInPage(t, firstPage)
	seen = append(seen, eventRefsInPage(t, page)...)
	splitPages := countIncompletePageBatches(firstPage) + countIncompletePageBatches(page)
	for page["page"].(map[string]any)["has_more"] == true {
		cursor := page["page"].(map[string]any)["next_cursor"].(string)
		page = runJSON(t, binary, "query", "--repo", repo, "--namespace", "org/example", "--limit", "1", "--cursor", cursor, "--json")
		seen = append(seen, eventRefsInPage(t, page)...)
		splitPages += countIncompletePageBatches(page)
	}
	unique := map[string]bool{}
	for _, ref := range seen {
		if unique[ref] {
			t.Fatalf("pagination duplicated %s: %v", ref, seen)
		}
		unique[ref] = true
	}
	if len(seen) != queryReturned(namespaceResult) || splitPages < 2 {
		t.Fatalf("pagination omitted results or failed to mark split batch: seen=%v total=%d split=%d", seen, queryReturned(namespaceResult), splitPages)
	}

	malformed := "cursor-do-not-echo-" + strings.Repeat("x", 32)
	bad := runErrorJSON(t, 2, binary, "query", "--repo", repo, "--namespace", "org/example", "--limit", "1", "--cursor", malformed, "--json")
	if strings.Contains(stableJSON(t, bad), malformed) || bad["details"].(map[string]any)["code"] != "cursor_invalid" {
		t.Fatalf("malformed cursor diagnostic = %#v", bad)
	}
	mismatch := runErrorJSON(t, 2, binary, "query", "--repo", repo, "--namespace", "org/example/child", "--limit", "1", "--cursor", firstCursor, "--json")
	if mismatch["details"].(map[string]any)["code"] != "cursor_query_mismatch" || strings.Contains(stableJSON(t, mismatch), firstCursor) {
		t.Fatalf("query-mismatch cursor diagnostic = %#v", mismatch)
	}

	commitEvents(t, binary, workspace, repo, keyPath, "cursor-stale.json", "org/example/source", "2026-08-24T12:10:00Z", nil, phase2Event("cursor-stale", "audit.cursor", "subject/cursor", "observation"))
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	stale := runErrorJSON(t, 9, binary, "query", "--repo", repo, "--namespace", "org/example", "--limit", "1", "--cursor", firstCursor, "--json")
	if stale["details"].(map[string]any)["code"] != "cursor_stale" || strings.Contains(stableJSON(t, stale), firstCursor) {
		t.Fatalf("stale cursor diagnostic = %#v", stale)
	}
}

func TestCLIIndexStateRefusalAndRecovery(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)

	t.Run("stale", func(t *testing.T) {
		repo, keyPath, eventRef := indexedRepository(t, binary, t.TempDir())
		commitEvents(t, binary, filepath.Dir(repo), repo, keyPath, "stale-next.json", "org/example/state", "2026-08-24T13:01:00Z", nil, phase2Event("next", "state.next", "state/next", "action"))
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "stale", "unavailable", true)
		verified := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
		if verified["ok"] != true || verified["index_status"] != "stale" {
			t.Fatalf("verify stale = %#v", verified)
		}
		assertIndexedReadsRefuse(t, binary, repo, "index_stale")
		recoverIndexAndAssertCanonical(t, binary, repo, eventRef)
	})

	t.Run("corrupt", func(t *testing.T) {
		repo, _, eventRef := indexedRepository(t, binary, t.TempDir())
		indexPath := liveIndexPath(repo)
		if err := os.WriteFile(indexPath, []byte("not a sqlite database"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "corrupt", "unavailable", true)
		if show := runJSON(t, binary, "show", "--repo", repo, eventRef, "--json"); show["kind"] != "event" {
			t.Fatalf("show with corrupt index = %#v", show)
		}
		if verify := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json"); verify["ok"] != true || verify["index_status"] != "corrupt" {
			t.Fatalf("verify with corrupt index = %#v", verify)
		}
		assertIndexedReadsRefuse(t, binary, repo, "index_corrupt")
		recoverIndexAndAssertCanonical(t, binary, repo, eventRef)
	})

	t.Run("incompatible", func(t *testing.T) {
		repo, _, eventRef := indexedRepository(t, binary, t.TempDir())
		db, err := sql.Open("sqlite", liveIndexPath(repo))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "incompatible", "unavailable", true)
		assertIndexedReadsRefuse(t, binary, repo, "index_incompatible")
		recoverIndexAndAssertCanonical(t, binary, repo, eventRef)
	})

	t.Run("partial-build", func(t *testing.T) {
		repo, _, eventRef := indexedRepository(t, binary, t.TempDir())
		st, err := store.Open(repo)
		if err != nil {
			t.Fatal(err)
		}
		scan, err := ledger.Scan(context.Background(), st, ledger.ScanOptions{Limits: ledger.Phase2Limits})
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := index.Project(context.Background(), scan)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Events) != 1 {
			t.Fatalf("fixture events = %#v", snapshot.Events)
		}
		snapshot.Events[0].Subject = "state/derived-divergence"
		digest, err := index.LogicalDigest(context.Background(), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", liveIndexPath(repo))
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec("UPDATE events SET subject = ? WHERE event_ref = ?", snapshot.Events[0].Subject, eventRef); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec("UPDATE index_meta SET value = ? WHERE key = 'logical_digest'", digest); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		assertIndexState(t, runJSON(t, binary, "index", "status", "--repo", repo, "--json"), "partial-build", "unavailable", true)
		assertIndexedReadsRefuse(t, binary, repo, "index_partial_build")
		recoverIndexAndAssertCanonical(t, binary, repo, eventRef)
	})
}

func initializeIndexRepository(t *testing.T, binary, workspace, namespace string) (string, string, map[string]any) {
	t.Helper()
	repo := filepath.Join(workspace, "project")
	keyPath := filepath.Join(workspace, "operator.key.json")
	mustMkdir(t, repo)
	runJSON(t, binary, "init", "--repo", repo, "--namespace", namespace, "--json")
	key := runJSON(t, binary, "keygen", "--actor", "Phase 2 Operator", "--out", keyPath, "--json")
	runJSON(t, binary, "trust-add", "--repo", repo, "--key-file", keyPath, "--json")
	return repo, keyPath, key
}

func phase2Event(localID, eventType, subject, kind string) map[string]any {
	return map[string]any{
		"local_id": localID, "kind": kind, "type": eventType, "subject": subject,
		"schema_ref": "pact:core/generic-object/v1", "payload": map[string]any{},
		"evidence": []any{}, "caused_by": []any{}, "supersedes": []any{}, "tags": []any{"phase2"},
	}
}

func commitEvents(t *testing.T, binary, workspace, repo, keyPath, name, namespace, observedAt string, parents []string, events ...map[string]any) map[string]any {
	t.Helper()
	batchPath := filepath.Join(workspace, name)
	items := make([]any, len(events))
	for index := range events {
		items[index] = events[index]
	}
	raw, err := json.Marshal(map[string]any{"events": items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(batchPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	args := make([]string, 0, 12+2*len(parents))
	args = append(args, "commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--namespace", namespace, "--observed-at", observedAt)
	for _, parent := range parents {
		args = append(args, "--parent", parent)
	}
	args = append(args, "--json")
	return runJSON(t, binary, args...)
}

func assertIndexState(t *testing.T, result map[string]any, state, coverage string, rebuild bool) {
	t.Helper()
	info := result["index"].(map[string]any)
	if info["state"] != state || info["coverage"] != coverage || info["rebuild_required"] != rebuild {
		t.Fatalf("index info = %#v, want state=%s coverage=%s rebuild=%v", info, state, coverage, rebuild)
	}
}

func orderedEventRefs(t *testing.T, result map[string]any) []string {
	t.Helper()
	var refs []string
	for _, rawBatch := range result["batches"].([]any) {
		for _, rawItem := range rawBatch.(map[string]any)["items"].([]any) {
			refs = append(refs, rawItem.(map[string]any)["event_ref"].(string))
		}
	}
	return refs
}

func position(values []string, wanted string) int {
	for index, value := range values {
		if value == wanted {
			return index
		}
	}
	return len(values) + 1
}

func containsString(values []string, wanted string) bool {
	return position(values, wanted) < len(values)
}

func containsEvent(result map[string]any, wanted string) bool {
	return slices.Contains(eventRefsInResult(result), wanted)
}

func queryReturned(result map[string]any) int {
	return int(result["page"].(map[string]any)["returned"].(float64))
}

func runFilterQuery(t *testing.T, binary, repo string, filters []string) map[string]any {
	t.Helper()
	args := make([]string, 0, 4+len(filters))
	args = append(args, "query", "--repo", repo)
	args = append(args, filters...)
	args = append(args, "--json")
	return runJSON(t, binary, args...)
}

func eventRefsInResult(result map[string]any) []string {
	refs := make([]string, 0)
	for _, rawBatch := range result["batches"].([]any) {
		for _, rawItem := range rawBatch.(map[string]any)["items"].([]any) {
			refs = append(refs, rawItem.(map[string]any)["event_ref"].(string))
		}
	}
	for _, rawItem := range result["unresolved"].([]any) {
		refs = append(refs, rawItem.(map[string]any)["event_ref"].(string))
	}
	return refs
}

func eventRefsInPage(t *testing.T, result map[string]any) []string {
	t.Helper()
	refs := eventRefsInResult(result)
	if len(refs) != queryReturned(result) {
		t.Fatalf("page returned=%d but contains refs=%v", queryReturned(result), refs)
	}
	return refs
}

func countIncompletePageBatches(result map[string]any) int {
	count := 0
	for _, rawBatch := range result["batches"].([]any) {
		if rawBatch.(map[string]any)["complete_in_page"] == false {
			count++
		}
	}
	return count
}

func indexedRepository(t *testing.T, binary, workspace string) (string, string, string) {
	t.Helper()
	repo, keyPath, _ := initializeIndexRepository(t, binary, workspace, "org/example/state")
	created := commitEvents(t, binary, workspace, repo, keyPath, "state-original.json", "org/example/state", "2026-08-24T13:00:00Z", nil, phase2Event("original", "state.original", "state/original", "observation"))
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	return repo, keyPath, created["event_refs"].([]any)[0].(string)
}

func liveIndexPath(repo string) string {
	return filepath.Join(repo, ".pact", "index", "pact-v1.sqlite3")
}

func assertIndexedReadsRefuse(t *testing.T, binary, repo, code string) {
	t.Helper()
	for _, args := range [][]string{
		{"log", "--repo", repo, "--json"},
		{"query", "--repo", repo, "--subject", "state/original", "--json"},
	} {
		failure := runErrorJSON(t, 9, binary, args...)
		if failure["details"].(map[string]any)["code"] != code {
			t.Fatalf("%v refusal = %#v, want %s", args, failure, code)
		}
	}
}

func recoverIndexAndAssertCanonical(t *testing.T, binary, repo, eventRef string) {
	t.Helper()
	rebuilt := runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	assertIndexState(t, rebuilt, "current", "complete", false)
	query := runJSON(t, binary, "query", "--repo", repo, "--event-ref", eventRef, "--json")
	if queryReturned(query) != 1 || query["batches"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)["subject"] != "state/original" {
		t.Fatalf("recovered query did not hydrate canonical bytes: %#v", query)
	}
}

func stableJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func privateKeyMaterial(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	private, _ := key["private_key"].(string)
	return private
}

func protectedRepositoryDigest(t *testing.T, repo string) map[string]string {
	t.Helper()
	result := map[string]string{}
	root := filepath.Join(repo, ".pact")
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && (relative == "index" || relative == "tmp") {
			return filepath.SkipDir
		}
		if entry.IsDir() || relative == ".gitignore" || relative == "format.json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		result[filepath.ToSlash(relative)] = hex.EncodeToString(digest[:])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Fatal("protected repository digest is empty")
	}
	return result
}
