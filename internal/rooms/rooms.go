package rooms

import (
	"sync"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/config"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/logs"
	"github.com/gorilla/websocket"
)

// Store is a minimal in-memory room registry.
// It supports exactly what the WS handler needs: Join/Leave/Relay/Broadcast/RoomSize.
type Store struct {
	mu       sync.RWMutex
	rooms    map[string]*Room
	maxPeers int // usually 2
}

type Room struct {
	peers map[string]*websocket.Conn // peerId -> ws
}

// NewStore matches the old ctor signature used in main.go.
// We only use cfg.MaxPeersPerRoom; logger is accepted for compatibility.
func NewStore(cfg config.Config, _ logs.Logger) *Store {
	max := cfg.MaxPeersPerRoom
	if max <= 0 {
		max = 2
	}
	return &Store{
		rooms:    make(map[string]*Room),
		maxPeers: max,
	}
}

// Close gracefully closes all sockets and clears memory (so `defer store.Close()` compiles and works).
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rooms {
		for id, ws := range r.peers {
			_ = ws.Close()
			delete(r.peers, id)
		}
	}
	s.rooms = make(map[string]*Room)
}

// Join adds peerId to roomId if there's capacity.
// Returns (selfID, otherPeerIDs, error). If full, error == ErrRoomFull.
func (s *Store) Join(roomID, peerID string, ws *websocket.Conn) (string, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.rooms[roomID]
	if r == nil {
		r = &Room{peers: make(map[string]*websocket.Conn, s.maxPeers)}
		s.rooms[roomID] = r
	}

	// If room already has max distinct peers and this peerId isn't present, reject.
	if len(r.peers) >= s.maxPeers && r.peers[peerID] == nil {
		return "", nil, ErrRoomFull
	}

	// Add/replace this peerId with the new socket.
	r.peers[peerID] = ws

	// Collect others
	others := make([]string, 0, len(r.peers)-1)
	for id := range r.peers {
		if id != peerID {
			others = append(others, id)
		}
	}
	return peerID, others, nil
}

// Leave removes peerId; deletes the room if it becomes empty.
func (s *Store) Leave(roomID, peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r := s.rooms[roomID]; r != nil {
		delete(r.peers, peerID)
		if len(r.peers) == 0 {
			delete(s.rooms, roomID)
		}
	}
}

// RoomSize returns the number of peers currently in the room.
func (s *Store) RoomSize(roomID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r := s.rooms[roomID]; r != nil {
		return len(r.peers)
	}
	return 0
}

// Relay sends a signaling payload either to a specific peer (when `to` != "")
// or to all other peers in the room (when `to` == "").
func (s *Store) Relay(roomID, from, to string, payload any) {
	s.mu.RLock()
	r := s.rooms[roomID]
	s.mu.RUnlock()
	if r == nil {
		return
	}

	msg := map[string]any{"type": "signal", "from": from, "data": payload}

	if to != "" {
		if ws := r.peers[to]; ws != nil {
			_ = ws.WriteJSON(msg)
		}
		return
	}
	for id, ws := range r.peers {
		if id == from {
			continue
		}
		_ = ws.WriteJSON(msg)
	}
}

// Broadcast sends a JSON payload to all peers in the room.
func (s *Store) Broadcast(roomID string, payload any) {
	s.mu.RLock()
	r := s.rooms[roomID]
	s.mu.RUnlock()
	if r == nil {
		return
	}
	for _, ws := range r.peers {
		_ = ws.WriteJSON(payload)
	}
}

// ErrRoomFull signals the room has reached capacity.
var ErrRoomFull = errRoomFull{}

type errRoomFull struct{}

func (errRoomFull) Error() string { return "room-full" }
