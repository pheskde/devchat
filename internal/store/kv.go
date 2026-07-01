package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GlobalScope is the KV scope for data shared across all sessions.
const GlobalScope = "global"

func (s *Store) SetKV(ctx context.Context, scope, key, value string) (KV, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kv_store (scope, key, value, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		scope, key, value, now.Format(timeLayout),
	)
	if err != nil {
		return KV{}, fmt.Errorf("set kv: %w", err)
	}
	return KV{Scope: scope, Key: key, Value: value, UpdatedAt: now}, nil
}

func (s *Store) GetKV(ctx context.Context, scope, key string) (KV, error) {
	var kv KV
	var updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT scope, key, value, updated_at FROM kv_store WHERE scope = ? AND key = ?`,
		scope, key,
	).Scan(&kv.Scope, &kv.Key, &kv.Value, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return KV{}, ErrNotFound
	}
	if err != nil {
		return KV{}, fmt.Errorf("get kv: %w", err)
	}
	kv.UpdatedAt, _ = time.Parse(timeLayout, updated)
	return kv, nil
}

func (s *Store) DeleteKV(ctx context.Context, scope, key string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM kv_store WHERE scope = ? AND key = ?`, scope, key)
	if err != nil {
		return fmt.Errorf("delete kv: %w", err)
	}
	return nil
}

// ListKV returns all entries in a scope, ordered by key.
func (s *Store) ListKV(ctx context.Context, scope string) ([]KV, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope, key, value, updated_at FROM kv_store WHERE scope = ? ORDER BY key ASC`,
		scope,
	)
	if err != nil {
		return nil, fmt.Errorf("list kv: %w", err)
	}
	defer rows.Close()

	var out []KV
	for rows.Next() {
		var kv KV
		var updated string
		if err := rows.Scan(&kv.Scope, &kv.Key, &kv.Value, &updated); err != nil {
			return nil, fmt.Errorf("scan kv: %w", err)
		}
		kv.UpdatedAt, _ = time.Parse(timeLayout, updated)
		out = append(out, kv)
	}
	return out, rows.Err()
}
