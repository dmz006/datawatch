package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// doneCheckpointFile is the worker's scratch checkpoint, read from the
// task's resolved work directory.
const doneCheckpointFile = "CHECKPOINT.md"

// readDoneCheckpoint returns the worker's CHECKPOINT.md text from dir, or ""
// when the file is absent or unreadable. The read is direct: the file is not
// relocated here.
func readDoneCheckpoint(dir string) string {
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, doneCheckpointFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// doneCheckpointVerify decides whether an activity-stall verdict should be
// overridden because the worker had already finished its work. It runs only
// when the checkpoint text says every step is done and the task has a
// verification script. A script that passes (exit 0, no runner error) means
// the work is complete: the session is killed, the deterministic completion
// commit runs through commit, and passed=true with a passing verdict comes
// back. Otherwise passed=false and the caller falls through to the normal
// stall handling, which snapshots the attempt before its own kill.
// scriptOutput carries the script's output when the script ran and failed, so
// the caller can put it in the retry hint; it is "" when the script did not run.
// commit may be nil.
func doneCheckpointVerify(killer attemptKiller, sessionID, checkpointText, scriptRel, dir string, commit func() error) (vr autonomouspkg.VerificationResult, scriptOutput string, passed bool) {
	if strings.TrimSpace(scriptRel) == "" || !autonomouspkg.CheckpointSaysAllDone(checkpointText) {
		return autonomouspkg.VerificationResult{}, "", false
	}
	out, ok, err := autonomouspkg.RunVerificationScript(scriptRel, dir)
	if err != nil {
		out = strings.TrimSpace(out + "\n" + err.Error())
	}
	if !ok || err != nil {
		return autonomouspkg.VerificationResult{}, out, false
	}
	if killer != nil {
		_ = killer.EndSession(sessionID, session.EndReason{Caller: endCallerDoneCheckpoint, Reason: doneCheckpointEndReason()})
	}
	if commit != nil {
		if cerr := commit(); cerr != nil {
			log.Printf("[autonomous] done-checkpoint verify: completion commit for session %s: %v", sessionID, cerr)
		}
	}
	return autonomouspkg.VerificationResult{
		OK:         true,
		Severity:   "low",
		Summary:    "verifier: worker stalled after its checkpoint reported all steps done; the verification script passed, so the attempt is accepted",
		VerifiedAt: time.Now(),
	}, out, true
}
