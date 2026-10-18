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

// ErrConflict is returned by conditional KV writes whose condition did not
// hold. The current (non-expired) value, if any, is returned with it.
var ErrConflict = errors.New("condition failed")

// KVCondition restricts a write. The zero value writes unconditionally.
type KVCondition struct {
	// IfAbsent: write only if the key does not exist or has expired.
	IfAbsent bool
	// IfValue: write only if the key currently holds exactly this value.
	IfValue *string
}

// SetKV writes unconditionally and without expiry.
func (s *Store) SetKV(ctx context.Context, scope, key, value string) (KV, error) {
	return s.SetKVCond(ctx, scope, key, value, 0, KVCondition{})
}

// SetKVCond writes a value atomically with respect to cond. ttl > 0 makes the
// entry expire after that long; expired entries behave as absent. On a failed
// condition it returns ErrConflict together with the current entry (zero KV
// if there is none).
func (s *Store) SetKVCond(ctx context.Context, scope, key, value string, ttl time.Duration, cond KVCondition) (KV, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return KV{}, fmt.Errorf("set kv: %w", err)
	}
	defer tx.Rollback()

	cur, found, err := getKVTx(ctx, tx, scope, key)
	if err != nil {
		return KV{}, err
	}
	if (cond.IfAbsent && found) || (cond.IfValue != nil && (!found || cur.Value != *cond.IfValue)) {
		return cur, ErrConflict
	}

	now := time.Now().UTC()
	var expires any
	var expiresAt *time.Time
	if ttl > 0 {
		t := now.Add(ttl)
		expiresAt = &t
		expires = t.Format(timeLayout)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO kv_store (scope, key, value, updated_at, expires_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, expires_at = excluded.expires_at`,
		scope, key, value, now.Format(timeLayout), expires,
	)
	if err != nil {
		return KV{}, fmt.Errorf("set kv: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return KV{}, fmt.Errorf("set kv: %w", err)
	}
	return KV{Scope: scope, Key: key, Value: value, UpdatedAt: now, ExpiresAt: expiresAt}, nil
}

// getKVTx reads one entry inside tx; an expired entry counts as not found.
func getKVTx(ctx context.Context, tx *sql.Tx, scope, key string) (KV, bool, error) {
	var kv KV
	var updated string
	var expires sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT scope, key, value, updated_at, expires_at FROM kv_store WHERE scope = ? AND key = ?`,
		scope, key,
	).Scan(&kv.Scope, &kv.Key, &kv.Value, &updated, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return KV{}, false, nil
	}
	if err != nil {
		return KV{}, false, fmt.Errorf("get kv: %w", err)
	}
	if isExpired(expires) {
		return KV{}, false, nil
	}
	kv.UpdatedAt, _ = time.Parse(timeLayout, updated)
	kv.ExpiresAt = parseExpiry(expires)
	return kv, true, nil
}

func isExpired(expires sql.NullString) bool {
	if !expires.Valid {
		return false
	}
	t, err := time.Parse(timeLayout, expires.String)
	return err == nil && !t.After(time.Now())
}

func parseExpiry(expires sql.NullString) *time.Time {
	if !expires.Valid {
		return nil
	}
	t, err := time.Parse(timeLayout, expires.String)
	if err != nil {
		return nil
	}
	return &t
}

func (s *Store) GetKV(ctx context.Context, scope, key string) (KV, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return KV{}, fmt.Errorf("get kv: %w", err)
	}
	defer tx.Rollback()
	kv, found, err := getKVTx(ctx, tx, scope, key)
	if err != nil {
		return KV{}, err
	}
	if !found {
		return KV{}, ErrNotFound
	}
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

// DeleteKVIfValue deletes the key only if it currently holds value. It
// returns ErrConflict (with the current entry) otherwise.
func (s *Store) DeleteKVIfValue(ctx context.Context, scope, key, value string) (KV, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return KV{}, fmt.Errorf("delete kv: %w", err)
	}
	defer tx.Rollback()
	cur, found, err := getKVTx(ctx, tx, scope, key)
	if err != nil {
		return KV{}, err
	}
	if !found || cur.Value != value {
		return cur, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kv_store WHERE scope = ? AND key = ?`, scope, key); err != nil {
		return KV{}, fmt.Errorf("delete kv: %w", err)
	}
	return KV{}, tx.Commit()
}

// ListKV returns all non-expired entries in a scope, ordered by key.
func (s *Store) ListKV(ctx context.Context, scope string) ([]KV, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope, key, value, updated_at, expires_at FROM kv_store WHERE scope = ? ORDER BY key ASC`,
		scope,
	)
	if err != nil {
		return nil, fmt.Errorf("list kv: %w", err)
	}
	defer rows.Close()

	out := []KV{}
	for rows.Next() {
		var kv KV
		var updated string
		var expires sql.NullString
		if err := rows.Scan(&kv.Scope, &kv.Key, &kv.Value, &updated, &expires); err != nil {
			return nil, fmt.Errorf("scan kv: %w", err)
		}
		if isExpired(expires) {
			continue
		}
		kv.UpdatedAt, _ = time.Parse(timeLayout, updated)
		kv.ExpiresAt = parseExpiry(expires)
		out = append(out, kv)
	}
	return out, rows.Err()
}
