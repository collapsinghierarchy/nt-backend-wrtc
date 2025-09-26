package ws_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/hub"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/ws"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func dial(t *testing.T, ts *httptest.Server, appID, side string) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	q := u.Query()
	q.Set("appID", appID)
	q.Set("side", side)
	u.RawQuery = q.Encode()

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial %s failed: %v", side, err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	return c
}

func TestWSRoomFullAndRelay(t *testing.T) {
	h := hub.New()
	mux := http.NewServeMux()
	// allow all origins (dev=true), small heartbeat for test
	handler := ws.NewWSHandler(h, nil, nil, true, ws.WithLimits(1<<20, 2*time.Second))
	mux.Handle("/ws", handler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	appID := uuid.NewString()

	// A connects
	a := dial(t, ts, appID, "A")
	defer a.Close()

	// B connects -> both should receive room_full
	b := dial(t, ts, appID, "B")
	defer b.Close()

	type frame struct {
		Type string `json:"type"`
	}
	readType := func(c *websocket.Conn) string {
		_, p, err := c.ReadMessage()
		if err != nil {
			return ""
		}
		var f frame
		_ = json.Unmarshal(p, &f)
		return f.Type
	}

	// Expect room_full on A or B (order not guaranteed). Read from both with short deadlines.
	gotA := readType(a)
	gotB := readType(b)
	if gotA != "room_full" && gotB != "room_full" {
		t.Fatalf("expected room_full on connect, got A=%q B=%q", gotA, gotB)
	}

	// Relay a small signal from A -> B
	if err := a.WriteMessage(websocket.TextMessage, []byte(`{"type":"offer","sdp":"x"}`)); err != nil {
		t.Fatalf("write offer: %v", err)
	}
	_ = b.SetReadDeadline(time.Now().Add(1 * time.Second))
	mt, payload, err := b.ReadMessage()
	if err != nil {
		t.Fatalf("read relay: %v", err)
	}
	if mt != websocket.TextMessage || string(payload) != `{"type":"offer","sdp":"x"}` {
		t.Fatalf("relay mismatch: %q", payload)
	}
}
