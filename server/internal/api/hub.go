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

const (
	// clientSendBuffer bounds how many messages may queue for a single client
	// before it is considered too slow and dropped. A dropped client reconnects
	// and replays missed calls via ?since=, so no traffic is silently lost.
	clientSendBuffer = 256
	// wsWriteTimeout bounds a single frame write to one client.
	wsWriteTimeout = 10 * time.Second
)

// hubClient is a single connected WebSocket client with its own outbound queue
// and dedicated writer goroutine, so a slow client never blocks delivery to
// other clients.
type hubClient struct {
	conn *websocket.Conn
	user authUser
	send chan []byte
}

// hub manages active WebSocket connections and broadcasts messages to all of them.
type hub struct {
	mu      sync.Mutex
	clients map[*hubClient]struct{}
	logger  *slog.Logger
}

type wsHeartbeatEvent struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
}

func newHub(logger *slog.Logger) *hub {
	h := &hub{
		clients: make(map[*hubClient]struct{}),
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
func (h *hub) broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("ws broadcast marshal failed", "error", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		h.enqueueLocked(c, data)
	}
}

// broadcastForUsers sends a user-specific JSON message to every connected client.
func (h *hub) broadcastForUsers(build func(authUser) any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		data, err := json.Marshal(build(c.user))
		if err != nil {
			h.logger.Error("ws broadcast marshal failed", "error", err)
			continue
		}
		h.enqueueLocked(c, data)
	}
}

// enqueueTo queues a single pre-marshalled message for one client.
func (h *hub) enqueueTo(c *hubClient, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enqueueLocked(c, data)
}

// enqueueLocked performs a non-blocking enqueue. Callers must hold h.mu. If the
// client's queue is full it is dropped (its reconnect will replay missed calls).
func (h *hub) enqueueLocked(c *hubClient, data []byte) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	select {
	case c.send <- data:
	default:
		h.logger.Debug("ws client backlogged, dropping")
		h.removeClientLocked(c)
	}
}

// removeClientLocked unregisters a client and tears down its connection. Callers
// must hold h.mu. Safe to call multiple times for the same client.
func (h *hub) removeClientLocked(c *hubClient) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	close(c.send)
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// writeLoop drains the client's outbound queue. It is the only goroutine that
// writes to the connection, so concurrent broadcasts never corrupt frames.
func (c *hubClient) writeLoop() {
	for data := range c.send {
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		err := c.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			// Closing the connection unblocks CloseRead so the read side cleans up.
			_ = c.conn.Close(websocket.StatusAbnormalClosure, "write error")
			return
		}
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

	client := &hubClient{
		conn: conn,
		user: user,
		send: make(chan []byte, clientSendBuffer),
	}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		client.writeLoop()
	}()

	h.logger.Debug("ws client connected")

	// Replay any calls the client missed while disconnected. This runs after the
	// client is registered for live broadcasts, so a call arriving mid-replay is
	// still delivered (the client de-duplicates by call id).
	handler.replayMissedCalls(h, client, r)

	// CloseRead drains incoming messages and returns when the connection closes.
	ctx := conn.CloseRead(r.Context())
	<-ctx.Done()

	h.mu.Lock()
	h.removeClientLocked(client)
	h.mu.Unlock()
	<-writerDone

	h.logger.Debug("ws client disconnected")
}
