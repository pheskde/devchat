package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
		// TTLSeconds > 0 makes the entry expire; expired entries read as absent.
		TTLSeconds int `json:"ttl_seconds"`
		// IfAbsent writes only if the key is absent or expired (atomic).
		IfAbsent bool `json:"if_absent"`
		// IfValue writes only if the key currently holds exactly this value (atomic compare-and-set).
		IfValue *string `json:"if_value"`
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
	kv, err := s.store.SetKVCond(r.Context(), req.Scope, req.Key, req.Value,
		time.Duration(req.TTLSeconds)*time.Second, store.KVCondition{IfAbsent: req.IfAbsent, IfValue: req.IfValue})
	if errors.Is(err, store.ErrConflict) {
		// 409 carries the current entry (value empty/zero when the key is absent).
		writeJSON(w, http.StatusConflict, map[string]any{"error": "condition failed", "current": kv})
		return
	}
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
	if ifValue, ok := r.URL.Query()["if_value"]; ok && len(ifValue) > 0 {
		cur, err := s.store.DeleteKVIfValue(r.Context(), kvScope(r), key, ifValue[0])
		if errors.Is(err, store.ErrConflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "condition failed", "current": cur})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
