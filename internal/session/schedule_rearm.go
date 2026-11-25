package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// Re-arm modes: what a schedule does after a start that failed for a real
// reason. A wait for capacity is never a failure and ignores the mode.
const (
	// RearmNextFire tries a recurring schedule again at its next scheduled
	// time and never gives up; a one-time schedule becomes failed. This is the
	// default and the behaviour before the policy existed.
	RearmNextFire = "next_fire"
	// RearmBackoff retries sooner with a growing pause, up to MaxRetries
	// failures in a row, then gives up: the schedule stays failed and alerts.
	RearmBackoff = "backoff"
	// RearmStayFailed marks the schedule failed on the first real failure and
	// alerts.
	RearmStayFailed = "stay_failed"
)

// Re-arm states shown beside a schedule.
const (
	RearmStateRetrying = "retrying"
	RearmStateWaiting  = "waiting_capacity"
	RearmStateGaveUp   = "gave_up"
)

const (
	defaultRearmBackoff    = 30 * time.Second
	defaultRearmBackoffMax = 15 * time.Minute
	// DefaultOneTimeMaxWait is how long a one-time schedule refused for
	// capacity waits before it gives up.
	DefaultOneTimeMaxWait = 12 * time.Hour
	// maxLastErrorChars bounds the stored reason of a refused start.
	maxLastErrorChars = 300
)

// ScheduleFireHistoryLimit is how many fires and attempts a schedule keeps.
const ScheduleFireHistoryLimit = 20

// ScheduleWaitAlertAfter is the number of waits for capacity in a row after
// which one alert is raised (then one per twelve times as many).
const ScheduleWaitAlertAfter = 5

// ErrMaxSessions is the refusal Start returns when the session cap is full.
// Its text is the stable prefix "max sessions" that callers elsewhere match.
var ErrMaxSessions = errors.New("max sessions")

// RearmPolicy says how a failed schedule start is re-armed. The zero value is
// the default policy: next_fire, no retry limit, 30 s first backoff, 15 min
// backoff ceiling.
type RearmPolicy struct {
	Mode       string
	MaxRetries int // backoff mode: failures in a row before giving up; 0 = never give up
	Backoff    time.Duration
	BackoffMax time.Duration
	// OneTimeMaxWait is how long a one-time schedule refused for capacity
	// keeps waiting before it gives up. Recurring schedules never use it.
	OneTimeMaxWait time.Duration
}

// DefaultRearmPolicy is the policy that keeps the behaviour from before the
// policy existed.
func DefaultRearmPolicy() RearmPolicy { return RearmPolicy{}.Normalised() }

// Normalised fills defaults and repairs unusable values.
func (p RearmPolicy) Normalised() RearmPolicy {
	switch p.Mode {
	case RearmNextFire, RearmBackoff, RearmStayFailed:
	default:
		p.Mode = RearmNextFire
	}
	if p.MaxRetries < 0 {
		p.MaxRetries = 0
	}
	if p.Backoff <= 0 {
		p.Backoff = defaultRearmBackoff
	}
	if p.BackoffMax <= 0 {
		p.BackoffMax = defaultRearmBackoffMax
	}
	if p.BackoffMax < p.Backoff {
		p.BackoffMax = p.Backoff
	}
	if p.OneTimeMaxWait <= 0 {
		p.OneTimeMaxWait = DefaultOneTimeMaxWait
	}
	return p
}

// delay is the pause before attempt n (1-based): Backoff doubled each time,
// capped at BackoffMax.
func (p RearmPolicy) delay(n int) time.Duration {
	d := p.Backoff
	for i := 1; i < n && d < p.BackoffMax; i++ {
		d *= 2
	}
	if d > p.BackoffMax {
		d = p.BackoffMax
	}
	return d
}

// StartFailure is what RecordStartFailure did to the schedule.
type StartFailure struct {
	Consecutive int       `json:"consecutive_failures"`
	Waits       int       `json:"consecutive_waits"`
	Wait        bool      `json:"wait"`       // the refusal was a wait for capacity
	Rearmed     bool      `json:"rearmed"`    // the schedule is pending again
	GaveUp      bool      `json:"gave_up"`    // the policy gave up: the schedule stays failed
	WaitLimit   bool      `json:"wait_limit"` // the give-up was a one-time schedule that waited for capacity too long
	NextAt      time.Time `json:"next_at"`
}

