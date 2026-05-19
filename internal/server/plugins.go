// BL33 — REST surface for the subprocess plugin framework.
//
// Endpoints (all bearer-authenticated):
//   GET    /api/plugins                    list discovered + status
//   POST   /api/plugins/reload             rescan dir
//   GET    /api/plugins/{name}             one plugin
//   POST   /api/plugins/{name}/enable
//   POST   /api/plugins/{name}/disable
//   POST   /api/plugins/{name}/test        body: {hook, payload}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dmz006/datawatch/internal/federation"
)

// PluginsAPI is the narrow interface the REST layer needs from
// internal/plugins.Registry. Defined here so server tests don't need
// to import the plugins package. The daemon wires a concrete
// implementation via SetPluginsAPI.
type PluginsAPI interface {
	List() []any
	Get(name string) (any, bool)
	Reload() error
	SetEnabled(name string, on bool) bool
	Test(ctx context.Context, name, hook string, payload map[string]any) (any, error)
	Install(sourceDir string) error
}

// SetPluginsAPI — called from main.go.
func (s *Server) SetPluginsAPI(p PluginsAPI) { s.pluginsAPI = p }

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/plugins")
	rest = strings.TrimPrefix(rest, "/")
	// BL325 — POST /api/plugins/install {registry, name}
	// Finds the plugin in the named registry's local clone and installs it.
	if rest == "install" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		var req struct {
			Registry string `json:"registry"`
			Name     string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Registry == "" || req.Name == "" {
			http.Error(w, "registry and name required", http.StatusBadRequest)
			return
		}
		if s.skillsMgr == nil {
			http.Error(w, "skills registry not available", http.StatusServiceUnavailable)
			return
		}
		cachePath, err := s.skillsMgr.RegistryCachePath(req.Registry)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Search plugins/<category>/<name>/ tree in the registry clone.
		sourceDir, findErr := findPluginDir(cachePath, req.Name)
		if findErr != nil {
			http.Error(w, findErr.Error(), http.StatusNotFound)
			return
		}
		if s.pluginsAPI == nil {
			http.Error(w, "subprocess plugins disabled — set plugins.enabled in config", http.StatusServiceUnavailable)
			return
		}
		if err := s.pluginsAPI.Install(sourceDir); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok", "installed": req.Name, "from_registry": req.Registry})
		return
	}

	// BL325 — GET /api/plugins/browse?registry=<name>
	// Lists plugins available in a connected registry's local clone.
	if rest == "browse" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		registryName := r.URL.Query().Get("registry")
		if registryName == "" {
			http.Error(w, "registry query param required", http.StatusBadRequest)
			return
		}
		if s.skillsMgr == nil {
			http.Error(w, "skills registry not available", http.StatusServiceUnavailable)
			return
		}
		cachePath, err := s.skillsMgr.RegistryCachePath(registryName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		plugins, err := browsePluginDir(cachePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"registry": registryName, "plugins": plugins})
		return
	}

	// Native list is always available, even if subprocess plugins are
	// disabled. Only the GET-list path is permitted in that case;
	// per-plugin actions still require the subprocess registry.
	if rest == "" && r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		var subprocess []any
		if s.pluginsAPI != nil {
			subprocess = s.pluginsAPI.List()
		}
		writeJSONOK(w, map[string]any{
			"plugins": subprocess,
			"native":  s.listNativePlugins(),
		})
		return
	}
	if s.pluginsAPI == nil {
		http.Error(w, "subprocess plugins disabled (set plugins.enabled in config)", http.StatusServiceUnavailable)
		return
	}
	if rest == "" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rest == "reload" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if err := s.pluginsAPI.Reload(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok", "count": len(s.pluginsAPI.List())})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		p, ok := s.pluginsAPI.Get(name)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, p)
	case "enable", "disable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if !s.pluginsAPI.SetEnabled(name, action == "enable") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok", "name": name, "action": action})
	case "test":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		var req struct {
			Hook    string         `json:"hook"`
			Payload map[string]any `json:"payload,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Hook == "" {
			http.Error(w, "hook required", http.StatusBadRequest)
			return
		}
		resp, err := s.pluginsAPI.Test(r.Context(), name, req.Hook, req.Payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, resp)
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

// listNativePlugins computes a serialisable list of native subsystem
// entries (observer, future native bridges) for /api/plugins.
func (s *Server) listNativePlugins() []map[string]any {
	out := make([]map[string]any, 0, len(s.nativePlugins))
	for _, p := range s.nativePlugins {
		entry := map[string]any{
			"name":        p.Name,
			"kind":        "native",
			"description": p.Description,
		}
		if p.Status != nil {
			st := p.Status()
			entry["enabled"] = st.Enabled
			if st.Version != "" {
				entry["version"] = st.Version
			}
			if st.Message != "" {
				entry["message"] = st.Message
			}
		}
		out = append(out, entry)
	}
	return out
}

// pluginManifest is a minimal manifest for browsing — just the fields
// needed to identify and describe a plugin from a registry clone.
type pluginManifest struct {
	Name             string   `yaml:"name" json:"name"`
	Description      string   `yaml:"description,omitempty" json:"description,omitempty"`
	Version          string   `yaml:"version,omitempty" json:"version,omitempty"`
	Author           string   `yaml:"author,omitempty" json:"author,omitempty"`
	License          string   `yaml:"license,omitempty" json:"license,omitempty"`
	Category         string   `yaml:"category,omitempty" json:"category,omitempty"`
	Hooks            []string `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	ContributorNotes string   `yaml:"contributor_notes,omitempty" json:"contributor_notes,omitempty"`
	DatawatchMinVer  string   `yaml:"datawatch_min_version,omitempty" json:"datawatch_min_version,omitempty"`
}

type availablePlugin struct {
	Name     string         `json:"name"`
	Path     string         `json:"path"`
	Manifest pluginManifest `json:"manifest"`
}

// findPluginDir searches for a plugin directory named `name` under
// <registryCachePath>/plugins/. Returns the directory path or error.
func findPluginDir(cacheRoot, name string) (string, error) {
	pluginsRoot := filepath.Join(cacheRoot, "plugins")
	if _, err := os.Stat(pluginsRoot); err != nil {
		return "", fmt.Errorf("registry has no plugins/ directory")
	}
	var found string
	err := filepath.WalkDir(pluginsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() != name {
			return nil
		}
		// Confirm it has a manifest.yaml.
		if _, statErr := os.Stat(filepath.Join(path, "manifest.yaml")); statErr == nil {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search plugins dir: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("plugin %q not found in registry", name)
	}
	return found, nil
}

// browsePluginDir walks <cacheRoot>/plugins/ and returns all discovered plugins.
func browsePluginDir(cacheRoot string) ([]availablePlugin, error) {
	pluginsRoot := filepath.Join(cacheRoot, "plugins")
	if _, err := os.Stat(pluginsRoot); err != nil {
		return []availablePlugin{}, nil // no plugins dir = zero results, not error
	}
	var out []availablePlugin
	_ = filepath.WalkDir(pluginsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "manifest.yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var m pluginManifest
		if err := yaml.Unmarshal(data, &m); err != nil || m.Name == "" {
			return nil
		}
		pluginDir := filepath.Dir(path)
		rel, _ := filepath.Rel(cacheRoot, pluginDir)
		out = append(out, availablePlugin{Name: m.Name, Path: rel, Manifest: m})
		return nil
	})
	return out, nil
}
