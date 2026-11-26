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
	SchedTypeSpawn      = "spawn"       // start a fresh ephemeral one-shot session (GH#128)
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

	// FilterID is the output filter that queued this command; it is then
	// typed marked as that filter's, and only while the filter may type.
	FilterID string `json:"filter_id,omitempty"`

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

	// SessionName (BL353) — human-readable session name. A command schedule
	// with one is typed into SessionID while that session can take input, else
	// into the live session with this name; it outlives the sessions it is
	// typed into (see schedule_target.go).
	SessionName string `json:"session_name,omitempty"`
	// NameWaitAlerted is true once the operator was told that no live session
	// has SessionName; a delivery clears it.
	NameWaitAlerted bool `json:"name_wait_alerted,omitempty"`

	// CronExpr (BL353) — 5-field cron expression for recurring schedules.
	// When set, overrides RecurEverySeconds-based recurrence. After each fire
	// the scheduler computes the next RunAt via CronNext(CronExpr, now).
	CronExpr string `json:"cron_expr,omitempty"`

	// ScheduleName (BL353) — optional human-readable label for this schedule entry.
	// Used for lookup and cancel by name.
	ScheduleName string `json:"schedule_name,omitempty"`

	// GH#128 run-history fields (spawn/new_session types only).
	// ActiveSpawnID is the FullID of the last spawned session while it is still
	// running. Cleared (via RecordFire) once the session reaches a terminal state.
	// Used by the overlap guard in the scheduler loop.
	ActiveSpawnID  string    `json:"active_spawn_id,omitempty"`
	LastFireAt     time.Time `json:"last_fire_at,omitempty"`
	LastFireResult string    `json:"last_fire_result,omitempty"` // "spawned", "skipped", "failed"
	FireCount      int       `json:"fire_count,omitempty"`
	// ConsecutiveFailures counts spawn fires that failed to start in a row;
	// reset by a successful spawn.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`
	// ConsecutiveWaits counts starts in a row that were refused for capacity
	// (session cap, cooldown, draining). A wait is not a failure.
	ConsecutiveWaits int `json:"consecutive_waits,omitempty"`
	// WaitingSince is when the current run of waits for capacity began; it
	// bounds how long a one-time schedule waits. Cleared by a spawned fire.
	WaitingSince time.Time `json:"waiting_since,omitempty"`
	// RearmState is "", "retrying", "waiting_capacity", "gave_up" or
	// "waiting_session".
	RearmState string `json:"rearm_state,omitempty"`
	// LastError is the reason of the last refused or failed start.
	LastError string `json:"last_error,omitempty"`
	// FireHistory is the newest fires and attempts, oldest first, bounded.
	FireHistory []ScheduleFire `json:"fire_history,omitempty"`
	// Kind marks a command datawatch itself queued for a purpose:
	// SchedKindRateLimitResume, or empty.
	Kind string `json:"kind,omitempty"`
	// FiringSince is set, and saved, before a due command is typed, and
	// cleared when the outcome is recorded. One found at daemon start means
	// the daemon stopped in between (see schedule_claim.go).
	FiringSince *time.Time `json:"firing_since,omitempty"`
}

