// BL331 — /api/channel/routing REST handler.
//
// Stores a simple JSON config that maps channel identity patterns to
// federation peer names and automata configs. The config is persisted
// to ~/.datawatch/channel_routing.json.
//
// Routes:
//
//	GET  /api/channel/routing  — return current config
//	PUT  /api/channel/routing  — replace config
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/federation"
)

// channelRoutingRule describes how a channel identity pattern maps to a
// federation peer and optional automata configuration.
type channelRoutingRule struct {
	ChannelPattern   string `json:"channel_pattern"`
	PeerName         string `json:"peer_name"`
	AutomataType     string `json:"automata_type,omitempty"`
	DefaultProjectDir string `json:"default_project_dir,omitempty"`
}

// channelRoutingConfig is the top-level document persisted to disk.
type channelRoutingConfig struct {
	Rules []channelRoutingRule `json:"rules"`
}

func (s *Server) channelRoutingPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".datawatch", "channel_routing.json")
}

func (s *Server) handleChannelRouting(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
		data, err := os.ReadFile(s.channelRoutingPath())
		if err != nil {
			if os.IsNotExist(err) {
				writeJSONOK(w, &channelRoutingConfig{Rules: []channelRoutingRule{}})
				return
			}
			http.Error(w, "read failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var cfg channelRoutingConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			http.Error(w, "parse failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if cfg.Rules == nil {
			cfg.Rules = []channelRoutingRule{}
		}
		writeJSONOK(w, &cfg)

	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
		var cfg channelRoutingConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if cfg.Rules == nil {
			cfg.Rules = []channelRoutingRule{}
		}
		for i, r := range cfg.Rules {
			if r.ChannelPattern == "" {
				http.Error(w, fmt.Sprintf("rule[%d]: channel_pattern required", i), http.StatusBadRequest)
				return
			}
		}
		path := s.channelRoutingPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			http.Error(w, "mkdir failed", http.StatusInternalServerError)
			return
		}
		out, err := json.MarshalIndent(&cfg, "", "  ")
		if err != nil {
			http.Error(w, "encode failed", http.StatusInternalServerError)
			return
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, out, 0o600); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp, path); err != nil {
			http.Error(w, "rename failed", http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, &cfg)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
