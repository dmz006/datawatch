package server

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// taskRoutingChecks builds the task routing validation lookups from the
// live LLM registry (the lookup the llm_list and llm_get surfaces use).
// With no registry wired the backend check is skipped.
func (s *Server) taskRoutingChecks() config.TaskRoutingChecks {
	if s.inferenceReg == nil {
		return autonomous.TaskRoutingChecksFor(nil)
	}
	reg := s.inferenceReg
	return autonomous.TaskRoutingChecksFor(func(backend string) bool {
		_, err := reg.Get(backend)
		return err == nil
	})
}

// allTaskRoutingKeys reports whether every skipped key is a task routing key,
// so the PUT /api/config reply explains the skip in routing terms rather than
// with the token-rotation note.
func allTaskRoutingKeys(keys []string) bool {
	for _, k := range keys {
		if k != "autonomous.task_routing" && !strings.HasPrefix(k, "autonomous.task_routing.") {
			return false
		}
	}
	return len(keys) > 0
}

// ConfigHandler serves /api/config (GET and PUT) on its own, so a surface's
// tests can drive the real config handler without starting a daemon.
func (s *Server) ConfigHandler() http.Handler { return http.HandlerFunc(s.handleConfig) }
