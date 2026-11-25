package server

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/pause"
)

// The daemon wires *autonomous.API into the server. Each optional interface a
// route type-asserts is held here at compile time, so a missing method fails
// the build instead of turning the route into a 503.
var (
	_ gateRepairsReader           = (*autonomous.API)(nil)
	_ gateRepairRunInvoker        = (*autonomous.API)(nil)
	_ mergeRunner                 = (*autonomous.API)(nil)
	_ eventPreviewLifecycleSource = (*autonomous.API)(nil)
	_ baseRepoReader              = (*autonomous.API)(nil)
	_ memoryCeilingProvider       = (*autonomous.API)(nil)
	_ branchRefreshStatser        = (*autonomous.API)(nil)
	_ branchRefreshOverrideSetter = (*autonomous.API)(nil)
)

// operatorPauseBlock reports whether the operator paused new work in dir.
func (s *Server) operatorPauseBlock(dir string) (pause.Block, bool) {
	if s.pauseStore == nil {
		return pause.Block{}, false
	}
	return s.pauseStore.Blocked(dir)
}

// deferredDeployRetryHeld reports whether the retry of a deferred deploy at
// daemon start waits for the operator: it starts a new deploy. Finishing a
// deploy the restart interrupted is work already running and is not held.
func (s *Server) deferredDeployRetryHeld(dir string) bool {
	_, blocked := s.operatorPauseBlock(dir)
	return blocked
}
