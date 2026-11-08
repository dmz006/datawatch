package autonomous

// decideRetryResume decides whether a retried task attempt should resume the
// previous backend session instead of starting a fresh one. The decision is a
// pure function of the inputs so it can be tested without a running session.

// retryResumeInput carries the facts the retry path knows about the previous
// attempt. MaxContextTokens <= 0 means the operator has not enabled resume.
// ContextTokens <= 0 means the context size is unknown.
type retryResumeInput struct {
	SessionAlive          bool
	BackendSupportsResume bool
	ContextTokens         int
	MaxContextTokens      int
}

// decideRetryResume reports whether to resume and a short operator-readable
// reason for the outcome. Checks run in this order: session alive, backend
// support, enabled-by-setting, then context limit.
func decideRetryResume(in retryResumeInput) (resume bool, reason string) {
	switch {
	case !in.SessionAlive:
		return false, "session ended; starting a fresh attempt"
	case !in.BackendSupportsResume:
		return false, "backend cannot resume; starting a fresh attempt"
	case in.MaxContextTokens <= 0:
		return false, "resume disabled by setting; starting a fresh attempt"
	case in.ContextTokens > in.MaxContextTokens:
		return false, "context above the limit; starting a fresh attempt"
	default:
		return true, "resumed previous session"
	}
}
