package api

import (
	"sync"

	"github.com/pheskde/devchat/internal/store"
)

// hub fans out newly written messages to any websocket subscribers of a
// given session, so the viewer can live-follow a chat without polling.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.Message]struct{}
}

func newHub() *hub {
	return &hub{subs: make(map[string]map[chan store.Message]struct{})}
}

func (h *hub) subscribe(sessionID string) chan store.Message {
	ch := make(chan store.Message, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[sessionID] == nil {
		h.subs[sessionID] = make(map[chan store.Message]struct{})
	}
	h.subs[sessionID][ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(sessionID string, ch chan store.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[sessionID], ch)
	if len(h.subs[sessionID]) == 0 {
		delete(h.subs, sessionID)
	}
	close(ch)
}

func (h *hub) publish(m store.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[m.SessionID] {
		select {
		case ch <- m:
		default:
			// Slow subscriber: drop the message rather than block publishers.
		}
	}
}
