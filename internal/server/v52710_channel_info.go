// v5.27.10 (BL216) — channel-bridge introspection endpoint.
//
// Operator question: "ring-laptop daemon is on Go bridge per the log,
// but Claude on this host is reading ~/.mcp.json that points at node;
// how do I know at a glance which bridge a session is on, and where
// did the stale .mcp.json come from?" — answered by exposing the
// resolved bridge state through the same parity surfaces every other
// feature ships through.

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/channel"
)

// ChannelInfo is the on-the-wire shape of GET /api/channel/info.
type ChannelInfo struct {
	// Kind: "go" when the daemon is launching sessions through the
	// native Go bridge (datawatch-channel binary); "js" when it's
	// using the embedded node + channel.js fallback.
	Kind string `json:"kind"`

	// Path: resolved bridge path. For "go" this is the binary; for
	// "js" this is the extracted channel.js (when present) or empty.
	Path string `json:"path"`

	// Ready: true when the active bridge is usable. Mirrors
	// channel.Probe(dataDir).Ready.
	Ready bool `json:"ready"`

	// Hint: human-readable explanation when not ready, or a short
	// confirmation when ready.
	Hint string `json:"hint"`

	// NodePath / NodeModules: present so an operator on the JS path
	// can see where the runtime came from without separate calls.
	NodePath    string `json:"node_path,omitempty"`
	NodeModules bool   `json:"node_modules"`

	// StaleMCPJSON: paths to `.mcp.json` files (typically in $HOME or
	// project roots) whose `datawatch` entry points at a JS file that
	// no longer exists. Pure read — daemon never deletes operator
	// files — but flags them so the operator can clean up.
	StaleMCPJSON []StaleMCPJSONEntry `json:"stale_mcp_json,omitempty"`
}

// StaleMCPJSONEntry is one stale `.mcp.json` location.
type StaleMCPJSONEntry struct {
	// Path of the `.mcp.json` file.
	Path string `json:"path"`
	// MissingChannelJS is the channel.js path the entry points at
	// that no longer exists.
	MissingChannelJS string `json:"missing_channel_js"`
}

// handleChannelInfo serves GET /api/channel/info.
func (s *Server) handleChannelInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dataDir := os.Getenv("DATAWATCH_DATA_DIR")
	if dataDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, ".datawatch")
		}
	}

	info := ChannelInfo{
		Kind: channel.BridgeKind(),
		Path: channel.BridgePath(),
	}

	probe := channel.Probe(dataDir)
	info.Ready = probe.Ready
	info.Hint = probe.Hint
	info.NodePath = probe.NodePath
	info.NodeModules = probe.NodeModules

	// On the JS path BridgePath() is empty; report the extracted
	// channel.js location when present so the UI has something to show.
	if info.Kind == "js" && info.Path == "" {
		js := filepath.Join(dataDir, "channel", "channel.js")
		if _, err := os.Stat(js); err == nil {
			info.Path = js
		}
	}

	info.StaleMCPJSON = scanStaleMCPJSON()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

// scanStaleMCPJSON checks the well-known locations BL109's
// WriteProjectMCPConfig has historically written to (operator's $HOME
// when sessions ran with projectDir=$HOME pre-v5.27.10) and reports
// any whose `datawatch` entry points at a missing channel.js.
//
// We deliberately do NOT walk the filesystem looking for project-root
// .mcp.json files — that would surprise operators with noise from
// unrelated projects. The check is bounded to $HOME/.mcp.json which
// is the only one the daemon ever wrote outside of an explicit
// project dir.
func scanStaleMCPJSON() []StaleMCPJSONEntry {
	out := []StaleMCPJSONEntry{}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return out
	}
	candidates := []string{filepath.Join(home, ".mcp.json")}
	for _, p := range candidates {
		stale, jsPath, err := channel.IsStaleProjectMCPConfig(p)
		if err != nil {
			continue // file missing / not readable — ignore
		}
		if stale {
			out = append(out, StaleMCPJSONEntry{
				Path:             p,
				MissingChannelJS: jsPath,
			})
		}
	}
	return out
}
