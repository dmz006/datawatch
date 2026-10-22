// Surfaces model-to-node collision warnings on the config and capacity APIs.

package server

import "github.com/dmz006/datawatch/internal/inference"

// modelNodeWarnings returns the current model-to-node collisions for the
// live config, or nil when there are none or no config is loaded.
func (s *Server) modelNodeWarnings() []inference.ModelNodeConflict {
	if s.cfg == nil {
		return nil
	}
	return inference.DetectModelNodeConflicts(s.cfg.ModelNodeInputs())
}
