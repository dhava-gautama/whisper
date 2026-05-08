package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

type Session struct {
	ID        string
	UserID    int
	CSRFToken string
	IPAddr    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func CreateSession(d *sql.DB, userID int, ip string, ttl time.Duration) (*Session, error) {
	id, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	csrf, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate csrf token: %w", err)
	}

	now := time.Now().UTC()
	expires := now.Add(ttl)

	_, err = d.Exec(
		"INSERT INTO sessions (id, user_id, csrf_token, ip_addr, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, userID, csrf, ip,
		now.Format(time.RFC3339Nano),
		expires.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:        id,
		UserID:    userID,
		CSRFToken: csrf,
		IPAddr:    ip,
		CreatedAt: now,
		ExpiresAt: expires,
	}, nil
}

func GetSession(d *sql.DB, sessionID string) (*Session, error) {
	s := &Session{}
	var createdAt, expiresAt string
	err := d.QueryRow(
		"SELECT id, user_id, csrf_token, ip_addr, created_at, expires_at FROM sessions WHERE id = ?",
		sessionID,
	).Scan(&s.ID, &s.UserID, &s.CSRFToken, &s.IPAddr, &createdAt, &expiresAt)
	if err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	s.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)

	if time.Now().UTC().After(s.ExpiresAt) {
		DeleteSession(d, sessionID)
		return nil, sql.ErrNoRows
	}
	return s, nil
}

func DeleteSession(d *sql.DB, sessionID string) error {
	_, err := d.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

func PurgeExpiredSessions(d *sql.DB) (int64, error) {
	res, err := d.Exec("DELETE FROM sessions WHERE expires_at < ?",
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
