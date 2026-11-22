package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// Outcome values recorded for a finished deploy.
const (
	OutcomeOK             = "ok"
	OutcomeRolledBack     = "rolled_back"
	OutcomeRollbackFailed = "rollback_failed"
)

// OutcomeRecord is one deploy outcome. The autonomous triage record is tied
// to one Automaton (PRDID, stored on the PRD), which a daemon deploy is not,
// so a deploy outcome gets this small record and its own store file.
type OutcomeRecord struct {
	At             time.Time `json:"at"`
	Outcome        string    `json:"outcome"`
	FailedCheck    string    `json:"failed_check,omitempty"`
	Restored       bool      `json:"restored"`
	FinalHealth    string    `json:"final_health,omitempty"`
	SnapshotHash   string    `json:"snapshot_hash,omitempty"`
	Summary        string    `json:"summary"`
	DeployStarted  time.Time `json:"deploy_started"`
	MissingSession []string  `json:"missing_sessions,omitempty"`
}

// OutcomeStore records deploy outcomes.
type OutcomeStore interface {
	Append(OutcomeRecord) error
}

// OutcomeFile is an OutcomeStore that appends JSON lines to one file in the
// daemon data directory.
type OutcomeFile struct{ Path string }

// Append implements OutcomeStore.
func (f OutcomeFile) Append(r OutcomeRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	fh, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- daemon data dir
	if err != nil {
		return err
	}
	if _, err := fh.Write(append(b, '\n')); err != nil {
		_ = fh.Close()
		return err
	}
	return fh.Close()
}

// QuestionRaiser raises an operator question; *autonomous.QuestionStore is one.
type QuestionRaiser interface {
	RaiseUnlessOpenSourceLine(q autonomous.Question) (autonomous.Question, bool, error)
}

// DeployQuestionAsker names the asker of a deploy question.
const DeployQuestionAsker = "deploy"

// Resumer finishes a deploy the restart interrupted.
type Resumer struct {
	Config   config.DeployConfig
	Dir      string
	DataDir  string
	Cmd      CommandRunner
	Clock    Clock
	Store    StateStore
	Sessions RestartSource
	// Ready blocks until the daemon is ready to be checked; nil means ready.
	Ready     func(ctx context.Context) error
	Questions QuestionRaiser // nil: no question is raised
	Outcomes  OutcomeStore   // nil: no record is written
}

// HealthRetryInterval is the wait between health attempts.
const HealthRetryInterval = 2 * time.Second

// ResumeResult says what Resume did.
type ResumeResult struct {
	Phase  Phase
	Acted  bool
	Reason string
}

// Resume loads the state file and, when the phase is restarting, checks the
// daemon and rolls back on a failure. A state in any other phase does nothing,
// so a second call is a no-op.
func (r *Resumer) Resume(ctx context.Context) (ResumeResult, error) {
	st, ok, err := r.Store.Load()
	if err != nil {
		return ResumeResult{}, err
	}
	if !ok || st.Phase != PhaseRestarting {
		return ResumeResult{Phase: st.Phase, Reason: "no interrupted deploy to resume"}, nil
	}
	if r.Ready != nil {
		if err := r.Ready(ctx); err != nil {
			return ResumeResult{}, fmt.Errorf("wait for ready: %w", err)
		}
	}

	failed, missing, health := r.check(ctx, st)
	if failed == "" {
		st.Phase, st.Reason = PhaseDone, ""
		if err := r.save(st); err != nil {
			return ResumeResult{}, err
		}
		r.record(OutcomeRecord{Outcome: OutcomeOK, Restored: false, FinalHealth: health,
			SnapshotHash: st.SnapshotHash, DeployStarted: st.RequestedAt,
			Summary: "deploy finished: the new binary is healthy and every session and lease came back"})
		return ResumeResult{Phase: PhaseDone, Acted: true}, nil
	}
	return r.rollback(ctx, st, failed, missing)
}

