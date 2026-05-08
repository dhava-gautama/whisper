package db

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mutecomm/go-sqlcipher/v4"
)

func Open(dbPath, dbKey string) (*sql.DB, error) {
	dsn := dbPath
	if dbKey != "" {
		dsn = fmt.Sprintf("%s?_pragma_key=%s&_pragma_cipher_page_size=4096",
			dbPath, url.QueryEscape(dbKey))
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	db.SetMaxOpenConns(2)

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func Migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	if err != nil {
		return err
	}

	var version int
	err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO schema_version (version) VALUES (0)")
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	migrations := []string{
		// Migration 1: Base schema
		`CREATE TABLE IF NOT EXISTS users (
			id         INTEGER PRIMARY KEY,
			username   TEXT    NOT NULL UNIQUE,
			pass_hash  TEXT    NOT NULL,
			created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);

		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT    PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id),
			csrf_token TEXT    NOT NULL,
			ip_addr    TEXT    NOT NULL,
			created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			expires_at TEXT    NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

		CREATE TABLE IF NOT EXISTS media (
			id            TEXT    PRIMARY KEY,
			user_id       INTEGER NOT NULL REFERENCES users(id),
			filename      TEXT    NOT NULL,
			content_type  TEXT    NOT NULL,
			size_bytes    INTEGER NOT NULL,
			created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);

		CREATE TABLE IF NOT EXISTS messages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id     INTEGER NOT NULL REFERENCES users(id),
			kind        TEXT    NOT NULL DEFAULT 'text',
			content     TEXT    NOT NULL,
			media_id    TEXT    REFERENCES media(id),
			reply_to_id INTEGER REFERENCES messages(id),
			deleted     INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);

		CREATE TABLE IF NOT EXISTS reactions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES messages(id),
			user_id    INTEGER NOT NULL REFERENCES users(id),
			emoji      TEXT    NOT NULL,
			created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			UNIQUE(message_id, user_id, emoji)
		);

		CREATE TABLE IF NOT EXISTS read_receipts (
			user_id       INTEGER PRIMARY KEY REFERENCES users(id),
			last_read_id  INTEGER NOT NULL DEFAULT 0
		);`,

		// Migration 2: Add last_seen to users
		`ALTER TABLE users ADD COLUMN last_seen TEXT NOT NULL DEFAULT '';`,
	}

	for i, m := range migrations {
		if i+1 <= version {
			continue
		}
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := db.Exec("UPDATE schema_version SET version = ?", i+1); err != nil {
			return err
		}
	}

	return nil
}
