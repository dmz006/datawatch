package router

// endCallerCommChannelKill names the comm channel kill command as the caller of
// a session end. The reason names the channel the command arrived on.
const endCallerCommChannelKill = "comm.kill"

// commChannelName returns the name of the messaging backend the router sends
// on, for an end reason. It is "unknown" when no backend is wired.
func (r *Router) commChannelName() string {
	if r.backend == nil {
		return "unknown"
	}
	return r.backend.Name()
}
