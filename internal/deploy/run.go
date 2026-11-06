package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// CmdResult is the outcome of one command line.
type CmdResult struct {
	Out  string
	Code int
}

// CommandRunner runs a shell command line in dir.
type CommandRunner interface {
	Run(ctx context.Context, dir, commandLine string) (CmdResult, error)
}

// DrainSource reports how many final-gate tasks are running.
type DrainSource interface {
	RunningFinalGate(ctx context.Context) (int, error)
}

// Clock supplies time and waiting.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// RestartSource reports what the daemon holds before a restart.
type RestartSource interface {
	RunningSessionIDs(ctx context.Context) ([]string, error)
	HeldLeaseCount(ctx context.Context) (int, error)
}

// Status is the kind of result Run returns.
type Status string

const (
	StatusSkipped    Status = "skipped"
	StatusDeferred   Status = "deferred"
	StatusFailed     Status = "failed"
	StatusRestarting Status = "restarting"
)

// Result is what Run and Retry return.
type Result struct {
	Status Status
	Reason string
	State  State
}

// PollInterval is how often the drain is polled.
const PollInterval = 5 * time.Second

// Runner runs the deploy sequence up to and including the restart command.
type Runner struct {
	Config  config.DeployConfig
	Dir     string // project directory commands run in
	DataDir string // daemon data directory; snapshots go only here
	Cmd     CommandRunner
	Drain   DrainSource
	Clock   Clock
	Store   StateStore
	Restart RestartSource
}

func (r *Runner) snapshotPath() string {
	return filepath.Join(r.DataDir, "deploy", "binary.snapshot")
}

func (r *Runner) save(s State) error {
	s.UpdatedAt = r.Clock.Now()
	return r.Store.Save(s)
}

func fail(st State, why string) Result {
	return Result{Status: StatusFailed, Reason: why, State: st}
}

// Run executes the sequence.
func (r *Runner) Run(ctx context.Context) (Result, error) {
	c := r.Config
	if c.BuildCommand == "" && c.InstallCommand == "" && c.RestartCommand == "" {
		return Result{Status: StatusSkipped, Reason: "no deploy commands are configured, so nothing was run"}, nil
	}
	st := State{RequestedAt: r.Clock.Now()}

	if c.InstallCommand != "" {
		if c.BinaryPath == "" {
			return fail(st, "install_command needs binary_path"), nil
		}
		snap, err := takeSnapshot(c.BinaryPath, r.snapshotPath(), r.DataDir, r.Clock.Now())
		if err != nil {
			return fail(st, "snapshot failed, nothing was changed: "+err.Error()), nil
		}
		st.SnapshotPath, st.SnapshotHash, st.SnapshotTime = snap.Path, snap.Hash, snap.Time
	}

	if c.BuildCommand != "" {
		res, err := r.Cmd.Run(ctx, r.Dir, c.BuildCommand)
		if err != nil || res.Code != 0 {
			why := "build failed, nothing was installed"
			if err != nil {
				why += ": " + redact(err.Error())
			} else {
				why += fmt.Sprintf(" (exit %d): %s", res.Code, redact(tail(res.Out, 500)))
			}
			return fail(st, why), nil
		}
	}

	if res, done, err := r.drain(ctx, &st); done || err != nil {
		return res, err
	}
	return r.proceed(ctx, st)
}

// drain polls until no final-gate task runs. done is true when the run must
// return res (deferred).
func (r *Runner) drain(ctx context.Context, st *State) (Result, bool, error) {
	timeout := r.Config.EffectiveDrainTimeout()
	deadline := r.Clock.Now().Add(timeout)
	for {
		n, err := r.Drain.RunningFinalGate(ctx)
		if err != nil {
			return Result{}, true, fmt.Errorf("drain check: %w", err)
		}
		if n == 0 {
			return Result{}, false, nil
		}
		if !r.Clock.Now().Before(deadline) {
			st.Phase = PhaseDeferred
			st.Reason = fmt.Sprintf("%d final-gate task(s) still running after %s", n, timeout)
			if err := r.save(*st); err != nil {
				return Result{}, true, err
			}
			return Result{Status: StatusDeferred, Reason: st.Reason, State: *st}, true, nil
		}
		if err := r.Clock.Sleep(ctx, PollInterval); err != nil {
			return Result{}, true, err
		}
	}
}

// Retry resumes a deferred deploy once the drain has finished. It does
// nothing when no deferred state exists.
func (r *Runner) Retry(ctx context.Context) (Result, error) {
	s, ok, err := r.Store.Load()
	if err != nil {
		return Result{}, err
	}
	if !ok || s.Phase != PhaseDeferred {
		return Result{Status: StatusSkipped, Reason: "no deferred deploy to retry"}, nil
	}
	return r.Run(ctx)
}

func (r *Runner) proceed(ctx context.Context, st State) (Result, error) {
	c := r.Config
	ids, err := r.Restart.RunningSessionIDs(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("pre-restart sessions: %w", err)
	}
	leases, err := r.Restart.HeldLeaseCount(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("pre-restart leases: %w", err)
	}
	st.PreRestart = PreRestart{SessionIDs: ids, LeaseCount: leases}
	st.Phase, st.Reason = PhaseRestarting, ""
	if err := r.save(st); err != nil {
		return fail(st, "state could not be saved, so install did not run: "+err.Error()), nil
	}

	if c.InstallCommand != "" {
		res, err := r.Cmd.Run(ctx, r.Dir, c.InstallCommand)
		if err != nil || res.Code != 0 {
			why := "install failed"
			if err != nil {
				why += ": " + redact(err.Error())
			} else {
				why += fmt.Sprintf(" (exit %d): %s", res.Code, redact(tail(res.Out, 500)))
			}
			st.Phase, st.Reason = PhaseInstallFailed, why
			if serr := r.save(st); serr != nil {
				why += "; state save failed: " + serr.Error()
			}
			return fail(st, why), nil
		}
	}
	if c.RestartCommand != "" {
		res, err := r.Cmd.Run(ctx, r.Dir, c.RestartCommand)
		if err != nil || res.Code != 0 {
			why := "restart command failed"
			if err != nil {
				why += ": " + redact(err.Error())
			} else {
				why += fmt.Sprintf(" (exit %d): %s", res.Code, redact(tail(res.Out, 500)))
			}
			st.Phase, st.Reason = PhaseRestartFailed, why
			_ = r.save(st)
			return fail(st, why), nil
		}
	}
	return Result{Status: StatusRestarting, State: st}, nil
}

// ---- production implementations ----

// ExecCommandRunner runs `sh -c` with TMUX and TMUX_TMPDIR removed.
type ExecCommandRunner struct{}

// Run implements CommandRunner.
func (ExecCommandRunner) Run(ctx context.Context, dir, line string) (CmdResult, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", line) // #nosec G204 -- operator's configured command
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := CmdResult{Out: out.String()}
	if err == nil {
		return res, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ctx.Err() == nil {
		res.Code = ee.ExitCode()
		return res, nil
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	return res, err
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep implements Clock.
func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
