package ws

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/hub"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/metrics"
)

type wsOpts struct {
	readBuf, writeBuf int
	maxMsg            int64
	heartbeat         time.Duration
	rl                interface{ AllowWS(*http.Request) bool } // nil => no limit
}
type Option func(*wsOpts)

func WithRateLimiter(rl interface{ AllowWS(*http.Request) bool }) Option {
	return func(o *wsOpts) { o.rl = rl }
}

func WithBuffers(read, write int) Option {
	return func(o *wsOpts) { o.readBuf, o.writeBuf = read, write }
}
func WithLimits(max int64, heartbeat time.Duration) Option {
	return func(o *wsOpts) { o.maxMsg, o.heartbeat = max, heartbeat }
}

// originAllowed checks if the Origin header is in the allowlist.
// - Empty Origin (non-browser clients) is allowed.
// - Items in allowedOrigins can be full origins (https://example.com) or hostnames (example.com).
func originAllowed(allowedOrigins []string, origin string) bool {
	if origin == "" {
		return true // non-browser clients typically omit Origin
	}
	if len(allowedOrigins) == 0 {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	for _, a := range allowedOrigins {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		// exact origin match
		if strings.EqualFold(a, origin) {
			return true
		}
		// hostname match
		if strings.EqualFold(a, host) {
			return true
		}
	}
	return false
}

func NewWSHandler(h *hub.Hub, allowedOrigins []string, lg *slog.Logger, dev bool, options ...Option) http.Handler {
	if lg == nil {
		lg = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	cfg := wsOpts{readBuf: 64 << 10, writeBuf: 64 << 10, maxMsg: 1 << 20, heartbeat: 60 * time.Second}
	for _, opt := range options {
		opt(&cfg)
	}
	pingPeriod := cfg.heartbeat * 9 / 10

	up := websocket.Upgrader{
		// Use the same policy everywhere: allow empty Origin (CLI),
		// allow full-origins or hostnames from allowedOrigins.
		CheckOrigin: func(r *http.Request) bool {
			if dev {
				return true
			}
			return originAllowed(allowedOrigins, r.Header.Get("Origin"))
		},
		ReadBufferSize:  cfg.readBuf,
		WriteBufferSize: cfg.writeBuf,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appID := r.URL.Query().Get("appID")
		if _, err := uuid.Parse(appID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}
		side := r.URL.Query().Get("side")
		if side != "A" && side != "B" {
			http.Error(w, "invalid side", http.StatusBadRequest)
			return
		}
		sessionID := r.URL.Query().Get("sid")

		// (Optional) for a clearer 403 body,
		if !dev && !originAllowed(allowedOrigins, r.Header.Get("Origin")) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}

		if cfg.rl != nil && !cfg.rl.AllowWS(r) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			lg.Warn("ws upgrade failed", "err", err)
			return
		}
		defer conn.Close()
		metrics.WSConnections.Inc()
		conn.SetReadLimit(cfg.maxMsg)
		_ = conn.SetReadDeadline(time.Now().Add(cfg.heartbeat))
		conn.SetPongHandler(func(data string) error {
			if err := conn.SetReadDeadline(time.Now().Add(cfg.heartbeat)); err != nil {
				return err
			}
			if ts, err := strconv.ParseInt(data, 10, 64); err == nil {
				metrics.WSRTTSeconds.Observe(time.Since(time.Unix(0, ts)).Seconds())
			}
			return nil
		})

		if err := h.Register(appID, side, sessionID, conn); err != nil {
			lg.Warn("hub register failed", "err", err, "appID", appID, "side", side)
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()))
			return
		}
		defer h.Unregister(appID, conn)

		if h.RoomSize(appID) == 2 {
			h.BroadcastEvent(appID, map[string]any{"type": "room_full"})
		}

		go func() {
			t := time.NewTicker(pingPeriod)
			defer t.Stop()
			for range t.C {
				payload := []byte(strconv.FormatInt(time.Now().UnixNano(), 10))
				if err := h.Ping(appID, side, payload); err != nil {
					_ = conn.Close()
					return
				}
			}
		}()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				// quiet on normal closes
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					lg.Warn("ws read error", "err", err)
				}
				return
			}
			metrics.WSFrameSize.WithLabelValues("in").Observe(float64(len(msg)))
			if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
				continue
			}
			var peek struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(msg, &peek); err != nil {
				continue
			}
			t := strings.ToLower(peek.Type)
			if t == "" {
				t = "unknown"
			}
			metrics.SignalMsg.WithLabelValues(t).Inc()
			metrics.SignalBytes.WithLabelValues("in", t).Add(float64(len(msg)))
			switch t {
			case "offer", "answer", "ice", "sender_ready":
				metrics.WSFrameSize.WithLabelValues("out").Observe(float64(len(msg)))
				metrics.SignalBytes.WithLabelValues("out", t).Add(float64(len(msg)))
				h.Broadcast(appID, conn, msg)
			case "hello":
				var m struct {
					DeliveredUpTo uint64 `json:"deliveredUpTo"`
				}
				if err := json.Unmarshal(msg, &m); err == nil {
					h.Hello(appID, side, sessionID, m.DeliveredUpTo)
				}
			case "send":
				var m struct {
					To      string          `json:"to"`
					Payload json.RawMessage `json:"payload"`
				}
				if err := json.Unmarshal(msg, &m); err == nil {
					_ = h.Enqueue(appID, side, strings.ToUpper(m.To), m.Payload)
				}
			//{"type":"telemetry","event":"ice-connected"}
			case "telemetry":
				var tm struct {
					Event  string `json:"event"`
					Reason string `json:"reason"`
					Mode   string `json:"mode"`
				}
				_ = json.Unmarshal(msg, &tm)
				mode := strings.ToLower(strings.TrimSpace(tm.Mode))
				if mode == "" {
					mode = "unspecified"
				}
				switch strings.ToLower(tm.Event) {
				case "ice-connected":
					if dt, first := h.MarkEstablished(appID); first {
						metrics.SessionEstablished.WithLabelValues(mode).Inc()
						metrics.SessionTTF.Observe(dt.Seconds())
					}
				case "ice-failed":
					metrics.SessionFailed.WithLabelValues("ice-failed").Inc()
				default:
					// no-op
				}
			default:
				// ignore
			}
		}
	})
}
