// BL391 — /api/websearch/* REST surface: multi-provider search registry
// CRUD, usage stats, and search history. Extends the BL372 web_search
// feature (single SearXNG provider) to a named, independently enabled/
// disabled provider list (SearXNG and/or Brave Search API), with the
// registry/cache/usage-store living in internal/websearch.
//
// Legacy GET /api/web_search/stats (handleWebSearchStats, api.go) is kept
// as a deprecated alias returning the old flat shape.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/websearch"
)

// rebuildWebSearchRegistry reconstructs s.websearchReg from the current
// s.cfg.WebSearch.Providers after any CRUD mutation, resolving
// ${secret:name} API key refs against the daemon's configured secrets
// store. Errors are logged, not returned to the caller of the CRUD
// endpoint that triggered the rebuild — a provider with a bad secret ref
// shouldn't block saving the config change itself; the provider will just
// fail at search time with a clear error instead.
func (s *Server) rebuildWebSearchRegistry() {
	if s.cfg == nil {
		return
	}
	specs := make([]websearch.ProviderSpec, 0, len(s.cfg.WebSearch.Providers))
	for _, p := range s.cfg.WebSearch.Providers {
		apiKey := p.APIKey
		if apiKey != "" && s.secretsStore != nil {
			if resolved, err := secrets.ResolveRef(apiKey, s.secretsStore); err == nil {
				apiKey = resolved
			}
		}
		specs = append(specs, websearch.ProviderSpec{
			Name: p.Name, Type: p.Type, Enabled: p.Enabled, Priority: p.Priority,
			URL: p.URL, Engine: p.Engine, APIKey: apiKey,
			NumResults: p.NumResults, CacheTTLSeconds: p.CacheTTLSeconds,
		})
	}
	var cache *websearch.Cache
	if s.cfg.WebSearch.CacheEnabled {
		cache = websearch.NewCache(time.Duration(s.cfg.WebSearch.CacheTTLSeconds) * time.Second)
	}
	// Reuse the existing registry's Store (same SQLite file — only the
	// provider set + cache are rebuilt) rather than reopening the DB on
	// every config edit.
	var store *websearch.Store
	if s.websearchReg != nil {
		store = s.websearchReg.StoreHandle()
	}
	reg, err := websearch.NewRegistry(specs, cache, store, time.Duration(s.cfg.WebSearch.CacheTTLSeconds)*time.Second)
	if err != nil {
		return
	}
	s.websearchReg = reg
}

// saveWebSearchConfig persists s.cfg like saveConfig, but first restores any
// WebSearch provider's api_key that was resolved in-place from a
// "${secret:name}" ref at daemon startup (see Server.websearchAPIKeyRefs)
// back to that ref string, so the plaintext secret — kept resolved in
// memory purely so Registry.Search can use it — is never written to
// config.yaml by an unrelated edit (e.g. toggling enable/disable).
//
// skipName names a provider whose api_key the CURRENT request just set
// directly (a literal value or a fresh ref the operator typed themselves,
// via POST create or a PATCH that included api_key) — that value is the
// operator's explicit, current intent and must be saved as given, never
// masked back to a stale startup ref.
func (s *Server) saveWebSearchConfig(skipName string) error {
	if s.cfg == nil {
		return fmt.Errorf("config not available")
	}
	type restore struct {
		idx int
		val string
	}
	var restores []restore
	for i, p := range s.cfg.WebSearch.Providers {
		if p.Name == skipName {
			continue
		}
		ref, ok := s.websearchAPIKeyRefs[p.Name]
		if !ok || !strings.HasPrefix(ref, "${secret:") || ref == p.APIKey {
			continue
		}
		restores = append(restores, restore{idx: i, val: p.APIKey})
		s.cfg.WebSearch.Providers[i].APIKey = ref
	}
	err := s.saveConfig()
	for _, r := range restores {
		s.cfg.WebSearch.Providers[r.idx].APIKey = r.val
	}
	return err
}

// providerView is the REST-facing shape of a config.SearchProvider —
// currently identical to the config struct, kept as a separate type so the
// wire shape doesn't silently change if config.SearchProvider's internal
// field set evolves for unrelated (e.g. YAML-only) reasons.
type providerView struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	Priority        int    `json:"priority,omitempty"`
	URL             string `json:"url,omitempty"`
	Engine          string `json:"engine,omitempty"`
	APIKey          string `json:"api_key,omitempty"`
	NumResults      int    `json:"num_results,omitempty"`
	CacheTTLSeconds int    `json:"cache_ttl_seconds,omitempty"`
}

