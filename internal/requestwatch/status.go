package requestwatch

import "github.com/dmz006/datawatch/internal/config"

// Reader is the read side of the watch: the current observation of every
// watched session. An implementation must never make a model call.
type Reader interface {
	// Observations returns a copy of every current observation ordered by
	// session id. It is never nil.
	Observations() []Observation
	// Observation returns the current observation of one session.
	Observation(sessionID string) (Observation, bool)
}

// Filter narrows an observation list. An empty field matches everything.
type Filter struct {
	// Session keeps only the observation of this session id.
	Session string
	// Automaton keeps only the observations of this Automaton id.
	Automaton string
}

// Status is what GET /api/autonomous/request-watch returns.
type Status struct {
	Enabled           bool          `json:"enabled"`
	PollSeconds       int           `json:"poll_seconds"`
	NoProgressMinutes int           `json:"no_progress_minutes"`
	Observations      []Observation `json:"observations"`
}

// BuildStatus assembles the status from the configuration, the current
// observations and a filter. The list is never nil: an id that matches
// nothing, or a disabled feature, gives an empty list rather than an error.
// The input slice is not modified.
func BuildStatus(cfg config.RequestWatchConfig, obs []Observation, filter Filter) Status {
	st := Status{
		Enabled:           cfg.EffectiveEnabled(),
		PollSeconds:       cfg.EffectivePollSeconds(),
		NoProgressMinutes: cfg.EffectiveNoProgressMinutes(),
		Observations:      []Observation{},
	}
	if !st.Enabled {
		return st
	}
	for _, o := range obs {
		if filter.Session != "" && o.SessionID != filter.Session {
			continue
		}
		if filter.Automaton != "" && o.AutomatonID != filter.Automaton {
			continue
		}
		st.Observations = append(st.Observations, o)
	}
	return st
}
