# Whisper

Private self-hosted chat room for two. Sakura Falls theme.

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)
![SQLite](https://img.shields.io/badge/SQLite-encrypted-003B57?logo=sqlite)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)

## Features

**Chat**
- Real-time messaging via WebSocket
- Markdown rendering (`**bold**`, `*italic*`, `` `code` ``, `~~strike~~`)
- Clickable links
- Emoji shortcodes (`:smile:` `:heart:` `:fire:` etc.)
- Typing indicator with auto-timeout
- Read receipts (checkmarks)
- Message reactions (right-click or long-press)
- Reply to messages
- Delete your own messages
- Search messages
- Message history with infinite scroll

**Media**
- Image/file/audio upload with drag & drop
- Clipboard paste (Ctrl+V images)
- Voice messages (browser recording)
- Image lightbox viewer
- Media gallery

**Security**
- Password-only login (no username needed)
- Argon2id password hashing
- Encrypted SQLite database (SQLCipher)
- Session-based auth with CSRF protection
- Rate-limited login (5 attempts/min/IP)
- Security headers (CSP, X-Frame-Options, etc.)
- WebSocket rate limiting (20 msg/sec)
- Auto-logout after 1 hour idle
- Failed login attempt logging

**UI/UX**
- Sakura Falls theme (dark + light)
- Falling cherry blossom petals animation
- Custom wallpaper support
- PWA installable
- Mobile responsive
- Browser notifications
- Notification sound (toggleable)
- Offline message queue
- Chat export (JSON)

## Quick Start

```bash
# Clone
git clone https://github.com/dhava-gautama/whisper.git
cd whisper

# Run (requires Go 1.22+ and gcc for SQLCipher)
WHISPER_USER1_PASS=your_password_1 \
WHISPER_USER2_PASS=your_password_2 \
WHISPER_DEV=true \
go run .
```

Open `http://localhost:8080` and enter either password to login.

## Docker Deployment

```bash
# Copy and edit environment file
cp .env.example .env
nano .env

# Start
docker compose up -d
```

Edit `Caddyfile` to set your domain:
```
whisper.yourdomain.com {
    reverse_proxy whisper:8080
}
```

Caddy handles TLS automatically via Let's Encrypt.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `WHISPER_ADDR` | `:8080` | Listen address |
| `WHISPER_USER1_NAME` | `alice` | Display name for user 1 |
| `WHISPER_USER1_PASS` | *required* | Password for user 1 |
| `WHISPER_USER2_NAME` | `bob` | Display name for user 2 |
| `WHISPER_USER2_PASS` | *required* | Password for user 2 |
| `WHISPER_DB_PATH` | `./data/whisper.db` | SQLite database path |
| `WHISPER_DB_KEY` | *(empty)* | SQLCipher encryption key |
| `WHISPER_MEDIA_DIR` | `./data/media` | Media storage directory |
| `WHISPER_MAX_UPLOAD_MB` | `50` | Max file upload size in MB |
| `WHISPER_SESSION_TTL` | `24h` | Session lifetime |
| `WHISPER_DEV` | `false` | Dev mode (disables secure cookies) |

## Architecture

```
Browser A ←WSS→ [Caddy TLS] ←→ [Go Server] ←→ [SQLite + Files]
Browser B ←WSS→
```

- Single Go binary (12.5 MB)
- Frontend: 97 KB total (vanilla HTML/CSS/JS)
- Runtime RAM: ~2 MB
- 5 Go dependencies, everything else is stdlib
- SQLite with WAL mode for concurrent reads

## Project Structure

```
whisper/
├── main.go                 # Entry point, router, handlers
├── internal/
│   ├── auth/               # Argon2, sessions, rate limiter, middleware
│   ├── chat/               # WebSocket hub + handler
│   ├── config/             # Environment configuration
│   ├── db/                 # SQLite operations + migrations
│   └── media/              # File upload/download
├── web/
│   ├── index.html          # Login page
│   ├── chat.html           # Chat interface
│   ├── css/style.css       # Sakura Falls theme
│   ├── js/login.js
│   ├── js/chat.js          # All chat logic
│   ├── favicon.svg
│   ├── manifest.json       # PWA manifest
│   └── sw.js               # Service worker
├── Dockerfile
├── docker-compose.yml
├── Caddyfile
└── .env.example
```

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/login` | No | Login with password |
| `POST` | `/api/logout` | Yes | End session |
| `GET` | `/api/messages` | Yes | Message history (paginated) |
| `GET` | `/api/messages/search?q=` | Yes | Search messages |
| `GET` | `/api/messages/export` | Yes | Download chat as JSON |
| `POST` | `/api/media/upload` | Yes | Upload file |
| `GET` | `/api/media/{id}` | Yes | Download file |
| `GET` | `/api/media` | Yes | List all media (gallery) |
| `GET` | `/api/version` | No | Server version |
| `GET` | `/api/storage` | Yes | Storage usage |
| `GET` | `/ws` | Yes | WebSocket connection |

## WebSocket Protocol

Messages are JSON over WebSocket.

**Client sends:**
```json
{"type": "message", "content": "Hello!", "media_id": null, "reply_to_id": null}
{"type": "typing", "is_typing": true}
{"type": "reaction", "message_id": 42, "emoji": "👍"}
{"type": "delete", "message_id": 42}
{"type": "read", "last_read_id": 42}
```

**Server sends:**
```json
{"type": "message", "id": 1, "user": {"id": 1, "username": "alice"}, "content": "Hello!", ...}
{"type": "typing", "user": {...}, "data": {"is_typing": true}}
{"type": "presence", "user": {...}, "data": {"online": true}}
{"type": "reaction", "id": 42, "data": {"reactions": [...]}}
{"type": "delete", "id": 42}
{"type": "read", "user": {...}, "data": {"last_read_id": 42}}
```

## License

MIT