// check returns "" when healthy, else the failed check. It also returns the
// sessions that did not come back and the health text.
func (r *Resumer) check(ctx context.Context, st State) (failed string, missing []string, health string) {
	if msg := r.waitHealthy(ctx); msg != "" {
		return "health check: " + msg, nil, msg
	}
	health = "healthy"
	ids, err := r.Sessions.RunningSessionIDs(ctx)
	if err != nil {
		return "session list: " + err.Error(), nil, health
	}
	have := map[string]bool{}
	for _, id := range ids {
		have[id] = true
	}
	for _, id := range st.PreRestart.SessionIDs {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Sprintf("%d session(s) running before the restart are missing: %s",
			len(missing), strings.Join(missing, ", ")), missing, health
	}
	n, err := r.Sessions.HeldLeaseCount(ctx)
	if err != nil {
		return "lease count: " + err.Error(), nil, health
	}
	if n < st.PreRestart.LeaseCount {
		return fmt.Sprintf("held capacity leases are %d, below the %d held before the restart",
			n, st.PreRestart.LeaseCount), nil, health
	}
	return "", nil, health
}

// waitHealthy runs the health check until it passes or health_timeout ends.
// It returns "" when healthy (a missing health_check counts as passing).
func (r *Resumer) waitHealthy(ctx context.Context) string {
	if r.Config.HealthCheck == "" {
		return ""
	}
	deadline := r.Clock.Now().Add(r.Config.EffectiveHealthTimeout())
	for {
		msg := r.healthOnce(ctx)
		if msg == "" {
			return ""
		}
		if !r.Clock.Now().Before(deadline) {
			return "still failing after " + r.Config.EffectiveHealthTimeout().String() + ": " + msg
		}
		if err := r.Clock.Sleep(ctx, HealthRetryInterval); err != nil {
			return "interrupted: " + err.Error()
		}
	}
}

func (r *Resumer) healthOnce(ctx context.Context) string {
	if r.Config.HealthCheck == "" {
		return ""
	}
	res, err := r.Cmd.Run(ctx, r.Dir, r.Config.HealthCheck)
	if err != nil {
		return redact(err.Error())
	}
	if res.Code != 0 {
		return fmt.Sprintf("exit %d: %s", res.Code, redact(tail(res.Out, 300)))
	}
	return ""
}

