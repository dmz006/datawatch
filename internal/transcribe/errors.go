package transcribe

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// Failure causes reported by ClassifyError.
const (
	// CauseTimeout means the context deadline expired or the request was cancelled.
	CauseTimeout = "timeout"
	// CauseKilled means the process ended by SIGKILL, usually the kernel OOM killer.
	CauseKilled = "killed"
	// CauseOutOfMemory means the child reported it ran out of memory (host RAM or GPU).
	CauseOutOfMemory = "out_of_memory"
	// CauseFFmpegMissing means ffmpeg is not installed or not on PATH.
	CauseFFmpegMissing = "ffmpeg_missing"
	// CauseBackendError is any other non-zero exit or HTTP backend failure.
	CauseBackendError = "backend_error"
	// CauseUnknown is an error that matches no known cause.
	CauseUnknown = "unknown"
)

// maxChildOutput caps the child output appended to a whisper failure error so a
// long Python traceback does not flood logs.
const maxChildOutput = 2000

// Failure is an operator-readable classification of a transcription error.
type Failure struct {
	// Cause is one of the Cause* constants, or "" when there was no error.
	Cause string
	// ExitCode is the child's exit status, or -1 when there was none
	// (no child process, or it ended by signal).
	ExitCode int
	// Signal is the name of the signal that ended the child ("killed"), or "".
	Signal string
	// Message is a short sentence naming the real cause.
	Message string
	// Hint is one actionable sentence for the operator.
	Hint string
}

// Error returns the failure message so a Failure can be used as an error.
func (f Failure) Error() string { return f.Message }

// ClassifyError works out why a transcription failed. A nil error returns a
// zero Failure. Checks run in order: timeout, killed, out of memory, ffmpeg
// missing, backend error; anything left is CauseUnknown.
func ClassifyError(err error) Failure {
	if err == nil {
		return Failure{}
	}
	f := Failure{ExitCode: -1}
	text := err.Error()
	lower := strings.ToLower(text)

	var exitErr *exec.ExitError
	haveExit := errors.As(err, &exitErr)
	if haveExit {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				f.Signal = ws.Signal().String()
			} else {
				f.ExitCode = ws.ExitStatus()
			}
		} else {
			f.ExitCode = exitErr.ExitCode()
		}
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		f.Cause = CauseTimeout
		f.Message = "transcription ran past its time limit and was stopped"
		f.Hint = "Use a shorter recording or a smaller whisper.model such as small or base."
	case errors.Is(err, context.Canceled):
		f.Cause = CauseTimeout
		f.Message = "transcription was cancelled before it finished"
		f.Hint = "Use a shorter recording or a smaller whisper.model such as small or base."
	case haveExit && f.Signal == syscall.SIGKILL.String():
		f.Cause = CauseKilled
		f.Message = "the whisper process was killed by SIGKILL, usually the kernel out-of-memory killer"
		f.Hint = "Use a smaller whisper.model such as small or base, or lower whisper.chunk_seconds."
	case containsAny(lower, "memoryerror", "cannot allocate memory", "out of memory"):
		f.Cause = CauseOutOfMemory
		f.Message = "whisper ran out of memory"
		f.Hint = "Use a smaller whisper.model such as small or base, or lower whisper.chunk_seconds."
	case isFFmpegMissing(err, lower):
		f.Cause = CauseFFmpegMissing
		f.Message = "ffmpeg was not found, and whisper needs it to read the audio"
		f.Hint = "Install ffmpeg on the daemon host and make sure it is on PATH."
	case haveExit || errors.Is(err, exec.ErrNotFound) || containsAny(lower, "http ", "transcribe(openai_compat)", "whisper failed"):
		f.Cause = CauseBackendError
		f.Message = backendMessage(f, err)
		f.Hint = "Check the daemon log for the backend output, then retry."
	default:
		f.Cause = CauseUnknown
		f.Message = "transcription failed for an unrecognised reason: " + firstLine(text)
		f.Hint = "Check the daemon log for the full error."
	}
	return f
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// isFFmpegMissing reports a missing ffmpeg: an exec lookup failure naming
// ffmpeg, whisper's own missing-file message, or the splitter's PATH message.
func isFFmpegMissing(err error, lower string) bool {
	if errors.Is(err, exec.ErrNotFound) && strings.Contains(lower, "ffmpeg") {
		return true
	}
	return containsAny(lower, "no such file or directory: 'ffmpeg'", "ffmpeg is not on path")
}

func backendMessage(f Failure, err error) string {
	switch {
	case f.Signal != "":
		return fmt.Sprintf("the transcription backend was stopped by signal %q", f.Signal)
	case f.ExitCode >= 0:
		return fmt.Sprintf("the transcription backend exited with status %d", f.ExitCode)
	case errors.Is(err, exec.ErrNotFound):
		return "the transcription backend executable was not found"
	default:
		return "the transcription backend returned an error: " + firstLine(err.Error())
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// tailOutput returns at most max trailing bytes of s, starting on a rune
// boundary and marked with a leading ellipsis when truncated.
func tailOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for len(s) > 0 && !isRuneStart(s[0]) {
		s = s[1:]
	}
	return "..." + s
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
