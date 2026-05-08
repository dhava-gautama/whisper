package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"whisper/internal/auth"
	"whisper/internal/db"
)

type IncomingMessage struct {
	Type      string  `json:"type"`
	Content   string  `json:"content"`
	MediaID   *string `json:"media_id"`
	ReplyToID *int64  `json:"reply_to_id"`
	Typing    *bool   `json:"is_typing"`
	// For reactions
	MessageID *int64  `json:"message_id"`
	Emoji     string  `json:"emoji"`
	// For read receipts
	LastReadID *int64 `json:"last_read_id"`
}

func WSHandler(hub *Hub, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.GetUser(r.Context())
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			slog.Error("websocket accept", "error", err)
			return
		}

		conn := hub.Register(user.ID, user.Username, ws)
		defer func() {
			hub.Unregister(user.ID)
			ws.CloseNow()
		}()

		go keepalive(conn)

		// Rate limit: 20 messages per second
		msgBucket := 20
		msgTicker := time.NewTicker(time.Second)
		defer msgTicker.Stop()
		go func() {
			for {
				select {
				case <-conn.ctx.Done():
					return
				case <-msgTicker.C:
					if msgBucket < 20 {
						msgBucket = 20
					}
				}
			}
		}()

		for {
			_, data, err := ws.Read(conn.ctx)
			if err != nil {
				if websocket.CloseStatus(err) != -1 {
					slog.Debug("websocket closed", "user", user.Username)
				}
				return
			}

			if msgBucket <= 0 {
				sendError(conn, "slow down")
				continue
			}
			msgBucket--

			var msg IncomingMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				sendError(conn, "invalid message format")
				continue
			}

			switch msg.Type {
			case "message":
				handleMessage(hub, database, user, conn, &msg)
			case "typing":
				handleTyping(hub, user, &msg)
			case "reaction":
				handleReaction(hub, database, user, conn, &msg)
			case "delete":
				handleDelete(hub, database, user, conn, &msg)
			case "read":
				handleRead(hub, database, user, &msg)
			case "ping":
				sendPong(conn)
			default:
				sendError(conn, "unknown message type")
			}
		}
	}
}

func handleMessage(hub *Hub, database *sql.DB, user *auth.ContextUser, conn *Conn, msg *IncomingMessage) {
	content := strings.TrimSpace(msg.Content)
	if content == "" && msg.MediaID == nil {
		sendError(conn, "empty message")
		return
	}
	if len(content) > 10000 {
		sendError(conn, "message too long (max 10000 characters)")
		return
	}

	kind := "text"
	var mediaInfo *MediaInfo

	if msg.MediaID != nil {
		media, err := db.GetMedia(database, *msg.MediaID)
		if err != nil {
			sendError(conn, "media not found")
			return
		}
		if strings.HasPrefix(media.ContentType, "image/") {
			kind = "image"
		} else if strings.HasPrefix(media.ContentType, "audio/") {
			kind = "voice"
		} else if strings.HasPrefix(media.ContentType, "video/") {
			kind = "video"
		} else {
			kind = "file"
		}
		mediaInfo = &MediaInfo{
			ID:          media.ID,
			Filename:    media.Filename,
			ContentType: media.ContentType,
			SizeBytes:   media.SizeBytes,
		}
	}

	msgID, err := db.InsertMessage(database, user.ID, kind, content, msg.MediaID, msg.ReplyToID)
	if err != nil {
		slog.Error("insert message", "error", err)
		sendError(conn, "failed to save message")
		return
	}

	out := OutgoingMessage{
		Type:      "message",
		ID:        msgID,
		User:      &UserInfo{ID: user.ID, Username: user.Username},
		Kind:      kind,
		Content:   content,
		Media:     mediaInfo,
		ReplyToID: msg.ReplyToID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// If replying, include reply preview
	if msg.ReplyToID != nil {
		// Fetch the replied message for preview
		msgs, _, _ := db.GetMessages(database, nil, 1)
		_ = msgs // simplified - client already has the message
	}

	hub.Broadcast(out)
}

func handleTyping(hub *Hub, user *auth.ContextUser, msg *IncomingMessage) {
	isTyping := msg.Typing != nil && *msg.Typing
	hub.broadcast(OutgoingMessage{
		Type: "typing",
		User: &UserInfo{ID: user.ID, Username: user.Username},
		Data: map[string]any{"is_typing": isTyping},
	}, user.ID)
}

func handleReaction(hub *Hub, database *sql.DB, user *auth.ContextUser, conn *Conn, msg *IncomingMessage) {
	if msg.MessageID == nil || msg.Emoji == "" {
		sendError(conn, "missing message_id or emoji")
		return
	}
	// Limit emoji length
	if len(msg.Emoji) > 8 {
		sendError(conn, "invalid emoji")
		return
	}

	// Toggle: add if not exists, remove if exists
	reactions, _ := db.GetReactions(database, *msg.MessageID)
	found := false
	for _, r := range reactions {
		if r.UserID == user.ID && r.Emoji == msg.Emoji {
			found = true
			break
		}
	}

	if found {
		db.RemoveReaction(database, *msg.MessageID, user.ID, msg.Emoji)
	} else {
		db.AddReaction(database, *msg.MessageID, user.ID, msg.Emoji)
	}

	// Broadcast updated reactions
	updatedReactions, _ := db.GetReactions(database, *msg.MessageID)
	hub.Broadcast(OutgoingMessage{
		Type: "reaction",
		ID:   *msg.MessageID,
		Data: map[string]any{"reactions": updatedReactions},
	})
}

func handleDelete(hub *Hub, database *sql.DB, user *auth.ContextUser, conn *Conn, msg *IncomingMessage) {
	if msg.MessageID == nil {
		sendError(conn, "missing message_id")
		return
	}

	if err := db.DeleteMessage(database, *msg.MessageID, user.ID); err != nil {
		sendError(conn, "failed to delete message")
		return
	}

	hub.Broadcast(OutgoingMessage{
		Type: "delete",
		ID:   *msg.MessageID,
		User: &UserInfo{ID: user.ID, Username: user.Username},
	})
}

func handleRead(hub *Hub, database *sql.DB, user *auth.ContextUser, msg *IncomingMessage) {
	if msg.LastReadID == nil {
		return
	}
	db.SetLastRead(database, user.ID, *msg.LastReadID)

	// Notify the other user
	hub.broadcast(OutgoingMessage{
		Type: "read",
		User: &UserInfo{ID: user.ID, Username: user.Username},
		Data: map[string]any{"last_read_id": *msg.LastReadID},
	}, user.ID)
}

func keepalive(conn *Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-conn.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(conn.ctx, 10*time.Second)
			err := conn.WS.Ping(ctx)
			cancel()
			if err != nil {
				conn.cancel()
				return
			}
		}
	}
}

func sendError(conn *Conn, message string) {
	data, _ := json.Marshal(OutgoingMessage{Type: "error", Content: message})
	ctx, cancel := context.WithTimeout(conn.ctx, 5*time.Second)
	conn.WS.Write(ctx, websocket.MessageText, data)
	cancel()
}

func sendPong(conn *Conn) {
	data, _ := json.Marshal(OutgoingMessage{Type: "pong"})
	ctx, cancel := context.WithTimeout(conn.ctx, 5*time.Second)
	conn.WS.Write(ctx, websocket.MessageText, data)
	cancel()
}
