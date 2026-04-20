package usecases

import (
	"sync"

	"github.com/gorilla/websocket"
)

// WSHub stores websocket clients grouped by session ID.
type WSHub struct {
	mu      sync.RWMutex
	clients map[string]map[*websocket.Conn]struct{}
}

func NewWSHub() *WSHub {
	return &WSHub{clients: make(map[string]map[*websocket.Conn]struct{})}
}

func (h *WSHub) Register(sessionID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[sessionID]; !ok {
		h.clients[sessionID] = make(map[*websocket.Conn]struct{})
	}
	h.clients[sessionID][conn] = struct{}{}
}

func (h *WSHub) Unregister(sessionID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conns, ok := h.clients[sessionID]
	if !ok {
		return
	}
	delete(conns, conn)
	if len(conns) == 0 {
		delete(h.clients, sessionID)
	}
}

func (h *WSHub) Broadcast(sessionID string, message []byte) {
	h.mu.RLock()
	conns := make([]*websocket.Conn, 0)
	if m, ok := h.clients[sessionID]; ok {
		for conn := range m {
			conns = append(conns, conn)
		}
	}
	h.mu.RUnlock()

	for _, conn := range conns {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			h.Unregister(sessionID, conn)
			_ = conn.Close()
		}
	}
}
