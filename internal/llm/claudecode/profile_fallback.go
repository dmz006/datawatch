// Worker-profile apply fallback for the claude-code launch.
//
// A launch that carries a worker profile materializes it into the session
// directory (worker-mcp.json, worker-starting-context.md). When that cannot be
// done — the write fails, or the profile names an MCP server that is not
// configured — the launch does not fail the task. It starts again with no
// profile, so its command equals a launch with nothing configured, and the
// error text is returned to the caller as the fallback reason.

package claudecode

import (
	"errors"
	"fmt"
)

// ProfileApplyError is returned by profileFlags when a launch's worker profile
// cannot be applied. The launch falls back to no profile on this error and
// no other.
type ProfileApplyError struct {
	// Profile is the name of the profile that could not be applied.
	Profile string
	// Err is the underlying cause.
	Err error
}

func newProfileApplyError(profile string, err error) error {
	return &ProfileApplyError{Profile: profile, Err: err}
}

// Error names the profile and the cause.
func (e *ProfileApplyError) Error() string {
	return fmt.Sprintf("apply worker profile %q: %v", e.Profile, e.Err)
}

// Unwrap returns the underlying cause.
func (e *ProfileApplyError) Unwrap() error { return e.Err }

// launchFlags returns the flags a launch with w adds after --add-dir: the
// settings flag plus the profile flags. When w's profile cannot be applied it
// retries with no profile, so the flags equal a launch built with a zero
// WorkerProfileLaunch, and returns the apply error text as fallbackReason.
// Any other error is returned unchanged.
func (b *Backend) launchFlags(projectDir string, w WorkerProfileLaunch) (flags, fallbackReason string, err error) {
	profileFlags, err := b.profileFlags(projectDir, w)
	if err != nil {
		var applyErr *ProfileApplyError
		if !errors.As(err, &applyErr) {
			return "", "", err
		}
		fallbackReason = err.Error()
		w = WorkerProfileLaunch{}
		profileFlags = ""
	}
	return b.settingsFlag(projectDir, w) + profileFlags, fallbackReason, nil
}
