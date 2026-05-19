package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// hub manages active WebSocket connections and broadcasts messages to all of them.
type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]authUser
	logger  *slog.Logger
}

type wsHeartbeatEvent struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
}

func newHub(logger *slog.Logger) *hub {
	h := &hub{
		clients: make(map[*websocket.Conn]authUser),
		logger:  logger,
	}
	go h.startHeartbeatLoop()
	return h
}

func (h *hub) startHeartbeatLoop() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		h.broadcast(wsHeartbeatEvent{Type: "heartbeat", TS: time.Now().Unix()})
	}
}

// broadcast sends msg as JSON to every connected client.
// Slow or disconnected clients are removed silently.
func (h *hub) broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("ws broadcast marshal failed", "error", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := c.Write(ctx, websocket.MessageText, data); err != nil {
			h.logger.Debug("ws write failed, removing client", "error", err)
			delete(h.clients, c)
		}
		cancel()
	}
}

// broadcastForUsers sends a user-specific JSON message to every connected client.
// Slow or disconnected clients are removed silently.
func (h *hub) broadcastForUsers(build func(authUser) any) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c, user := range h.clients {
		data, err := json.Marshal(build(user))
		if err != nil {
			h.logger.Error("ws broadcast marshal failed", "error", err)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := c.Write(ctx, websocket.MessageText, data); err != nil {
			h.logger.Debug("ws write failed, removing client", "error", err)
			delete(h.clients, c)
		}
		cancel()
	}
}

// handleWebSocket upgrades the connection and keeps it open until the client disconnects.
func (h *hub) handleWebSocket(handler *handler, w http.ResponseWriter, r *http.Request) {
	user, ok := handler.requireAuthenticated(w, r)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow cross-origin connections (dev and reverse-proxy setups).
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error("ws accept failed", "error", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = user
	h.mu.Unlock()

	h.logger.Debug("ws client connected")

	// CloseRead drains incoming messages and returns when the connection closes.
	ctx := conn.CloseRead(r.Context())
	<-ctx.Done()

	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()

	h.logger.Debug("ws client disconnected")
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
