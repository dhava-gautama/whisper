package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"whisper/internal/auth"
	"whisper/internal/chat"
	"whisper/internal/config"
	"whisper/internal/db"
	"whisper/internal/media"
)

const appVersion = "1.0.0"

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	// Ensure data directories exist
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0750); err != nil {
		slog.Error("create db dir", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.MediaDir, 0750); err != nil {
		slog.Error("create media dir", "error", err)
		os.Exit(1)
	}

	// Hash passwords at startup
	hash1, err := auth.HashPassword(cfg.User1.RawPass)
	if err != nil {
		slog.Error("hash user1 password", "error", err)
		os.Exit(1)
	}
	hash2, err := auth.HashPassword(cfg.User2.RawPass)
	if err != nil {
		slog.Error("hash user2 password", "error", err)
		os.Exit(1)
	}
	cfg.User1.PassHash = hash1
	cfg.User1.RawPass = ""
	cfg.User2.PassHash = hash2
	cfg.User2.RawPass = ""

	// Open database
	database, err := db.Open(cfg.DBPath, cfg.DBKey)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// Seed users
	if _, err := db.UpsertUser(database, cfg.User1.Name, cfg.User1.PassHash); err != nil {
		slog.Error("upsert user1", "error", err)
		os.Exit(1)
	}
	if _, err := db.UpsertUser(database, cfg.User2.Name, cfg.User2.PassHash); err != nil {
		slog.Error("upsert user2", "error", err)
		os.Exit(1)
	}

	// Start session purge goroutine
	go func() {
		for {
			time.Sleep(15 * time.Minute)
			if n, err := db.PurgeExpiredSessions(database); err != nil {
				slog.Error("purge sessions", "error", err)
			} else if n > 0 {
				slog.Info("purged expired sessions", "count", n)
			}
		}
	}()

	// Initialize components
	hub := chat.NewHub(database)
	rateLimiter := auth.NewRateLimiter()
	mediaHandler := &media.Handler{
		DB:       database,
		MediaDir: cfg.MediaDir,
		MaxBytes: cfg.MaxUploadMB * 1024 * 1024,
	}

	// Build router
	mux := http.NewServeMux()

	// Static files
	webDir := "./web"
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.ServeFile(w, r, filepath.Join(webDir, r.URL.Path))
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})

	// Chat page (auth required)
	mux.Handle("GET /chat", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(webDir, "chat.html"))
	})))

	// API: Login
	mux.Handle("POST /api/login", rateLimiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogin(w, r, database, cfg)
	})))

	// API: Logout
	mux.Handle("POST /api/logout", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogout(w, r, database)
	})))

	// API: Messages
	mux.Handle("GET /api/messages", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleMessages(w, r, database)
	})))

	// API: Search
	mux.Handle("GET /api/messages/search", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSearch(w, r, database)
	})))

	// API: Export
	mux.Handle("GET /api/messages/export", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleExport(w, r, database)
	})))

	// API: Media upload
	mux.Handle("POST /api/media/upload", auth.RequireAuth(database, auth.RequireCSRF(http.HandlerFunc(mediaHandler.Upload))))

	// API: Media download
	mux.Handle("GET /api/media/{id}", auth.RequireAuth(database, http.HandlerFunc(mediaHandler.Download)))

	// API: Media list (gallery)
	mux.Handle("GET /api/media", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleMediaList(w, r, database)
	})))

	// API: Version
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":"%s"}`, appVersion)
	})

	// API: Storage info
	mux.Handle("GET /api/storage", auth.RequireAuth(database, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleStorage(w, cfg)
	})))

	// WebSocket
	mux.Handle("GET /ws", auth.RequireAuth(database, http.HandlerFunc(chat.WSHandler(hub, database))))

	// Wrap with security headers
	handler := securityHeaders(mux)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	slog.Info("whisper starting", "addr", cfg.Addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request, database *sql.DB, cfg *config.Config) {
	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// Try password against both users
	var user *db.User
	for _, name := range []string{cfg.User1.Name, cfg.User2.Name} {
		u, err := db.GetUserByUsername(database, name)
		if err != nil {
			continue
		}
		valid, err := auth.VerifyPassword(u.PassHash, req.Password)
		if err == nil && valid {
			user = u
			break
		}
	}

	if user == nil {
		slog.Warn("failed login attempt", "ip", extractIP(r))
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	slog.Info("login success", "user", user.Username, "ip", extractIP(r))

	ip := extractIP(r)
	session, err := db.CreateSession(database, user.ID, ip, cfg.SessionTTL)
	if err != nil {
		slog.Error("create session", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "whisper_session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   !cfg.Dev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cfg.SessionTTL.Seconds()),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
		},
		"csrf_token": session.CSRFToken,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	cookie, err := r.Cookie("whisper_session")
	if err == nil {
		db.DeleteSession(database, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "whisper_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func handleMessages(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	var before *int64
	if b := r.URL.Query().Get("before"); b != "" {
		if id, err := strconv.ParseInt(b, 10, 64); err == nil {
			before = &id
		}
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	messages, hasMore, err := db.GetMessages(database, before, limit)
	if err != nil {
		slog.Error("get messages", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		// Fetch reactions for each message
		reactions, _ := db.GetReactions(database, m.ID)

		item := map[string]any{
			"id":          m.ID,
			"user":        map[string]any{"id": m.UserID, "username": m.Username},
			"kind":        m.Kind,
			"content":     m.Content,
			"media":       nil,
			"reply_to_id": m.ReplyToID,
			"reply_to":    nil,
			"deleted":     m.Deleted,
			"reactions":   reactions,
			"created_at":  m.CreatedAt.Format(time.RFC3339Nano),
		}
		if m.Media != nil {
			item["media"] = map[string]any{
				"id":           m.Media.ID,
				"filename":     m.Media.Filename,
				"content_type": m.Media.ContentType,
				"size_bytes":   m.Media.SizeBytes,
			}
		}
		if m.ReplyTo != nil {
			item["reply_to"] = map[string]any{
				"id":       m.ReplyTo.ID,
				"user":     map[string]any{"id": m.ReplyTo.UserID, "username": m.ReplyTo.Username},
				"content":  m.ReplyTo.Content,
			}
		}
		out = append(out, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"messages": out,
		"has_more": hasMore,
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"messages":[]}`)
		return
	}

	messages, err := db.SearchMessages(database, q, 30)
	if err != nil {
		slog.Error("search messages", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		out = append(out, map[string]any{
			"id":         m.ID,
			"user":       map[string]any{"id": m.UserID, "username": m.Username},
			"kind":       m.Kind,
			"content":    m.Content,
			"created_at": m.CreatedAt.Format(time.RFC3339Nano),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"messages": out})
}

func handleMediaList(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	rows, err := database.Query(`
		SELECT m.id, m.filename, m.content_type, m.size_bytes, m.created_at, u.username
		FROM media m JOIN users u ON u.id = m.user_id
		ORDER BY m.created_at DESC LIMIT 200`)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		var id, filename, ct, username, createdAt string
		var size int64
		if err := rows.Scan(&id, &filename, &ct, &size, &createdAt, &username); err != nil {
			continue
		}
		items = append(items, map[string]any{
			"id": id, "filename": filename, "content_type": ct,
			"size_bytes": size, "created_at": createdAt, "username": username,
		})
	}
	if items == nil {
		items = []map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"media": items})
}

func handleStorage(w http.ResponseWriter, cfg *config.Config) {
	var total int64
	filepath.Walk(cfg.MediaDir, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"used_bytes": total,
		"used_mb":    float64(total) / 1048576,
	})
}

func handleExport(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	var allMsgs []map[string]any
	var cursor *int64
	for {
		msgs, more, err := db.GetMessages(database, cursor, 200)
		if err != nil {
			http.Error(w, `{"error":"export failed"}`, http.StatusInternalServerError)
			return
		}
		for _, m := range msgs {
			allMsgs = append(allMsgs, map[string]any{
				"id": m.ID, "user": m.Username, "content": m.Content,
				"kind": m.Kind, "created_at": m.CreatedAt.Format(time.RFC3339),
			})
		}
		if !more || len(msgs) == 0 {
			break
		}
		first := msgs[0].ID
		cursor = &first
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="whisper-export.json"`)
	json.NewEncoder(w).Encode(map[string]any{"messages": allMsgs, "exported_at": time.Now().UTC().Format(time.RFC3339)})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self'; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self' wss: ws:; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return xff
	}
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