// IsStartWait reports whether a start error means "no capacity right now": a
// full session cap, the global rate-limit cooldown, a draining daemon or a
// capacity wait that timed out. It also matches the same text arriving from a
// peer. None of these is a schedule failure.
func IsStartWait(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMaxSessions) || errors.Is(err, ErrGlobalCooldown) ||
		errors.Is(err, ErrDaemonDraining) || errors.Is(err, capacity.ErrWaitTimeout) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"max sessions", "global rate-limit cooldown", "daemon is shutting down", "capacity wait timed out"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// naturalNext is the next scheduled time of a recurring schedule after now,
// or false for a one-time schedule or one past its RecurUntil.
func (sc *ScheduledCommand) naturalNext(now time.Time) (time.Time, bool) {
	var next time.Time
	switch {
	case sc.CronExpr != "":
		n, err := CronNext(sc.CronExpr, now)
		if err != nil {
			return time.Time{}, false
		}
		next = n
	case sc.RecurEverySeconds > 0:
		next = now.Add(time.Duration(sc.RecurEverySeconds) * time.Second)
	default:
		return time.Time{}, false
	}
	if !sc.RecurUntil.IsZero() && next.After(sc.RecurUntil) {
		return time.Time{}, false
	}
	return next, true
}

// appendFire adds one entry to the bounded fire history. Callers hold s.mu.
func (sc *ScheduledCommand) appendFire(f ScheduleFire) {
	sc.FireHistory = append(sc.FireHistory, f)
	if n := len(sc.FireHistory); n > ScheduleFireHistoryLimit {
		sc.FireHistory = append([]ScheduleFire(nil), sc.FireHistory[n-ScheduleFireHistoryLimit:]...)
	}
}

// waitHours words a wait limit for a reason text: "12 hours", "1 hour".
func waitHours(d time.Duration) string {
	h := int(d / time.Hour)
	if d%time.Hour != 0 || h < 1 {
		return d.String()
	}
	if h == 1 {
		return "1 hour"
	}
	return fmt.Sprintf("%d hours", h)
}

func truncateReason(err error) string {
	if err == nil {
		return ""
	}
	r := err.Error()
	if len(r) > maxLastErrorChars {
		r = r[:maxLastErrorChars] + "…"
	}
	return r
}

