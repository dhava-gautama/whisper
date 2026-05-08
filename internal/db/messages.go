package db

import (
	"database/sql"
	"time"
)

type Message struct {
	ID        int64
	UserID    int
	Username  string
	Kind      string
	Content   string
	MediaID   *string
	Media     *MediaRecord
	ReplyToID *int64
	ReplyTo   *ReplyPreview
	Deleted   bool
	Reactions []Reaction
	CreatedAt time.Time
}

type ReplyPreview struct {
	ID      int64
	UserID  int
	Username string
	Content string
}

type Reaction struct {
	Emoji    string `json:"emoji"`
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
}

func InsertMessage(d *sql.DB, userID int, kind, content string, mediaID *string, replyToID *int64) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO messages (user_id, kind, content, media_id, reply_to_id) VALUES (?, ?, ?, ?, ?)",
		userID, kind, content, mediaID, replyToID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func DeleteMessage(d *sql.DB, msgID int64, userID int) error {
	_, err := d.Exec("UPDATE messages SET deleted = 1, content = '' WHERE id = ? AND user_id = ?", msgID, userID)
	return err
}

func SearchMessages(d *sql.DB, query string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := d.Query(`
		SELECT m.id, m.user_id, u.username, m.kind, m.content, m.media_id, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.user_id
		WHERE m.deleted = 0 AND m.content LIKE ?
		ORDER BY m.id DESC LIMIT ?`, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var createdAt string
		var mediaID sql.NullString
		if err := rows.Scan(&m.ID, &m.UserID, &m.Username, &m.Kind, &m.Content, &mediaID, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if mediaID.Valid {
			s := mediaID.String
			m.MediaID = &s
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func AddReaction(d *sql.DB, messageID int64, userID int, emoji string) error {
	_, err := d.Exec(
		"INSERT OR IGNORE INTO reactions (message_id, user_id, emoji) VALUES (?, ?, ?)",
		messageID, userID, emoji)
	return err
}

func RemoveReaction(d *sql.DB, messageID int64, userID int, emoji string) error {
	_, err := d.Exec("DELETE FROM reactions WHERE message_id = ? AND user_id = ? AND emoji = ?",
		messageID, userID, emoji)
	return err
}

func GetReactions(d *sql.DB, messageID int64) ([]Reaction, error) {
	rows, err := d.Query(`
		SELECT r.emoji, r.user_id, u.username
		FROM reactions r JOIN users u ON u.id = r.user_id
		WHERE r.message_id = ?`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reaction
	for rows.Next() {
		var r Reaction
		if err := rows.Scan(&r.Emoji, &r.UserID, &r.Username); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func SetLastRead(d *sql.DB, userID int, messageID int64) error {
	_, err := d.Exec(`INSERT INTO read_receipts (user_id, last_read_id) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET last_read_id = excluded.last_read_id`,
		userID, messageID)
	return err
}

func GetLastRead(d *sql.DB, userID int) (int64, error) {
	var id int64
	err := d.QueryRow("SELECT last_read_id FROM read_receipts WHERE user_id = ?", userID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func GetMessages(d *sql.DB, before *int64, limit int) ([]Message, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// Fetch limit+1 to check if there are more
	fetchLimit := limit + 1

	var rows *sql.Rows
	var err error
	baseQuery := `
		SELECT m.id, m.user_id, u.username, m.kind, m.content, m.media_id, m.reply_to_id, m.deleted, m.created_at,
		       md.id, md.filename, md.content_type, md.size_bytes,
		       rm.id, rm.user_id, ru.username, rm.content
		FROM messages m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN media md ON md.id = m.media_id
		LEFT JOIN messages rm ON rm.id = m.reply_to_id
		LEFT JOIN users ru ON ru.id = rm.user_id`

	if before != nil {
		rows, err = d.Query(baseQuery+" WHERE m.id < ? ORDER BY m.id DESC LIMIT ?", *before, fetchLimit)
	} else {
		rows, err = d.Query(baseQuery+" ORDER BY m.id DESC LIMIT ?", fetchLimit)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var createdAt string
		var mediaID, mdID, mdFilename, mdContentType sql.NullString
		var mdSize sql.NullInt64
		var replyToID sql.NullInt64
		var deleted int
		var rmID sql.NullInt64
		var rmUserID sql.NullInt64
		var rmUsername, rmContent sql.NullString

		err := rows.Scan(
			&m.ID, &m.UserID, &m.Username, &m.Kind, &m.Content, &mediaID, &replyToID, &deleted, &createdAt,
			&mdID, &mdFilename, &mdContentType, &mdSize,
			&rmID, &rmUserID, &rmUsername, &rmContent,
		)
		if err != nil {
			return nil, false, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		m.Deleted = deleted == 1
		if m.Deleted {
			m.Content = ""
		}
		if mediaID.Valid {
			s := mediaID.String
			m.MediaID = &s
		}
		if replyToID.Valid {
			v := replyToID.Int64
			m.ReplyToID = &v
		}
		if mdID.Valid {
			m.Media = &MediaRecord{
				ID:          mdID.String,
				Filename:    mdFilename.String,
				ContentType: mdContentType.String,
				SizeBytes:   mdSize.Int64,
			}
		}
		if rmID.Valid {
			m.ReplyTo = &ReplyPreview{
				ID:       rmID.Int64,
				UserID:   int(rmUserID.Int64),
				Username: rmUsername.String,
				Content:  rmContent.String,
			}
		}
		msgs = append(msgs, m)
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	// Reverse to ascending order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	return msgs, hasMore, nil
}

func GetLatestMessage(d *sql.DB) (*Message, error) {
	m := &Message{}
	var createdAt string
	var mediaID sql.NullString
	err := d.QueryRow(`
		SELECT m.id, m.user_id, u.username, m.kind, m.content, m.media_id, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.user_id
		ORDER BY m.id DESC LIMIT 1`,
	).Scan(&m.ID, &m.UserID, &m.Username, &m.Kind, &m.Content, &mediaID, &createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if mediaID.Valid {
		s := mediaID.String
		m.MediaID = &s
	}
	return m, nil
}
