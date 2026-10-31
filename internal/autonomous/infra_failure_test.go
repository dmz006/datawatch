package autonomous

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestInfraFailure_ClassifyError(t *testing.T) {
	spawnCause := errors.New("fork/exec failed")
	cases := []struct {
		name  string
		err   error
		class string
	}{
		{"spawn typed", &InfraSpawnError{Cause: spawnCause}, config.InfraRetryClassSpawnError},
		{"spawn typed nil cause", &InfraSpawnError{}, config.InfraRetryClassSpawnError},
		{"spawn wrapped", fmt.Errorf("task t1: %w", &InfraSpawnError{Cause: spawnCause}), config.InfraRetryClassSpawnError},
		{"storage ENOSPC", syscall.ENOSPC, config.InfraRetryClassStorageWrite},
		{"storage EDQUOT", syscall.EDQUOT, config.InfraRetryClassStorageWrite},
		{"storage EROFS", syscall.EROFS, config.InfraRetryClassStorageWrite},
		{"storage wrapped", fmt.Errorf("write config: %w", syscall.ENOSPC), config.InfraRetryClassStorageWrite},
		{"storage path error", &fs.PathError{Op: "write", Path: "tmp/config.json", Err: syscall.ENOSPC}, config.InfraRetryClassStorageWrite},
		{"storage path error wrapped", fmt.Errorf("save: %w", &fs.PathError{Op: "open", Path: "x", Err: syscall.EROFS}), config.InfraRetryClassStorageWrite},
		{"storage text full", errors.New("write failed: no space left on device"), config.InfraRetryClassStorageWrite},
		{"storage text quota", errors.New("Disk quota exceeded"), config.InfraRetryClassStorageWrite},
		{"storage text readonly", errors.New("cannot write config: read-only file system"), config.InfraRetryClassStorageWrite},
		{"session killed", LaunchFailureError("s1", "claude-code", "killed"), config.InfraRetryClassSessionKilled},
		{"session killed wrapped", fmt.Errorf("verify: %w", LaunchFailureError("s1", "claude-code", "killed")), config.InfraRetryClassSessionKilled},
		{"session killed sentinel", ErrSessionKilledBeforeDelivery, config.InfraRetryClassSessionKilled},
		{"daemon restart sentinel", ErrDaemonRestart, config.InfraRetryClassDaemonRestart},
		{"daemon restart wrapped", fmt.Errorf("attempt: %w", ErrDaemonRestart), config.InfraRetryClassDaemonRestart},
		{"daemon restart joined with context error", errors.Join(context.Canceled, ErrDaemonRestart), config.InfraRetryClassDaemonRestart},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyInfraFailure(tc.err)
			if !ok || got != tc.class {
				t.Fatalf("ClassifyInfraFailure(%v) = (%q, %v), want (%q, true)", tc.err, got, ok, tc.class)
			}
		})
	}
}

func TestInfraFailure_ClassifyErrorNegative(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"verifier failure", errors.New("verification failed: tests do not pass")},
		{"content failure", errors.New("the generated function does not handle empty input")},
		{"scope failure", errors.New("task writes outside the allowed directories")},
		{"plain stall", fmt.Errorf("no output for 10 minutes: %w", ErrWorkerStalled)},
		{"never started", ErrSessionNeverStarted},
		{"never started and stalled", errors.Join(ErrSessionNeverStarted, ErrWorkerStalled)},
		{"never started wrapped", fmt.Errorf("%s: %w; %w", SessionStartFailureReason, ErrSessionNeverStarted, ErrWorkerStalled)},
		{"never started with storage errno", errors.Join(ErrSessionNeverStarted, syscall.ENOSPC)},
		{"plain context cancel", context.Canceled},
		{"deadline exceeded", context.DeadlineExceeded},
		{"rate limit", errors.New("rate limit reached, try again later")},
		{"usage limit", errors.New("usage limit reached for this plan")},
		{"other errno", syscall.EACCES},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := ClassifyInfraFailure(tc.err); ok {
				t.Fatalf("ClassifyInfraFailure(%v) = (%q, true), want ok=false", tc.err, got)
			}
		})
	}
}

