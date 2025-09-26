package hub

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wrap a websocket.Conn to serialize all writes
type connWrap struct {
	c  *websocket.Conn
	mu sync.Mutex
}

func (w *connWrap) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.WriteJSON(v)
}
func (w *connWrap) WriteMessage(mt int, p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.WriteMessage(mt, p)
}

func (w *connWrap) WriteControl(mt int, data []byte, deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.WriteControl(mt, data, deadline)
}

type room struct {
	conns map[string]*connWrap
	seq   map[string]uint64
	deliv map[string]uint64
	box   map[string][]mailItem
	start time.Time
	estd  time.Time
}

type mailItem struct {
	Seq     uint64
	Payload json.RawMessage
}

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*room
}

func New() *Hub { return &Hub{rooms: make(map[string]*room)} }

func (h *Hub) get(appID string) *room {
	r := h.rooms[appID]
	if r == nil {
		r = &room{
			conns: make(map[string]*connWrap),
			seq:   map[string]uint64{"A": 0, "B": 0},
			deliv: map[string]uint64{"A": 0, "B": 0},
			box:   map[string][]mailItem{"A": nil, "B": nil},
			start: time.Now(),
		}
		h.rooms[appID] = r
	}
	return r
}

func (h *Hub) Register(appID, side, _sid string, c *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.get(appID)
	if _, ok := r.conns[side]; ok {
		return fmt.Errorf("side %s busy", side)
	}
	r.conns[side] = &connWrap{c: c}
	return nil
}

func (h *Hub) Unregister(appID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[appID]; r != nil {
		for s, cw := range r.conns {
			if cw.c == conn {
				delete(r.conns, s)
			}
		}
		if len(r.conns) == 0 {
			delete(h.rooms, appID)
		}
	}
}

func (h *Hub) RoomSize(appID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if r := h.rooms[appID]; r != nil {
		return len(r.conns)
	}
	return 0
}

func (h *Hub) BroadcastEvent(appID string, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if r := h.rooms[appID]; r != nil {
		for _, c := range r.conns {
			_ = c.WriteJSON(payload)
		}
	}
}

func (h *Hub) Broadcast(appID string, sender *websocket.Conn, raw []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if r := h.rooms[appID]; r != nil {
		for _, cw := range r.conns {
			if cw.c != sender {
				_ = cw.WriteMessage(websocket.TextMessage, raw)
			}
		}
	}
}

func (h *Hub) Hello(appID, side, _sid string, deliveredUpTo uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[appID]; r != nil {
		if deliveredUpTo > r.deliv[side] {
			r.deliv[side] = deliveredUpTo
		}
		box := r.box[side]
		i := 0
		for i < len(box) && box[i].Seq <= r.deliv[side] {
			i++
		}
		r.box[side] = box[i:]
		if c := r.conns[side]; c != nil {
			for _, it := range r.box[side] {
				_ = c.WriteJSON(map[string]any{"type": "send", "seq": it.Seq, "payload": it.Payload})
			}
		}
	}
}

func (h *Hub) Enqueue(appID, _from, to string, payload json.RawMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.get(appID)
	seq := r.seq[to]
	r.seq[to] = seq + 1
	it := mailItem{Seq: seq, Payload: payload}
	r.box[to] = append(r.box[to], it)
	if c := r.conns[to]; c != nil {
		_ = c.WriteJSON(map[string]any{"type": "send", "seq": it.Seq, "payload": it.Payload})
	}
	return nil
}

func (h *Hub) AckUpTo(appID, side string, upTo uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[appID]; r != nil {
		if upTo > r.deliv[side] {
			r.deliv[side] = upTo
		}
		box := r.box[side]
		i := 0
		for i < len(box) && box[i].Seq <= upTo {
			i++
		}
		r.box[side] = box[i:]
	}
}

func (h *Hub) MarkEstablished(appID string) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[appID]; r != nil {
		if r.estd.IsZero() {
			r.estd = time.Now()
			return r.estd.Sub(r.start), true
		}
	}
	return 0, false
}

func (h *Hub) Ping(appID, side string, data []byte) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if r := h.rooms[appID]; r != nil {
		if cw := r.conns[side]; cw != nil {
			return cw.WriteControl(websocket.PingMessage, data, time.Now().Add(10*time.Second))
		}
	}
	return nil
}

// BroadcastEventAll sends a JSON payload to all connections in all rooms.
// Best-effort; ignores write errors.
func (h *Hub) BroadcastEventAll(payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, r := range h.rooms {
		for _, c := range r.conns {
			_ = c.WriteJSON(payload)
		}
	}
}
