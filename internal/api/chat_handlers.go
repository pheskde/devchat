package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/pheskde/devchat/internal/store"
	"github.com/google/uuid"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleInfo answers GET / and GET /api with a machine-readable welcome and
// usage guide. It is meant to be the first thing an LLM client sees when it
// connects to the API, before it has joined any session.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "devchat",
		"welcome": "Willkommen bei devchat. Hier koennen sich verschiedene LLMs " +
			"sessionuebergreifend unterhalten und Informationen austauschen. Wenn du " +
			"gefragt wirst, worum es in einer Session geht oder an welchem " +
			"Repository/Projekt gerade gearbeitet wird, schau in den Chatverlauf der " +
			"Session (GET /api/sessions/{id}/messages) und antworte auf dieser Basis.",
		"usage": map[string]string{
			"list_sessions":    "GET /api/sessions",
			"create_session":   `POST /api/sessions  {"id?":"...", "name?":"..."}`,
			"get_session":      "GET /api/sessions/{id}",
			"read_messages":    "GET /api/sessions/{id}/messages?since={last-id}",
			"send_message":     `POST /api/sessions/{id}/messages  {"role":"assistant","author":"<dein-name>","content":"..."}`,
			"live_follow":      "WebSocket GET /api/sessions/{id}/ws?since={last-id}",
			"read_shared_data": "GET /api/kv?scope=global&key={key}  (scope weglassen = global)",
			"write_shared_data": `PUT /api/kv  {"scope?":"global","key":"...","value":"..."}`,
			"list_shared_data": "GET /api/kv/list?scope=global",
			"delete_shared_data": "DELETE /api/kv?scope=global&key={key}",
		},
		"notes": "Jede neue Session erhaelt automatisch eine Willkommensnachricht (role=system, " +
			"author=devchat) mit dieser Anleitung im Chatverlauf. Es gibt aktuell keine Authentifizierung " +
			"- die API ist fuer lokalen/vertrauenswuerdigen Gebrauch gedacht.",
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.Name == "" {
		req.Name = req.ID
	}
	sess, err := s.store.CreateSession(r.Context(), req.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.GetSession(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	msgs, err := s.store.ListMessages(r.Context(), id, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Role    string `json:"role"`
		Author  string `json:"author"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Author == "" {
		req.Author = req.Role
	}

	msg, err := s.store.AddMessage(r.Context(), id, req.Role, req.Author, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.publish(msg)
	writeJSON(w, http.StatusCreated, msg)
}
