package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// judgeResumeOutputFile resolves the on-disk judge result file for one
// guardrail invocation — the exact hash the guardrail spawn path uses
// (UnitID|Guardrail|prompt → .guardrail-output-<hex8>.json under the
// project dir), so this reads back the file the in-flight judge wrote.
func judgeResumeOutputFile(inv autonomouspkg.GuardrailInvocation) string {
	prompt := buildAutonomousGuardrailPrompt(inv)
	if inv.Prompt != "" {
		// The plan-review judge supplies its whole prompt; the spawn path
		// uses it the same way, so the file name matches there too.
		prompt = inv.Prompt
	}
	specHash := sha256.Sum256([]byte(inv.UnitID + "|" + inv.Guardrail + "|" + prompt))
	return filepath.Join(inv.ProjectDir, ".guardrail-output-"+hex.EncodeToString(specHash[:8])+".json")
}

// judgeResumeReadVerdict reads and parses the surviving guardrail-judge
// result file for one guardrail invocation. ok is false when the file is
// missing, unparseable, or not a verdict at all — a garbled result is
// treated as no result, so the flow falls through to the judge run
// instead of recording nonsense. A parsed verdict is stamped with the
// collecting session and the file path; the normal record path (via
// verdict.JudgeOutputPath) cleans the file up after recording.
func judgeResumeReadVerdict(inv autonomouspkg.GuardrailInvocation, sessionID string) (*autonomouspkg.GuardrailVerdict, bool) {
	path := judgeResumeOutputFile(inv)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var v autonomouspkg.GuardrailVerdict
	if err := json.Unmarshal([]byte(extractJSON(string(b))), &v); err != nil {
		return nil, false
	}
	if v.Outcome == "" {
		return nil, false
	}
	stamped := judgeVerdictWithSession(v, sessionID, path)
	return &stamped, true
}

// wireJudgeResume wires amgr's judge-resume hooks to the daemon's session
// layer:
//
//   - SetJudgeReattachFn: for each guardrail-judge session the manager
//     adopts (ReconcileJudges, boot — the tmux/CLI session itself survives
//     daemon restarts; what does not is the in-memory waiter the
//     pre-restart flow attached to it), a background waiter is attached.
//     It polls the session's result file — the session may already have
//     ended with the file on disk, or still be running — and hands the
//     verdict to the manager (ConsumeJudgeResult) once written: recorded,
//     and no second judge spawns. When the session ends without a result
//     file it signals nil instead, resuming the judge run immediately.
//   - SetJudgeResultReader: for a judgment with no (live or tracked)
//     session, a surviving, still-unrecorded result file is read as that
//     judgment's verdict.
func wireJudgeResume(amgr *autonomouspkg.Manager, mgr *session.Manager, cfg *config.Config) {
	amgr.SetJudgeResultReader(func(prdID string, inv autonomouspkg.GuardrailInvocation) (*autonomouspkg.GuardrailVerdict, bool) {
		_ = prdID // the file name already pins the invocation
		return judgeResumeReadVerdict(inv, "")
	})
	amgr.SetJudgeReattachFn(func(prdID string, inv autonomouspkg.GuardrailInvocation, sessionID string) {
		// Attach and return promptly: the manager's wait on this verdict is
		// bounded, and a delivery it misses is a no-op — the fall-through
		// judge run re-adopts the same session (judgeLiveSession) instead
		// of spawning a second one.
		go func() {
			// The session was already in flight when the daemon restarted; it
			// may still be running, or it may have ended with its result on
			// disk. The file is the verdict's source of truth in both cases;
			// stop as soon as the session is gone without one.
			budget := time.Duration(config.NormalizeJudgeTimeoutSeconds(cfg.Autonomous.JudgeTimeoutSeconds))*time.Second + 2*time.Minute
			deadline := time.Now().Add(budget)
			for time.Now().Before(deadline) {
				if v, ok := judgeResumeReadVerdict(inv, sessionID); ok {
					amgr.ConsumeJudgeResult(prdID, inv.UnitID, v) // true while the manager still waits; a no-op if the judge run already took over
					return
				}
				if !sessionRunning(mgr, sessionID) {
					amgr.ConsumeJudgeResult(prdID, inv.UnitID, nil) // no result to recover: resume the judge run now, don't burn its wait budget
					return
				}
				time.Sleep(3 * time.Second)
			}
			// Out of budget: the manager has already resumed its
			// fall-through judge run, and that run's adoption guard
			// re-waits on the session where it still lives.
			return
		}()
	})
}