// RecordStartFailure records a spawn fire that did not start a session and
// re-arms the schedule under the policy.
//
//   - A wait for capacity (IsStartWait) never fails a recurring schedule: it
//     stays pending and retries after a growing pause, never later than its
//     next scheduled time. It does not count as a failure and does not spend
//     MaxRetries. A one-time schedule waits the same way, but only for
//     OneTimeMaxWait from its first refusal; then it fails with the reason
//     (rearm_state gave_up) and the operator is alerted.
//   - Any other failure follows the policy mode.
//
// A cancelled schedule stays cancelled.
func (s *ScheduleStore) RecordStartFailure(id string, cause error, now time.Time, p RearmPolicy) (StartFailure, error) {
	p = p.Normalised()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID != id {
			continue
		}
		if sc.State == SchedCancelled {
			return StartFailure{Consecutive: sc.ConsecutiveFailures}, nil
		}
		reason := truncateReason(cause)
		natural, hasNatural := sc.naturalNext(now)
		sc.LastFireAt = now
		sc.LastError = reason
		var out StartFailure
		retryAt := func(d time.Duration) time.Time {
			next := now.Add(d)
			if hasNatural && natural.Before(next) {
				next = natural
			}
			return next
		}
		if IsStartWait(cause) && !hasNatural && !sc.WaitingSince.IsZero() && !now.Before(sc.WaitingSince.Add(p.OneTimeMaxWait)) {
			// A one-time schedule has waited as long as the setting allows.
			reason = fmt.Sprintf("gave up waiting for capacity after %s: %s", waitHours(p.OneTimeMaxWait), reason)
			sc.FireCount++
			sc.ConsecutiveWaits++
			sc.LastError, sc.LastFireResult = reason, "failed"
			sc.State, sc.RearmState, sc.DoneAt = SchedFailed, RearmStateGaveUp, now
			sc.appendFire(ScheduleFire{At: now, Result: "failed", Error: reason})
			out = StartFailure{GaveUp: true, WaitLimit: true, NextAt: sc.RunAt}
		} else if IsStartWait(cause) {
			if sc.ConsecutiveWaits == 0 || sc.WaitingSince.IsZero() {
				sc.WaitingSince = now
			}
			sc.ConsecutiveWaits++
			sc.LastFireResult = "deferred"
			sc.RearmState = RearmStateWaiting
			sc.RunAt, sc.State = retryAt(p.delay(sc.ConsecutiveWaits)), SchedPending
			if limit := sc.WaitingSince.Add(p.OneTimeMaxWait); !hasNatural && sc.RunAt.After(limit) {
				sc.RunAt = limit // the last retry is the one at the limit
			}
			sc.appendFire(ScheduleFire{At: now, Result: "deferred", Error: reason})
			out = StartFailure{Wait: true, Rearmed: true, NextAt: sc.RunAt}
		} else {
			sc.FireCount++
			sc.ConsecutiveFailures++
			sc.ConsecutiveWaits = 0
			sc.LastFireResult = "failed"
			sc.appendFire(ScheduleFire{At: now, Result: "failed", Error: reason})
			switch p.Mode {
			case RearmStayFailed:
				out.GaveUp = true
			case RearmBackoff:
				if p.MaxRetries > 0 && sc.ConsecutiveFailures > p.MaxRetries {
					out.GaveUp = true
				} else {
					sc.RunAt, sc.State, sc.RearmState = retryAt(p.delay(sc.ConsecutiveFailures)), SchedPending, RearmStateRetrying
					out.Rearmed = true
				}
			default: // next_fire
				if hasNatural {
					sc.RunAt, sc.State, sc.RearmState = natural, SchedPending, RearmStateRetrying
					out.Rearmed = true
				} else {
					out.GaveUp = true
				}
			}
			if out.GaveUp {
				sc.State, sc.RearmState, sc.DoneAt = SchedFailed, RearmStateGaveUp, now
			}
			out.NextAt = sc.RunAt
		}
		out.Consecutive, out.Waits = sc.ConsecutiveFailures, sc.ConsecutiveWaits
		return out, s.save()
	}
	return StartFailure{}, fmt.Errorf("scheduled command %q not found", id)
}

// ScheduleAlert is what the alert hook receives when a schedule needs the
// operator: it waited for capacity many times in a row, the policy gave up,
// (NoSession) no live session has the name a command schedule is for, or
// (Interrupted) the daemon stopped while a command was being typed.
type ScheduleAlert struct {
	Schedule  ScheduledCommand
	Outcome   StartFailure
	Err       error
	NoSession bool
	// Interrupted: the command may or may not have arrived.
	Interrupted bool
}

// SetScheduleAlertHook sets the function that raises the operator alert for a
// schedule that keeps waiting, that the policy gave up on, or whose session
// name has no live session.
func (m *Manager) SetScheduleAlertHook(fn func(ScheduleAlert)) {
	m.mu.Lock()
	m.schedAlertHook = fn
	m.mu.Unlock()
}

// SetScheduleRearmPolicy sets the provider of the re-arm policy. It is read at
// every failed start, so a changed setting applies without a restart.
func (m *Manager) SetScheduleRearmPolicy(fn func() RearmPolicy) {
	m.mu.Lock()
	m.schedRearmPolicy = fn
	m.mu.Unlock()
}

func (m *Manager) scheduleRearmPolicy() RearmPolicy {
	m.mu.Lock()
	fn := m.schedRearmPolicy
	m.mu.Unlock()
	if fn == nil {
		return DefaultRearmPolicy()
	}
	return fn().Normalised()
}

