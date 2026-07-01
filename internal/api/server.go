// Package api exposes devchat's HTTP/WebSocket surface: session and message
// CRUD for chat clients, and a small key/value store LLM sessions can use to
// exchange information across sessions.
package api

import (
	"log"
	"net/http"

	"github.com/pheskde/devchat/internal/store"
)

type Server struct {
	store *store.Store
	hub   *hub
	log   *log.Logger
	mux   *http.ServeMux
}

func New(st *store.Store, logger *log.Logger) *Server {
	s := &Server{
		store: st,
		hub:   newHub(),
		log:   logger,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return withLogging(s.log, s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /", s.handleInfo)
	s.mux.HandleFunc("GET /api", s.handleInfo)

	s.mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/invite", s.handleInvite)
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.handleListMessages)
	s.mux.HandleFunc("POST /api/sessions/{id}/messages", s.handlePostMessage)
	s.mux.HandleFunc("GET /api/sessions/{id}/ws", s.handleWS)

	s.mux.HandleFunc("GET /api/kv", s.handleGetKV)
	s.mux.HandleFunc("PUT /api/kv", s.handlePutKV)
	s.mux.HandleFunc("DELETE /api/kv", s.handleDeleteKV)
	s.mux.HandleFunc("GET /api/kv/list", s.handleListKV)
}

func withLogging(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
