// ABOUTME: Defines and installs the exact version-one disposable SQLite index schema.
// ABOUTME: Keeps all SQLite DDL and driver use inside the index package.
package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

const (
	SchemaVersion = 1
	ApplicationID = 0x50414354
	IndexFormat   = "pact/sqlite-index/v1"
)

const schemaDDL = `CREATE TABLE index_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT, WITHOUT ROWID;

CREATE TABLE objects (
  object_id TEXT PRIMARY KEY,
  object_type TEXT NOT NULL CHECK (object_type IN ('commit','checkpoint')),
  namespace TEXT NOT NULL,
  body_digest TEXT NOT NULL,
  actor_key_id TEXT NOT NULL,
  actor_label TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  integrity_state TEXT NOT NULL CHECK (integrity_state = 'valid'),
  structure_state TEXT NOT NULL CHECK (structure_state = 'valid'),
  authenticity_state TEXT NOT NULL CHECK (authenticity_state = 'valid'),
  completeness_state TEXT NOT NULL CHECK (completeness_state IN ('complete','partial'))
) STRICT, WITHOUT ROWID;

CREATE TABLE commits (
  commit_id TEXT PRIMARY KEY REFERENCES objects(object_id),
  event_count INTEGER NOT NULL CHECK (event_count BETWEEN 1 AND 1024)
) STRICT, WITHOUT ROWID;

CREATE TABLE parent_edges (
  child_id TEXT NOT NULL REFERENCES commits(commit_id),
  parent_id TEXT NOT NULL,
  resolved INTEGER NOT NULL CHECK (resolved IN (0,1)),
  PRIMARY KEY (child_id, parent_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE events (
  event_ref TEXT PRIMARY KEY,
  commit_id TEXT NOT NULL REFERENCES commits(commit_id),
  local_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('observation','assertion','action','decision','control')),
  event_type TEXT NOT NULL,
  subject TEXT NOT NULL,
  schema_ref TEXT NOT NULL,
  causal_batch INTEGER,
  causal_status TEXT NOT NULL CHECK (causal_status IN ('ordered','unresolved')),
  UNIQUE (commit_id, local_id),
  CHECK ((causal_status = 'ordered' AND causal_batch IS NOT NULL AND causal_batch >= 0) OR
         (causal_status = 'unresolved' AND causal_batch IS NULL))
) STRICT, WITHOUT ROWID;

CREATE TABLE event_tags (
  event_ref TEXT NOT NULL REFERENCES events(event_ref),
  tag TEXT NOT NULL,
  PRIMARY KEY (event_ref, tag)
) STRICT, WITHOUT ROWID;

CREATE TABLE event_links (
  source_ref TEXT NOT NULL REFERENCES events(event_ref),
  relation TEXT NOT NULL CHECK (relation IN ('caused_by','supersedes')),
  target_ref TEXT NOT NULL,
  resolved INTEGER NOT NULL CHECK (resolved IN (0,1)),
  PRIMARY KEY (source_ref, relation, target_ref)
) STRICT, WITHOUT ROWID;

CREATE TABLE checkpoints (
  checkpoint_id TEXT PRIMARY KEY REFERENCES objects(object_id),
  scope TEXT NOT NULL,
  policy_ref TEXT NOT NULL,
  authority_epoch TEXT NOT NULL,
  previous_checkpoint TEXT
) STRICT, WITHOUT ROWID;

CREATE TABLE checkpoint_schema_refs (
  checkpoint_id TEXT NOT NULL REFERENCES checkpoints(checkpoint_id),
  schema_ref TEXT NOT NULL,
  PRIMARY KEY (checkpoint_id, schema_ref)
) STRICT, WITHOUT ROWID;

CREATE TABLE checkpoint_frontier (
  checkpoint_id TEXT NOT NULL REFERENCES checkpoints(checkpoint_id),
  namespace TEXT NOT NULL,
  head_id TEXT NOT NULL,
  resolved INTEGER NOT NULL CHECK (resolved IN (0,1)),
  PRIMARY KEY (checkpoint_id, namespace, head_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE heads (
  namespace TEXT NOT NULL,
  commit_id TEXT NOT NULL REFERENCES commits(commit_id),
  PRIMARY KEY (namespace, commit_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE completeness_blockers (
  source_id TEXT NOT NULL,
  code TEXT NOT NULL CHECK (code IN (
    'missing_parent','missing_event_reference',
    'missing_checkpoint_head','missing_previous_checkpoint'
  )),
  field TEXT NOT NULL,
  missing_ref TEXT NOT NULL,
  PRIMARY KEY (source_id, code, field, missing_ref)
) STRICT, WITHOUT ROWID;

CREATE INDEX objects_namespace_idx ON objects(namespace, object_type, object_id);
CREATE INDEX objects_actor_idx ON objects(actor_key_id, object_id);
CREATE INDEX events_type_idx ON events(event_type, causal_batch, event_ref);
CREATE INDEX events_kind_idx ON events(kind, causal_batch, event_ref);
CREATE INDEX events_subject_idx ON events(subject, causal_batch, event_ref);
CREATE INDEX events_schema_idx ON events(schema_ref, causal_batch, event_ref);
CREATE INDEX events_order_idx ON events(causal_status, causal_batch, event_ref);
CREATE INDEX events_commit_idx ON events(commit_id, local_id);
CREATE INDEX event_tags_tag_idx ON event_tags(tag, event_ref);
CREATE INDEX event_links_target_idx ON event_links(target_ref, relation, source_ref);
CREATE INDEX parent_edges_parent_idx ON parent_edges(parent_id, child_id);`

// SchemaDigest identifies the exact checked-in schema DDL bytes.
func SchemaDigest() string {
	digest := sha256.Sum256([]byte(schemaDDL))
	return fmt.Sprintf("sha256:%x", digest)
}

func createSchema(ctx context.Context, db *sql.DB) (err error) {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	connection, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open schema connection: %w", err)
	}
	defer func() { err = errors.Join(err, connection.Close()) }()

	if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable schema foreign keys: %w", err)
	}
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = transaction.Rollback()
		}
	}()
	if err = createSchemaInTransaction(ctx, transaction); err != nil {
		return err
	}
	if err = transaction.Commit(); err != nil {
		return fmt.Errorf("commit schema transaction: %w", err)
	}
	return nil
}

func createSchemaInTransaction(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("create schema objects: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "PRAGMA application_id = 1346454356"); err != nil {
		return fmt.Errorf("set schema application ID: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
		return fmt.Errorf("set schema user version: %w", err)
	}
	return nil
}
