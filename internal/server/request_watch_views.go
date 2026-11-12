// Request-watch observation on the session and task views: the same
// observation GET /api/autonomous/request-watch lists, attached to the session
// JSON as model_request and to the task rows of an Automaton, so no surface
// has to join two calls. Read only: nothing here samples a node or changes a
// session or task.

package server

import (
	"encoding/json"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/requestwatch"
	"github.com/dmz006/datawatch/internal/session"
)

// requestWatchReader returns the wired request-watch reader, or nil when none
// is wired or the watch is switched off. The status route reads the switch on
// every request, so the views read it too: with autonomous.request_watch.enabled
// false they carry no observation at once, instead of the last one the poller
// held until its next tick clears it.
func (s *Server) requestWatchReader() requestwatch.Reader {
	if s.requestWatch == nil {
		return nil
	}
	if s.requestWatchCfg != nil && !s.requestWatchCfg().EffectiveEnabled() {
		return nil
	}
	return s.requestWatch
}

// sessionObservation returns the observation of sess, or nil when the watch is
// not wired or holds none for it. The observation is keyed by the full session
// id; the short id is tried as well.
func (s *Server) sessionObservation(sess *session.Session) *requestwatch.Observation {
	r := s.requestWatchReader()
	if r == nil || sess == nil {
		return nil
	}
	for _, id := range []string{sess.FullID, sess.ID} {
		if id == "" {
			continue
		}
		if o, ok := r.Observation(id); ok {
			return &o
		}
	}
	return nil
}

// automatonObservations indexes the observations of one Automaton by task id.
// A task with both a worker and a verifier observation keeps the one most
// recently polled, which is the request currently in flight. It returns nil
// when the watch is not wired or holds nothing for the Automaton.
func (s *Server) automatonObservations(automatonID string) map[string]*requestwatch.Observation {
	r := s.requestWatchReader()
	if r == nil || automatonID == "" {
		return nil
	}
	var out map[string]*requestwatch.Observation
	for _, o := range r.Observations() {
		if o.AutomatonID != automatonID || o.TaskID == "" {
			continue
		}
		if out == nil {
			out = map[string]*requestwatch.Observation{}
		}
		if prev, ok := out[o.TaskID]; ok && prev.LastPoll > o.LastPoll {
			continue
		}
		o := o
		out[o.TaskID] = &o
	}
	return out
}

// attachTaskObservations fills ModelRequest on every entry of view from the
// observation of its own Automaton and task. Entries without one are left as
// they are.
func (s *Server) attachTaskObservations(view *autonomous.TasksView) {
	obs := s.automatonObservations(view.ID)
	if len(obs) == 0 {
		return
	}
	for i := range view.Tasks {
		if o, ok := obs[view.Tasks[i].ID]; ok {
			view.Tasks[i].ModelRequest = o
		}
	}
}

// prdWithObservations returns the Automaton detail as a generic JSON value
// with model_request added to each task row that has an observation. The
// stored record is not touched. When there is nothing to add, or the record
// cannot be re-read as a generic value, it returns prd unchanged.
func (s *Server) prdWithObservations(prd any, automatonID string) any {
	obs := s.automatonObservations(automatonID)
	if len(obs) == 0 {
		return prd
	}
	b, err := json.Marshal(prd)
	if err != nil {
		return prd
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return prd
	}
	stories, _ := doc["stories"].([]any)
	for _, st := range stories {
		story, _ := st.(map[string]any)
		tasks, _ := story["tasks"].([]any)
		for _, t := range tasks {
			task, _ := t.(map[string]any)
			id, _ := task["id"].(string)
			if o, ok := obs[id]; ok {
				task["model_request"] = o
			}
		}
	}
	return doc
}
