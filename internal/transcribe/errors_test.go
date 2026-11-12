package transcribe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// scriptTranscriber writes an executable shell script to a temp dir and returns
// a WhisperTranscriber that runs it in place of python. The transcriber invokes
// it as `<script> -c <code> audio model lang out`; the scripts ignore that.
func scriptTranscriber(t *testing.T, body string) *WhisperTranscriber {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not available on windows")
	}
	path := filepath.Join(t.TempDir(), "fake-python")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil { // #nosec G306 -- test script must be executable
		t.Fatalf("write script: %v", err)
	}
	return &WhisperTranscriber{Python: path, Model: "tiny", Language: "en"}
}

func TestClassifyError_KilledBySigkill(t *testing.T) {
	w := scriptTranscriber(t, "kill -9 $$")
	_, err := w.Transcribe(context.Background(), "audio.wav")
	if err == nil {
		t.Fatal("expected an error")
	}
	f := ClassifyError(err)
	if f.Cause != CauseKilled {
		t.Fatalf("cause = %q, want %q (err: %v)", f.Cause, CauseKilled, err)
	}
	if f.Signal != "killed" {
		t.Errorf("signal = %q, want killed", f.Signal)
	}
	if f.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1", f.ExitCode)
	}
	if f.Message == "" || f.Hint == "" {
		t.Errorf("message and hint must be set: %+v", f)
	}
	if !strings.Contains(f.Hint, "whisper.chunk_seconds") {
		t.Errorf("hint should mention whisper.chunk_seconds: %q", f.Hint)
	}
}

func TestClassifyError_BackendErrorExitCode(t *testing.T) {
	w := scriptTranscriber(t, "exit 3")
	_, err := w.Transcribe(context.Background(), "audio.wav")
	f := ClassifyError(err)
	if f.Cause != CauseBackendError {
		t.Fatalf("cause = %q, want %q (err: %v)", f.Cause, CauseBackendError, err)
	}
	if f.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", f.ExitCode)
	}
	if f.Signal != "" {
		t.Errorf("signal = %q, want empty", f.Signal)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("Transcribe error must keep wrapping *exec.ExitError: %v", err)
	}
}

func TestClassifyError_OutOfMemoryOutput(t *testing.T) {
	w := scriptTranscriber(t, "echo MemoryError >&2\nexit 1")
	_, err := w.Transcribe(context.Background(), "audio.wav")
	f := ClassifyError(err)
	if f.Cause != CauseOutOfMemory {
		t.Fatalf("cause = %q, want %q (err: %v)", f.Cause, CauseOutOfMemory, err)
	}
	if f.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", f.ExitCode)
	}
}

func TestClassifyError_FFmpegMissingOutput(t *testing.T) {
	w := scriptTranscriber(t, "echo \"FileNotFoundError: No such file or directory: 'ffmpeg'\" >&2\nexit 1")
	_, err := w.Transcribe(context.Background(), "audio.wav")
	f := ClassifyError(err)
	if f.Cause != CauseFFmpegMissing {
		t.Fatalf("cause = %q, want %q (err: %v)", f.Cause, CauseFFmpegMissing, err)
	}
	if !strings.Contains(f.Hint, "ffmpeg") {
		t.Errorf("hint should mention ffmpeg: %q", f.Hint)
	}
}

func TestClassifyError_TimeoutDeadline(t *testing.T) {
	// exec makes the shell the sleeper so killing it frees the output pipe.
	w := scriptTranscriber(t, "exec sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := w.Transcribe(ctx, "audio.wav")
	if err == nil {
		t.Fatal("expected an error")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("deadline was not enforced, took %v", time.Since(start))
	}
	f := ClassifyError(err)
	if f.Cause != CauseTimeout {
		t.Fatalf("cause = %q, want %q (err: %v)", f.Cause, CauseTimeout, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error should wrap context.DeadlineExceeded: %v", err)
	}
}

func TestClassifyError_TruncatesChildOutput(t *testing.T) {
	w := scriptTranscriber(t, "i=0\nwhile [ $i -lt 400 ]; do echo \"traceback line number $i padding padding\"; i=$((i+1)); done\nexit 1")
	_, err := w.Transcribe(context.Background(), "audio.wav")
	if err == nil {
		t.Fatal("expected an error")
	}
	_, out, ok := strings.Cut(err.Error(), "\noutput: ")
	if !ok {
		t.Fatalf("error has no output section: %v", err)
	}
	if len(out) > maxChildOutput+len("...")+1 {
		t.Errorf("child output is %d bytes, want at most %d", len(out), maxChildOutput+len("...")+1)
	}
	if !strings.Contains(out, "line number 399") {
		t.Errorf("the tail of the output must be kept: %q", out[len(out)-60:])
	}
}

func TestClassifyError_Table(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		cause string
	}{
		{"nil", nil, ""},
		{"wrapped deadline", fmt.Errorf("transcribe: whisper failed: %w", context.DeadlineExceeded), CauseTimeout},
		{"wrapped cancel", fmt.Errorf("transcribe: %w", context.Canceled), CauseTimeout},
		{"exec not found ffmpeg", &exec.Error{Name: "ffmpeg", Err: exec.ErrNotFound}, CauseFFmpegMissing},
		{"exec not found python", &exec.Error{Name: "python3", Err: exec.ErrNotFound}, CauseBackendError},
		{"splitter ffmpeg message", errors.New(`transcribe(split): "a.webm" is not a PCM WAV and ffmpeg is not on PATH — cannot split`), CauseFFmpegMissing},
		{"cuda out of memory", errors.New("CUDA out of memory. Tried to allocate 2.00 GiB"), CauseOutOfMemory},
		{"cannot allocate", errors.New("OSError: [Errno 12] Cannot allocate memory"), CauseOutOfMemory},
		{"http failure", errors.New("transcribe(openai_compat): HTTP 500: boom"), CauseBackendError},
		{"timeout beats out of memory text", fmt.Errorf("%w: out of memory", context.DeadlineExceeded), CauseTimeout},
		{"plain error", errors.New("something odd"), CauseUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ClassifyError(tc.err)
			if f.Cause != tc.cause {
				t.Fatalf("cause = %q, want %q", f.Cause, tc.cause)
			}
			if tc.err == nil {
				if f != (Failure{}) {
					t.Errorf("nil error must give a zero Failure, got %+v", f)
				}
				return
			}
			if f.Message == "" || f.Hint == "" {
				t.Errorf("message and hint must be set: %+v", f)
			}
			if f.Error() != f.Message {
				t.Errorf("Error() = %q, want message %q", f.Error(), f.Message)
			}
		})
	}
}
