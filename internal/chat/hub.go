package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"

	"whisper/internal/db"
)

type Hub struct {
	mu    sync.RWMutex
	conns map[int]*Conn
	db    *sql.DB
}

type Conn struct {
	WS       *websocket.Conn
	UserID   int
	Username string
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewHub(database *sql.DB) *Hub {
	return &Hub{
		conns: make(map[int]*Conn),
		db:    database,
	}
}

func (h *Hub) Register(userID int, username string, ws *websocket.Conn) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		WS:       ws,
		UserID:   userID,
		Username: username,
		ctx:      ctx,
		cancel:   cancel,
	}

	h.mu.Lock()
	// Close existing connection for this user (single connection per user)
	if old, exists := h.conns[userID]; exists {
		old.cancel()
		old.WS.Close(websocket.StatusGoingAway, "replaced by new connection")
	}
	h.conns[userID] = c
	h.mu.Unlock()

	// Notify others about presence
	h.broadcast(OutgoingMessage{
		Type: "presence",
		User: &UserInfo{ID: userID, Username: username},
		Data: map[string]any{"online": true},
	}, 0)

	return c
}

func (h *Hub) Unregister(userID int) {
	h.mu.Lock()
	if c, exists := h.conns[userID]; exists {
		c.cancel()
		delete(h.conns, userID)
	}
	h.mu.Unlock()

	// Update last_seen
	db.UpdateLastSeen(h.db, userID)

	// Get username for presence notification
	user, err := db.GetUserByID(h.db, userID)
	if err == nil {
		h.broadcast(OutgoingMessage{
			Type: "presence",
			User: &UserInfo{ID: userID, Username: user.Username},
			Data: map[string]any{"online": false, "last_seen": user.LastSeen},
		}, 0)
	}
}

func (h *Hub) IsOnline(userID int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, exists := h.conns[userID]
	return exists
}

func (h *Hub) Broadcast(msg OutgoingMessage) {
	h.broadcast(msg, 0)
}

func (h *Hub) broadcast(msg OutgoingMessage, excludeUser int) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal broadcast message", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for uid, c := range h.conns {
		if uid == excludeUser {
			continue
		}
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		if err := c.WS.Write(ctx, websocket.MessageText, data); err != nil {
			slog.Debug("write to client failed", "user", uid, "error", err)
		}
		cancel()
	}
}

func (h *Hub) SendTo(userID int, msg OutgoingMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	c, exists := h.conns[userID]
	h.mu.RUnlock()

	if exists {
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		c.WS.Write(ctx, websocket.MessageText, data)
		cancel()
	}
}

// OutgoingMessage is the JSON structure sent to clients.
type OutgoingMessage struct {
	Type      string         `json:"type"`
	ID        int64          `json:"id,omitempty"`
	User      *UserInfo      `json:"user,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	Content   string         `json:"content,omitempty"`
	Media     *MediaInfo     `json:"media,omitempty"`
	ReplyToID *int64         `json:"reply_to_id,omitempty"`
	Deleted   bool           `json:"deleted,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type UserInfo struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type MediaInfo struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}
