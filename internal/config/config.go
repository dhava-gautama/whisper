package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type UserConfig struct {
	Name     string
	PassHash string // set after startup hashing
	RawPass  string // cleared after hashing
}

type Config struct {
	Addr         string
	User1        UserConfig
	User2        UserConfig
	DBPath       string
	DBKey        string
	MediaDir     string
	MaxUploadMB  int64
	SessionTTL   time.Duration
	BaseURL      string
	Dev          bool
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

	u1Pass := os.Getenv("WHISPER_USER1_PASS")
	u2Pass := os.Getenv("WHISPER_USER2_PASS")
	if u1Pass == "" || u2Pass == "" {
		return nil, fmt.Errorf("WHISPER_USER1_PASS and WHISPER_USER2_PASS must be set")
	}

	cfg := &Config{
		Addr: envOr("WHISPER_ADDR", ":8080"),
		User1: UserConfig{
			Name:    envOr("WHISPER_USER1_NAME", "alice"),
			RawPass: u1Pass,
		},
		User2: UserConfig{
			Name:    envOr("WHISPER_USER2_NAME", "bob"),
			RawPass: u2Pass,
		},
		DBPath:      envOr("WHISPER_DB_PATH", "./data/whisper.db"),
		DBKey:       os.Getenv("WHISPER_DB_KEY"),
		MediaDir:    envOr("WHISPER_MEDIA_DIR", "./data/media"),
		MaxUploadMB: maxUpload,
		SessionTTL:  sessionTTL,
		BaseURL:     os.Getenv("WHISPER_BASE_URL"),
		Dev:         os.Getenv("WHISPER_DEV") == "true",
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
