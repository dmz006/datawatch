package server

// Callers name the operator surface that ended a session. The caller is the
// "by=" field of the journal line and of the session timeline; the reason says
// who asked for the end, using the identity the surface already audits with.
const (
	endCallerRESTSessionKill   = "rest.session-kill"
	endCallerCommandKill       = "command.kill"
	endCallerAutonomousPRDStop = "rest.autonomous-prd"
	endCallerLLMForceDelete    = "rest.llm-force-delete"
)
