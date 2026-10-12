// BL403 — per-plugin push tokens for POST /api/plugins/{name}/widgets/{id}.
//
// This mirrors internal/observer.PeerRegistry's token shape (32 random
// bytes, base64url, bcrypt hash at rest, tmp+rename persistence at 0o600)
// but is deliberately a separate registry: a plugin is not a federation
// peer, and reusing PeerRegistry would put plugins into the observer peer
// list, the compute/federation views and peers.json.

package plugins

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ErrUnknownPluginToken is returned by Verify when no token has been
// minted for the plugin.
var ErrUnknownPluginToken = errors.New("no widget push token registered for plugin")

// ErrInvalidPluginToken is returned by Verify when the presented token does
// not match the plugin's stored hash.
var ErrInvalidPluginToken = errors.New("invalid widget push token")

// pluginTokenEntry is one persisted row of widget_tokens.json.
type pluginTokenEntry struct {
	Plugin    string    `json:"plugin"`
	TokenHash string    `json:"token_hash"`
	IssuedAt  time.Time `json:"issued_at"`
}

// PluginTokenRegistry holds one widget push token per plugin. Only bcrypt
// hashes are kept; the plaintext token leaves the registry once, through
// Register's return value.
type PluginTokenRegistry struct {
	mu      sync.RWMutex
	path    string
	entries map[string]*pluginTokenEntry
}

// NewPluginTokenRegistry loads the registry from
// <dataDir>/plugins/widget_tokens.json. An empty dataDir gives a
// memory-only registry. A missing or empty file is treated as empty; a
// corrupt file is returned as an error (matching the peer registry).
func NewPluginTokenRegistry(dataDir string) (*PluginTokenRegistry, error) {
	r := &PluginTokenRegistry{entries: map[string]*pluginTokenEntry{}}
	if dataDir == "" {
		return r, nil
	}
	dir := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir plugins dir: %w", err)
	}
	r.path = filepath.Join(dir, "widget_tokens.json")
	data, err := os.ReadFile(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r, nil
		}
		return nil, fmt.Errorf("read widget_tokens.json: %w", err)
	}
	if len(data) == 0 {
		return r, nil
	}
	var stored []pluginTokenEntry
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse widget_tokens.json: %w", err)
	}
	for i := range stored {
		e := stored[i]
		r.entries[e.Plugin] = &e
	}
	return r, nil
}

// Register mints a new token for plugin, stores its bcrypt hash and
// persists. Calling it again rotates the token: the previous one stops
// verifying. Returns the plaintext token (the only opportunity to read it).
func (r *PluginTokenRegistry) Register(plugin string) (string, error) {
	if plugin == "" {
		return "", errors.New("plugin name required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("token rand: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	prev, hadPrev := r.entries[plugin]
	r.entries[plugin] = &pluginTokenEntry{Plugin: plugin, TokenHash: string(hash), IssuedAt: time.Now().UTC()}
	if err := r.persistLocked(); err != nil {
		// Keep memory and disk in agreement: the caller never sees the
		// new token, so the old one must stay valid.
		if hadPrev {
			r.entries[plugin] = prev
		} else {
			delete(r.entries, plugin)
		}
		return "", err
	}
	return token, nil
}

// Verify checks a presented token against plugin's stored bcrypt hash.
func (r *PluginTokenRegistry) Verify(plugin, token string) error {
	r.mu.RLock()
	e, ok := r.entries[plugin]
	r.mu.RUnlock()
	if !ok {
		return ErrUnknownPluginToken
	}
	if err := bcrypt.CompareHashAndPassword([]byte(e.TokenHash), []byte(token)); err != nil {
		return ErrInvalidPluginToken
	}
	return nil
}

// Revoke removes plugin's token, if any. Subsequent pushes are rejected
// until a new token is registered.
func (r *PluginTokenRegistry) Revoke(plugin string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[plugin]; !ok {
		return nil
	}
	delete(r.entries, plugin)
	return r.persistLocked()
}

// persistLocked writes the registry via tmp file + rename at 0o600. It is
// a no-op for a memory-only registry. Caller must hold r.mu (write lock).
func (r *PluginTokenRegistry) persistLocked() error {
	if r.path == "" {
		return nil
	}
	out := make([]pluginTokenEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
