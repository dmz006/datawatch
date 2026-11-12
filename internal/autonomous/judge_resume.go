package autonomous

import (
	"context"
	"log"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// judgeResumeKey scopes one re-attached judge's verdict to one (PRD, unit)
// pair — the same pairing the guardrail flow records the verdict under.
func judgeResumeKey(prdID, unitID string) string {
	return prdID + "\n" + unitID
}

// JudgeResume is what boot-resume does with a guardrail judgment that was
// still in flight for a unit when the daemon restarted.
type JudgeResume int

const (
	// JudgeResumeRunOnce: neither a live judge session nor an unconsumed
	// result file survived the restart, so the guardrail flow runs the
	// judge once, as usual. (Where the session did survive but the manager
	// lost track of it — nothing adopted, nothing to consume — the
	// daemon-side live-session adoption in the judge run re-waits on the
	// judge instead of spawning a second one.)
	JudgeResumeRunOnce JudgeResume = iota
	// JudgeResumeReattach: a guardrail-judge session adopted into this
	// Manager (ReconcileJudges, boot) is (per the liveness probe) still
	// judging the unit. A fresh waiter is attached to that surviving
	// session (SetJudgeReattachFn) — the verdict its collection hands back
	// through ConsumeJudgeResult is the outcome; no second judge spawns.
	JudgeResumeReattach
	// JudgeResumeConsume: the judge session is gone, but its result file
	// survived the restart unrecorded (the judge finished writing before
	// the restart and the daemon died before the verdict was recorded).
	// The file is read (SetJudgeResultReader) and its verdict recorded as
	// the outcome — no judge runs.
	JudgeResumeConsume
)

// ResolveJudgeResume decides what boot-resume does with an in-flight
// guardrail judgment for one unit. Pure: no I/O, no manager access. A
// still-live adopted session still owns the verdict and outranks
// everything else; a parseable result file is consumed as the outcome;
// anything else runs the judge once in the normal guardrail flow. alive
// reports liveness for the adopted session (nil — no session is live or
// trackable); verdictFile hands back the surviving, still-unrecorded
// result (nil — nothing to consume); consulted in that order.
func ResolveJudgeResume(sessionID string, alive func(sessionID string) bool, verdictFile func() (*GuardrailVerdict, bool)) JudgeResume {
	if sessionID != "" && alive != nil && alive(sessionID) {
		return JudgeResumeReattach
	}
	if verdictFile != nil {
		if v, ok := verdictFile(); ok && v != nil {
			return JudgeResumeConsume
		}
	}
	return JudgeResumeRunOnce
}

// SetJudgeReattachFn wires the waiter attach for a guardrail-judge session
// that may have survived a daemon restart. fn is called once with the PRD,
// the guardrail invocation and the adopted session, and must return
// promptly — it attaches its own background waiter (the code that waits
// on the judge session's output), collects the verdict once the session
// has ended and written its result, and hands it back with the public
// ConsumeJudgeResult (or signals nil there when there is no result to
// recover). A nil fn leaves no attach mechanism: the guardrail flow falls
// through to a normal judge run, whose daemon-side session adoption
// re-waits on a surviving session instead of spawning a second one.
func (m *Manager) SetJudgeReattachFn(fn func(prdID string, inv GuardrailInvocation, sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.judgeReattachFn = fn
}

// SetJudgeResultReader wires the reader for surviving guardrail-judge
// result files (the JSON verdict the judge wrote before the daemon died
// and is still unrecorded). fn returns the parsed verdict with true when
// such a file exists and parses, or false otherwise; set
// v.JudgeOutputPath to the file read so the normal record path cleans it
// up after recording. A nil reader leaves nothing to consume.
func (m *Manager) SetJudgeResultReader(fn func(prdID string, inv GuardrailInvocation) (*GuardrailVerdict, bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.judgeResultReader = fn
}

// judgeResumeWaitBudget is how long judgeResume blocks on a re-attached
// judge's verdict before falling through to the normal judge run: the
// judge's normal budget (the session was already in flight, so allow a
// full one) plus a small tail for the result write and collection.
// Package var so tests can shorten it.
var judgeResumeWaitBudget = func(judgeTimeoutSeconds int) time.Duration {
	return time.Duration(config.NormalizeJudgeTimeoutSeconds(judgeTimeoutSeconds))*time.Second + 90*time.Second
}

// ConsumeJudgeResult hands the daemon's judge-resume waiter
// (SetJudgeReattachFn) the verdict it collected from a surviving judge
// session — or nil, meaning it waited and found no result to recover
// (session ended without a result file): the waiting guardrail flow then
// resumes to the normal judge run immediately, without burning its wait
// budget. A non-nil verdict reaches the flow waiting on that (PRD, unit)
// pair (the judgeResume select in runGuardrailWithInfraRetry), and the
// flow records it exactly like a fresh one (judgment decision, save,
// output cleanup via v.JudgeOutputPath). Idempotent: false when no flow
// is waiting on that pair — e.g. the wait budget already elapsed and the
// judge run and its daemon-side session adoption took over.
func (m *Manager) ConsumeJudgeResult(prdID, unitID string, v *GuardrailVerdict) bool {
	key := judgeResumeKey(prdID, unitID)
	m.judgeWaitMu.Lock()
	ch := m.judgeWaiters[key]
	m.judgeWaitMu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- v: // buffered: the send can never block its waiter
		return true
	default:
		// already delivered (verdict or giveup signal); the verdict was
		// whatever delivered first.
		return false
	}
}

// judgeResume is the boot-resume half of the guardrail flow, called from
// runGuardrailWithInfraRetry: it returns (verdict, true) when the judgment
// in flight for this unit at restart time can be resumed instead of judged
// again.
//
//   - Re-attach — a guardrail-judge session was adopted for this unit
//     (ReconcileJudges, boot) and the daemon can attach to it. The
//     session survives daemon restarts; what dies is the in-memory waiter
//     the pre-restart flow attached to it. The attach hook is consulted
//     (returns promptly), and this call blocks on the verdict its
//     background collection hands back through ConsumeJudgeResult — nil
//     there means the session is gone without a recoverable result (or
//     already collected the surviving result file on disk), and the
//     fall-through judge run starts right away. If the wait budget
//     expires first, the fall-through judge run re-adopts the live
//     session daemon-side and no second judge spawns.
//   - Consume — no session to re-attach, but the judge's result file
//     survived unrecorded: the reader parses it, and the verdict is the
//     outcome; the caller applies it exactly like a fresh one.
//
// With nothing surviving — or no resume hooks wired at all — it returns
// (zero, false): the caller runs the judge once, as usual.
func (m *Manager) judgeResume(ctx context.Context, prd *PRD, inv GuardrailInvocation) (*GuardrailVerdict, bool) {
	if prd == nil {
		return nil, false
	}
	prdID := inv.PRDID
	if prdID == "" {
		prdID = prd.ID
	}
	m.mu.Lock()
	sessionID := m.judgeAdopts[inv.UnitID]
	reattach := m.judgeReattachFn
	reader := m.judgeResultReader
	judgeTimeout := m.cfg.JudgeTimeoutSeconds
	m.mu.Unlock()
	if sessionID == "" && reader == nil {
		// nothing adopted at boot, nothing to read: the judge runs once,
		// exactly as it always has.
		return nil, false
	}
	if sessionID != "" && reattach != nil {
		// An adopted guardrail-judge session and an attach mechanism: the
		// session is the verdict's owner whether it is still running or
		// already ended with its result on disk — the daemon-side waiter
		// (which knows the real session liveness) collects either and
		// hands it back here, or signals that there is nothing to
		// recover.
		key := judgeResumeKey(prdID, inv.UnitID)
		ch := make(chan *GuardrailVerdict, 1) // buffered: a delivery can never block its waiter
		m.judgeWaitMu.Lock()
		if m.judgeWaiters == nil {
			m.judgeWaiters = map[string]chan *GuardrailVerdict{}
		}
		m.judgeWaiters[key] = ch
		m.judgeWaitMu.Unlock()
		defer func() { // the entry is not closed while a delivery can still race it
			m.judgeWaitMu.Lock()
			if m.judgeWaiters[key] == ch {
				delete(m.judgeWaiters, key)
			}
			m.judgeWaitMu.Unlock()
		}()
		reattach(prdID, inv, sessionID) // attach the background waiter; returns promptly
		select {
		case v := <-ch:
			if v != nil {
				return v, true
			}
			// waited and found nothing to recover: stop waiting now, let
			// the judge run once below.
			return nil, false
		case <-ctx.Done():
			return nil, false
		case <-time.After(judgeResumeWaitBudget(judgeTimeout)):
			// the surviving judge outlived the budget, or its collection
			// lost the race: the fall-through judge run takes over —
			// re-adopting the live session daemon-side where one
			// still runs, or judging fresh where the session truly
			// failed.
			log.Printf("[autonomous] judge-resume: %s elapsed waiting on re-attached judge session %s for unit %s; the judge run takes it over", judgeResumeWaitBudget(judgeTimeout), sessionID, inv.UnitID)
		}
		return nil, false
	}
	if reader != nil {
		// no tracked session to re-attach (or no attach mechanism): a
		// surviving, still-unrecorded result file is the outcome, if the
		// reader hands one back.
		if v, ok := reader(prdID, inv); ok && v != nil {
			return v, true
		}
		// the file vanished or a competing flow consumed it in the
		// meantime: fall through and let the judge run once below.
	}
	return nil, false
}