// ScheduleFire is one fire or start attempt of a schedule.
type ScheduleFire struct {
	At        time.Time `json:"at"`
	Result    string    `json:"result"` // spawned, skipped, failed, deferred
	SessionID string    `json:"session_id,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// DeferredSession holds parameters for creating a new session at a scheduled time.
type DeferredSession struct {
	Task       string `json:"task"`
	ProjectDir string `json:"project_dir"`
	Backend    string `json:"backend"`
	Name       string `json:"name"`
	// v8.11.0 — extended spawn fields (GH#128)
	LLMRef     string `json:"llm_ref,omitempty"`    // unified LLM registry ref
	Model      string `json:"model,omitempty"`      // model name override
	Effort     string `json:"effort,omitempty"`     // quick/normal/thorough
	OneShot    bool   `json:"one_shot,omitempty"`   // terminate on DATAWATCH_COMPLETE:
	Ephemeral  bool   `json:"ephemeral,omitempty"`  // reap workspace directory on delete
	Subprocess bool   `json:"subprocess,omitempty"` // run via bash -c, not TUI (exit code = completion)
	Git        bool   `json:"git,omitempty"`        // opt in to git init/snapshot/commit (default false)
	// GH#204 Phase 1 — name of the schedule entry that owns this spawn,
	// carried server-side into the spawned session's Session.ScheduleName
	// so results/sessions can be grouped per schedule_name.
	ScheduleName string `json:"schedule_name,omitempty"` // originating schedule entry name
	// LaunchRole, when set, is carried server-side into the spawned
	// session's Session.LaunchRole — e.g. "supervisor" so the spawned pass
	// takes the lean supervisor profile instead of a full worker.
	// Empty leaves the session untagged.
	LaunchRole string `json:"launch_role,omitempty"`
}

// ScheduleStore persists scheduled commands to a JSON file.
type ScheduleStore struct {
	mu      sync.Mutex
	path    string
	encKey  []byte
	entries []*ScheduledCommand
	// typing holds the sessions a schedule is being typed into now.
	typing map[string]bool
	// settleHook is told of the schedules datawatch ended undelivered.
	settleHook func([]ScheduledCommand)
	// targetCheck, when set, refuses new schedules that could never run.
	targetCheck func(sessionID string) error
	// fileSum and seen tell an outside change of the file from our own.
	fileSum [32]byte
	haveSum bool
	seen    map[string]bool
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
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return err
	}
	s.noteFileLocked()
	return nil
}

func (s *ScheduleStore) save() error {
	s.adoptLocked() // never write over what was added outside
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := secfile.WriteFile(s.path, data, 0600, s.encKey); err != nil {
		return err
	}
	s.noteFileLocked()
	return nil
}

// Add inserts a new scheduled command and returns it.
func (s *ScheduleStore) Add(sessionID, command string, runAt time.Time, runAfterID string) (*ScheduledCommand, error) {
	if err := s.checkTarget(sessionID, ""); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkNewLocked(runAfterID); err != nil {
		return nil, err
	}

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

// AddOptions holds all options for AddFull (BL353).
type AddOptions struct {
	SessionID    string
	SessionName  string
	ScheduleName string
	FilterID     string
	Command      string
	RunAt        time.Time
	RunAfterID   string
	CronExpr     string
	RecurEvery   int
	RecurUntil   time.Time
}

// AddFull inserts a scheduled command with all BL353 options.
func (s *ScheduleStore) AddFull(opts AddOptions) (*ScheduledCommand, error) {
	if err := s.checkTarget(opts.SessionID, opts.SessionName); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkNewLocked(opts.RunAfterID); err != nil {
		return nil, err
	}

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	sc := &ScheduledCommand{
		ID:                id,
		SessionID:         opts.SessionID,
		SessionName:       opts.SessionName,
		ScheduleName:      opts.ScheduleName,
		FilterID:          opts.FilterID,
		Command:           opts.Command,
		RunAt:             opts.RunAt,
		RunAfterID:        opts.RunAfterID,
		CronExpr:          opts.CronExpr,
		RecurEverySeconds: opts.RecurEvery,
		RecurUntil:        opts.RecurUntil,
		State:             SchedPending,
		CreatedAt:         time.Now(),
	}
	s.entries = append(s.entries, sc)
	return sc, s.save()
}

// GetByScheduleName returns the first pending schedule with the given ScheduleName.
func (s *ScheduleStore) GetByScheduleName(name string) (*ScheduledCommand, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.State == SchedPending && sc.ScheduleName == name {
			return sc, true
		}
	}
	return nil, false
}

// CancelByScheduleName cancels all pending schedules with the given ScheduleName.
func (s *ScheduleStore) CancelByScheduleName(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := 0
	for _, sc := range s.entries {
		if sc.State == SchedPending && sc.ScheduleName == name {
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

// AddSpawnOptions holds parameters for scheduling an ephemeral one-shot session (GH#128).
type AddSpawnOptions struct {
	Task         string    // task prompt for the spawned session (required)
	ProjectDir   string    // working directory
	Backend      string    // LLM backend name (legacy; prefer LLMRef)
	LLMRef       string    // unified LLM registry reference
	Model        string    // model name override
	Effort       string    // quick/normal/thorough
	SessionName  string    // human-readable name for the spawned session
	ScheduleName string    // name for this schedule entry (for lookup/cancel)
	CronExpr     string    // 5-field cron for recurring spawns
	RunAt        time.Time // explicit first fire time (zero = computed from CronExpr)
	OneShot      bool      // auto-terminate session on DATAWATCH_COMPLETE: (default true for spawn)
	Ephemeral    bool      // reap workspace directory when session is deleted (default true)
	Subprocess   bool      // run task as subprocess (exit code = completion, no TUI scraping)
	Git          bool      // opt in to git init/snapshot/commit (default false)
	// LaunchRole, when non-empty, is recorded on DeferredSession so the
	// fire path stamps the spawned session's launch role (e.g.
	// supervisorloop.LaunchRoleSupervisor for the lean supervisor profile).
	LaunchRole string
}

// AddSpawn schedules an ephemeral one-shot session to start at a future time or
// on a recurring cron, independent of any running session (GH#128).
func (s *ScheduleStore) AddSpawn(opts AddSpawnOptions) (*ScheduledCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addSpawnLocked(opts)
}

// addSpawnLocked is AddSpawn's body; callers hold s.mu.
func (s *ScheduleStore) addSpawnLocked(opts AddSpawnOptions) (*ScheduledCommand, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}

	runAt := opts.RunAt
	if runAt.IsZero() && opts.CronExpr != "" {
		runAt, err = CronNext(opts.CronExpr, time.Now())
		if err != nil {
			return nil, fmt.Errorf("cron: %w", err)
		}
	}

	sc := &ScheduledCommand{
		ID:           id,
		Type:         SchedTypeSpawn,
		Command:      opts.Task,
		RunAt:        runAt,
		CronExpr:     opts.CronExpr,
		ScheduleName: opts.ScheduleName,
		State:        SchedPending,
		CreatedAt:    time.Now(),
		DeferredSession: &DeferredSession{
			Task:         opts.Task,
			ProjectDir:   opts.ProjectDir,
			Backend:      opts.Backend,
			Name:         opts.SessionName,
			LLMRef:       opts.LLMRef,
			Model:        opts.Model,
			Effort:       opts.Effort,
			OneShot:      opts.OneShot,
			Ephemeral:    opts.Ephemeral,
			Subprocess:   opts.Subprocess,
			Git:          opts.Git,
			ScheduleName: opts.ScheduleName,
			LaunchRole:   opts.LaunchRole,
		},
	}
	s.entries = append(s.entries, sc)
	return sc, s.save()
}

// EnsureNamedSpawn is the idempotent named set-up for recurring spawns:
// it returns the pending spawn schedule named opts.ScheduleName, adding it
// from opts only when no pending spawn under that name exists yet. A second
// call for the same name returns the existing entry (created == false)
// instead of adding a second one.
func (s *ScheduleStore) EnsureNamedSpawn(opts AddSpawnOptions) (sc *ScheduledCommand, created bool, err error) {
	if opts.ScheduleName == "" {
		return nil, false, fmt.Errorf("ensure named spawn: ScheduleName is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.Type == SchedTypeSpawn && e.State == SchedPending && e.ScheduleName == opts.ScheduleName {
			return e, false, nil
		}
	}
	nsc, aerr := s.addSpawnLocked(opts)
	if aerr != nil {
		return nil, false, aerr
	}
	return nsc, true, nil
}

// DuePendingSessions returns pending deferred sessions (new_session or spawn type)
// that are due to start by time t.
func (s *ScheduleStore) DuePendingSessions(t time.Time) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		if sc.Type != SchedTypeNewSession && sc.Type != SchedTypeSpawn {
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
// Matches both short IDs and full IDs (hostname-id). Command schedules created
// with a session name are kept: they wait for the next session with that name.
func (s *ScheduleStore) CancelBySession(sessionID string) int {
	var settled []ScheduledCommand
	defer func() { s.reportSettled(settled) }() // runs after the unlock
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := 0
	for _, sc := range s.entries {
		if sc.State != SchedPending || sc.NameTargeted() {
			continue
		}
		if sc.SessionID == sessionID || strings.HasSuffix(sc.SessionID, "-"+sessionID) || strings.HasSuffix(sessionID, "-"+sc.SessionID) {
			settled = append(settled, s.settleLocked(sc, SchedCancelled, settledSessionEndedKill, time.Now())...)
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
// Only returns command-type (default/empty or "command") entries.
// new_session and spawn entries are handled by DuePendingSessions; returning them
// here would cause the command-dispatch loop to mark them failed (no session match).
func (s *ScheduleStore) DuePending(t time.Time) []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State != SchedPending {
			continue
		}
		// GH#128: skip session-spawning types; they are handled by DuePendingSessions.
		if sc.Type != SchedTypeCommand && sc.Type != "" {
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
	var settled []ScheduledCommand
	defer func() { s.reportSettled(settled) }() // runs after the unlock
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id {
			// GH#128 — if the entry was cancelled between DuePending/DuePendingSessions
			// and MarkDone (e.g. user cancelled while scheduler was mid-fire), respect
			// the cancellation and do not re-arm. This prevents the race where a cron
			// re-arm overwrites an explicit cancel.
			if sc.State == SchedCancelled {
				return nil
			}
			// The claim goes in the same save as the outcome.
			sc.FiringSince = nil
			if !failed {
				sc.endNameWait()
				sc.endInterruptedNote()
				// A command that was typed ends a run of failed sends.
				// Spawn schedules count their own in RecordFire.
				if (sc.Type == "" || sc.Type == SchedTypeCommand) && sc.ConsecutiveFailures > 0 {
					sc.ConsecutiveFailures, sc.LastError, sc.LastFireResult = 0, "", ""
				}
			}
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
			// BL353 — cron-based recurrence: compute next fire time via CronNext.
			if !failed && sc.CronExpr != "" {
				next, err := CronNext(sc.CronExpr, time.Now())
				if err == nil {
					if !sc.RecurUntil.IsZero() && next.After(sc.RecurUntil) {
						sc.State = SchedDone
						sc.DoneAt = time.Now()
					} else {
						sc.RunAt = next
						sc.State = SchedPending
					}
					return s.save()
				}
			}
			if failed {
				if sc.State == SchedPending {
					settled = s.settleLocked(sc, SchedFailed, settledNotDelivered, time.Now())
					return s.save()
				}
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

// RecordFire updates run-history fields on a spawn/new_session schedule entry.
// Call after MarkDone (which re-arms the cron) so the new RunAt is preserved.
// sessionID is the FullID of the spawned session (empty on failure/skip).
// result is "spawned", "skipped", or "failed". A spawned fire clears the
// failure and wait counters and the re-arm state.
func (s *ScheduleStore) RecordFire(id, sessionID, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id {
			sc.ActiveSpawnID = sessionID
			sc.LastFireAt = time.Now()
			sc.LastFireResult = result
			sc.FireCount++
			if result == "spawned" {
				sc.ConsecutiveFailures = 0
				sc.ConsecutiveWaits = 0
				sc.WaitingSince = time.Time{}
				sc.RearmState = ""
				sc.LastError = ""
			}
			sc.appendFire(ScheduleFire{At: sc.LastFireAt, Result: result, SessionID: sessionID})
			return s.save()
		}
	}
	return fmt.Errorf("scheduled command %q not found", id)
}

// RecordSpawnFailure records a spawn fire that failed to start a session under
// the default re-arm policy: a recurring schedule (cron or fixed interval) is
// re-armed for its next fire and stays pending, so one transient start failure
// does not silently end an hourly job; a one-time schedule becomes failed.
// Returns the number of consecutive failures and whether the schedule was
// re-armed. RecordStartFailure takes a cause and a policy.
func (s *ScheduleStore) RecordSpawnFailure(id string) (consecutive int, rearmed bool, err error) {
	out, err := s.RecordStartFailure(id, nil, time.Now(), DefaultRearmPolicy())
	return out.Consecutive, out.Rearmed, err
}

// SkipFire advances the cron RunAt without spawning (overlap guard).
// Updates LastFireResult="skipped" and FireCount without changing ActiveSpawnID.
func (s *ScheduleStore) SkipFire(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID == id {
			sc.LastFireAt = time.Now()
			sc.LastFireResult = "skipped"
			sc.FireCount++
			sc.appendFire(ScheduleFire{At: sc.LastFireAt, Result: "skipped", SessionID: sc.ActiveSpawnID})
			if sc.CronExpr != "" {
				next, err := CronNext(sc.CronExpr, time.Now())
				if err == nil {
					sc.RunAt = next
				}
			} else if sc.RecurEverySeconds > 0 {
				sc.RunAt = time.Now().Add(time.Duration(sc.RecurEverySeconds) * time.Second)
			}
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
