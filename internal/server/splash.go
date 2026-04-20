// BL69 — splash logo + tagline endpoints.
//
//   GET /api/splash/logo     serves the file at session.splash_logo_path,
//                            or 404 when unset (the web UI then falls
//                            back to the bundled favicon).
//   GET /api/splash/info     returns {logo_url, tagline, version, hostname}
//                            so the splash component can render once.

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleSplashLogo serves the operator-provided custom logo.
func (s *Server) handleSplashLogo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil || s.cfg.Session.SplashLogoPath == "" {
		http.NotFound(w, r)
		return
	}
	path := s.cfg.Session.SplashLogoPath
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, path)
}

// handleSplashInfo returns the splash render context.
func (s *Server) handleSplashInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out := map[string]any{
		"version":  Version,
		"hostname": s.hostname,
	}
	if s.cfg != nil {
		if s.cfg.Session.SplashLogoPath != "" {
			out["logo_url"] = "/api/splash/logo"
		}
		if s.cfg.Session.SplashTagline != "" {
			out["tagline"] = s.cfg.Session.SplashTagline
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