// scheduleAlertDue says whether this outcome warrants an alert: the policy gave
// up; or a wait reached ScheduleWaitAlertAfter (then once per twelve times as
// many); or a real failure is the first in a row (then once per 24).
func scheduleAlertDue(o StartFailure) bool {
	switch {
	case o.GaveUp:
		return true
	case o.Wait:
		return o.Waits == ScheduleWaitAlertAfter ||
			(o.Waits > ScheduleWaitAlertAfter && o.Waits%(ScheduleWaitAlertAfter*12) == 0)
	default:
		return o.Consecutive == 1 || o.Consecutive%24 == 0
	}
}

// RecordScheduleStartFailure records a failed or refused start of a due
// schedule under the configured policy and raises the alert when due.
func (m *Manager) RecordScheduleStartFailure(sc *ScheduledCommand, cause error) StartFailure {
	out, err := m.schedStore.RecordStartFailure(sc.ID, cause, time.Now(), m.scheduleRearmPolicy())
	if err != nil {
		fmt.Printf("[schedule] record start failure for %s: %v\n", sc.ID, err)
		return out
	}
	fmt.Printf("[schedule] start refused (type=%s sched=%s wait=%v failures=%d waits=%d rearmed=%v gave_up=%v): %v\n",
		sc.Type, sc.ID, out.Wait, out.Consecutive, out.Waits, out.Rearmed, out.GaveUp, cause)
	if scheduleAlertDue(out) {
		m.mu.Lock()
		hook := m.schedAlertHook
		m.mu.Unlock()
		if hook != nil {
			if cur, ok := m.schedStore.Get(sc.ID); ok {
				hook(ScheduleAlert{Schedule: *cur, Outcome: out, Err: cause})
			}
		}
	}
	return out
}

// FireDueSessions starts every spawn and new-session schedule that is due at
// now. The manager's own schedule loop and the daemon's scheduler goroutine
// both call it, and a lock makes concurrent calls take turns, so one due entry
// starts one session whichever caller sees it first.
func (m *Manager) FireDueSessions(ctx context.Context, now time.Time) {
	if m.schedStore == nil {
		return
	}
	m.schedFireMu.Lock()
	defer m.schedFireMu.Unlock()
	for _, sc := range m.schedStore.DuePendingSessions(now) {
		m.fireDueSession(ctx, sc)
	}
}

