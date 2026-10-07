// Package proxy provides remote datawatch server communication for proxy mode.
package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

// RemoteDispatcher handles communication with remote datawatch servers
// for command routing and session discovery.
type RemoteDispatcher struct {
	servers []config.RemoteServerConfig
	client  *http.Client

	// BL316 S2 — store holds the live, runtime-mutable server registry
	// (YAML-seeded + dynamically-added peers, e.g. via `federation peer
	// add` or `server add`). When set, effectiveServers() reads from it
	// on every call instead of the frozen `servers` snapshot captured at
	// construction time — fixing the CLI `--server` and comm-channel
	// cross-host dispatch gap for peers added after startup.
	store atomic.Pointer[multiserver.Store]

	// Session discovery cache: sessionID → serverName
	cacheMu   sync.RWMutex
	cache     map[string]string // short ID or full ID → server name
	cacheTime time.Time
	cacheTTL  time.Duration
}

// NewRemoteDispatcher creates a new dispatcher from the configured server list.
func NewRemoteDispatcher(servers []config.RemoteServerConfig) *RemoteDispatcher {
	return &RemoteDispatcher{
		servers:  servers,
		client:   &http.Client{Timeout: 10 * time.Second},
		cache:    make(map[string]string),
		cacheTTL: 30 * time.Second,
	}
}

// SetStore wires the live multiserver registry into the dispatcher. Call
// once, right after the store is constructed at daemon startup — every
// dispatch method switches from the static YAML snapshot to this store's
// current contents (which the store itself keeps merged with YAML seeds)
// from that point on.
func (d *RemoteDispatcher) SetStore(store *multiserver.Store) {
	d.store.Store(store)
}

// effectiveServers returns the current server list: the live store's
// contents when SetStore has been called, otherwise the static snapshot
// passed to NewRemoteDispatcher (startup window before the store exists,
// or tests that construct a dispatcher directly).
func (d *RemoteDispatcher) effectiveServers() []config.RemoteServerConfig {
	store := d.store.Load()
	if store == nil {
		return d.servers
	}
	entries := store.List()
	out := make([]config.RemoteServerConfig, 0, len(entries))
	for _, e := range entries {
		out = append(out, config.RemoteServerConfig{
			Name:         e.Name,
			URL:          e.URL,
			Token:        e.Token,
			Enabled:      e.Enabled,
			Federated:    e.Federated,
			AuthType:     e.AuthType,
			Capabilities: e.Capabilities,
		})
	}
	return out
}

// HasServers returns true if there are any enabled remote servers.
func (d *RemoteDispatcher) HasServers() bool {
	for _, s := range d.effectiveServers() {
		if s.Enabled {
			return true
		}
	}
	return false
}

// FindSession looks up which remote server owns a session by short ID.
// Returns the server name, or "" if not found on any remote.
func (d *RemoteDispatcher) FindSession(sessionID string) string {
	// Check cache first
	d.cacheMu.RLock()
	if srv, ok := d.cache[sessionID]; ok && time.Since(d.cacheTime) < d.cacheTTL {
		d.cacheMu.RUnlock()
		return srv
	}
	d.cacheMu.RUnlock()

	// Refresh cache
	d.refreshCache()

	d.cacheMu.RLock()
	defer d.cacheMu.RUnlock()
	return d.cache[sessionID]
}

// refreshCache fetches session lists from all remote servers.
func (d *RemoteDispatcher) refreshCache() {
	newCache := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, sv := range d.effectiveServers() {
		if !sv.Enabled {
			continue
		}
		wg.Add(1)
		go func(sv config.RemoteServerConfig) {
			defer wg.Done()
			sessions := d.fetchSessions(sv)
			mu.Lock()
			for _, s := range sessions {
				newCache[s.ID] = sv.Name
				newCache[s.FullID] = sv.Name
			}
			mu.Unlock()
		}(sv)
	}
	wg.Wait()

	d.cacheMu.Lock()
	d.cache = newCache
	d.cacheTime = time.Now()
	d.cacheMu.Unlock()
}

// fetchSessions gets the session list from a remote server.
func (d *RemoteDispatcher) fetchSessions(sv config.RemoteServerConfig) []*session.Session {
	url := strings.TrimRight(sv.URL, "/") + "/api/sessions"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if sv.Token != "" {
		req.Header.Set("Authorization", "Bearer "+sv.Token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		log.Printf("[proxy] %s: fetch sessions: %v", sv.Name, err)
		return nil
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	var sessions []*session.Session
	json.Unmarshal(body, &sessions) //nolint:errcheck
	return sessions
}

// ForwardCommand sends a command to a specific remote server via its test/message
// endpoint and returns the responses.
func (d *RemoteDispatcher) ForwardCommand(serverName, text string) ([]string, error) {
	servers := d.effectiveServers()
	var sv *config.RemoteServerConfig
	for i := range servers {
		if servers[i].Name == serverName && servers[i].Enabled {
			sv = &servers[i]
			break
		}
	}
	if sv == nil {
		return nil, fmt.Errorf("server %q not found", serverName)
	}

	url := strings.TrimRight(sv.URL, "/") + "/api/test/message"
	body := fmt.Sprintf(`{"text":%q}`, text)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if sv.Token != "" {
		req.Header.Set("Authorization", "Bearer "+sv.Token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy to %s: %w", serverName, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Responses []string `json:"responses"`
	}
	json.Unmarshal(respBody, &result) //nolint:errcheck
	return result.Responses, nil
}

// ForwardHTTP forwards an HTTP request to a remote server's API.
// Used for session commands like send, kill, status when the session is remote.
func (d *RemoteDispatcher) ForwardHTTP(serverName, method, path string, body io.Reader) ([]byte, int, error) {
	servers := d.effectiveServers()
	var sv *config.RemoteServerConfig
	for i := range servers {
		if servers[i].Name == serverName && servers[i].Enabled {
			sv = &servers[i]
			break
		}
	}
	if sv == nil {
		return nil, 0, fmt.Errorf("server %q not found", serverName)
	}

	url := strings.TrimRight(sv.URL, "/") + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if sv.Token != "" {
		req.Header.Set("Authorization", "Bearer "+sv.Token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// ListAllSessions returns sessions from all remote servers, tagged with server name.
func (d *RemoteDispatcher) ListAllSessions() map[string][]*session.Session {
	result := make(map[string][]*session.Session)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, sv := range d.effectiveServers() {
		if !sv.Enabled {
			continue
		}
		wg.Add(1)
		go func(sv config.RemoteServerConfig) {
			defer wg.Done()
			sessions := d.fetchSessions(sv)
			mu.Lock()
			result[sv.Name] = sessions
			mu.Unlock()
		}(sv)
	}
	wg.Wait()
	return result
}
