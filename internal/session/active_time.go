package session

import "time"

// activeClock is the clock used for active-time accounting; tests replace it.
var activeClock = time.Now

// trackIdle folds waiting_input time into IdleSeconds. It is called on every
// store save, so state transitions made anywhere are observed.
func (s *Session) trackIdle(now time.Time) {
	switch {
	case s.State == StateWaitingInput && s.IdleSince == nil:
		t := now
		s.IdleSince = &t
	case s.State != StateWaitingInput && s.IdleSince != nil:
		if d := now.Sub(*s.IdleSince).Seconds(); d > 0 {
			s.IdleSeconds += d
		}
		s.IdleSince = nil
	}
}

// IdleTotal returns idle seconds including an open waiting_input stretch,
// ending it at end.
func (s Session) IdleTotal(end time.Time) float64 {
	idle := s.IdleSeconds
	if s.IdleSince != nil {
		if d := end.Sub(*s.IdleSince).Seconds(); d > 0 {
			idle += d
		}
	}
	return idle
}

// ActiveSeconds is session start to end (UpdatedAt once terminal, else now)
// minus idle time, never negative.
func (s Session) ActiveSeconds(now time.Time) float64 {
	end := now
	if s.State.IsTerminal() && !s.UpdatedAt.IsZero() {
		end = s.UpdatedAt
	}
	if s.CreatedAt.IsZero() {
		return 0
	}
	a := end.Sub(s.CreatedAt).Seconds() - s.IdleTotal(end)
	if a < 0 {
		return 0
	}
	return a
}
