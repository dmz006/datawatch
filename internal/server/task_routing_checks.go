package server

import (
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
