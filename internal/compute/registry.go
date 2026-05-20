// v7.0.0 S1 — ComputeNode registry. Persists to JSON in the data dir
// (mirrors how other v6 registries — personas, secrets — handle
// state). cfg.compute_nodes seeds the registry at startup; runtime
// CRUD writes back to JSON.

package compute

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/dmz006/datawatch/internal/secfile"
	"time"
)

// ErrNotFound is returned by Get / Update / Delete when the named
// Node is unknown.
var ErrNotFound = errors.New("compute node not found")

// ErrConflict is returned by Add when a Node with the same name
// already exists.
var ErrConflict = errors.New("compute node already exists")

// Registry is the in-memory + on-disk store for ComputeNodes.
// Goroutine-safe.
type Registry struct {
	mu     sync.RWMutex
	path   string
	nodes  map[string]*Node
	encKey []byte // BL334 T43g — non-nil when --secure is active
}

// NewRegistry opens / creates the JSON registry at the given path.
// path is normally <data-dir>/compute/nodes.json.
func NewRegistry(path string) (*Registry, error) {
	return newRegistry(path, nil)
}

// NewRegistryEncrypted is like NewRegistry but encrypts nodes.json (BL334 T43g).
func NewRegistryEncrypted(path string, key []byte) (*Registry, error) {
	return newRegistry(path, key)
}

func newRegistry(path string, key []byte) (*Registry, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("compute registry: mkdir: %w", err)
	}
	r := &Registry{path: path, nodes: map[string]*Node{}, encKey: key}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// Seed merges cfg.compute_nodes into the in-memory registry. Called
// once at daemon startup. Existing entries (from JSON) are kept;
// cfg-supplied entries fill gaps but do NOT overwrite operator
// runtime edits — to force-overwrite use the explicit Update path.
func (r *Registry) Seed(cfgNodes []Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range cfgNodes {
		n := cfgNodes[i] // copy
		if _, exists := r.nodes[n.Name]; exists {
			continue
		}
		if n.SchedulingPriority == 0 {
			n.SchedulingPriority = 50
		}
		now := time.Now().UTC()
		if n.CreatedAt.IsZero() {
			n.CreatedAt = now
		}
		n.UpdatedAt = now
		r.nodes[n.Name] = &n
	}
	_ = r.persistLocked()
}

// List returns every Node, sorted by name (deterministic for tests
// and stable UI rendering).
func (r *Registry) List() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		cp := *n
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a copy of the Node with the given name, or ErrNotFound.
func (r *Registry) Get(name string) (*Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *n
	return &cp, nil
}

// Add creates a new Node. Returns ErrConflict if name is taken.
func (r *Registry) Add(n *Node) error {
	if err := n.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.nodes[n.Name]; exists {
		return ErrConflict
	}
	now := time.Now().UTC()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	if n.SchedulingPriority == 0 {
		n.SchedulingPriority = 50
	}
	cp := *n
	r.nodes[n.Name] = &cp
	return r.persistLocked()
}

// Update replaces the named Node. Returns ErrNotFound if absent.
func (r *Registry) Update(n *Node) error {
	if err := n.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.nodes[n.Name]
	if !ok {
		return ErrNotFound
	}
	if old.CreatedAt.IsZero() {
		old.CreatedAt = time.Now().UTC()
	}
	n.CreatedAt = old.CreatedAt
	n.UpdatedAt = time.Now().UTC()
	cp := *n
	r.nodes[n.Name] = &cp
	return r.persistLocked()
}

// Delete removes the named Node. Returns ErrNotFound if absent.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.nodes[name]; !ok {
		return ErrNotFound
	}
	delete(r.nodes, name)
	return r.persistLocked()
}

// EnsureFromStatsPeer is the auto-create path: when a datawatch-stats
// peer push arrives and no matching Node exists, create one with
// sensible defaults (kind=remote, max_concurrent_models=1, etc.).
// Returns the existing or newly-created Node.
//
// peerAddr is the http.Request RemoteAddr (host:port form is OK; we
// store as-is so operator can refine).
func (r *Registry) EnsureFromStatsPeer(peerName, peerAddr, shape string) (*Node, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.nodes[peerName]; ok {
		// Refresh address if it changed (peer might have moved).
		if peerAddr != "" && existing.Address != peerAddr {
			existing.Address = peerAddr
			existing.UpdatedAt = time.Now().UTC()
			_ = r.persistLocked()
		}
		cp := *existing
		return &cp, false, nil
	}
	n := AutoCreatedFromStatsPeer(peerName, peerAddr, shape)
	if err := n.Validate(); err != nil {
		return nil, false, err
	}
	cp := *n
	r.nodes[n.Name] = &cp
	if err := r.persistLocked(); err != nil {
		return nil, false, err
	}
	return n, true, nil
}

// load reads the JSON file (if present). Missing file = empty registry.
func (r *Registry) load() error {
	b, err := secfile.ReadFile(r.path, r.encKey)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("compute registry: read: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	var stored []Node
	if err := json.Unmarshal(b, &stored); err != nil {
		return fmt.Errorf("compute registry: parse: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range stored {
		n := stored[i] // copy
		r.nodes[n.Name] = &n
	}
	return nil
}

// persistLocked writes the registry to disk (caller holds r.mu).
func (r *Registry) persistLocked() error {
	out := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("compute registry: marshal: %w", err)
	}
	if err := secfile.WriteFile(r.path, data, 0o600, r.encKey); err != nil {
		return fmt.Errorf("compute registry: write: %w", err)
	}
	return nil
}
