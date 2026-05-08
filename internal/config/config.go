package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type UserConfig struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	PassHash string `json:"-"`
}

type Config struct {
	Addr        string
	Users       []UserConfig
	DBPath      string
	DBKey       string
	MediaDir    string
	MaxUploadMB int64
	SessionTTL  time.Duration
	BaseURL     string
	Dev         bool
}

func Load() (*Config, error) {
	maxUpload, err := strconv.ParseInt(envOr("WHISPER_MAX_UPLOAD_MB", "50"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid WHISPER_MAX_UPLOAD_MB: %w", err)
	}

	sessionTTL, err := time.ParseDuration(envOr("WHISPER_SESSION_TTL", "24h"))
	if err != nil {
		return nil, fmt.Errorf("invalid WHISPER_SESSION_TTL: %w", err)
	}

	cfg := &Config{
		Addr:        envOr("WHISPER_ADDR", ":8080"),
		DBPath:      envOr("WHISPER_DB_PATH", "./data/whisper.db"),
		DBKey:       os.Getenv("WHISPER_DB_KEY"),
		MediaDir:    envOr("WHISPER_MEDIA_DIR", "./data/media"),
		MaxUploadMB: maxUpload,
		SessionTTL:  sessionTTL,
		BaseURL:     os.Getenv("WHISPER_BASE_URL"),
		Dev:         os.Getenv("WHISPER_DEV") == "true",
	}

	// Load users from users.json (primary) or fall back to env vars (backward compat)
	usersFile := envOr("WHISPER_USERS_FILE", "./users.json")
	if data, err := os.ReadFile(usersFile); err == nil {
		var users []UserConfig
		if err := json.Unmarshal(data, &users); err != nil {
			return nil, fmt.Errorf("parse %s: %w", usersFile, err)
		}
		for i, u := range users {
			if u.Name == "" || u.Password == "" {
				return nil, fmt.Errorf("user %d in %s: name and password required", i, usersFile)
			}
		}
		if len(users) < 2 {
			return nil, fmt.Errorf("%s must have at least 2 users", usersFile)
		}
		cfg.Users = users
	} else {
		// Fallback: env vars (backward compatible with 2-user setup)
		u1Pass := os.Getenv("WHISPER_USER1_PASS")
		u2Pass := os.Getenv("WHISPER_USER2_PASS")
		if u1Pass == "" || u2Pass == "" {
			return nil, fmt.Errorf("no users.json found and WHISPER_USER1_PASS/WHISPER_USER2_PASS not set")
		}
		cfg.Users = []UserConfig{
			{Name: envOr("WHISPER_USER1_NAME", "alice"), Password: u1Pass},
			{Name: envOr("WHISPER_USER2_NAME", "bob"), Password: u2Pass},
		}
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
