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

// Reasons a retry starts a fresh attempt or resumes, as the attempt record
// carries them (ResumeReason). resumeReasonResumed is the reason for a resume.
const (
	resumeReasonResumed = "resumed previous session"
	// sessionEndedResumeReason is the reason when the previous session is gone,
	// or the task has no session left to resume.
	sessionEndedResumeReason = "session ended; starting a fresh attempt"
	// resumeFallbackNoSendReason is the reason when a resume was decided but no
	// send-input hook is wired, so the attempt starts fresh.
	resumeFallbackNoSendReason = "no send-input hook; starting a fresh attempt"
	// resumeFallbackSendFailedReason is the reason when a resume was decided but
	// the failure summary could not be sent, so the attempt starts fresh.
	resumeFallbackSendFailedReason = "send into session failed; starting a fresh attempt"
)

// decideRetryResume reports whether to resume and a short operator-readable
// reason for the outcome. Checks run in this order: session alive, backend
// support, enabled-by-setting, then context limit.
func decideRetryResume(in retryResumeInput) (resume bool, reason string) {
	switch {
	case !in.SessionAlive:
		return false, sessionEndedResumeReason
	case !in.BackendSupportsResume:
		return false, "backend cannot resume; starting a fresh attempt"
	case in.MaxContextTokens <= 0:
		return false, "resume disabled by setting; starting a fresh attempt"
	case in.ContextTokens > in.MaxContextTokens:
		return false, "context above the limit; starting a fresh attempt"
	default:
		return true, resumeReasonResumed
	}
}
