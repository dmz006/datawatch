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
	if n.LastAnsweredAt.IsZero() {
		n.LastAnsweredAt = old.LastAnsweredAt
	}
	n.UpdatedAt = time.Now().UTC()
	cp := *n
	r.nodes[n.Name] = &cp
	return r.persistLocked()
}

// AnsweredPersistInterval is how much newer a LastAnsweredAt must be
// than the stored one before MarkAnswered rewrites the file, so health
// polling does not persist on every call.
const AnsweredPersistInterval = 10 * time.Minute

// MarkAnswered records that the named node's address answered at the
// given time. Only LastAnsweredAt changes; the registry is persisted
// only when the stored value was zero or is older than
// AnsweredPersistInterval. Returns ErrNotFound if absent.
func (r *Registry) MarkAnswered(name string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[name]
	if !ok {
		return ErrNotFound
	}
	at = at.UTC()
	if !n.LastAnsweredAt.IsZero() && at.Sub(n.LastAnsweredAt) < AnsweredPersistInterval {
		return nil
	}
	n.LastAnsweredAt = at
	return r.persistLocked()
}

// UnansweredNode is one entry of the unanswered auto-created report.
type UnansweredNode struct {
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

// UnansweredAutoCreated returns the auto-created nodes whose address
// has never answered. It is a computed report; nothing is changed.
func (r *Registry) UnansweredAutoCreated() []UnansweredNode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []UnansweredNode{}
	for _, n := range r.nodes {
		if n.AutoCreated && n.LastAnsweredAt.IsZero() {
			out = append(out, UnansweredNode{Name: n.Name, Address: n.Address, Kind: string(n.Kind), CreatedAt: n.CreatedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RecordBackendVersion stamps a version probe result onto the named
// Node and persists it. Returns ErrNotFound if absent. Used by the
// save-time wiring in internal/server so operators can see the
// captured backend version in the node detail payload.
func (r *Registry) RecordBackendVersion(name string, vr VersionResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[name]
	if !ok {
		return ErrNotFound
	}
	cp := *n
	cp.RecordBackendVersion(vr)

	cp.UpdatedAt = time.Now().UTC()
	r.nodes[name] = &cp
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

// SetDrain sets or clears the drain state of the named Node under the
// registry lock and persists through the same save path the other node
// mutators use. now is a parameter so tests can use fixed times:
//
//   - drained=true marks the node drained. If until is non-zero it
//     bounds the drain (expired drains read as not active but the stored
//     fields are kept). A non-zero until that is not after now is
//     rejected with a clear error, so the caller gets a definite answer
//     for a past end time.
//   - drained=false (undrain) clears Drained, DrainUntil and DrainReason.
//
// InMaintenance and AllowsConsumer behaviour is unchanged by draining.
func (r *Registry) SetDrain(name string, drained bool, until time.Time, now time.Time, reason string) error {
	if drained && !until.IsZero() && !until.After(now) {
		return fmt.Errorf("compute node %q: drain_until %s must be after now %s", name,
			until.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[name]
	if !ok {
		return ErrNotFound
	}
	if drained {
		n.Drained = true
		n.DrainUntil = until
		n.DrainReason = reason
	} else {
		n.Drained = false
		n.DrainUntil = time.Time{}
		n.DrainReason = ""
	}
	n.UpdatedAt = now
	return r.persistLocked()
}

// BindObservedPeer records the observer binding for a registered
// observer peer. When a node with the peer's name exists and its
// ObserverPeer is empty, ObserverPeer is set to the peer name; no other
// field (Address, Kind, ...) is ever touched. When no node carries the
// name nothing is created and bound is false. The registry is persisted
// only when ObserverPeer actually changed.
func (r *Registry) BindObservedPeer(peerName string) (bound bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[peerName]
	if !ok {
		return false, nil
	}
	if n.ObserverPeer != "" {
		return true, nil
	}
	n.ObserverPeer = peerName
	n.UpdatedAt = time.Now().UTC()
	if err := r.persistLocked(); err != nil {
		return false, err
	}
	return true, nil
}

// ObservedPeer is an observer peer as the registry sees it: a name and
// a shape, with no compute node bound to it.
type ObservedPeer struct {
	Name  string `json:"name"`
	Shape string `json:"shape,omitempty"`
}

// BoundPeerNames returns the set of observer peer names that a compute
// node is bound to: either a node carries the name, or a node's
// ObserverPeer equals it. This is the one definition of "bound" shared
// by the free-peers route and ObservedOnly.
func (r *Registry) BoundPeerNames() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bound := make(map[string]bool, len(r.nodes)*2)
	for _, n := range r.nodes {
		if n.ObserverPeer != "" {
			bound[n.ObserverPeer] = true
		}
		bound[n.Name] = true
	}
	return bound
}

// ObservedOnly returns the given peers that have no compute node bound
// to them. It is a computed view; nothing is stored.
func (r *Registry) ObservedOnly(peers []ObservedPeer) []ObservedPeer {
	bound := r.BoundPeerNames()
	out := []ObservedPeer{}
	for _, p := range peers {
		if !bound[p.Name] {
			out = append(out, p)
		}
	}
	return out
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
