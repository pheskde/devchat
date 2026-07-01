package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pheskde/devchat/internal/store"

	"github.com/gorilla/websocket"
)

type client struct {
	apiBase string
	http    *http.Client
}

func newClient(apiBase string) *client {
	return &client{
		apiBase: strings.TrimRight(apiBase, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *client) get(path string, out any) error {
	resp, err := c.http.Get(c.apiBase + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: unexpected status %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) listSessions() ([]store.Session, error) {
	var sessions []store.Session
	if err := c.get("/api/sessions", &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (c *client) getInvite(sessionID string) (string, error) {
	resp, err := c.http.Get(c.apiBase + "/api/sessions/" + url.PathEscape(sessionID) + "/invite")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("invite: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *client) listMessages(sessionID string, since int64) ([]store.Message, error) {
	var msgs []store.Message
	path := fmt.Sprintf("/api/sessions/%s/messages?since=%d&limit=500", url.PathEscape(sessionID), since)
	if err := c.get(path, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// watchSession opens a websocket to the given session and forwards every
// message it receives on the returned channel until stop is closed. The
// channel is closed when the connection ends.
func (c *client) watchSession(sessionID string, since int64, stop <-chan struct{}) <-chan store.Message {
	out := make(chan store.Message)

	wsURL := strings.Replace(c.apiBase, "http", "ws", 1) +
		fmt.Sprintf("/api/sessions/%s/ws?since=%d", url.PathEscape(sessionID), since)

	go func() {
		defer close(out)

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		go func() {
			<-stop
			conn.Close()
		}()

		for {
			var m store.Message
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			select {
			case out <- m:
			case <-stop:
				return
			}
		}
	}()

	return out
}
