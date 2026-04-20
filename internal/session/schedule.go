package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Scheduled command states.
const (
	SchedPending   = "pending"
	SchedDone      = "done"
	SchedCancelled = "cancelled"
	SchedFailed    = "failed"
)

// Schedule entry types.
const (
	SchedTypeCommand    = "command"     // send input to existing session
	SchedTypeNewSession = "new_session" // start a new session at scheduled time
)

// ScheduledCommand is a command queued to be sent to a session at a specific time
// or when the session next enters waiting_input state.
type ScheduledCommand struct {
	// ID is a random 8-hex-char identifier.
	ID string `json:"id"`

	// Type distinguishes between sending a command and starting a new session.
	// Default (empty or "command") = send to existing session.
	Type string `json:"type,omitempty"`

	// SessionID is the short or full session ID to send the command to.
	// For new_session type, this is filled after the session starts.
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

	// DeferredSession holds new session parameters (only for SchedTypeNewSession).
	DeferredSession *DeferredSession `json:"deferred_session,omitempty"`

	// RecurEverySeconds (BL26) — when > 0, the scheduler reschedules
	// this command at RunAt + RecurEverySeconds after each successful
	// execution. State stays "pending" (instead of transitioning to
	// "done"). Cancellation requires an explicit comm/REST cancel.
	RecurEverySeconds int `json:"recur_every_seconds,omitempty"`

	// RecurUntil (BL26) — optional deadline; the recurrence stops
	// firing once Now > RecurUntil. Zero = unlimited.
	RecurUntil time.Time `json:"recur_until,omitempty"`
}

// DeferredSession holds parameters for creating a new session at a scheduled time.
type DeferredSession struct {
	Task       string `json:"task"`
	ProjectDir string `json:"project_dir"`
	Backend    string `json:"backend"`
	Name       string `json:"name"`
}

// ScheduleStore persists scheduled commands to a JSON file.
type ScheduleStore struct {
	mu      sync.Mutex
	path    string
	encKey  []byte
	entries []*ScheduledCommand
}

// NewScheduleStore creates or loads the schedule store at path (no encryption).
func NewScheduleStore(path string) (*ScheduleStore, error) {
	return newScheduleStoreWithKey(path, nil)
}

// NewScheduleStoreEncrypted creates a ScheduleStore with AES-256-GCM encryption at rest.
func NewScheduleStoreEncrypted(path string, key []byte) (*ScheduleStore, error) {
	return newScheduleStoreWithKey(path, key)
}

func newScheduleStoreWithKey(path string, key []byte) (*ScheduleStore, error) {
	s := &ScheduleStore{path: path, encKey: key}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ScheduleStore) load() error {
	data, err := secfile.ReadFile(s.path, s.encKey)
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
	return secfile.WriteFile(s.path, data, 0600, s.encKey)
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

// AddDeferredSession schedules a new session to be started at runAt.
func (s *ScheduleStore) AddDeferredSession(name, task, projectDir, backend string, runAt time.Time) (*ScheduledCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	sc := &ScheduledCommand{
		ID:        id,
		Type:      SchedTypeNewSession,
		Command:   task,
		RunAt:     runAt,
		State:     SchedPending,
		CreatedAt: time.Now(),
		DeferredSession: &DeferredSession{
			Task:       task,
			ProjectDir: projectDir,
			Backend:    backend,
			Name:       name,
		},
	}
	s.entries = append(s.entries, sc)
	return sc, s.save()
}

// DuePendingSessions returns pending deferred sessions that are due to start by time t.
func (s *ScheduleStore) DuePendingSessions(t time.Time) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State != SchedPending || sc.Type != SchedTypeNewSession {
			continue
		}
		if !sc.RunAt.IsZero() && !sc.RunAt.After(t) {
			out = append(out, sc)
		}
	}
	return out
}

// CountForSession (BL116) returns the number of pending scheduled
// commands for the given session. Used by session-list renderers
// (web UI badge, comm-channel `session list`) to surface that work
// is queued without requiring callers to walk the full slice.
func (s *ScheduleStore) CountForSession(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		if sc.SessionID == sessionID {
			n++
		}
	}
	return n
}

// PendingForSession returns all pending scheduled commands for a session.
func (s *ScheduleStore) PendingForSession(sessionID string) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		if sc.SessionID == sessionID || (sc.DeferredSession != nil && sc.SessionID == sessionID) {
			out = append(out, sc)
		}
	}
	return out
}

// Update modifies a scheduled command (for editing).
func (s *ScheduleStore) Update(id string, command string, runAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id && sc.State == SchedPending {
			if command != "" {
				sc.Command = command
			}
			if !runAt.IsZero() {
				sc.RunAt = runAt
			}
			return s.save()
		}
	}
	return fmt.Errorf("scheduled command %q not found or not pending", id)
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

// Delete removes a scheduled command entry entirely (any state).
func (s *ScheduleStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range s.entries {
		if sc.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("scheduled command %q not found", id)
}

// CancelBySession cancels all pending scheduled commands for a given session ID.
// Matches both short IDs and full IDs (hostname-id).
func (s *ScheduleStore) CancelBySession(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := 0
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		if sc.SessionID == sessionID || strings.HasSuffix(sc.SessionID, "-"+sessionID) || strings.HasSuffix(sessionID, "-"+sc.SessionID) {
			sc.State = SchedCancelled
			sc.DoneAt = time.Now()
			cancelled++
		}
	}
	if cancelled > 0 {
		_ = s.save()
	}
	return cancelled
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
			// BL26 — recurring schedule: bump RunAt instead of marking done,
			// unless the entry hit its RecurUntil deadline or the run failed.
			if !failed && sc.RecurEverySeconds > 0 {
				next := time.Now().Add(time.Duration(sc.RecurEverySeconds) * time.Second)
				if !sc.RecurUntil.IsZero() && next.After(sc.RecurUntil) {
					sc.State = SchedDone
					sc.DoneAt = time.Now()
				} else {
					sc.RunAt = next
					sc.State = SchedPending
				}
				return s.save()
			}
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