func TestInfraFailure_ClassifyText(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		class string
		ok    bool
	}{
		{"spawn", (&InfraSpawnError{Cause: errors.New("no free slot")}).Error(), config.InfraRetryClassSpawnError, true},
		{"launch failure", LaunchFailureError("s1", "claude-code", "killed").Error(), config.InfraRetryClassSessionKilled, true},
		{"restart", fmt.Sprintf("task failed: %v", ErrDaemonRestart), config.InfraRetryClassDaemonRestart, true},
		{"device full", "Error: ENOSPC: No space left on device, open '/tmp/x/config.json'", config.InfraRetryClassStorageWrite, true},
		{"device is full", "the device is full", config.InfraRetryClassStorageWrite, true},
		{"quota", "write: disk quota exceeded", config.InfraRetryClassStorageWrite, true},
		{"read-only", "EROFS: Read-only file system", config.InfraRetryClassStorageWrite, true},
		{"empty", "", "", false},
		{"verifier", "verification failed: missing test", "", false},
		{"stall", "worker stalled", "", false},
		{"never started", SessionStartFailureReason + ": no assistant output after 2m0s", "", false},
		{"rate limit", "429 rate limit exceeded", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyInfraText(tc.text)
			if ok != tc.ok || got != tc.class {
				t.Fatalf("ClassifyInfraText(%q) = (%q, %v), want (%q, %v)", tc.text, got, ok, tc.class, tc.ok)
			}
		})
	}
}

func TestInfraFailure_TextMatchesTypedErrors(t *testing.T) {
	for _, err := range []error{
		&InfraSpawnError{Cause: errors.New("boom")},
		LaunchFailureError("s", "b", "killed"),
		ErrDaemonRestart,
		&fs.PathError{Op: "write", Path: "f", Err: syscall.ENOSPC},
	} {
		want, ok := ClassifyInfraFailure(err)
		got, tok := ClassifyInfraText(err.Error())
		if !ok || !tok || got != want {
			t.Fatalf("%v: error class (%q,%v) != text class (%q,%v)", err, want, ok, got, tok)
		}
	}
}

func TestInfraFailure_InfraSpawnErrorUnwrap(t *testing.T) {
	cause := syscall.EAGAIN
	err := &InfraSpawnError{Cause: cause}
	if !errors.Is(err, cause) {
		t.Fatal("InfraSpawnError must unwrap to its cause")
	}
}

func TestInfraFailure_LaunchFailureWrapsBothSentinels(t *testing.T) {
	err := LaunchFailureError("s1", "claude-code", "killed")
	if !errors.Is(err, ErrSessionKilledBeforeDelivery) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("LaunchFailureError must wrap both sentinels: %v", err)
	}
	want := LaunchFailureReason + " (session=s1, backend=claude-code, state=killed): worker stalled"
	if err.Error() != want {
		t.Fatalf("message changed: %q, want %q", err.Error(), want)
	}
}

func TestInfraFailure_WithContextCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrDaemonRestart)
	err := WithContextCause(ctx, ctx.Err())
	if class, ok := ClassifyInfraFailure(err); !ok || class != config.InfraRetryClassDaemonRestart {
		t.Fatalf("restart cancel = (%q, %v), want daemon_restart", class, ok)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("the context error must stay visible")
	}

	// A plain cancel carries no cause beyond context.Canceled: not infra.
	plain, cancelPlain := context.WithCancel(context.Background())
	cancelPlain()
	if class, ok := ClassifyInfraFailure(WithContextCause(plain, plain.Err())); ok {
		t.Fatalf("plain cancel classified as %q", class)
	}

	// A non-context error is returned unchanged.
	other := errors.New("verification failed")
	if got := WithContextCause(ctx, other); got != other {
		t.Fatalf("non-context error changed: %v", got)
	}
	if WithContextCause(ctx, nil) != nil {
		t.Fatal("nil error must stay nil")
	}
}
