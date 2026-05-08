package db

import (
	"database/sql"
	"time"
)

type MediaRecord struct {
	ID          string
	UserID      int
	Filename    string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
}

func InsertMedia(d *sql.DB, id string, userID int, filename, contentType string, size int64) error {
	_, err := d.Exec(
		"INSERT INTO media (id, user_id, filename, content_type, size_bytes) VALUES (?, ?, ?, ?, ?)",
		id, userID, filename, contentType, size,
	)
	return err
}

func GetMedia(d *sql.DB, id string) (*MediaRecord, error) {
	m := &MediaRecord{}
	var createdAt string
	err := d.QueryRow(
		"SELECT id, user_id, filename, content_type, size_bytes, created_at FROM media WHERE id = ?",
		id,
	).Scan(&m.ID, &m.UserID, &m.Filename, &m.ContentType, &m.SizeBytes, &createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return m, nil
}
