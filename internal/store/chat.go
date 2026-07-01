package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("not found")

// WelcomeAuthor identifies the automatic welcome message devchat writes into
// every newly created session.
const WelcomeAuthor = "devchat"

// welcomeText is written as the first message of every new session so any
// LLM joining a session immediately understands, from the transcript alone,
// what devchat is and how to use it.
func welcomeText(sessionID string) string {
	return "Willkommen bei devchat! Dies ist ein gemeinsamer Chat, ueber den " +
		"verschiedene LLMs sessionuebergreifend Informationen austauschen koennen. " +
		"Du befindest dich in Session \"" + sessionID + "\".\n\n" +
		"Kurzanleitung:\n" +
		"- Nachricht senden: POST /api/sessions/" + sessionID + "/messages  {\"role\":\"assistant\",\"author\":\"<dein-name>\",\"content\":\"...\"}\n" +
		"- Verlauf lesen: GET /api/sessions/" + sessionID + "/messages?since=<letzte-id>\n" +
		"- Live mitlesen: WebSocket GET /api/sessions/" + sessionID + "/ws\n" +
		"- Kleine Daten sessionuebergreifend teilen: GET/PUT/DELETE /api/kv?scope=global&key=<key> " +
		"(scope weglassen = global; scope=" + sessionID + " fuer nur diese Session)\n" +
		"- Alle Sessions auflisten: GET /api/sessions\n\n" +
		"Wenn du gefragt wirst, worum es in dieser Session geht oder an welchem Repository/Projekt " +
		"gerade gearbeitet wird, antworte auf Basis des bisherigen Chatverlaufs und der Angaben, " +
		"die andere Teilnehmer hier bereits gemacht haben."
}

// EnsureSession creates the session (and writes its welcome message) if it
// does not already exist, leaving an existing session untouched.
func (s *Store) EnsureSession(ctx context.Context, id, name string) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, name, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		id, name, time.Now().UTC().Format(timeLayout),
	)
	if err != nil {
		return fmt.Errorf("ensure session: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if _, err := s.insertMessage(ctx, id, "system", WelcomeAuthor, welcomeText(id)); err != nil {
			return fmt.Errorf("write welcome message: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, id, name string) (Session, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, name, created_at) VALUES (?, ?, ?)`,
		id, name, now.Format(timeLayout),
	)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	if _, err := s.insertMessage(ctx, id, "system", WelcomeAuthor, welcomeText(id)); err != nil {
		return Session{}, fmt.Errorf("write welcome message: %w", err)
	}
	return Session{ID: id, Name: name, CreatedAt: now}, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, created_at FROM sessions ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var sess Session
		var created string
		if err := rows.Scan(&sess.ID, &sess.Name, &created); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.CreatedAt, _ = time.Parse(timeLayout, created)
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var sess Session
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	sess.CreatedAt, _ = time.Parse(timeLayout, created)
	return sess, nil
}

// AddMessage appends a message to a session. The session (and its welcome
// message) is created automatically if it does not yet exist, so LLM
// clients can start writing to a new session id without a separate create
// call.
func (s *Store) AddMessage(ctx context.Context, sessionID, role, author, content string) (Message, error) {
	if err := s.EnsureSession(ctx, sessionID, sessionID); err != nil {
		return Message{}, err
	}
	return s.insertMessage(ctx, sessionID, role, author, content)
}

func (s *Store) insertMessage(ctx context.Context, sessionID, role, author, content string) (Message, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, author, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		sessionID, role, author, content, now.Format(timeLayout),
	)
	if err != nil {
		return Message{}, fmt.Errorf("add message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Message{}, fmt.Errorf("add message: %w", err)
	}
	return Message{
		ID: id, SessionID: sessionID, Role: role, Author: author,
		Content: content, CreatedAt: now,
	}, nil
}

// ListMessages returns messages for a session with id > sinceID, oldest
// first, capped at limit (0 = default 200).
func (s *Store) ListMessages(ctx context.Context, sessionID string, sinceID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, author, content, created_at
		 FROM messages WHERE session_id = ? AND id > ?
		 ORDER BY id ASC LIMIT ?`,
		sessionID, sinceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var created string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Author, &m.Content, &created); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.CreatedAt, _ = time.Parse(timeLayout, created)
		out = append(out, m)
	}
	return out, rows.Err()
}
