// Package identity (BL257 Phase 1, v6.8.0) implements the operator
// identity / Telos layer.
//
// An Identity is a structured operator self-description (role, goals,
// values, focus, context notes) loaded from ~/.datawatch/identity.yaml.
// The daemon injects it into every LLM session's L0 system prompt via
// PromptText so AI work stays anchored to operator priorities.
//
// The PAI source concept is "Telos" (see
// docs/plans/2026-05-02-pai-comparison-analysis.md §8). Datawatch's
// adaptation drops PAI's life-goal scope (health/finances/relationships
// — out of scope for a technical tool) and keeps the work-relevant
// fields. The interview-style init flow lives in BL257 Phase 2.
package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Identity is the operator-supplied self-description.
//
// All fields are optional; an empty Identity yields an empty PromptText
// so the wake-up stack adds nothing. Field-by-field merge semantics are
// implemented by Manager.Update.
type Identity struct {
	Role            string    `yaml:"role,omitempty" json:"role,omitempty"`
	NorthStarGoals  []string  `yaml:"north_star_goals,omitempty" json:"north_star_goals,omitempty"`
	CurrentProjects []string  `yaml:"current_projects,omitempty" json:"current_projects,omitempty"`
	Values          []string  `yaml:"values,omitempty" json:"values,omitempty"`
	CurrentFocus    string    `yaml:"current_focus,omitempty" json:"current_focus,omitempty"`
	ContextNotes    string    `yaml:"context_notes,omitempty" json:"context_notes,omitempty"`
	UpdatedAt       time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
}

// IsEmpty reports whether the identity has no operator-supplied content.
// UpdatedAt is ignored — a default-zero identity returns true.
func (id Identity) IsEmpty() bool {
	return id.Role == "" &&
		len(id.NorthStarGoals) == 0 &&
		len(id.CurrentProjects) == 0 &&
		len(id.Values) == 0 &&
		id.CurrentFocus == "" &&
		id.ContextNotes == ""
}

// Manager owns the on-disk identity.yaml and provides safe concurrent
// access. NewManager loads the existing file (if any). Set/Update
// rewrite atomically and cache the parsed value.
type Manager struct {
	mu   sync.RWMutex
	path string
	cur  Identity
}

// NewManager constructs a Manager rooted at path. If path does not
// exist, Manager starts empty (no error). Read errors other than
// not-exist are returned.
func NewManager(path string) (*Manager, error) {
	m := &Manager{path: path}
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return m, nil
}

// Path returns the on-disk file path (read-only).
func (m *Manager) Path() string { return m.path }

func (m *Manager) load() error {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}
	var id Identity
	if err := yaml.Unmarshal(b, &id); err != nil {
		return fmt.Errorf("identity yaml: %w", err)
	}
	m.mu.Lock()
	m.cur = id
	m.mu.Unlock()
	return nil
}

// Reload re-reads the file from disk. Useful when a hot-reload signal
// fires. A missing file is treated as empty (no error).
func (m *Manager) Reload() error {
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(nil) { // no-op marker
	}
	return nil
}

// Get returns a snapshot of the current identity.
func (m *Manager) Get() Identity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Set replaces the entire identity, stamps UpdatedAt, persists to disk
// (0600), and updates the cache. Empty identities are still written.
func (m *Manager) Set(next Identity) (Identity, error) {
	next.UpdatedAt = time.Now().UTC()
	if err := m.write(next); err != nil {
		return Identity{}, err
	}
	m.mu.Lock()
	m.cur = next
	m.mu.Unlock()
	return next, nil
}

// Update merges non-empty fields from patch into the current identity,
// stamps UpdatedAt, persists, and returns the merged result. List
// fields use replace-when-non-empty semantics (a non-nil empty slice
// keeps the existing value); pass an explicit zero list to clear via
// Set instead.
func (m *Manager) Update(patch Identity) (Identity, error) {
	m.mu.Lock()
	cur := m.cur
	if patch.Role != "" {
		cur.Role = patch.Role
	}
	if len(patch.NorthStarGoals) > 0 {
		cur.NorthStarGoals = patch.NorthStarGoals
	}
	if len(patch.CurrentProjects) > 0 {
		cur.CurrentProjects = patch.CurrentProjects
	}
	if len(patch.Values) > 0 {
		cur.Values = patch.Values
	}
	if patch.CurrentFocus != "" {
		cur.CurrentFocus = patch.CurrentFocus
	}
	if patch.ContextNotes != "" {
		cur.ContextNotes = patch.ContextNotes
	}
	cur.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()
	if err := m.write(cur); err != nil {
		return Identity{}, err
	}
	m.mu.Lock()
	m.cur = cur
	m.mu.Unlock()
	return cur, nil
}

func (m *Manager) write(id Identity) error {
	b, err := yaml.Marshal(&id)
	if err != nil {
		return fmt.Errorf("identity marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return fmt.Errorf("identity mkdir: %w", err)
	}
	if err := os.WriteFile(m.path, b, 0o600); err != nil {
		return fmt.Errorf("identity write: %w", err)
	}
	return nil
}

// PromptText renders the identity as plain text suitable for L0 wake-up
// injection. Returns empty string when the identity is empty so the
// wake-up scaffold adds no section header.
func (m *Manager) PromptText() string {
	id := m.Get()
	if id.IsEmpty() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Operator Identity\n")
	if id.Role != "" {
		fmt.Fprintf(&sb, "Role: %s\n", id.Role)
	}
	if len(id.NorthStarGoals) > 0 {
		sb.WriteString("North-Star Goals:\n")
		for _, g := range id.NorthStarGoals {
			fmt.Fprintf(&sb, "  - %s\n", g)
		}
	}
	if len(id.CurrentProjects) > 0 {
		sb.WriteString("Current Projects:\n")
		for _, p := range id.CurrentProjects {
			fmt.Fprintf(&sb, "  - %s\n", p)
		}
	}
	if len(id.Values) > 0 {
		sb.WriteString("Values:\n")
		for _, v := range id.Values {
			fmt.Fprintf(&sb, "  - %s\n", v)
		}
	}
	if id.CurrentFocus != "" {
		fmt.Fprintf(&sb, "Current Focus: %s\n", id.CurrentFocus)
	}
	if id.ContextNotes != "" {
		fmt.Fprintf(&sb, "Context Notes: %s\n", id.ContextNotes)
	}
	return sb.String()
}

// SetField updates a single string field by name. Used by CLI/comm
// surfaces that take a (field, value) pair. Returns the merged
// Identity. Unknown field names error.
//
// List fields accept a comma-separated value.
func (m *Manager) SetField(field, value string) (Identity, error) {
	field = strings.ToLower(strings.TrimSpace(field))
	patch := Identity{}
	switch field {
	case "role":
		patch.Role = value
	case "north_star_goals", "goals", "north-star-goals":
		patch.NorthStarGoals = splitList(value)
	case "current_projects", "projects":
		patch.CurrentProjects = splitList(value)
	case "values":
		patch.Values = splitList(value)
	case "current_focus", "focus":
		patch.CurrentFocus = value
	case "context_notes", "notes":
		patch.ContextNotes = value
	default:
		return Identity{}, fmt.Errorf("unknown identity field %q (allowed: role, north_star_goals, current_projects, values, current_focus, context_notes)", field)
	}
	return m.Update(patch)
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
