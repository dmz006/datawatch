package session

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// ExitHookAction is the action to take when a hook fires.
type ExitHookAction string

const (
	ExitHookRestart ExitHookAction = "restart" // relaunch session with same task
	ExitHookNotify  ExitHookAction = "notify"  // send message to another session
)

// ExitHookEntry defines a single crash/exit hook.
type ExitHookEntry struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`                     // session name to match (exact)
	Action          ExitHookAction `json:"action"`                   // "restart" or "notify"
	NotifySession   string         `json:"notify_session,omitempty"` // for action=notify
	NotifyMessage   string         `json:"notify_message,omitempty"` // message to send
	CooldownSeconds int            `json:"cooldown_seconds"`         // min seconds between firings (default 300)
	Enabled         bool           `json:"enabled"`
	CreatedAt       time.Time      `json:"created_at"`
	LastFiredAt     time.Time      `json:"last_fired_at,omitempty"`
}

// ExitHookStore persists exit hooks to a JSON file.
type ExitHookStore struct {
	mu      sync.Mutex
	path    string
	entries []*ExitHookEntry
}

// NewExitHookStore creates or loads an exit hook store at path.
func NewExitHookStore(path string) (*ExitHookStore, error) {
	s := &ExitHookStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ExitHookStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read exit hooks: %w", err)
	}
	return json.Unmarshal(data, &s.entries)
}

func (s *ExitHookStore) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// List returns all entries.
func (s *ExitHookStore) List() []*ExitHookEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ExitHookEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Get returns an entry by ID.
func (s *ExitHookStore) Get(id string) (*ExitHookEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			return e, true
		}
	}
	return nil, false
}

// GetBySessionName returns enabled hooks matching session name.
func (s *ExitHookStore) GetBySessionName(name string) []*ExitHookEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ExitHookEntry
	for _, e := range s.entries {
		if e.Enabled && e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

// Add inserts a new hook entry and returns it.
func (s *ExitHookStore) Add(name string, action ExitHookAction, notifySession, notifyMessage string, cooldownSeconds int) (*ExitHookEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	if cooldownSeconds <= 0 {
		cooldownSeconds = 300
	}
	e := &ExitHookEntry{
		ID:              id,
		Name:            name,
		Action:          action,
		NotifySession:   notifySession,
		NotifyMessage:   notifyMessage,
		CooldownSeconds: cooldownSeconds,
		Enabled:         true,
		CreatedAt:       time.Now(),
	}
	s.entries = append(s.entries, e)
	return e, s.save()
}

// Update modifies fields of an existing hook entry.
func (s *ExitHookStore) Update(id string, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID != id {
			continue
		}
		if v, ok := updates["name"]; ok {
			if sv, ok2 := v.(string); ok2 {
				e.Name = sv
			}
		}
		if v, ok := updates["action"]; ok {
			if sv, ok2 := v.(string); ok2 {
				e.Action = ExitHookAction(sv)
			}
		}
		if v, ok := updates["notify_session"]; ok {
			if sv, ok2 := v.(string); ok2 {
				e.NotifySession = sv
			}
		}
		if v, ok := updates["notify_message"]; ok {
			if sv, ok2 := v.(string); ok2 {
				e.NotifyMessage = sv
			}
		}
		if v, ok := updates["cooldown_seconds"]; ok {
			switch iv := v.(type) {
			case int:
				e.CooldownSeconds = iv
			case float64:
				e.CooldownSeconds = int(iv)
			}
		}
		if v, ok := updates["enabled"]; ok {
			if bv, ok2 := v.(bool); ok2 {
				e.Enabled = bv
			}
		}
		return s.save()
	}
	return fmt.Errorf("exit hook %q not found", id)
}

// Delete removes a hook entry by ID.
func (s *ExitHookStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("exit hook %q not found", id)
}

// SetEnabled enables or disables a hook.
func (s *ExitHookStore) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			e.Enabled = enabled
			return s.save()
		}
	}
	return fmt.Errorf("exit hook %q not found", id)
}

// MarkFired records the last fired time for cooldown tracking.
func (s *ExitHookStore) MarkFired(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			e.LastFiredAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("exit hook %q not found", id)
}

// IsCoolingDown returns true if the hook is within its cooldown window.
func (s *ExitHookStore) IsCoolingDown(entry *ExitHookEntry) bool {
	if entry.CooldownSeconds <= 0 || entry.LastFiredAt.IsZero() {
		return false
	}
	return time.Since(entry.LastFiredAt) < time.Duration(entry.CooldownSeconds)*time.Second
}
