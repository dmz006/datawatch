// BL172 (S11) — peer registry for Shape B / Shape C observer peers.
//
// Persists to <data_dir>/observer/peers.json so a parent restart does
// not orphan registered peers (peer-side will re-auth via re-register
// on the first 401). Tokens are stored as bcrypt hashes; plaintext
// only ever leaves NewPeer through the return value of Register.

package observer

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

// PeerEntry is one row in the registry — what gets persisted to
// peers.json + returned by /api/observer/peers (TokenHash redacted).
type PeerEntry struct {
	Name        string         `json:"name"`
	TokenHash   string         `json:"token_hash,omitempty"`
	Shape       string         `json:"shape,omitempty"`            // "B" or "C"
	HostInfo    map[string]any `json:"host_info,omitempty"`
	Version     string         `json:"version,omitempty"`
	RegisteredAt time.Time     `json:"registered_at"`
	LastPushAt  time.Time      `json:"last_push_at,omitempty"`
	LastPayload *StatsResponse `json:"-"`
}

// PeerRegistry holds the registered Shape B / C peers.
type PeerRegistry struct {
	mu    sync.RWMutex
	path  string
	peers map[string]*PeerEntry
}

// NewPeerRegistry loads the registry from <dataDir>/observer/peers.json
// (creating the dir if necessary). Returns an empty registry on first
// boot. Failure to read an existing file is fatal — operators should
// see corruption rather than silently lose peers.
func NewPeerRegistry(dataDir string) (*PeerRegistry, error) {
	dir := filepath.Join(dataDir, "observer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir observer dir: %w", err)
	}
	r := &PeerRegistry{
		path:  filepath.Join(dir, "peers.json"),
		peers: map[string]*PeerEntry{},
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r, nil
		}
		return nil, fmt.Errorf("read peers.json: %w", err)
	}
	if len(data) == 0 {
		return r, nil
	}
	var stored []PeerEntry
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse peers.json: %w", err)
	}
	for i := range stored {
		p := stored[i]
		r.peers[p.Name] = &p
	}
	return r, nil
}

// Register mints a new token, stores its bcrypt hash, and persists.
// Returns the plaintext token (only opportunity — never stored).
// Idempotent on name: re-registration rotates the token.
func (r *PeerRegistry) Register(name, shape, version string, hostInfo map[string]any) (token string, err error) {
	if name == "" {
		return "", errors.New("peer name required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("token rand: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(tokenBytes)
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	entry := &PeerEntry{
		Name:         name,
		TokenHash:    string(hash),
		Shape:        shape,
		Version:      version,
		HostInfo:     hostInfo,
		RegisteredAt: time.Now().UTC(),
	}
	r.mu.Lock()
	r.peers[name] = entry
	if err := r.persistLocked(); err != nil {
		r.mu.Unlock()
		return "", err
	}
	r.mu.Unlock()
	return token, nil
}

// Verify checks a presented bearer token against the stored bcrypt hash.
// Returns the entry on success or an error otherwise. Constant-time on
// the hash comparison via bcrypt.
func (r *PeerRegistry) Verify(name, token string) (*PeerEntry, error) {
	r.mu.RLock()
	entry, ok := r.peers[name]
	r.mu.RUnlock()
	if !ok {
		return nil, errors.New("unknown peer")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(entry.TokenHash), []byte(token)); err != nil {
		return nil, errors.New("invalid token")
	}
	return entry, nil
}

// RecordPush stores the latest snapshot + bumps LastPushAt. Does NOT
// persist the snapshot — operators don't want a peers.json that grows
// every 5 s. Persists only the metadata change.
func (r *PeerRegistry) RecordPush(name string, snap *StatsResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.peers[name]
	if !ok {
		return errors.New("unknown peer")
	}
	entry.LastPushAt = time.Now().UTC()
	entry.LastPayload = snap
	return r.persistLocked()
}

// Get returns a copy of the peer entry (TokenHash redacted) suitable
// for /api/observer/peers list / detail responses.
func (r *PeerRegistry) Get(name string) (PeerEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.peers[name]
	if !ok {
		return PeerEntry{}, false
	}
	return redact(*entry), true
}

// LastPayload returns the latest pushed snapshot for name, or nil.
func (r *PeerRegistry) LastPayload(name string) *StatsResponse {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.peers[name]
	if !ok {
		return nil
	}
	return entry.LastPayload
}

// List returns all peers (TokenHash redacted) sorted by name.
func (r *PeerRegistry) List() []PeerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PeerEntry, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, redact(*p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Delete removes a peer. Subsequent push attempts will see a 401 and
// the peer-side will auto-re-register.
func (r *PeerRegistry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.peers[name]; !ok {
		return errors.New("unknown peer")
	}
	delete(r.peers, name)
	return r.persistLocked()
}

// persistLocked writes the registry to disk via tempfile + rename.
// Caller must hold r.mu (write lock).
func (r *PeerRegistry) persistLocked() error {
	if r.path == "" {
		return nil
	}
	out := make([]PeerEntry, 0, len(r.peers))
	for _, p := range r.peers {
		// Persist everything EXCEPT the LastPayload — that's runtime
		// state, not config, and would balloon the file.
		copy := *p
		copy.LastPayload = nil
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
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

func redact(p PeerEntry) PeerEntry {
	p.TokenHash = ""
	return p
}
