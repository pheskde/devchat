// Package store implements SQLite-backed persistence for chat sessions,
// messages and the cross-session key/value exchange.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	author     TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id, id);

CREATE TABLE IF NOT EXISTS kv_store (
	scope      TEXT NOT NULL,
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (scope, key)
);

CREATE TABLE IF NOT EXISTS presence (
	session_id TEXT NOT NULL,
	author     TEXT NOT NULL,
	last_seen  TEXT NOT NULL,
	PRIMARY KEY (session_id, author)
);
`

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema. The "default" session is ensured to exist so clients have a
// sensible place to write to out of the box.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles a single writer at a time; keeping one connection avoids
	// "database is locked" errors under concurrent goroutines.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &Store{db: db}
	if err := s.EnsureSession(ctx, "default", "default"); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure default session: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
