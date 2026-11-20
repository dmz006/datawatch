package supervisorloop

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Supervisor session restart. A long supervisor session is replaced by a
// fresh one when its context grows past the configured size or its age
// passes the configured limit. The order is fixed: start the new session,
// confirm it answers, then end the old one. The old session is never ended
// before the new one has answered, so a failure at any step leaves a
// working supervisor behind.
//
// The restart is recorded step by step in a RestartStore. When the daemon
// dies halfway, the next Check finds the record and carries on from the
// recorded step instead of starting a second new session, so repeating the
// call is safe.

// RestartStep is how far a restart has got.
type RestartStep string

const (
	// RestartStarted: the new session exists; it has not yet answered.
	RestartStarted RestartStep = "started"
	// RestartConfirmed: the new session answered; the old one is still up.
	RestartConfirmed RestartStep = "confirmed"
)

// RestartReason says why a restart was due.
type RestartReason string

const (
	ReasonContextSize RestartReason = "context_size"
	ReasonContextAge  RestartReason = "context_age"
)

// ErrRestartNotConfirmed is returned when the new session never answered.
var ErrRestartNotConfirmed = errors.New("new supervisor session did not answer")

// ContextReader reads how much context a session holds, in tokens.
type ContextReader interface {
	ContextSize(ctx context.Context, sessionID string) (int, error)
}

// RestartSessions is the session layer the restart needs. The daemon wires
// the real implementation.
type RestartSessions interface {
	// Start launches the new supervisor session for the project, seeded
	// with the handover text, and returns its id.
	Start(ctx context.Context, project, model, handover string) (string, error)
	// Confirm returns nil once the session answers a liveness probe.
	Confirm(ctx context.Context, sessionID string) error
	// End ends a session. Ending a session that is already gone is not an
	// error.
	End(ctx context.Context, sessionID string) error
}

// HandoverText renders the handover the new session starts from.
type HandoverText interface {
	Handover(project string) (string, error)
}

// RestartRecord is the saved state of a restart that is under way.
type RestartRecord struct {
	Project   string
	OldID     string
	NewID     string
	Step      RestartStep
	Reason    RestartReason
	StartedAt time.Time
}

// RestartStore keeps the restart record per project. Load returns nil when
// no restart is under way.
type RestartStore interface {
	Load(project string) (*RestartRecord, error)
	Save(rec RestartRecord) error
	Clear(project string) error
}

// SupervisorSession names the live supervisor session of a project.
type SupervisorSession struct {
	ID        string
	StartedAt time.Time
}

// RestartOutcome says what a Check did.
type RestartOutcome string

const (
	// RestartNone: no limit was passed; the session stays.
	RestartNone RestartOutcome = "none"
	// RestartDone: the new session runs and the old one has ended.
	RestartDone RestartOutcome = "done"
)

// RestartResult reports a Check.
type RestartResult struct {
	Outcome RestartOutcome
	Reason  RestartReason
	// SessionID is the session that should be treated as live afterwards.
	SessionID string
	// Resumed is true when the call finished a restart an earlier call began.
	Resumed bool
}

// Restarter replaces long supervisor sessions.
type Restarter struct {
	Settings SupervisorSettings
	Reader   ContextReader
	Sessions RestartSessions
	Handover HandoverText
	Store    RestartStore
	Clock    Clock
}

func (r *Restarter) ready() error {
	if r.Reader == nil || r.Sessions == nil || r.Handover == nil || r.Store == nil || r.Clock == nil {
		return errors.New("restarter: reader, sessions, handover, store and clock are required")
	}
	return r.Settings.Validate()
}

// Due reports whether the session has passed a limit, and which one.
func (r *Restarter) Due(ctx context.Context, cur SupervisorSession) (RestartReason, bool, error) {
	if r.Reader == nil || r.Clock == nil {
		return "", false, errors.New("restarter: reader and clock are required")
	}
	size, err := r.Reader.ContextSize(ctx, cur.ID)
	if err != nil {
		return "", false, fmt.Errorf("read context size: %w", err)
	}
	if r.Settings.ContextLimit > 0 && size > r.Settings.ContextLimit {
		return ReasonContextSize, true, nil
	}
	if r.Settings.ContextAgeLimit > 0 && r.Clock.Now().Sub(cur.StartedAt) > r.Settings.ContextAgeLimit {
		return ReasonContextAge, true, nil
	}
	return "", false, nil
}

// Check restarts the session when it is due, or finishes a restart that an
// earlier call left halfway. It is safe to call again after any failure.
func (r *Restarter) Check(ctx context.Context, project string, cur SupervisorSession) (RestartResult, error) {
	if err := r.ready(); err != nil {
		return RestartResult{}, err
	}
	rec, err := r.Store.Load(project)
	if err != nil {
		return RestartResult{}, fmt.Errorf("load restart record: %w", err)
	}
	if rec != nil {
		return r.finish(ctx, *rec, true)
	}
	reason, due, err := r.Due(ctx, cur)
	if err != nil {
		return RestartResult{}, err
	}
	if !due {
		return RestartResult{Outcome: RestartNone, SessionID: cur.ID}, nil
	}
	text, err := r.Handover.Handover(project)
	if err != nil {
		return RestartResult{}, fmt.Errorf("assemble handover: %w", err)
	}
	newID, err := r.Sessions.Start(ctx, project, r.Settings.Model, text)
	if err != nil {
		return RestartResult{}, fmt.Errorf("start new session: %w", err)
	}
	next := RestartRecord{Project: project, OldID: cur.ID, NewID: newID, Step: RestartStarted, Reason: reason, StartedAt: r.Clock.Now()}
	if err := r.Store.Save(next); err != nil {
		// Without the record a repeat would start another session; end this one.
		_ = r.Sessions.End(ctx, newID)
		return RestartResult{}, fmt.Errorf("save restart record: %w", err)
	}
	return r.finish(ctx, next, false)
}

// finish carries a recorded restart from its saved step to the end.
func (r *Restarter) finish(ctx context.Context, rec RestartRecord, resumed bool) (RestartResult, error) {
	if rec.Step != RestartConfirmed {
		if err := r.Sessions.Confirm(ctx, rec.NewID); err != nil {
			// The old session is still live. Drop the new one and the record
			// so the next check begins again cleanly.
			if endErr := r.Sessions.End(ctx, rec.NewID); endErr != nil {
				return RestartResult{}, fmt.Errorf("%w: %v (ending new session: %v)", ErrRestartNotConfirmed, err, endErr)
			}
			if clrErr := r.Store.Clear(rec.Project); clrErr != nil {
				return RestartResult{}, fmt.Errorf("clear restart record: %w", clrErr)
			}
			return RestartResult{}, fmt.Errorf("%w: %v", ErrRestartNotConfirmed, err)
		}
		rec.Step = RestartConfirmed
		if err := r.Store.Save(rec); err != nil {
			return RestartResult{}, fmt.Errorf("save restart record: %w", err)
		}
	}
	if err := r.Sessions.End(ctx, rec.OldID); err != nil {
		return RestartResult{}, fmt.Errorf("end old session: %w", err)
	}
	if err := r.Store.Clear(rec.Project); err != nil {
		return RestartResult{}, fmt.Errorf("clear restart record: %w", err)
	}
	return RestartResult{Outcome: RestartDone, Reason: rec.Reason, SessionID: rec.NewID, Resumed: resumed}, nil
}