func (r *Resumer) rollback(ctx context.Context, st State, failed string, missing []string) (ResumeResult, error) {
	var restoreErr error
	restored := false
	finalHealth := "not run"
	switch {
	case st.SnapshotPath == "" || r.Config.BinaryPath == "":
		restoreErr = fmt.Errorf("no snapshot or binary_path is recorded, so there is nothing to restore")
	default:
		restoreErr = restoreBinary(st.SnapshotPath, r.Config.BinaryPath, st.SnapshotHash)
	}
	if restoreErr == nil {
		restored = true
		if r.Config.RestartCommand != "" {
			res, err := r.Cmd.Run(ctx, r.Dir, r.Config.RestartCommand)
			if err != nil {
				restoreErr = fmt.Errorf("second restart failed: %s", redact(err.Error()))
			} else if res.Code != 0 {
				restoreErr = fmt.Errorf("second restart failed (exit %d): %s", res.Code, redact(tail(res.Out, 300)))
			}
		}
	}
	if restoreErr == nil {
		if r.Ready != nil {
			_ = r.Ready(ctx)
		}
		if msg := r.healthOnce(ctx); msg == "" {
			finalHealth = "healthy"
		} else {
			finalHealth = "failing: " + msg
		}
	}

	phase, outcome := PhaseRolledBack, OutcomeRolledBack
	text := fmt.Sprintf("Deploy rolled back. A new binary was installed at %s. Failed check: %s. Previous binary restored: yes. Final health after the second restart: %s.",
		r.Config.BinaryPath, failed, finalHealth)
	if restoreErr != nil {
		phase, outcome = PhaseRollbackFailed, OutcomeRollbackFailed
		text = fmt.Sprintf("Deploy rollback FAILED. A new binary was installed at %s. Failed check: %s. Previous binary restored: %s. Final health: %s. Reason: %s. No further automatic action is taken.",
			r.Config.BinaryPath, failed, yesNo(restored), finalHealth, restoreErr.Error())
	}
	st.Phase, st.Reason = phase, failed
	if restoreErr != nil {
		st.Reason = failed + "; " + restoreErr.Error()
	}
	saveErr := r.save(st)
	r.record(OutcomeRecord{Outcome: outcome, FailedCheck: failed, Restored: restored,
		FinalHealth: finalHealth, SnapshotHash: st.SnapshotHash, Summary: text,
		DeployStarted: st.RequestedAt, MissingSession: missing})
	qErr := r.ask(st, phase, text)
	if saveErr != nil {
		return ResumeResult{Phase: phase, Acted: true, Reason: failed}, saveErr
	}
	return ResumeResult{Phase: phase, Acted: true, Reason: failed}, qErr
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (r *Resumer) save(s State) error {
	s.UpdatedAt = r.Clock.Now()
	return r.Store.Save(s)
}

func (r *Resumer) record(rec OutcomeRecord) {
	if r.Outcomes == nil {
		return
	}
	rec.At = r.Clock.Now()
	_ = r.Outcomes.Append(rec)
}

func (r *Resumer) ask(st State, phase Phase, text string) error {
	if r.Questions == nil {
		return nil
	}
	sum := sha256.Sum256([]byte("deploy-outcome\x00" + st.RequestedAt.UTC().Format(time.RFC3339Nano) + "\x00" + string(phase)))
	opts := []autonomous.QuestionOption{
		{ID: "acknowledge", Label: "Acknowledge", Tradeoffs: "Nothing further is done automatically."},
		{ID: "investigate", Label: "Investigate by hand", Tradeoffs: "You inspect the daemon and the binary yourself."},
	}
	q := autonomous.Question{
		Label: "decision", AskedBy: DeployQuestionAsker,
		AskerKind: autonomous.QuestionAskerSystem, Context: autonomous.QuestionContextNone,
		Text: text, Options: opts, Default: "acknowledge",
		SourceLineSHA256: hex.EncodeToString(sum[:]),
	}
	if _, _, err := r.Questions.RaiseUnlessOpenSourceLine(q); err != nil {
		return fmt.Errorf("raise the deploy question: %w", err)
	}
	return nil
}

// restoreBinary copies snap over dst atomically: a temp file in dst's
// directory, then a rename, keeping the snapshot's mode. When wantHash is set
// the snapshot must still match it.
func restoreBinary(snap, dst, wantHash string) error {
	in, err := os.Open(snap) // #nosec G304 -- snapshot recorded by the sequence
	if err != nil {
		return fmt.Errorf("snapshot is not readable: %w", err)
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".restore-*")
	if err != nil {
		return fmt.Errorf("restore temp file: %w", err)
	}
	name := tmp.Name()
	bad := func(e error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return e
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		return bad(err)
	}
	if wantHash != "" && hex.EncodeToString(h.Sum(nil)) != wantHash {
		return bad(fmt.Errorf("snapshot no longer matches its recorded hash"))
	}
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		return bad(err)
	}
	if err := tmp.Sync(); err != nil {
		return bad(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// RetryDeferred retries a deferred deploy with retry (Runner.Retry) until it
// is no longer deferred. It returns at once when no deferred state exists. Each
// Retry waits out the drain itself, so the loop only adds a short pause.
func RetryDeferred(ctx context.Context, store StateStore, clock Clock, retry func(context.Context) (Result, error)) (Result, error) {
	for {
		st, ok, err := store.Load()
		if err != nil {
			return Result{}, err
		}
		if !ok || st.Phase != PhaseDeferred {
			return Result{Status: StatusSkipped, Reason: "no deferred deploy to retry"}, nil
		}
		res, err := retry(ctx)
		if err != nil || res.Status != StatusDeferred {
			return res, err
		}
		if err := clock.Sleep(ctx, PollInterval); err != nil {
			return Result{}, err
		}
	}
}
