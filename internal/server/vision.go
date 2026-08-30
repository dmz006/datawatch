// Server handler for POST /api/vision/describe — BL368.
//
// Generic image description endpoint. Callers POST an image blob and receive
// a natural-language description from the configured vision model.
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

// visionSurface is the narrow interface the handler needs; the full
// vision.Describer implements it. Defined here so server tests can
// plug a fake without importing vision/HTTP machinery.
type visionSurface interface {
	Describe(ctx context.Context, imageData []byte, contentType, prompt string) (string, error)
}

// SetVisioner wires the vision Describer for /api/vision/describe.
// Nil disables the endpoint (503).
func (s *Server) SetVisioner(v visionSurface) { s.visioner = v }

// handleVisionDescribe implements POST /api/vision/describe.
//
// Accepts multipart/form-data with fields:
//
//	image    required — image blob (jpeg/png/webp/gif)
//	prompt   optional — override the default description prompt
//
// Response (200):
//
//	{ description, latency_ms }
func (s *Server) handleVisionDescribe(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.visioner == nil {
		http.Error(w, "vision not enabled", http.StatusServiceUnavailable)
		return
	}

	start := time.Now()
	// Limit uploads to 10 MB.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image field", http.StatusBadRequest)
		return
	}
	defer file.Close() //nolint:errcheck

	prompt := r.FormValue("prompt")

	tmpDir, err := os.MkdirTemp("", "dw-vision-")
	if err != nil {
		http.Error(w, "temp: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".jpg"
	}
	tmpPath := filepath.Join(tmpDir, "image"+ext)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "temp create: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(tmpFile, file); err != nil {
		_ = tmpFile.Close()
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmpFile.Close()

	imageData, err := os.ReadFile(tmpPath)
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Detect content type from header or extension.
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = extToMIME(ext)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	description, err := s.visioner.Describe(ctx, imageData, contentType, prompt)
	if err != nil {
		http.Error(w, "vision: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"description": description,
		"latency_ms":  time.Since(start).Milliseconds(),
	})
}

// extToMIME maps a file extension to an image MIME type.
func extToMIME(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
