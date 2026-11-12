// restart_daemon guard: the MCP restart refuses, with the Automata interruption
// report in the tool result, while running work would be interrupted, unless
// force=true is passed. See docs/howto/restart-with-work-running.md.

package mcp

import (
	"encoding/json"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// restartDaemonReport reads GET /api/autonomous/restart-interruptions through
// the loopback. ok is false when the report cannot be read.
func (s *Server) restartDaemonReport() (autonomous.RestartInterruption, bool) {
	body, err := s.proxyGet("/api/autonomous/restart-interruptions", nil)
	if err != nil {
		return autonomous.RestartInterruption{}, false
	}
	rep, err := autonomous.ParseRestartInterruption(body)
	if err != nil {
		return autonomous.RestartInterruption{}, false
	}
	return rep, true
}

// restartDaemonRefusal is the tool-result text for a refused restart: what it
// would interrupt in words, then the same report as JSON, then how to force.
func restartDaemonRefusal(rep autonomous.RestartInterruption) string {
	raw, _ := json.Marshal(rep)
	return "restart refused: running work would be interrupted. Pass force=true to restart anyway.\n" +
		autonomous.RestartInterruptionText(rep) + string(raw)
}
