package db

import (
	"database/sql"
	"time"
)

type User struct {
	ID        int
	Username  string
	PassHash  string
	LastSeen  string
	CreatedAt time.Time
}

func UpsertUser(d *sql.DB, username, passHash string) (int, error) {
	_, err := d.Exec(`
		INSERT INTO users (username, pass_hash) VALUES (?, ?)
		ON CONFLICT(username) DO UPDATE SET pass_hash = excluded.pass_hash`,
		username, passHash)
	if err != nil {
		return 0, err
	}

	var id int
	err = d.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&id)
	return id, err
}

func GetUserByUsername(d *sql.DB, username string) (*User, error) {
	u := &User{}
	var createdAt string
	err := d.QueryRow(
		"SELECT id, username, pass_hash, created_at, last_seen FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PassHash, &createdAt, &u.LastSeen)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return u, nil
}

func GetUserByID(d *sql.DB, id int) (*User, error) {
	u := &User{}
	var createdAt string
	err := d.QueryRow(
		"SELECT id, username, pass_hash, created_at, last_seen FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PassHash, &createdAt, &u.LastSeen)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return u, nil
}

func UpdateLastSeen(d *sql.DB, userID int) error {
	_, err := d.Exec("UPDATE users SET last_seen = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), userID)
	return err
}

func UpdatePassword(d *sql.DB, userID int, newHash string) error {
	_, err := d.Exec("UPDATE users SET pass_hash = ? WHERE id = ?", newHash, userID)
	return err
}

func GetAllUsers(d *sql.DB) ([]User, error) {
	rows, err := d.Query("SELECT id, username, last_seen FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Username, &u.LastSeen)
		users = append(users, u)
	}
	return users, nil
}
