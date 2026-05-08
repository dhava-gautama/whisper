# Whisper

Private self-hosted chat room for 2 users.

## Overview

- **Purpose**: Secure, private 1-on-1 chat with media sharing
- **Users**: Exactly 2 users, password-only authentication
- **Security**: HTTPS + Argon2 password hashing + encrypted SQLite at rest + rate limiting
- **No E2E encryption** — server is self-hosted and trusted

## Tech Stack

- **Backend**: Go (single binary, low resource)
- **Frontend**: Vanilla HTML/CSS/JS (no framework, minimal)
- **Database**: SQLite (encrypted at rest via SQLCipher)
- **Real-time**: WebSocket for live messaging
- **Media**: File upload with server-side storage
- **Deploy**: Single Docker container behind Caddy (auto-TLS)

## Architecture

```
Browser (User A) <--WSS--> [Caddy TLS] <--> [Go Server] <--> [SQLite + Files]
Browser (User B) <--WSS-->
```

- Server handles auth, message relay, media storage
- WebSocket for real-time message delivery
- REST endpoints for login, media upload/download
- No registration — users are pre-configured via config/env

## Project Structure

```
whisper/
├── CLAUDE.md
├── main.go              # Entry point
├── go.mod
├── internal/
│   ├── auth/            # Password auth, session management
│   ├── chat/            # WebSocket handler, message logic
│   ├── media/           # File upload/download
│   ├── db/              # SQLite operations
│   └── config/          # App configuration
├── web/                 # Frontend static files
│   ├── index.html       # Login page
│   ├── chat.html        # Chat interface
│   ├── css/
│   └── js/
├── data/                # Runtime data (gitignored)
│   ├── whisper.db
│   └── media/
├── Dockerfile
├── docker-compose.yml
└── Caddyfile
```

## Commands

```bash
# Development
go run .                          # Run server
go test ./...                     # Run tests

# Docker
docker compose up -d              # Start
docker compose down               # Stop
docker compose logs -f whisper    # Logs
```

## Security Rules

- All passwords hashed with Argon2id before storage
- Sessions expire after 24h, stored server-side
- Rate limit login attempts (5 per minute per IP)
- Media files stored outside web root, served through auth-gated endpoint
- No user registration endpoint — users configured at startup
- CSRF protection on all POST endpoints
- Content-Security-Policy headers on all responses