func (m *Manager) fireDueSession(ctx context.Context, sc *ScheduledCommand) {
	ds := sc.DeferredSession
	if ds == nil {
		_ = m.schedStore.MarkDone(sc.ID, true)
		fmt.Printf("[schedule] spawn [%s] has no DeferredSession, marking failed\n", sc.ID)
		return
	}
	// Overlap guard: skip while the previous spawn is still running.
	if sc.ActiveSpawnID != "" {
		if prior, ok := m.GetSession(sc.ActiveSpawnID); ok &&
			prior.State != StateComplete && prior.State != StateFailed && prior.State != StateKilled {
			fmt.Printf("[schedule] spawn [%s] skip: prior run %s still %s\n", sc.ID, sc.ActiveSpawnID, prior.State)
			_ = m.schedStore.SkipFire(sc.ID)
			return
		}
	}
	opts := &StartOptions{
		Name:               ds.Name,
		Backend:            ds.Backend,
		LLMRef:             ds.LLMRef,
		Model:              ds.Model,
		Effort:             ds.Effort,
		OneShot:            ds.OneShot,
		EphemeralWorkspace: ds.Ephemeral,
		Subprocess:         ds.Subprocess,
		Git:                ds.Git,
		// A schedule of the supervisor role launches in the supervisor
		// class; any other schedule is an operator start.
		AdmitClass: AdmitClassForLaunchRole(ds.LaunchRole),
	}
	task := ds.Task
	if ds.LaunchRole != "" {
		m.mu.Lock()
		hook := m.scheduledSpawnTask
		m.mu.Unlock()
		if hook != nil {
			if t := hook(*ds); t != "" {
				task = t
			}
		}
	}
	// A launch-role spawn resolves its worker profile the way an interactive
	// start of the same session kind does, so a scheduled supervisor pass
	// starts on the lean profile and records the profile or why it fell back.
	var profileFallback *string
	if ds.LaunchRole != "" {
		m.mu.Lock()
		pfn := m.scheduledSpawnProfile
		m.mu.Unlock()
		if pfn != nil {
			profileFallback = pfn(*ds, opts)
		}
	}
	newSess, err := m.Start(ctx, task, "", ds.ProjectDir, opts)
	if err != nil {
		m.RecordScheduleStartFailure(sc, err)
		return
	}
	if profileFallback != nil && *profileFallback != "" {
		newSess.ProfileFallbackReason = *profileFallback
		if serr := m.store.Save(newSess); serr != nil {
			fmt.Printf("[schedule] persist profile fallback on %s: %v\n", newSess.FullID, serr)
		}
	}
	// Spawn schedules must NOT store the launched session's ID: CancelBySession
	// fires when the spawned session is deleted, which would cancel the
	// recurring schedule.
	if sc.Type != SchedTypeSpawn {
		sc.SessionID = newSess.FullID
	}
	// The schedule that spawned this session and the launch role are stamped
	// server-side, never self-reported by the job.
	name := ds.ScheduleName
	if name == "" {
		name = sc.ScheduleName
	}
	attach := false
	if name != "" && newSess.ScheduleName == "" {
		newSess.ScheduleName = name
		attach = true
	}
	if ds.LaunchRole != "" && newSess.LaunchRole == "" {
		newSess.LaunchRole = ds.LaunchRole
		attach = true
	}
	if attach {
		if err := m.store.Save(newSess); err != nil {
			fmt.Printf("[schedule] persist spawn metadata on %s: %v\n", newSess.FullID, err)
		}
	}
	_ = m.schedStore.MarkDone(sc.ID, false)
	_ = m.schedStore.RecordFire(sc.ID, newSess.FullID, "spawned")
	fmt.Printf("[schedule] spawned session %s (%s type=%s)\n", newSess.FullID, ds.Name, sc.Type)
}

// RearmText is the one-line account of a schedule's next fire, last result and
// re-arm state, shared by the comm channel, the MCP list and the CLI:
//
//	next 21:05:00 · last deferred 21:00 (max sessions (20) reached) · waiting_capacity x3
func (sc *ScheduledCommand) RearmText() string {
	var parts []string
	switch {
	case sc.State == SchedFailed:
		parts = append(parts, "no next fire (failed)")
	case sc.State != SchedPending:
		parts = append(parts, sc.State)
	case sc.RunAt.IsZero():
		parts = append(parts, "next on input")
	default:
		parts = append(parts, "next "+sc.RunAt.Local().Format("2006-01-02 15:04:05"))
	}
	if sc.LastFireResult != "" {
		last := "last " + sc.LastFireResult
		if !sc.LastFireAt.IsZero() {
			last += " " + sc.LastFireAt.Local().Format("15:04")
		}
		if sc.LastError != "" && sc.LastFireResult != "spawned" {
			last += " (" + sc.LastError + ")"
		}
		parts = append(parts, last)
	}
	switch sc.RearmState {
	case RearmStateWaitingSession:
		parts = append(parts, fmt.Sprintf("%s (name:%s)", sc.RearmState, sc.SessionName))
	case RearmStateWaiting:
		parts = append(parts, fmt.Sprintf("%s x%d", sc.RearmState, sc.ConsecutiveWaits))
	case RearmStateGaveUp:
		if sc.ConsecutiveFailures == 0 && sc.ConsecutiveWaits > 0 {
			// A one-time schedule that waited for capacity too long.
			parts = append(parts, fmt.Sprintf("%s (waited %d tries)", sc.RearmState, sc.ConsecutiveWaits))
		} else {
			parts = append(parts, fmt.Sprintf("%s x%d", sc.RearmState, sc.ConsecutiveFailures))
		}
	case RearmStateRetrying:
		parts = append(parts, fmt.Sprintf("%s x%d", sc.RearmState, sc.ConsecutiveFailures))
	}
	return strings.Join(parts, " · ")
}
