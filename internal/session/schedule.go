package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Scheduled command states.
const (
	SchedPending   = "pending"
	SchedDone      = "done"
	SchedCancelled = "cancelled"
	SchedFailed    = "failed"
)

// ScheduledCommand is a command queued to be sent to a session at a specific time
// or when the session next enters waiting_input state.
type ScheduledCommand struct {
	// ID is a random 8-hex-char identifier.
	ID string `json:"id"`

	// SessionID is the short or full session ID to send the command to.
	SessionID string `json:"session_id"`

	// Command is the text to send as input to the session.
	Command string `json:"command"`

	// RunAt is the scheduled time. Zero value means "run when session enters waiting_input".
	RunAt time.Time `json:"run_at"`

	// RunAfterID, if non-empty, means this command runs after the referenced
	// scheduled command completes (RunAt is ignored when RunAfterID is set).
	RunAfterID string `json:"run_after_id,omitempty"`

	// State is one of: pending, done, cancelled, failed.
	State string `json:"state"`

	// CreatedAt records when this was scheduled.
	CreatedAt time.Time `json:"created_at"`

	// DoneAt records when this was executed or cancelled.
	DoneAt time.Time `json:"done_at,omitempty"`
}

// ScheduleStore persists scheduled commands to a JSON file.
type ScheduleStore struct {
	mu      sync.Mutex
	path    string
	entries []*ScheduledCommand
}

// NewScheduleStore creates or loads the schedule store at path.
func NewScheduleStore(path string) (*ScheduleStore, error) {
	s := &ScheduleStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ScheduleStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read schedule: %w", err)
	}
	return json.Unmarshal(data, &s.entries)
}

func (s *ScheduleStore) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Add inserts a new scheduled command and returns it.
func (s *ScheduleStore) Add(sessionID, command string, runAt time.Time, runAfterID string) (*ScheduledCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	sc := &ScheduledCommand{
		ID:         id,
		SessionID:  sessionID,
		Command:    command,
		RunAt:      runAt,
		RunAfterID: runAfterID,
		State:      SchedPending,
		CreatedAt:  time.Now(),
	}
	s.entries = append(s.entries, sc)
	return sc, s.save()
}

// Cancel marks a scheduled command as cancelled.
func (s *ScheduleStore) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id && sc.State == SchedPending {
			sc.State = SchedCancelled
			sc.DoneAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("scheduled command %q not found or not pending", id)
}

// List returns all scheduled commands matching the given states.
// If states is empty, all entries are returned.
func (s *ScheduleStore) List(states ...string) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(states) == 0 {
		out := make([]*ScheduledCommand, len(s.entries))
		copy(out, s.entries)
		return out
	}
	stateSet := make(map[string]bool, len(states))
	for _, st := range states {
		stateSet[st] = true
	}
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if stateSet[sc.State] {
			out = append(out, sc)
		}
	}
	return out
}

// DuePending returns all pending commands that are due to run by time t (RunAt <= t)
// and do not have a RunAfterID dependency.
func (s *ScheduleStore) DuePending(t time.Time) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		if sc.RunAfterID != "" {
			continue // handled separately
		}
		if !sc.RunAt.IsZero() && !sc.RunAt.After(t) {
			out = append(out, sc)
		}
	}
	return out
}

// WaitingInputPending returns pending commands for sessionID that should fire
// when the session enters waiting_input (RunAt is zero, no RunAfterID).
func (s *ScheduleStore) WaitingInputPending(sessionID string) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		if sc.SessionID != sessionID {
			continue
		}
		if sc.RunAt.IsZero() && sc.RunAfterID == "" {
			out = append(out, sc)
		}
	}
	return out
}

// AfterDone returns pending commands whose RunAfterID points to a now-done command.
func (s *ScheduleStore) AfterDone(doneID string) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State == SchedPending && sc.RunAfterID == doneID {
			out = append(out, sc)
		}
	}
	return out
}

// MarkDone marks a scheduled command as done or failed.
func (s *ScheduleStore) MarkDone(id string, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id {
			if failed {
				sc.State = SchedFailed
			} else {
				sc.State = SchedDone
			}
			sc.DoneAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("scheduled command %q not found", id)
}

// Get returns a scheduled command by ID.
func (s *ScheduleStore) Get(id string) (*ScheduledCommand, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id {
			return sc, true
		}
	}
	return nil, false
}

func randomID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
