package federation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// ErrGroupNotFound is returned when a named custom group does not exist.
var ErrGroupNotFound = errors.New("group not found")

// ErrGroupConflict is returned when a group name already exists.
var ErrGroupConflict = errors.New("group name already exists")

// ErrGroupBuiltin is returned when an attempt is made to mutate or delete a
// builtin group name.
var ErrGroupBuiltin = errors.New("cannot modify a builtin group name")

// GroupStore persists operator-defined custom capability groups.
// Custom groups are stored at <dataDir>/federation/groups.json.
// Builtin groups (see BuiltinGroups) are never stored — they are compiled in.
type GroupStore struct {
	mu     sync.RWMutex
	path   string
	groups map[string]*CapabilityGroup
}

// NewGroupStore loads (or creates) the custom groups store.
func NewGroupStore(dataDir string) (*GroupStore, error) {
	path := filepath.Join(dataDir, "federation", "groups.json")
	gs := &GroupStore{
		path:   path,
		groups: map[string]*CapabilityGroup{},
	}
	if err := gs.load(); err != nil {
		return nil, err
	}
	return gs, nil
}

// List returns all custom groups.
func (gs *GroupStore) List() []*CapabilityGroup {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	out := make([]*CapabilityGroup, 0, len(gs.groups))
	for _, g := range gs.groups {
		cp := *g
		out = append(out, &cp)
	}
	return out
}

// Get returns the named custom group or ErrGroupNotFound.
func (gs *GroupStore) Get(name string) (*CapabilityGroup, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	g, ok := gs.groups[name]
	if !ok {
		return nil, ErrGroupNotFound
	}
	cp := *g
	return &cp, nil
}

// Add creates a new custom group. Returns ErrGroupConflict if name is taken
// (including by a builtin). Returns ErrGroupBuiltin if name shadows a builtin.
func (gs *GroupStore) Add(g *CapabilityGroup) error {
	if g.Name == "" {
		return errors.New("name is required")
	}
	if _, builtin := BuiltinGroups[g.Name]; builtin {
		return ErrGroupBuiltin
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if _, exists := gs.groups[g.Name]; exists {
		return ErrGroupConflict
	}
	cp := *g
	cp.Builtin = false
	gs.groups[g.Name] = &cp
	return gs.persist()
}

// Update replaces an existing custom group.
func (gs *GroupStore) Update(name string, g *CapabilityGroup) error {
	if _, builtin := BuiltinGroups[name]; builtin {
		return ErrGroupBuiltin
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if _, exists := gs.groups[name]; !exists {
		return ErrGroupNotFound
	}
	cp := *g
	cp.Name = name
	cp.Builtin = false
	gs.groups[name] = &cp
	return gs.persist()
}

// Delete removes a custom group.
func (gs *GroupStore) Delete(name string) error {
	if _, builtin := BuiltinGroups[name]; builtin {
		return ErrGroupBuiltin
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if _, exists := gs.groups[name]; !exists {
		return ErrGroupNotFound
	}
	delete(gs.groups, name)
	return gs.persist()
}

// AsMap returns the custom groups as a map suitable for Resolve().
func (gs *GroupStore) AsMap() map[string]*CapabilityGroup {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	out := make(map[string]*CapabilityGroup, len(gs.groups))
	for k, v := range gs.groups {
		cp := *v
		out[k] = &cp
	}
	return out
}

func (gs *GroupStore) persist() error {
	if err := os.MkdirAll(filepath.Dir(gs.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(gs.groups, "", "  ")
	if err != nil {
		return err
	}
	tmp := gs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, gs.path)
}

func (gs *GroupStore) load() error {
	data, err := os.ReadFile(gs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m map[string]*CapabilityGroup
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for k, g := range m {
		g.Builtin = false
		gs.groups[k] = g
	}
	return nil
}
