package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// devchat has no auth layer yet and is meant for localhost/LAN use only;
	// accept upgrades from any origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

const wsPingInterval = 30 * time.Second

// handleWS streams newly posted messages for a session to the caller as
// JSON text frames. On connect it first replays any messages the client
// missed, based on the optional "since" query param (a message id).
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Printf("ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Subscribe before replaying backlog so no message published in between
	// is lost.
	sub := s.hub.subscribe(id)
	defer s.hub.unsubscribe(id, sub)

	backlog, err := s.store.ListMessages(r.Context(), id, since, 0)
	if err != nil {
		s.log.Printf("ws backlog fetch failed: %v", err)
		return
	}
	lastSent := since
	for _, m := range backlog {
		if err := conn.WriteJSON(m); err != nil {
			return
		}
		lastSent = m.ID
	}

	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case m, ok := <-sub:
			if !ok {
				return
			}
			if m.ID <= lastSent {
				continue
			}
			if err := conn.WriteJSON(m); err != nil {
				return
			}
			lastSent = m.ID
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
