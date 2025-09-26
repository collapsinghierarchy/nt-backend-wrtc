// internal/ws/ws_race_test.go
package ws_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/hub"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/ws"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func TestPingAndBroadcastNoRace(t *testing.T) {
	h := hub.New()
	mux := http.NewServeMux()
	// small heartbeat to trigger frequent pings
	mux.Handle("/ws", ws.NewWSHandler(h, nil, nil, true, ws.WithLimits(1<<20, 200*time.Millisecond)))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	app := uuid.NewString()
	a := dial(t, ts, app, "A") // dial() comes from ws_handler_integration_test.go
	defer a.Close()
	b := dial(t, ts, app, "B")
	defer b.Close()

	// hammer: send many offers from A while server pings are running
	for i := 0; i < 200; i++ {
		msg := []byte(`{"type":"offer","sdp":"` + strconv.Itoa(i) + `"}`)
		if err := a.WriteMessage(websocket.TextMessage, msg); err != nil {
			t.Fatalf("write: %v", err)
		}

		// Read on B until we see an "offer" (skip room_full or other noise)
		var gotType string
		for attempts := 0; attempts < 5; attempts++ {
			_ = b.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			_, payload, err := b.ReadMessage()
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var peek struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(payload, &peek)
			gotType = peek.Type
			if gotType == "offer" {
				break
			}
			// else: keep looping; e.g., skip "room_full"
		}
		if gotType != "offer" {
			t.Fatalf("expected offer, got %q", gotType)
		}
	}
}

func TestMailboxSendHelloDelivered(t *testing.T) {
	h := hub.New()
	mux := http.NewServeMux()
	mux.Handle("/ws", ws.NewWSHandler(h, nil, nil, true, ws.WithLimits(1<<20, 1*time.Second)))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	app := uuid.NewString()
	a := dial(t, ts, app, "A")
	defer a.Close()
	b := dial(t, ts, app, "B")
	defer b.Close()

	// B says hello with deliveredUpTo=0 (trims mailbox if needed)
	if err := b.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello","deliveredUpTo":0}`)); err != nil {
		t.Fatalf("hello write: %v", err)
	}

	// A enqueues a mailbox item for B
	if err := a.WriteMessage(websocket.TextMessage, []byte(`{"type":"send","to":"B","payload":{"note":"hi"}}`)); err != nil {
		t.Fatalf("send write: %v", err)
	}

	// B should receive a "send" frame (skip any prior "room_full")
	var got struct {
		Type    string          `json:"type"`
		Seq     uint64          `json:"seq"`
		Payload json.RawMessage `json:"payload"`
	}
	for attempts := 0; attempts < 5; attempts++ {
		_ = b.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, p, err := b.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		_ = json.Unmarshal(p, &got)
		if got.Type == "send" {
			break
		}
	}
	if got.Type != "send" {
		t.Fatalf("expected send, got %q", got.Type)
	}

	// Ack delivery
	if err := b.WriteMessage(websocket.TextMessage, []byte(`{"type":"delivered","upTo":100}`)); err != nil {
		t.Fatalf("delivered write: %v", err)
	}
}