// providerToView converts a config.SearchProvider for a REST/CLI/MCP/comm-
// channel response, masking APIKey so a resolved plaintext secret (held in
// s.cfg purely for Registry.Search at runtime — see saveWebSearchConfig
// above) is never echoed back over any read path. A "${secret:name}" ref is
// shown as-is — the ref itself isn't sensitive and telling the operator
// which secret a provider points at is useful. A literal value (or an
// already-resolved plaintext, which is indistinguishable from one at this
// point) is replaced with a fixed placeholder.
func providerToView(p config.SearchProvider) providerView {
	v := providerView(p)
	switch {
	case v.APIKey == "":
	case strings.HasPrefix(v.APIKey, "${secret:"):
		// shown as-is
	default:
		v.APIKey = "********"
	}
	return v
}

func findProvider(providers []config.SearchProvider, name string) int {
	for i, p := range providers {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// handleWebSearchProviders handles GET/POST /api/websearch/providers and
// GET/PATCH/DELETE /api/websearch/providers/{name} (plus the
// /{name}/enable, /{name}/disable, /{name}/test action sub-paths).
func (s *Server) handleWebSearchProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/websearch/providers")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			out := make([]providerView, 0, len(s.cfg.WebSearch.Providers))
			for _, p := range s.cfg.WebSearch.Providers {
				out = append(out, providerToView(p))
			}
			writeJSONOK(w, map[string]any{"providers": out})
		case http.MethodPost:
			s.handleWebSearchProviderCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	segments := strings.SplitN(rest, "/", 2)
	name := segments[0]
	action := ""
	if len(segments) > 1 {
		action = segments[1]
	}

	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			idx := findProvider(s.cfg.WebSearch.Providers, name)
			if idx < 0 {
				http.Error(w, "provider not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, providerToView(s.cfg.WebSearch.Providers[idx]))
		case http.MethodPatch:
			s.handleWebSearchProviderUpdate(w, r, name)
		case http.MethodDelete:
			s.handleWebSearchProviderDelete(w, r, name)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "enable", "disable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		idx := findProvider(s.cfg.WebSearch.Providers, name)
		if idx < 0 {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		s.cfg.WebSearch.Providers[idx].Enabled = action == "enable"
		if err := s.saveWebSearchConfig(""); err != nil {
			http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.rebuildWebSearchRegistry()
		writeJSONOK(w, providerToView(s.cfg.WebSearch.Providers[idx]))
	case "test":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleWebSearchProviderTest(w, r, name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleWebSearchProviderCreate(w http.ResponseWriter, r *http.Request) {
	var req providerView
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if req.Type != "searxng" && req.Type != "brave" {
		http.Error(w, `type must be "searxng" or "brave"`, http.StatusBadRequest)
		return
	}
	if findProvider(s.cfg.WebSearch.Providers, req.Name) >= 0 {
		http.Error(w, "a provider with this name already exists", http.StatusConflict)
		return
	}
	s.cfg.WebSearch.Providers = append(s.cfg.WebSearch.Providers, config.SearchProvider(req))
	// req.Name's api_key is exactly what the operator just submitted
	// (literal or a ${secret:...} ref they typed themselves) — save as
	// given, skip the stale-ref masking saveWebSearchConfig otherwise does.
	if err := s.saveWebSearchConfig(req.Name); err != nil {
		http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.rebuildWebSearchRegistry()
	w.WriteHeader(http.StatusCreated)
	writeJSONOK(w, providerToView(config.SearchProvider(req)))
}

func (s *Server) handleWebSearchProviderUpdate(w http.ResponseWriter, r *http.Request, name string) {
	idx := findProvider(s.cfg.WebSearch.Providers, name)
	if idx < 0 {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}
	var req struct {
		Type            *string `json:"type,omitempty"`
		Enabled         *bool   `json:"enabled,omitempty"`
		Priority        *int    `json:"priority,omitempty"`
		URL             *string `json:"url,omitempty"`
		Engine          *string `json:"engine,omitempty"`
		APIKey          *string `json:"api_key,omitempty"`
		NumResults      *int    `json:"num_results,omitempty"`
		CacheTTLSeconds *int    `json:"cache_ttl_seconds,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	p := &s.cfg.WebSearch.Providers[idx]
	if req.Type != nil {
		if *req.Type != "searxng" && *req.Type != "brave" {
			http.Error(w, `type must be "searxng" or "brave"`, http.StatusBadRequest)
			return
		}
		p.Type = *req.Type
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		p.Priority = *req.Priority
	}
	if req.URL != nil {
		p.URL = *req.URL
	}
	if req.Engine != nil {
		p.Engine = *req.Engine
	}
	if req.APIKey != nil {
		p.APIKey = *req.APIKey
	}
	if req.NumResults != nil {
		p.NumResults = *req.NumResults
	}
	if req.CacheTTLSeconds != nil {
		p.CacheTTLSeconds = *req.CacheTTLSeconds
	}
	// Only skip masking for this provider when the operator explicitly set
	// api_key in this PATCH — a bare priority/enabled/etc. edit must not
	// let the stale resolved-plaintext value (from startup ResolveConfig)
	// leak to disk.
	skip := ""
	if req.APIKey != nil {
		skip = name
	}
	if err := s.saveWebSearchConfig(skip); err != nil {
		http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.rebuildWebSearchRegistry()
	writeJSONOK(w, providerToView(*p))
}

func (s *Server) handleWebSearchProviderDelete(w http.ResponseWriter, _ *http.Request, name string) {
	idx := findProvider(s.cfg.WebSearch.Providers, name)
	if idx < 0 {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}
	s.cfg.WebSearch.Providers = append(s.cfg.WebSearch.Providers[:idx], s.cfg.WebSearch.Providers[idx+1:]...)
	if err := s.saveWebSearchConfig(""); err != nil {
		http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.rebuildWebSearchRegistry()
	w.WriteHeader(http.StatusNoContent)
}

// handleWebSearchProviderTest runs one live query through a single named
// provider (bypassing priority fallback — the point is to test THIS
// provider specifically) and reports success/failure + a sample result,
// without requiring the provider to be Enabled first (so an operator can
// validate a new provider's credentials before flipping it on).
func (s *Server) handleWebSearchProviderTest(w http.ResponseWriter, r *http.Request, name string) {
	idx := findProvider(s.cfg.WebSearch.Providers, name)
	if idx < 0 {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}
	p := s.cfg.WebSearch.Providers[idx]
	apiKey := p.APIKey
	if apiKey != "" && s.secretsStore != nil {
		if resolved, err := secrets.ResolveRef(apiKey, s.secretsStore); err == nil {
			apiKey = resolved
		}
	}
	impl, err := websearch.NewProvider(p.Type, p.URL, p.Engine, apiKey, p.NumResults)
	if err != nil {
		writeJSONOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	results, err := impl.Search(ctx, "datawatch connectivity test", 3)
	if err != nil {
		writeJSONOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSONOK(w, map[string]any{"ok": true, "result_count": len(results), "sample": results})
}

// handleWebSearchStatsV2 returns the multi-provider usage summary + a
// zero-filled daily time series for the Dashboard "Search Usage" card
// graph. GET /api/websearch/stats?days=30
func (s *Server) handleWebSearchStatsV2(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	resp := map[string]any{
		"enabled":        s.cfg.WebSearch.Enabled,
		"cache_enabled":  s.cfg.WebSearch.CacheEnabled,
		"provider_names": s.websearchReg.ProviderNames(),
		"summary":        websearch.Summary{},
		"daily_series":   []websearch.DayCount{},
	}
	if s.websearchReg != nil {
		if summary, err := s.websearchReg.Summary(r.Context()); err == nil {
			resp["summary"] = summary
		}
		if series, err := s.websearchReg.DailySeries(r.Context(), days); err == nil {
			resp["daily_series"] = series
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// handleWebSearchHistory returns recent search events, newest first.
// GET /api/websearch/history?limit=50&offset=0
func (s *Server) handleWebSearchHistory(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	var history []websearch.HistoryEntry
	if s.websearchReg != nil {
		history, _ = s.websearchReg.History(r.Context(), limit, offset)
	}
	writeJSONOK(w, map[string]any{"history": history})
}
