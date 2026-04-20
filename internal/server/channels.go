// v4.0.3 — /api/channels CRUD. Unifies the per-type messaging config
// blocks (discord, slack, telegram, matrix, twilio, ntfy, email,
// webhook, github_webhook) behind a single REST surface so the
// mobile client can enumerate + enable/disable from Settings.
// Closes https://github.com/dmz006/datawatch/issues/8.
//
// Create (POST /api/channels) is NOT fully implemented in v4.0.3 —
// each channel type has its own schema; we return 501 and point
// callers at PUT /api/config, which already supports every field.
// A future patch can grow dedicated POST handlers per type.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// channelInfo is one element of GET /api/channels. Provider-specific
// fields are intentionally NOT included here — callers that need the
// full schema read GET /api/config and navigate to the matching block
// (e.g. cfg.discord.*). That path already masks secrets.
type channelInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/channels")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, map[string]any{"channels": enumerateChannels(s.cfg)})
		case http.MethodPost:
			http.Error(w, "POST /api/channels not implemented — use PUT /api/config with the matching backend block for now", http.StatusNotImplemented)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	id := rest
	switch r.Method {
	case http.MethodPatch, http.MethodPost:
		// PATCH / POST {enabled: bool}
		var req struct {
			Enabled *bool `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Enabled == nil {
			http.Error(w, "enabled required", http.StatusBadRequest)
			return
		}
		if !setChannelEnabled(s.cfg, id, *req.Enabled) {
			http.Error(w, "unknown channel id", http.StatusNotFound)
			return
		}
		if s.cfgPath != "" {
			if err := config.Save(s.cfg, s.cfgPath); err != nil {
				http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSONOK(w, map[string]any{"status": "ok", "id": id, "enabled": *req.Enabled})
	case http.MethodDelete:
		// Disable by convention. We do not zero the config because
		// the operator may want to re-enable without re-entering
		// secrets. A future dedicated "forget channel" surface can
		// add destructive delete.
		if !setChannelEnabled(s.cfg, id, false) {
			http.Error(w, "unknown channel id", http.StatusNotFound)
			return
		}
		if s.cfgPath != "" {
			if err := config.Save(s.cfg, s.cfgPath); err != nil {
				http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSONOK(w, map[string]any{"status": "ok", "id": id, "enabled": false})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func enumerateChannels(cfg *config.Config) []channelInfo {
	return []channelInfo{
		{ID: "discord", Name: "Discord", Type: "discord", Enabled: cfg.Discord.Enabled},
		{ID: "slack", Name: "Slack", Type: "slack", Enabled: cfg.Slack.Enabled},
		{ID: "telegram", Name: "Telegram", Type: "telegram", Enabled: cfg.Telegram.Enabled},
		{ID: "matrix", Name: "Matrix", Type: "matrix", Enabled: cfg.Matrix.Enabled},
		{ID: "twilio", Name: "Twilio", Type: "twilio", Enabled: cfg.Twilio.Enabled},
		{ID: "ntfy", Name: "ntfy", Type: "ntfy", Enabled: cfg.Ntfy.Enabled},
		{ID: "email", Name: "Email", Type: "email", Enabled: cfg.Email.Enabled},
		{ID: "webhook", Name: "Webhook", Type: "webhook", Enabled: cfg.Webhook.Enabled},
		{ID: "github_webhook", Name: "GitHub Webhook", Type: "github_webhook", Enabled: cfg.GitHubWebhook.Enabled},
	}
}

// setChannelEnabled flips the Enabled flag on the matching config
// block. Returns false if id doesn't match a known channel.
func setChannelEnabled(cfg *config.Config, id string, on bool) bool {
	switch id {
	case "discord":
		cfg.Discord.Enabled = on
	case "slack":
		cfg.Slack.Enabled = on
	case "telegram":
		cfg.Telegram.Enabled = on
	case "matrix":
		cfg.Matrix.Enabled = on
	case "twilio":
		cfg.Twilio.Enabled = on
	case "ntfy":
		cfg.Ntfy.Enabled = on
	case "email":
		cfg.Email.Enabled = on
	case "webhook":
		cfg.Webhook.Enabled = on
	case "github_webhook":
		cfg.GitHubWebhook.Enabled = on
	default:
		return false
	}
	return true
}
