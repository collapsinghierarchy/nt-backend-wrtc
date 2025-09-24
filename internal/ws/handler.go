// internal/ws/handler.go
package ws

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/config"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/logs"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rooms"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var up = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type wireMsg struct {
	Type string      `json:"type"`
	To   string      `json:"to,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

func NewHandler(cfg config.Config, log logs.Logger, store *rooms.Store) http.Handler {
	l := log.Named("ws")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			w.Header().Set("Connection", "Upgrade")
			w.Header().Set("Upgrade", "websocket")
			http.Error(w, "upgrade required", http.StatusUpgradeRequired)
			return
		}

		appID := r.URL.Query().Get("appID")
		side := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("side")))
		if _, err := uuid.Parse(appID); err != nil || (side != "A" && side != "B") {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}

		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			l.Warn("upgrade failed", logs.F("err", err))
			return
		}
		l.Info("ws-upgraded", logs.F("remote", r.RemoteAddr), logs.F("appID", appID), logs.F("side", side))
		defer func() {
			l.Info("ws-closed", logs.F("remote", r.RemoteAddr), logs.F("appID", appID), logs.F("side", side))
			_ = c.Close()
		}()

		// deadlines / heartbeat
		_ = c.SetReadDeadline(time.Now().Add(cfg.Handshake))
		c.SetPongHandler(func(string) error {
			_ = c.SetReadDeadline(time.Now().Add(cfg.Heartbeat * 2))
			return nil
		})
		ticker := time.NewTicker(cfg.Heartbeat)
		defer ticker.Stop()
		go func() {
			for range ticker.C {
				_ = c.WriteControl(websocket.PingMessage, []byte(""), time.Now().Add(2*time.Second))
			}
		}()

		// ---------- AUTO-JOIN HAPPENS HERE (no reads before this) ----------
		l.Info("JOIN-BEFORE", logs.F("appID", appID), logs.F("side", side), logs.F("size", store.RoomSize(appID)))

		self, _, err := store.Join(appID, side, c)
		if err == rooms.ErrRoomFull {
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1008, "room-full"), time.Now().Add(1*time.Second))
			l.Info("join-rejected-room-full", logs.F("appID", appID), logs.F("side", side))
			return
		}
		if err != nil {
			l.Warn("join-failed", logs.F("appID", appID), logs.F("side", side), logs.F("err", err))
			return
		}

		after := store.RoomSize(appID)
		l.Info("JOIN-AFTER", logs.F("appID", appID), logs.F("peer", self), logs.F("side", side), logs.F("size", after))

		if after == 2 {
			l.Info("ROOM_FULL_EMIT", logs.F("appID", appID))
			store.Broadcast(appID, map[string]any{"type": "room_full"})
		}

		_ = c.SetReadDeadline(time.Now().Add(cfg.Heartbeat * 2))

		// read/relay
		for {
			var msg wireMsg
			if err := c.ReadJSON(&msg); err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, io.EOF) {
					break
				}
				break
			}
			if msg.Type == "signal" {
				store.Relay(appID, self, msg.To, msg.Data)
			}
			// ignore everything else (hello, etc.)
		}

		// cleanup
		store.Leave(appID, self)
		store.Broadcast(appID, map[string]any{"type": "peer-left", "peerId": self})
	})
}
