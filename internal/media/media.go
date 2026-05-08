package media

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"whisper/internal/auth"
	"whisper/internal/db"
)

var allowedMIME = map[string]bool{
	"image/jpeg":               true,
	"image/png":                true,
	"image/gif":                true,
	"image/webp":               true,
	"application/pdf":          true,
	"text/plain":               true,
	"application/zip":          true,
	"audio/webm":               true,
	"audio/ogg":                true,
	"audio/mp4":                true,
	"audio/mpeg":               true,
	"audio/wav":                true,
	"video/webm":               true,
	"video/ogg":                true,
	"application/octet-stream": true,
}

type Handler struct {
	DB       *sql.DB
	MediaDir string
	MaxBytes int64
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.MaxBytes)
	if err := r.ParseMultipartForm(h.MaxBytes); err != nil {
		http.Error(w, `{"error":"file too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read first 512 bytes to detect content type
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		http.Error(w, `{"error":"failed to read file"}`, http.StatusInternalServerError)
		return
	}
	contentType := http.DetectContentType(buf[:n])

	// Normalize content type (strip params)
	if idx := strings.IndexByte(contentType, ';'); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	// Fallback: if detection gives generic type, check file extension
	if contentType == "application/octet-stream" || contentType == "text/plain" {
		ext := strings.ToLower(path.Ext(header.Filename))
		extMap := map[string]string{
			".webm": "audio/webm", ".ogg": "audio/ogg", ".mp3": "audio/mpeg",
			".mp4": "audio/mp4", ".wav": "audio/wav", ".m4a": "audio/mp4",
			".pdf": "application/pdf", ".zip": "application/zip",
			".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
			".gif": "image/gif", ".webp": "image/webp",
		}
		if mapped, ok := extMap[ext]; ok {
			contentType = mapped
		}
	}

	if !allowedMIME[contentType] {
		http.Error(w, `{"error":"file type not allowed"}`, http.StatusBadRequest)
		return
	}

	// Normalize video/webm to audio/webm for voice recordings
	if contentType == "video/webm" || contentType == "video/ogg" {
		contentType = strings.Replace(contentType, "video/", "audio/", 1)
	}

	// Seek back to start
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, `{"error":"failed to process file"}`, http.StatusInternalServerError)
		return
	}

	// Generate ID and sanitize filename
	mediaID := uuid.New().String()
	filename := sanitizeFilename(header.Filename)

	// Create directory
	dir := filepath.Join(h.MediaDir, mediaID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		http.Error(w, `{"error":"failed to store file"}`, http.StatusInternalServerError)
		return
	}

	// Write file
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		http.Error(w, `{"error":"failed to store file"}`, http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	size, err := io.Copy(dst, file)
	if err != nil {
		os.RemoveAll(dir)
		http.Error(w, `{"error":"failed to write file"}`, http.StatusInternalServerError)
		return
	}

	// Store in DB
	if err := db.InsertMedia(h.DB, mediaID, user.ID, filename, contentType, size); err != nil {
		os.RemoveAll(dir)
		http.Error(w, `{"error":"failed to save metadata"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"media_id":     mediaID,
		"filename":     filename,
		"content_type": contentType,
		"size_bytes":   size,
	})
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	mediaID := r.PathValue("id")
	if mediaID == "" {
		http.Error(w, `{"error":"missing media id"}`, http.StatusBadRequest)
		return
	}

	record, err := db.GetMedia(h.DB, mediaID)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	filePath := filepath.Join(h.MediaDir, record.ID, record.Filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", record.ContentType)

	if strings.HasPrefix(record.ContentType, "image/") {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`inline; filename="%s"`, record.Filename))
	} else {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"`, record.Filename))
	}

	http.ServeFile(w, r, filePath)
}

func sanitizeFilename(name string) string {
	// Strip path components
	name = path.Base(name)
	// Replace dangerous characters
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '\x00' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	// Limit length
	if len(name) > 255 {
		name = name[:255]
	}
	if name == "" || name == "." || name == ".." {
		name = "upload"
	}
	return name
}
