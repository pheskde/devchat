package store

import (
	"context"
	"fmt"
	"time"
)

// presenceActiveWindow is how recently an author must have been seen
// (message posted, message list polled, or websocket connected) to be
// considered "active" rather than just "known".
const presenceActiveWindow = 90 * time.Second

type Presence struct {
	SessionID string    `json:"session_id"`
	Author    string    `json:"author"`
	LastSeen  time.Time `json:"last_seen"`
	Active    bool      `json:"active"`
}

// TouchPresence records that author was just seen in sessionID. A blank
// author is a no-op, since anonymous callers (e.g. plain curl without ?as=)
// have nothing meaningful to report as present.
func (s *Store) TouchPresence(ctx context.Context, sessionID, author string) error {
	if author == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO presence (session_id, author, last_seen) VALUES (?, ?, ?)
		 ON CONFLICT(session_id, author) DO UPDATE SET last_seen = excluded.last_seen`,
		sessionID, author, time.Now().UTC().Format(timeLayout),
	)
	if err != nil {
		return fmt.Errorf("touch presence: %w", err)
	}
	return nil
}

// ListPresence returns every author ever seen in sessionID, most recently
// seen first, each flagged Active if seen within presenceActiveWindow.
func (s *Store) ListPresence(ctx context.Context, sessionID string) ([]Presence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, author, last_seen FROM presence WHERE session_id = ? ORDER BY last_seen DESC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list presence: %w", err)
	}
	defer rows.Close()

	cutoff := time.Now().UTC().Add(-presenceActiveWindow)

	var out []Presence
	for rows.Next() {
		var p Presence
		var lastSeen string
		if err := rows.Scan(&p.SessionID, &p.Author, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan presence: %w", err)
		}
		p.LastSeen, _ = time.Parse(timeLayout, lastSeen)
		p.Active = p.LastSeen.After(cutoff)
		out = append(out, p)
	}
	return out, rows.Err()
}
