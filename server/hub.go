package server

import (
	"lcserver/db"
	"lcserver/proto"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type broadcastMsg struct {
	from *Session
	data []byte
}

type Hub struct {
	mu        sync.RWMutex
	sessions  map[string]*Session // uuid -> session
	broadcast chan broadcastMsg
	db        *db.Database
	upgrader  websocket.Upgrader
}

func NewHub(database *db.Database) *Hub {
	return &Hub{
		sessions:  make(map[string]*Session),
		broadcast: make(chan broadcastMsg, 128),
		db:        database,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true // Accept all origins
			},
		},
	}
}

func (h *Hub) Run() {
	for {
		msg := <-h.broadcast
		h.mu.RLock()
		recipients := make([]*Session, 0, len(h.sessions))
		for _, s := range h.sessions {
			if s != msg.from {
				recipients = append(recipients, s)
			}
		}
		h.mu.RUnlock()
		for _, s := range recipients {
			s.sendRaw(msg.data)
		}
	}
}

// registerSession updates presence synchronously so the first FriendService
// login cannot race the hub and incorrectly report an already-connected user
// as offline.
func (h *Hub) registerSession(s *Session) {
	if s == nil || s.uuid == "" {
		return
	}
	h.mu.Lock()
	others := make([]*Session, 0, len(h.sessions))
	for _, other := range h.sessions {
		if other.uuid != s.uuid {
			others = append(others, other)
		}
	}
	h.sessions[s.uuid] = s
	h.mu.Unlock()

	// Prime the new connection immediately. SubscribeV2 remains the source of
	// truth when Lunar begins tracking an entity later.
	for _, other := range others {
		other.mu.Lock()
		if other.cosmetics != nil {
			pushData := proto.EncodePlayerCosmeticsPushV2(other.uuid, other.cosmetics)
			push := proto.EncodePush(
				"type.googleapis.com/lunarclient.websocket.cosmetic.v2.PlayerCosmeticsPushV2",
				pushData,
			)
			other.mu.Unlock()
			s.sendRaw(push)
			continue
		}
		other.mu.Unlock()
	}

	if s.cosmetics != nil {
		pushData := proto.EncodePlayerCosmeticsPushV2(s.uuid, s.cosmetics)
		h.broadcast <- broadcastMsg{from: s, data: proto.EncodePush(
			"type.googleapis.com/lunarclient.websocket.cosmetic.v2.PlayerCosmeticsPushV2",
			pushData,
		)}
	}
}

func (h *Hub) unregisterSession(s *Session) {
	if s == nil || s.uuid == "" {
		return
	}
	h.mu.Lock()
	if h.sessions[s.uuid] == s {
		delete(h.sessions, s.uuid)
	}
	h.mu.Unlock()
}

func (h *Hub) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	authMode := r.Header.Get("Accept") == "application/x-protobuf"
	session := newSession(conn, h, h.db, authMode)

	go session.Run()
}

func (h *Hub) PlayerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

func (h *Hub) Players() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	players := make([]string, 0, len(h.sessions))
	for _, s := range h.sessions {
		players = append(players, s.username)
	}
	return players
}

func (h *Hub) sendToUUID(uuid string, data []byte) bool {
	h.mu.RLock()
	s := h.sessions[uuid]
	h.mu.RUnlock()
	if s == nil {
		return false
	}
	s.sendRaw(data)
	return true
}

func (h *Hub) isOnline(uuid string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[uuid] != nil
}

func (h *Hub) session(uuid string) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[uuid]
}

func (h *Hub) isCurrent(s *Session) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return s != nil && h.sessions[s.uuid] == s
}
