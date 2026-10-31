// Infrastructure failure classification.
//
// ClassifyInfraFailure and ClassifyInfraText decide whether a task attempt
// ended because the machinery around the task failed (a worker that could not
// be spawned, a storage write that failed, a session killed before the task
// was delivered, a daemon restart) and which of the four classes it was. They
// look only at the error's type, its wrapped sentinels and its errno, or, for
// an already-recorded failure text, at fixed phrases the system itself or the
// operating system produces. They never judge work content: a verifier
// failure, a content or scope failure, a stall that is not a launch failure,
// a session that never started (which has its own start-failure budget) and a
// rate or usage limit are all left unclassified.
package autonomous

import (
	"context"
	"errors"
	"strings"
	"syscall"

	"github.com/dmz006/datawatch/internal/config"
)

// InfraSpawnFailureReason prefixes the message of an InfraSpawnError.
const InfraSpawnFailureReason = "worker spawn failed"

// daemonRestartText is the message of ErrDaemonRestart.
const daemonRestartText = "daemon restarted while the task was running"

// ErrDaemonRestart is the cause recorded when the daemon is shutting down or
// restarting and the running attempt is cancelled with it. Cancel a context
// with it (context.WithCancelCause) and pass the error through
// WithContextCause so the classifier can see the cause.
var ErrDaemonRestart = errors.New(daemonRestartText)

// InfraSpawnError marks an error from the worker spawn call: the worker was
// never started, so the task did not get a fair attempt.
type InfraSpawnError struct {
	Cause error
}

func (e *InfraSpawnError) Error() string {
	if e.Cause == nil {
		return InfraSpawnFailureReason
	}
	return InfraSpawnFailureReason + ": " + e.Cause.Error()
}

func (e *InfraSpawnError) Unwrap() error { return e.Cause }

// WithContextCause returns err joined with context.Cause(ctx) when err is a
// context error and the context carries a different cause, so that a
// cancellation made with a cause (ErrDaemonRestart) stays visible to errors.Is
// after the context error has been passed along. Any other err is returned
// unchanged.
func WithContextCause(ctx context.Context, err error) error {
	if err == nil || ctx == nil {
		return err
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	cause := context.Cause(ctx)
	if cause == nil || errors.Is(err, cause) {
		return err
	}
	return errors.Join(err, cause)
}

// ClassifyInfraFailure reports whether err is an infrastructure failure and
// which class (config.InfraRetryClass*). It returns ok=false for nil and for
// anything that is not one of the four classes.
func ClassifyInfraFailure(err error) (class string, ok bool) {
	if err == nil {
		return "", false
	}
	// A session that never started has its own budget and is never classified
	// here, whatever else the error also says.
	if errors.Is(err, ErrSessionNeverStarted) {
		return "", false
	}
	var spawnErr *InfraSpawnError
	switch {
	case errors.As(err, &spawnErr):
		return config.InfraRetryClassSpawnError, true
	case errors.Is(err, ErrSessionKilledBeforeDelivery):
		return config.InfraRetryClassSessionKilled, true
	case errors.Is(err, ErrDaemonRestart):
		return config.InfraRetryClassDaemonRestart, true
	case isStorageErrno(err):
		return config.InfraRetryClassStorageWrite, true
	}
	return ClassifyInfraText(err.Error())
}

// isStorageErrno reports whether the chain carries a full-disk, quota or
// read-only file system errno (also inside an *fs.PathError).
func isStorageErrno(err error) bool {
	return errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, syscall.EROFS)
}

// storageWriteTexts are the phrases, lower case, that say a write failed
// because the device is full, the quota is used up or the file system is
// read-only. A worker session reports these as text.
var storageWriteTexts = []string{
	"no space left on device",
	"device is full",
	"disk quota exceeded",
	"read-only file system",
}

// ClassifyInfraText applies the text rules to an already-recorded failure
// text: the messages of InfraSpawnError, LaunchFailureError and
// ErrDaemonRestart, and the storage phrases above. It returns ok=false for
// anything else, including the text of a session that never started.
func ClassifyInfraText(text string) (class string, ok bool) {
	lower := strings.ToLower(text)
	if strings.Contains(lower, strings.ToLower(SessionStartFailureReason)) {
		return "", false
	}
	switch {
	case strings.Contains(lower, InfraSpawnFailureReason):
		return config.InfraRetryClassSpawnError, true
	case strings.Contains(lower, LaunchFailureReason):
		return config.InfraRetryClassSessionKilled, true
	case strings.Contains(lower, daemonRestartText):
		return config.InfraRetryClassDaemonRestart, true
	}
	for _, p := range storageWriteTexts {
		if strings.Contains(lower, p) {
			return config.InfraRetryClassStorageWrite, true
		}
	}
	return "", false
}
