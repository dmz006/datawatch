package skills

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PAIDefaultRegistry is the canonical built-in default. The
// `skills registry add-default` verb on every surface inserts this
// entry idempotently per Q5 of the BL255 design.
var PAIDefaultRegistry = &Registry{
	Name:        "pai",
	Kind:        "git",
	URL:         "https://github.com/danielmiessler/Personal_AI_Infrastructure",
	Branch:      "main",
	Enabled:     true,
	Description: "Personal AI Infrastructure (danielmiessler/PAI) — built-in default skill registry. v1 sync target. See docs/skills.md.",
	IsBuiltin:   true,
}

// Manager wires Store + GitRegistry into a single API the daemon (and
// REST/MCP/CLI/comm/PWA surfaces) interact with.
type Manager struct {
	Store      *Store
	Git        *GitRegistry
	SyncedRoot string // ~/.datawatch/skills
}

// NewManager creates a Manager rooted at dataDir. Lazily creates the
// skills index file + cache + synced dirs.
func NewManager(dataDir string) (*Manager, error) {
	store, err := NewStore(filepath.Join(dataDir, "skills.json"))
	if err != nil {
		return nil, err
	}
	git, err := NewGitRegistry(filepath.Join(dataDir, ".skills-cache"))
	if err != nil {
		return nil, err
	}
	syncedRoot := filepath.Join(dataDir, "skills")
	if err := os.MkdirAll(syncedRoot, 0755); err != nil {
		return nil, fmt.Errorf("create skills dir: %w", err)
	}
	return &Manager{Store: store, Git: git, SyncedRoot: syncedRoot}, nil
}

// AddDefault inserts the PAI default registry idempotently. If a
// registry with name "pai" already exists, returns nil without changes.
func (m *Manager) AddDefault() error {
	if _, ok := m.Store.GetRegistry(PAIDefaultRegistry.Name); ok {
		return nil
	}
	r := *PAIDefaultRegistry
	return m.Store.CreateRegistry(&r)
}

// Connect runs the GitRegistry connect for a named registry and
// repopulates the available cache.
func (m *Manager) Connect(name string) ([]*AvailableSkill, error) {
	reg, ok := m.Store.GetRegistry(name)
	if !ok {
		_ = m.Store.SetLastSync(name, "registry not found")
		return nil, fmt.Errorf("registry %q not found", name)
	}
	if !reg.Enabled {
		return nil, fmt.Errorf("registry %q is disabled", name)
	}
	if err := m.Git.Connect(reg); err != nil {
		_ = m.Store.SetLastSync(name, err.Error())
		return nil, err
	}
	available, err := m.Git.Browse(reg)
	if err != nil {
		_ = m.Store.SetLastSync(name, err.Error())
		return nil, err
	}
	if err := m.Store.SetAvailable(name, available); err != nil {
		return nil, err
	}
	_ = m.Store.SetLastSync(name, "")
	return m.Store.Available(name), nil
}

// Browse returns the cached available list (calls Connect if cache is empty).
func (m *Manager) Browse(name string) ([]*AvailableSkill, error) {
	if avail := m.Store.Available(name); len(avail) > 0 {
		return avail, nil
	}
	return m.Connect(name)
}

// Sync copies selected skills from the registry cache into the synced
// area. If skillNames is empty, syncs nothing — callers pass `["*"]` or
// the result of Browse to mean "all available". Per BL255 design Q3, the
// sync model is select-then-copy, never bulk-by-default.
func (m *Manager) Sync(registry string, skillNames []string) ([]*Synced, error) {
	reg, ok := m.Store.GetRegistry(registry)
	if !ok {
		return nil, fmt.Errorf("registry %q not found", registry)
	}
	if len(skillNames) == 0 {
		return nil, fmt.Errorf("no skills selected; pass skill names or call SyncAll for the registry")
	}
	available := m.Store.Available(registry)
	if len(available) == 0 {
		// Auto-connect to populate cache
		if _, err := m.Connect(registry); err != nil {
			return nil, fmt.Errorf("auto-connect failed: %w", err)
		}
		available = m.Store.Available(registry)
	}
	wantAll := false
	want := map[string]bool{}
	for _, n := range skillNames {
		if n == "*" || strings.EqualFold(n, "all") {
			wantAll = true
			continue
		}
		want[n] = true
	}
	var out []*Synced
	for _, av := range available {
		if !wantAll && !want[av.Name] {
			continue
		}
		dst := filepath.Join(m.SyncedRoot, registry, av.Name)
		if err := copyDir(m.Git.SkillSourcePath(reg, av), dst); err != nil {
			return out, fmt.Errorf("copy skill %s/%s: %w", registry, av.Name, err)
		}
		s := &Synced{
			Registry: registry,
			Name:     av.Name,
			Path:     dst,
			Manifest: av.Manifest,
			SyncedAt: time.Now().UTC(),
		}
		if av.Manifest != nil {
			s.Version = av.Manifest.Version
		}
		if err := m.Store.UpsertSynced(s); err != nil {
			return out, err
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		names := strings.Join(skillNames, ", ")
		return nil, fmt.Errorf("no skills matched %q in registry %q (run `connect` to refresh cache)", names, registry)
	}
	return out, nil
}

// Unsync removes selected skills from the synced area + index.
func (m *Manager) Unsync(registry string, skillNames []string) ([]string, error) {
	if _, ok := m.Store.GetRegistry(registry); !ok {
		return nil, fmt.Errorf("registry %q not found", registry)
	}
	wantAll := false
	want := map[string]bool{}
	for _, n := range skillNames {
		if n == "*" || strings.EqualFold(n, "all") {
			wantAll = true
			continue
		}
		want[n] = true
	}
	var removed []string
	for _, sk := range m.Store.ListSynced(registry) {
		if !wantAll && !want[sk.Name] {
			continue
		}
		if err := os.RemoveAll(sk.Path); err != nil {
			return removed, fmt.Errorf("remove %s: %w", sk.Path, err)
		}
		if err := m.Store.RemoveSynced(registry, sk.Name); err != nil {
			return removed, err
		}
		removed = append(removed, sk.Name)
	}
	return removed, nil
}

// copyDir recursively copies src into dst (dst is created/replaced).
func copyDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close() //nolint:errcheck
	_, err = io.Copy(out, in)
	return err
}
