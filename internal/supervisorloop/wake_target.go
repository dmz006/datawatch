package supervisorloop

// Why the wake has, or has no, session to type into. The code is stable (the
// web app translates it); the reason is the plain sentence.
const (
	WakeTargetOK             = "ok"
	WakeTargetNoneDesignated = "none_designated"
	WakeTargetNotFound       = "not_found"
	WakeTargetAmbiguous      = "ambiguous"
	WakeTargetNotLive        = "not_live"
	WakeTargetCompleted      = "completed"
	WakeTargetUnreadable     = "output_unreadable"
)

// WakeTarget is the session a wake types into right now, and why.
type WakeTarget struct {
	// Session is the designation as configured (KeyWakeSession).
	Session string `json:"session"`
	// Target is the full id of the session a wake would type into, or "" when
	// nothing is typed.
	Target string `json:"target"`
	Code   string `json:"target_code"`
	Reason string `json:"target_reason"`
}
