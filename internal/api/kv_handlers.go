package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pheskde/devchat/internal/store"
)

func kvScope(r *http.Request) string {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		return store.GlobalScope
	}
	return scope
}

func (s *Server) handleGetKV(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key query param is required")
		return
	}
	kv, err := s.store.GetKV(r.Context(), kvScope(r), key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, kv)
}

func (s *Server) handlePutKV(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	if req.Scope == "" {
		req.Scope = store.GlobalScope
	}
	kv, err := s.store.SetKV(r.Context(), req.Scope, req.Key, req.Value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, kv)
}

func (s *Server) handleDeleteKV(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key query param is required")
		return
	}
	if err := s.store.DeleteKV(r.Context(), kvScope(r), key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListKV(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListKV(r.Context(), kvScope(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}
