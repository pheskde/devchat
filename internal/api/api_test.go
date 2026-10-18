package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pheskde/devchat/internal/store"
)

const testToken = "s3cret-token"

func newTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ts := httptest.NewServer(New(st, log.New(io.Discard, "", 0), token).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, method, url, token string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestAuth(t *testing.T) {
	ts := newTestServer(t, testToken)
	cases := []struct {
		name, path, token string
		want              int
	}{
		{"no token", "/api/sessions", "", 401},
		{"wrong token", "/api/sessions", "nope", 401},
		{"prefix of token", "/api/sessions", "s3cret", 401},
		{"right token", "/api/sessions", testToken, 200},
		{"root without token", "/", "", 401},
		{"health open", "/health", "", 200},
		{"healthz open", "/healthz", "", 200},
		{"kv without token", "/api/kv/list", "", 401},
	}
	for _, c := range cases {
		if got, _ := do(t, "GET", ts.URL+c.path, c.token, nil); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	// A non-Bearer scheme must not pass either.
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Basic "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("basic scheme: got %d, want 401", resp.StatusCode)
	}
}

func TestNoTokenConfiguredLeavesAPIOpen(t *testing.T) {
	ts := newTestServer(t, "")
	if got, _ := do(t, "GET", ts.URL+"/api/sessions", "", nil); got != 200 {
		t.Errorf("got %d, want 200", got)
	}
}

func put(t *testing.T, ts *httptest.Server, body map[string]any) int {
	t.Helper()
	code, _ := do(t, "PUT", ts.URL+"/api/kv", testToken, body)
	return code
}

func TestKVIfAbsentAndTTL(t *testing.T) {
	ts := newTestServer(t, testToken)
	lock := map[string]any{"scope": "dc", "key": "lock/demo", "value": "A", "if_absent": true, "ttl_seconds": 1}
	if c := put(t, ts, lock); c != 200 {
		t.Fatalf("first acquire: %d", c)
	}
	lock["value"] = "B"
	code, body := do(t, "PUT", ts.URL+"/api/kv", testToken, lock)
	if code != 409 {
		t.Fatalf("second acquire: got %d, want 409", code)
	}
	var conflict struct {
		Current store.KV `json:"current"`
	}
	json.Unmarshal(body, &conflict)
	if conflict.Current.Value != "A" {
		t.Errorf("409 should report holder A, got %q", conflict.Current.Value)
	}
	time.Sleep(1200 * time.Millisecond)
	if code, _ := do(t, "GET", ts.URL+"/api/kv?scope=dc&key=lock/demo", testToken, nil); code != 404 {
		t.Errorf("expired entry should read as absent, got %d", code)
	}
	if c := put(t, ts, lock); c != 200 {
		t.Errorf("acquire after expiry: %d", c)
	}
}

func TestKVCompareAndSet(t *testing.T) {
	ts := newTestServer(t, testToken)
	put(t, ts, map[string]any{"scope": "dc", "key": "k", "value": "v1"})
	if c := put(t, ts, map[string]any{"scope": "dc", "key": "k", "value": "v2", "if_value": "wrong"}); c != 409 {
		t.Errorf("CAS with wrong value: %d", c)
	}
	if c := put(t, ts, map[string]any{"scope": "dc", "key": "k", "value": "v2", "if_value": "v1"}); c != 200 {
		t.Errorf("CAS with right value: %d", c)
	}
	if c := put(t, ts, map[string]any{"scope": "dc", "key": "missing", "value": "x", "if_value": "v1"}); c != 409 {
		t.Errorf("CAS on absent key: %d", c)
	}
	if code, _ := do(t, "DELETE", ts.URL+"/api/kv?scope=dc&key=k&if_value=nope", testToken, nil); code != 409 {
		t.Errorf("conditional delete mismatch: %d", code)
	}
	if code, _ := do(t, "DELETE", ts.URL+"/api/kv?scope=dc&key=k&if_value=v2", testToken, nil); code != 204 {
		t.Errorf("conditional delete match: %d", code)
	}
	if code, _ := do(t, "GET", ts.URL+"/api/kv?scope=dc&key=k", testToken, nil); code != 404 {
		t.Errorf("deleted key still readable: %d", code)
	}
}

// Many clients race for the same lock: exactly one may win.
func TestKVIfAbsentConcurrentExactlyOneWinner(t *testing.T) {
	ts := newTestServer(t, testToken)
	const n = 40
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c := put(t, ts, map[string]any{"scope": "dc", "key": "lock/demo", "value": "owner" + strconv.Itoa(i),
				"if_absent": true, "ttl_seconds": 60})
			if c == 200 {
				wins.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want exactly 1", wins.Load())
	}
}

func TestKVListHidesExpired(t *testing.T) {
	ts := newTestServer(t, testToken)
	put(t, ts, map[string]any{"scope": "dc", "key": "a", "value": "1", "ttl_seconds": 1})
	put(t, ts, map[string]any{"scope": "dc", "key": "b", "value": "2"})
	time.Sleep(1200 * time.Millisecond)
	_, body := do(t, "GET", ts.URL+"/api/kv/list?scope=dc", testToken, nil)
	var items []store.KV
	json.Unmarshal(body, &items)
	if len(items) != 1 || items[0].Key != "b" {
		t.Errorf("list = %+v, want only b", items)
	}
}

func TestImportKeepsIDsAndIsIdempotent(t *testing.T) {
	ts := newTestServer(t, testToken)
	msgs := []map[string]any{
		{"id": 5, "role": "agent", "author": "A", "content": "one"},
		{"id": 9, "role": "agent", "author": "B", "content": "two"},
	}
	imp := func(m []map[string]any) map[string]int {
		code, body := do(t, "POST", ts.URL+"/api/sessions/eloniq/import", testToken, map[string]any{"messages": m})
		if code != 200 {
			t.Fatalf("import: %d %s", code, body)
		}
		var r map[string]int
		json.Unmarshal(body, &r)
		return r
	}
	if r := imp(msgs); r["added"] != 2 {
		t.Errorf("first import added %d", r["added"])
	}
	msgs = append(msgs, map[string]any{"id": 12, "role": "agent", "author": "C", "content": "three"})
	if r := imp(msgs); r["added"] != 1 {
		t.Errorf("delta import added %d, want 1", r["added"])
	}
	// New messages continue after the highest imported id.
	code, body := do(t, "POST", ts.URL+"/api/sessions/eloniq/messages", testToken,
		map[string]any{"role": "agent", "author": "D", "content": "four"})
	var m store.Message
	json.Unmarshal(body, &m)
	if code != 201 || m.ID != 13 {
		t.Errorf("next id = %d (code %d), want 13", m.ID, code)
	}
	_, body = do(t, "GET", ts.URL+"/api/sessions/eloniq/messages?since=5", testToken, nil)
	var got []store.Message
	json.Unmarshal(body, &got)
	if len(got) != 3 || got[0].ID != 9 {
		t.Errorf("since=5 returned %+v", got)
	}
}
