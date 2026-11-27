package session

// Copies returns a copy of every schedule, whatever its state, taken under the
// store's lock, for a reader that must not see one change under it.
func (s *ScheduleStore) Copies() []ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduledCommand, 0, len(s.entries))
	for _, sc := range s.entries {
		c := *sc
		if sc.FiringSince != nil {
			at := *sc.FiringSince
			c.FiringSince = &at
		}
		c.FireHistory = append([]ScheduleFire(nil), sc.FireHistory...)
		out = append(out, c)
	}
	return out
}
