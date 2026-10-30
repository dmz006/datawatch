// BL24+BL25 — adapter that lets the server package consume the
// autonomous Manager through the small AutonomousAPI interface
// (server/api.go) without an import cycle.
//
// Returns concrete autonomous types as `any` — the JSON encoder on
// the server side does the right thing.

package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	scanPkg "github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/config"
)

// API wraps a *Manager to satisfy server.AutonomousAPI.
//
// v4.0.1: Spawn/Verify fns can be wired via SetExecutors so the REST
// POST /api/autonomous/prds/{id}/run actually walks the task DAG
// (Manager.Run) rather than just flipping PRD.Status. When either fn
// is nil, Run() falls back to the v3.10 status-only behaviour so the
// daemon still starts cleanly on operators that haven't configured
// worker spawning yet.
type API struct {
	M       *Manager
	spawnFn SpawnFn
	verify  VerifyFn

	// v5.26.16 — track per-PRD run goroutines so Cancel / DeletePRD
	// can stop the executor mid-flight. Pre-v5.26.16 the goroutine
	// kept spawning new tmux sessions even after the PRD was
	// cancelled or hard-deleted, leaving orphan `autonomous:*`
	// sessions that ate the session.max_sessions cap. The cancel
	// callbacks are removed from the map when the goroutine exits.
	runMu      sync.Mutex
	runCancels map[string]context.CancelFunc

	// reviewWake carries operator review decisions to the live executor of
	// a PRD. It has its own mutex. ResolveTaskReview signals under runMu, and
	// the executor's final check and its release of the slot also run under
	// runMu, so a decision cannot slip in between that check and the release.
	reviewWake reviewWakes

	// execWG counts live executor goroutines (and the boot-resume
	// scan that may launch them) so WaitIdle can block until they have
	// all exited.
	execWG sync.WaitGroup

	// serverReadyCh, when non-nil, is closed by the HTTP server layer once
	// it is ready to accept requests. resumeRunningPRDs blocks on it so that
	// executor spawn calls don't fail with "connection refused" on daemon
	// restart. In tests the channel is nil and the wait is skipped.
	serverReadyCh <-chan struct{}
}

// NewAPI is a convenience constructor.
func NewAPI(m *Manager) *API {
	return &API{M: m, runCancels: map[string]context.CancelFunc{}}
}

// SetServerReadyCh wires the channel that signals when the HTTP server is
// ready to accept connections. resumeRunningPRDs blocks on it before
// spawning executor sessions, preventing "connection refused" errors on
// daemon restart. Call this before SetExecutors. The channel must be closed
// (not sent on) to unblock the wait.
func (a *API) SetServerReadyCh(ch <-chan struct{}) {
	a.serverReadyCh = ch
}

// SetExecutors wires the real spawn + verify indirections used by the
// executor walk. Called from main.go once session.Manager + BL103
// validator wiring are available. After wiring, re-launches executor
// goroutines for any PRD that was left in PRDRunning state by a prior
// daemon instance — those goroutines are lost on restart, leaving
// in-flight PRDs permanently stalled without this recovery step.
func (a *API) SetExecutors(spawn SpawnFn, verify VerifyFn) {
	a.spawnFn = spawn
	a.verify = verify
	a.M.SetStuckRunningHooks(a.refireRollup, a.executorLive)
	a.execWG.Add(1)
	go func() {
		defer a.execWG.Done()
		a.resumeRunningPRDs()
	}()
}

// refireRollup re-launches the executor for a running PRD whose tasks are all
// terminal, the same call boot-resume makes, so Run re-fires the completion
// rollup. A PRD with a live executor is left alone (started=false).
func (a *API) refireRollup(id string) (bool, error) {
	return a.runLocked(id, nil)
}

// executorLive reports whether a live executor holds the PRD's run slot.
func (a *API) executorLive(id string) bool {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	_, live := a.runCancels[id]
	return live
}

// WaitIdle cancels every live executor context and blocks until all
// executor goroutines (including the boot-resume scan) have exited. It
// returns an error if they are still running after timeout. Executors
// launched while draining are cancelled too.
func (a *API) WaitIdle(timeout time.Duration) error {
	idle := make(chan struct{})
	go func() {
		a.execWG.Wait()
		close(idle)
	}()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	retry := time.NewTicker(5 * time.Millisecond)
	defer retry.Stop()
	for {
		a.runMu.Lock()
		for _, cancel := range a.runCancels {
			cancel()
		}
		a.runMu.Unlock()
		select {
		case <-idle:
			return nil
		case <-deadline.C:
			return fmt.Errorf("autonomous: executor goroutines still running after %s", timeout)
		case <-retry.C:
		}
	}
}

// resumeRunningPRDs re-launches executor goroutines for PRDs that are
// stored in PRDRunning state but have no live executor (e.g. after a
// daemon restart). Also recovers PRDFailed PRDs where all failed tasks
// were killed by the daemon restart before they could run (their SessionID
// is empty or dead and they have no partial result).
// Called once from SetExecutors in a background goroutine.
func (a *API) resumeRunningPRDs() {
	// Wait for the HTTP server to be ready before spawning executor sessions.
	// SetExecutors fires this goroutine before httpServer.Start() is called;
	// without this gate the spawn calls get "connection refused" and tasks
	// fail immediately, re-producing PRDFailed. In tests serverReadyCh is
	// nil and the wait is skipped so tests don't need to fake a server.
	if ch := a.serverReadyCh; ch != nil {
		<-ch
	}

	// Verifier sessions left from before the restart are reconciled once,
	// before any task resumes verification, so a resumed flow waits for the
	// surviving verifier instead of spawning a second one.
	a.M.mu.Lock()
	vhost := a.M.verifierHost
	jhost := a.M.judgeHost
	a.M.mu.Unlock()
	if n := len(a.M.ReconcileVerifiers(vhost)); n > 0 {
		fmt.Printf("[autonomous] boot-resume: reconciled %d verifier session(s)\n", n)
	}
	if n := len(a.M.ReconcileJudges(jhost)); n > 0 {
		fmt.Printf("[autonomous] boot-resume: reconciled %d guardrail judge session(s)\n", n)
	}

	// B165 — counts for the per-PRD decision log + summary line.
	resumed, parked := 0, 0

	for _, prd := range a.M.Store().ListPRDs() {
		// ListPRDs returns the shared *PRD, so reading .Status here would race
		// against a concurrent Run() mutating it. Read it under the Manager mutex.
		status, _ := a.prdStatus(prd.ID)
		fmt.Printf("[autonomous] boot-resume: scanning prd=%s status=%s\n", prd.ID, status)
		switch status {
		case PRDPlanning:
			// A PRD stuck in planning means the daemon was restarted while
			// decomposeFnSession was running. The goroutine is gone; reset to
			// draft so the operator can re-trigger decompose.
			prd.Status = PRDDraft
			if a.M.store.SavePRD(prd) == nil {
				a.M.emitPRDStatus(prd, status, nil, nil)
			}
			a.M.killOrphanDecomposeSessions(prd.ID)
			fmt.Printf("[autonomous] boot-resume: prd=%s was planning on restart, reset to draft\n", prd.ID)
		case PRDCancelled:
			// An operator-cancelled PRD must never be re-decomposed. A live
			// planning session still linked to it is an orphan, so kill it.
			// No status mutation: the PRD stays cancelled.
			if n := a.M.killOrphanDecomposeSessions(prd.ID); n > 0 {
				fmt.Printf("[autonomous] boot-resume: prd=%s is cancelled, killed %d orphan decompose session(s)\n", prd.ID, n)
			}
		case PRDRunning:
			// A PRD can be legitimately Running because something else (an
			// operator's Run() call, or a Run() started earlier in this same
			// boot-resume pass) is already actively driving it — not because
			// it's an orphan left behind by a dead process. resetInProgressTasksForResume
			// unconditionally treats any in-progress task as orphaned and kills
			// its session, so it must only run once this call has claimed the
			// executor slot; runLocked claims first, then preps, and is a no-op
			// when an executor is already live.
			// A PRD that already has a live executor is left alone (runCancels
			// is the same liveness signal API.Run uses for its idempotency):
			// parking it would block a run that is actually still in flight.
			a.runMu.Lock()
			_, alreadyLive := a.runCancels[prd.ID]
			a.runMu.Unlock()
			if alreadyLive {
				continue
			}

			// B165 — boot-resume gate: before re-running a PRD left in
			// PRDRunning state by a prior daemon instance, decide whether its
			// pre-restart work is still worth picking up. Park (PRDBlocked +
			// plain-English reason + alert) when the newest recorded activity
			// is stale versus resume_max_age_seconds (0 = never resume, -1 =
			// no age limit), or the PRD's/task's project directory no longer
			// exists (removed temp workspace). Parked PRDs must NOT go through
			// resetInProgressTasksForResume or a run.
			dec, now, parkedNow := a.gateBootResume(prd, PRDRunning)
			if parkedNow {
				parked++
				continue
			}
			a.M.recordBootResume(BootResumeEntry{PRDID: prd.ID, Outcome: "resumed", AgeSeconds: dec.ageSec, Reason: dec.reason, At: now})
			fmt.Printf("[autonomous] boot-resume: prd=%s resumed age=%ds reason=%s\n", prd.ID, dec.ageSec, dec.reason)
			resumed++

			// The recreated executor goes through runLocked: claimRun takes the
			// runCancels slot before prep, so no other Run() can start a second
			// executor, and the PRD record stays PRDRunning throughout (prep
			// never touches prd.Status; only the park path above leaves it, and
			// that never reaches here). Adopted in_progress sessions are
			// resumed by that same executor, not by a separate goroutine.
			started, err := a.runLocked(prd.ID, func() bool {
				a.M.resetInProgressTasksForResume(prd)
				return true
			})
			if err != nil {
				fmt.Printf("[autonomous] boot-resume: prd=%s: %v\n", prd.ID, err)
			} else if started {
				fmt.Printf("[autonomous] boot-resume: re-launched executor for prd=%s\n", prd.ID)
			}
		case PRDFailed:
			// A PRDFailed PRD where every failed task has an empty or dead
			// session was most likely killed mid-run by a daemon SIGKILL.
			// resetKilledTasksForResume resets tasks to pending and saves the
			// PRD as PRDRunning; then Run re-launches the executor.
			// Defensive: a failed PRD should have no planning session, but
			// kill any that remain. Never decomposes and never mutates status here.
			a.M.killOrphanDecomposeSessions(prd.ID)

			// Same gate as the running case: a stale failed PRD, or one
			// whose project directory is gone, is parked rather than
			// recovered, and a PRD with a live executor is left alone.
			a.runMu.Lock()
			_, alreadyLive := a.runCancels[prd.ID]
			a.runMu.Unlock()
			if alreadyLive {
				continue
			}
			dec, now, parkedNow := a.gateBootResume(prd, PRDFailed)
			if parkedNow {
				parked++
				continue
			}
			fmt.Printf("[autonomous] boot-resume: prd=%s is PRDFailed, attempting recovery\n", prd.ID)
			started, err := a.runLocked(prd.ID, func() bool { return a.M.resetKilledTasksForResume(prd) })
			switch {
			case err != nil:
				fmt.Printf("[autonomous] boot-resume: failed-prd recovery %s: %v\n", prd.ID, err)
			case started:
				a.M.recordBootResume(BootResumeEntry{PRDID: prd.ID, Outcome: "resumed", AgeSeconds: dec.ageSec, Reason: dec.reason, At: now})
				resumed++
				fmt.Printf("[autonomous] boot-resume: recovered PRDFailed prd=%s (all tasks were daemon-killed)\n", prd.ID)
			default:
				fmt.Printf("[autonomous] boot-resume: prd=%s PRDFailed recovery skipped (tasks have results, live sessions or a live executor)\n", prd.ID)
			}
		}
	}
	fmt.Printf("[autonomous] boot-resume: resumed %d, parked %d\n", resumed, parked)

	// Last, so a daemon-killed member has been recovered above and is not
	// routed down on_failed: ready the coordinator and reconcile Automations.
	a.M.mu.Lock()
	coord := a.M.coordinator
	a.M.mu.Unlock()
	coord.reconcile()
}

// gateBootResume runs the boot-resume decision for a PRD whose prior status
// was prior. When the decision is to park, it parks the PRD, logs the
// decision, records a "parked" entry and returns parked=true; the caller
// must then not start any executor. Otherwise it returns the decision and
// the decision time for the caller to record once it actually resumes.
func (a *API) gateBootResume(prd *PRD, prior PRDStatus) (dec bootResumeDecision, now time.Time, parked bool) {
	a.M.mu.Lock()
	maxAge := a.M.cfg.ResumeMaxAgeSeconds
	a.M.mu.Unlock()
	now = time.Now()
	dec = decideBootResume(prd, now, maxAge, a.M.bootResumeHooks())
	if dec.resume {
		return dec, now, false
	}
	fmt.Printf("[autonomous] boot-resume: prd=%s parked age=%ds reason=%s\n", prd.ID, dec.ageSec, dec.reason)
	if err := a.M.parkPRDForBootResume(prd, prior, dec.reason); err != nil {
		fmt.Printf("[autonomous] boot-resume: prd=%s park save failed: %v\n", prd.ID, err)
	}
	a.M.recordBootResume(BootResumeEntry{PRDID: prd.ID, Outcome: "parked", AgeSeconds: dec.ageSec, Reason: dec.reason, At: now, LiveSessions: liveWorkerSessions(prd, a.M.bootResumeHooks())})
	return dec, now, true
}

func (a *API) Config() any { return a.M.Config() }

// SetConfig accepts json.RawMessage (what the REST handler passes) so
// callers don't need to know our concrete Config shape.
// BL304: also accepts legacy decomposition_backend/effort/model JSON keys
// and maps them to the new planning_backend/effort/model fields.
func (a *API) SetConfig(v any) error {
	raw, ok := v.(json.RawMessage)
	if !ok {
		// Try to marshal whatever was passed back through JSON.
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		raw = b
	}
	// BL304 — migrate old decomposition_* keys to planning_* before
	// unmarshaling. Parse as a generic map, rename keys, re-marshal.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err == nil {
		legacyMap := map[string]string{
			"decomposition_backend": "planning_backend",
			"decomposition_effort":  "planning_effort",
			"decomposition_model":   "planning_model",
		}
		changed := false
		for old, newKey := range legacyMap {
			if val, exists := m[old]; exists {
				if _, hasNew := m[newKey]; !hasNew {
					m[newKey] = val
				}
				delete(m, old)
				changed = true
			}
		}
		// autonomous.notify — the REST partial write (PUT /api/config
		// autonomous.notify.*) sends the global notify default as a nested
		// "notify" object, but the Manager carries the flat
		// NotifyDefaultEvents field, so lift default_events out of the
		// object into its field key before the merge below. An explicit
		// empty default_events list (notifications off) must survive as [] —
		// the merge unmarshal preserves it against NotifyDefaultEvents,
		// which is deliberately not omitempty.
		if rawNotify, hasNotify := m["notify"]; hasNotify {
			var notifySub map[string]json.RawMessage
			if json.Unmarshal(rawNotify, &notifySub) == nil {
				if de, okK := notifySub["default_events"]; okK {
					if _, exists := m["notify_default_events"]; !exists {
						m["notify_default_events"] = de
					}
					changed = true
				}
			}
			// Drop the nested object either way: it has no flat field to
			// merge into and must not leak into the config as an unknown key.
			delete(m, "notify")
		}
		if changed {
			if b, err := json.Marshal(m); err == nil {
				raw = b
			}
		}
	}
	// v8.36.9 — merge onto the CURRENT config rather than unmarshaling into
	// a fresh zero-valued one. Found live while adding verification_backends:
	// every existing caller of this path (REST, MCP's autonomous_config_set,
	// and the new comm-channel config-set) can legitimately send a PARTIAL
	// object — a single "enabled": true toggle, say — and json.Unmarshal
	// only touches keys actually present in the payload, so starting from a
	// zero-valued Config silently reset every field the caller didn't
	// happen to mention (verification_backend, security_scan, quality
	// gates, injection guard, capacity timeouts, ...) back to its default
	// on every partial update. Starting from the current config makes an
	// omitted field mean "leave as-is", matching what every caller of a
	// partial update actually expects.
	// v9.5.5 — reject unknown task_override_statuses names before any
	// merge. Both REST (PUT /api/autonomous/config) and MCP
	// autonomous_config_set land here, so one probe covers both. A
	// non-pointer slice means "absent" stays nil and passes; an explicit
	// [] is valid and reverts to the defaults.
	var probe struct {
		TaskOverrideStatuses []string `json:"task_override_statuses"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		if verr := ValidateTaskOverrideStatuses(probe.TaskOverrideStatuses); verr != nil {
			return verr
		}
	}
	// gate_disagreement is write-validated here too: anything other than
	// the strings "warn"/"block" is rejected; an omitted key leaves the
	// current value untouched (the merge below only touches present keys).
	var gateProbe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &gateProbe); err == nil {
		if b, present := gateProbe["gate_disagreement"]; present {
			var gd string
			if json.Unmarshal(b, &gd) != nil {
				return fmt.Errorf("autonomous.gate_disagreement: must be the string \"warn\" or \"block\"")
			}
			if gd != "warn" && gd != "block" {
				return fmt.Errorf("autonomous.gate_disagreement: invalid value %q (valid values: \"warn\", \"block\")", gd)
			}
		}
	}
	// failure_escalation is checked the same way as the per-Automaton and
	// per-task overrides: an out-of-range value or a malformed step is refused
	// before anything is merged. An omitted key leaves the current policy.
	var escProbe struct {
		FailureEscalation *FailureEscalationPolicy `json:"failure_escalation"`
	}
	if err := json.Unmarshal(raw, &escProbe); err == nil && escProbe.FailureEscalation != nil {
		if verr := validateFailureEscalationPolicy(escProbe.FailureEscalation); verr != nil {
			return verr
		}
	}
	// scope_lint — refuse an unknown mode on every write path (REST, MCP,
	// comm) instead of storing it and resolving it to enforce later. A
	// null or absent key leaves the current mode alone.
	var scopeProbe struct {
		ScopeLint *string `json:"scope_lint"`
	}
	if err := json.Unmarshal(raw, &scopeProbe); err == nil && scopeProbe.ScopeLint != nil {
		if verr := config.ValidateScopeLint(*scopeProbe.ScopeLint); verr != nil {
			return verr
		}
	}
	// loop_detect_turns — reject negatives before any merge (same probe
	// pattern as task_override_statuses above). 0 (detector off) is valid.
	// Inlined rather than calling config.ValidateLoopDetectTurns to keep
	// this package's import graph free of internal/config.
	var loopProbe struct {
		LoopDetectTurns *int `json:"loop_detect_turns"`
	}
	if err := json.Unmarshal(raw, &loopProbe); err == nil {
		if loopProbe.LoopDetectTurns != nil && *loopProbe.LoopDetectTurns < 0 {
			return fmt.Errorf("autonomous.loop_detect_turns must be >= 0 (0 turns loop detection off; default is 3)")
		}
	}
	cfg := a.M.Config()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := normalizeVerifierPolicyConfig(&cfg.VerifierPolicy); err != nil {
		return err
	}
	// Judge settings are normalized on every write path so the stored value
	// is the effective one: a zero or negative time limit becomes the 30m
	// default and blank or duplicate judge backends are dropped.
	cfg.JudgePolicy = config.NormalizeJudgePolicy(cfg.JudgePolicy)
	cfg.JudgeTimeoutSeconds = config.NormalizeJudgeTimeoutSeconds(cfg.JudgeTimeoutSeconds)
	// JudgeSizeThresholdLines passes through unchanged: 0 turns the size rule
	// off, so there is no zero-to-default rule here.
	if cfg.CapacityRateRequests < 0 || cfg.CapacityRateTokens < 0 || cfg.CapacityRateWindowSeconds < 0 {
		return fmt.Errorf("config: capacity_rate_requests, capacity_rate_tokens and capacity_rate_window_seconds must be >= 0")
	}
	// max_engines_per_node is clamped to 1 the same way config load does
	// (config.Load normalizes anything below 1), so every write path agrees.
	if cfg.MaxEnginesPerNode < 1 {
		cfg.MaxEnginesPerNode = 1
	}
	a.M.SetConfig(cfg)
	return nil
}

func (a *API) Status() any { return a.M.Status() }

// TaskVisibility returns the waiting and running task views (see
// Manager.TaskVisibility). The server reads it through an optional interface,
// so AutonomousAPI fakes need no change.
func (a *API) TaskVisibility() ([]TaskWaitView, []TaskHoldView) { return a.M.TaskVisibility() }

// ResolveHolder resolves a raw capacity holder id into an operator-readable
// name (capacity_titles.go). Like TaskVisibility it is read through an
// optional interface by the server, so AutonomousAPI fakes need no change.
func (a *API) ResolveHolder(holderID string) HolderName { return a.M.ResolveHolder(holderID) }

// ReadableWaitSentence words a wait reason readably for operators, naming
// the holders it mentions (see Manager.ReadableWaitSentence). The server
// reads it through an optional interface, so AutonomousAPI fakes need no
// change.
func (a *API) ReadableWaitSentence(reason string) string { return a.M.ReadableWaitSentence(reason) }

// ReportPairingWarnings returns the current planning/verification pairing
// warnings and logs any not yet logged. The REST config set handler reads it
// through an optional interface, so AutonomousAPI fakes need no change.
func (a *API) ReportPairingWarnings() []string { return a.M.ReportPairingWarnings() }

func (a *API) CreatePRD(spec, projectDir, backend, model, effort string) (any, error) {
	projectDir, err := a.M.CheckProjectDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("project_dir refused: %w", err)
	}
	prd, err := a.M.CreatePRD(spec, projectDir, backend, model, Effort(effort))
	if err == nil && prd != nil {
		a.M.EmitPRDUpdate(prd.ID)
	}
	return prd, err
}

func (a *API) GetPRD(id string) (any, bool) {
	p, ok := a.M.Store().GetPRD(id)
	if !ok {
		return nil, false
	}
	return a.M.PRDWithNotifyEffective(p), true
}

// FindTaskBySessionID maps a datawatch session id to its (prd_id,
// task_id). Used by the session status handler when the session record
// carries no TaskID. Returns ("","") for non-autonomous sessions.
func (a *API) FindTaskBySessionID(sessionID string) (prdID, taskID string) {
	return a.M.FindTaskBySessionID(sessionID)
}

// SetStoreSideFileCapKB sets the PRD store's side-file threshold in KB
// (session.store_side_file_cap_kb). n <= 0 restores the default.
func (a *API) SetStoreSideFileCapKB(n int) { a.M.Store().SetSideFileCap(n << 10) }

// SessionIDsForPRD walks the PRD's stories + tasks and returns every
// non-empty Task.SessionID. Used by the orchestrator REST handler
// (S13 follow-up) to join graph nodes against observer envelopes.
// Returns nil for unknown PRDs; empty slice for PRDs whose tasks
// haven't been scheduled yet.
func (a *API) SessionIDsForPRD(prdID string) []string {
	prd, ok := a.M.Store().GetPRD(prdID)
	if !ok {
		return nil
	}
	var out []string
	for _, s := range prd.Story {
		for _, t := range s.Tasks {
			if t.SessionID != "" {
				out = append(out, t.SessionID)
			}
		}
	}
	return out
}

func (a *API) ListPRDs() []any {
	return a.M.PRDsWithNotifyEffective(a.M.Store().ListPRDs())
}

func (a *API) Decompose(id string) (any, error) {
	out, err := a.M.Decompose(id)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// DecomposeStreaming (BL328) satisfies server.AutonomousAPI.
// Calls Manager.DecomposeStreaming with an any-typed callback wrapper.
// After completion, emits a PRD update broadcast.
func (a *API) DecomposeStreaming(id string, cb func(index, total int, story any)) (any, error) {
	out, err := a.M.DecomposeStreaming(id, cb)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// Run walks the PRD task DAG through Manager.Run when executors are
// wired. Falls back to status-only update when they're not, so the
// REST surface continues to behave sanely in bare-daemon mode.
//
// BL191 Q1 (v5.2.0) — Manager.Run now refuses to start unless the PRD
// is in PRDApproved (or legacy PRDActive) status. The status-only
// fallback path here also flips to PRDRunning so the operator's loop
// status reflects what's happening.
func (a *API) Run(id string) error {
	_, err := a.runLocked(id, nil)
	return err
}

// prdStatus reads a PRD's status under the Manager mutex, the lock executor
// status writes hold; the shared *PRD from the store must not be read bare.
func (a *API) prdStatus(id string) (PRDStatus, bool) {
	p, ok := a.M.Store().GetPRD(id)
	if !ok {
		return "", false
	}
	a.M.mu.Lock()
	defer a.M.mu.Unlock()
	return p.Status, true
}

// runnableStatus reports whether the PRD is in a state Manager.Run accepts.
func (a *API) runnableStatus(id string) bool {
	st, ok := a.prdStatus(id)
	return ok && (st == PRDApproved || st == PRDActive || st == PRDRunning)
}

// claimRun atomically registers an executor slot for the PRD in runCancels.
// It returns false when an executor is already live (B98: the live goroutine
// is never cancelled; it picks up newly-pending tasks on its next poll).
func (a *API) claimRun(id string) (context.Context, bool) {
	ctx, cancel := context.WithCancel(context.Background())
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.runCancels == nil {
		a.runCancels = map[string]context.CancelFunc{}
	}
	if _, alive := a.runCancels[id]; alive {
		cancel()
		return nil, false
	}
	a.runCancels[id] = cancel
	return ctx, true
}

// releaseRun drops a claimed slot (early-error path or goroutine exit).
func (a *API) releaseRun(id string) {
	a.runMu.Lock()
	a.releaseRunLocked(id)
	a.runMu.Unlock()
}

// releaseRunLocked is releaseRun for a caller already holding runMu.
func (a *API) releaseRunLocked(id string) {
	if cancel := a.runCancels[id]; cancel != nil {
		cancel()
		delete(a.runCancels, id)
	}
	a.reviewWake.disarm(id)
}

// runLocked is the shared body of Run and boot-resume. When executors are
// wired it claims the runCancels slot before any status mutation or goroutine
// launch, so a concurrent second call for the same PRD is a no-op. prep, if
// non-nil, runs after the claim and before the status flip; returning false
// aborts without starting. started reports whether this call launched (or, in
// status-only mode, marked running) the PRD.
func (a *API) runLocked(id string, prep func() bool) (started bool, err error) {
	if _, ok := a.prdStatus(id); !ok {
		return false, fmt.Errorf("prd %q not found", id)
	}
	execs := a.spawnFn != nil && a.verify != nil
	var ctx context.Context
	if execs {
		var ok bool
		if ctx, ok = a.claimRun(id); !ok {
			return false, nil
		}
	} else {
		// Status-only mode has no goroutine to hold a slot, but a slot
		// registered by a live executor must still suppress recovery prep.
		a.runMu.Lock()
		_, alive := a.runCancels[id]
		a.runMu.Unlock()
		if alive {
			return false, nil
		}
	}
	release := func() {
		if execs {
			a.releaseRun(id)
		}
	}
	if prep != nil && !prep() {
		release()
		return false, nil
	}
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		release()
		return false, fmt.Errorf("prd %q not found", id)
	}
	// Re-read after prep, which may have changed status (PRDFailed -> PRDRunning).
	a.M.mu.Lock()
	old := prd.Status
	if old != PRDApproved && old != PRDActive && old != PRDRunning {
		a.M.mu.Unlock()
		release()
		return false, fmt.Errorf("prd %q status %q is not runnable; call /approve first", id, old)
	}
	if old != PRDRunning {
		prd.Status = PRDRunning
	}
	a.M.mu.Unlock()
	if err := a.M.Store().SavePRD(prd); err != nil {
		release()
		return false, err
	}
	a.M.emitPRDStatus(prd, old, nil, nil)
	a.M.EmitPRDUpdate(id)
	if !execs {
		return true, nil // executors not configured — status-only mode
	}
	a.execWG.Add(1)
	go func() {
		defer a.execWG.Done()
		defer a.releaseRun(id)
		// The wake is armed here, before the first Run. A decision that
		// lands before the arm is already in the store, which Run reads
		// after it, so no decision is lost.
		runCtx := withReviewWake(ctx, a.reviewWake.arm(id))
		for {
			if err := a.M.Run(runCtx, id, a.spawnFn, a.verify); err != nil {
				if p, ok := a.M.Store().GetPRD(id); ok {
					// If cancelled by operator, leave status as cancelled.
					// Otherwise revert to PRDApproved so they can retry.
					if p.Status != PRDCancelled {
						old := p.Status
						p.Status = PRDApproved
						if a.M.Store().SavePRD(p) == nil {
							a.M.emitPRDStatus(p, old, nil, nil)
						}
					}
				}
			}
			// Emit a final update so the PWA reflects the
			// terminal state (completed / blocked / approved-on-error).
			a.M.EmitPRDUpdate(id)
			// An operator review decision that arrived while Run was
			// finishing is picked up by running again under the same
			// slot. The check and the release share one critical
			// section, so a decision cannot land between them.
			a.runMu.Lock()
			if a.reviewWake.takePending(id) && ctx.Err() == nil && a.runnableStatus(id) {
				a.runMu.Unlock()
				continue
			}
			a.releaseRunLocked(id)
			a.runMu.Unlock()
			return
		}
	}()
	return true, nil
}

// Pause (GH#172 D52) — see Manager.Pause's doc comment for the actual
// drain mechanics. This wrapper just adds the WS update emit.
func (a *API) Pause(id, actor string) (any, error) {
	updated, err := a.M.Pause(id, actor)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return updated, err
}

// Resume (GH#172 D52) flips the PRD back to running via Manager.Resume,
// then relaunches the executor exactly as a.Run already does — reusing
// its runCancels idempotency/goroutine-launch logic unchanged, since
// Resume leaves the PRD in PRDRunning, which Run's own gate accepts.
func (a *API) Resume(id, actor string) (any, error) {
	updated, err := a.M.Resume(id, actor)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(id)
	if err := a.Run(id); err != nil {
		return nil, err
	}
	return updated, nil
}

// ResumeParked is the operator action that resumes a PRD the boot-resume
// gate parked. Manager.ResumeParked validates and flips it back to running;
// the executor then starts through runLocked with the same prep as boot
// resume, so a live in_progress worker session is re-adopted rather than
// duplicated or killed.
func (a *API) ResumeParked(id, actor, note string) (any, error) {
	updated, err := a.M.ResumeParked(id, actor, note)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(id)
	if _, err := a.runLocked(id, func() bool {
		a.M.resetInProgressTasksForResume(updated)
		return true
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// cancelRun is the internal trampoline Cancel + DeletePRD share to
// stop the executor goroutine. Best-effort — silent if no run is
// active.
func (a *API) cancelRun(id string) {
	a.runMu.Lock()
	cancel, ok := a.runCancels[id]
	if ok {
		delete(a.runCancels, id)
	}
	a.runMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

func (a *API) Cancel(id string) error {
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	a.cancelRun(id)          // v5.26.16 — stop the executor goroutine first
	a.M.killPRDSessions(prd) // kill any task sessions still running
	old := prd.Status
	prd.Status = PRDCancelled
	// Unlike CancelStory/CancelTask, this whole-PRD cancel previously left
	// no trace in prd.Decisions — an operator (or a future session) had no
	// way to tell whether/when/why a PRD was cancelled versus failing on
	// its own. Log it the same way.
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "cancel", Actor: "operator"})
	if err := a.M.Store().SavePRD(prd); err != nil {
		return err
	}
	a.M.emitPRDStatus(prd, old, lastDecision(prd, "cancel"), nil)
	a.M.EmitPRDUpdate(id)
	// Worktree cleanup — cancelled end path: release the work branch
	// and its work tree if the work is on the base branch and the
	// entry is safe to remove (live sessions and leases still hold
	// it, and the reasons are then recorded). off mode does nothing.
	a.M.ReleaseAutomatonWorktree(prd)
	return nil
}

func (a *API) ResetToDraft(id, actor string) (any, error) {
	out, err := a.M.ResetToDraft(id, actor)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// BL191 Q1 (v5.2.0) — review/approve/reject/edit-task surfaces.
// v5.24.0 — every mutating wrapper emits a PRD update so the PWA
// can refresh the Autonomous tab over WS without operator action.

func (a *API) Approve(id, actor, note string) (any, error) {
	out, err := a.M.Approve(id, actor, note)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) Archive(id string) (any, error) {
	out, err := a.M.Archive(id)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) Reject(id, actor, reason string) (any, error) {
	out, err := a.M.Reject(id, actor, reason)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// AcknowledgePlanCoverage — operator acknowledgments of a PRD's failing
// plan-requirement coverage report (the block-mode gate). The manager pins
// the ack to the exact failing surface (fingerprint) and requires a
// non-empty note; see Manager.AcknowledgePlanCoverage.
func (a *API) AcknowledgePlanCoverage(prdID, actor, note string) (*PRD, error) {
	out, err := a.M.AcknowledgePlanCoverage(prdID, actor, note)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// 2026-10-17 — plan review gate: operator-triggered re-run of the
// pre-approval judge (Manager.ReviewPlan in plan_review.go). Re-running
// replaces the stored verdict, so a passing verdict clears a blocking
// one; mode off returns the PRD as stored.
func (a *API) ReviewPlan(id, actor string) (any, error) {
	out, err := a.M.ReviewPlan(id, actor)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) RequestRevision(id, actor, note string) (any, error) {
	out, err := a.M.RequestRevision(id, actor, note)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) EditTaskSpec(prdID, taskID, newSpec, actor string) (any, error) {
	out, err := a.M.EditTaskSpec(prdID, taskID, newSpec, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) ResetTask(prdID, taskID, actor string, force bool) (any, error) {
	return a.ResetTaskWithOptions(prdID, taskID, actor, force, false)
}
func (a *API) ResetTaskWithOptions(prdID, taskID, actor string, force, cleanRestart bool) (any, error) {
	out, err := a.M.ResetTaskWithOptions(prdID, taskID, actor, force, cleanRestart)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) RejudgeCompletionGuardrail(id, actor string) (any, error) {
	// Manager.RejudgeCompletionGuardrail records its own verdict and
	// guardrail_rejudge Decision and fires the status event itself, so
	// no EmitPRDUpdate here (a second notify would be a duplicate).
	return a.M.RejudgeCompletionGuardrail(context.Background(), id, actor)
}
func (a *API) RepairDependsOn(prdID, actor string) (any, error) {
	out, err := a.M.RepairDependsOn(prdID, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) CancelStory(prdID, storyID, actor, reason string) (any, error) {
	out, err := a.M.CancelStory(prdID, storyID, actor, reason)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) CancelTask(prdID, taskID, actor, reason string) (any, error) {
	out, err := a.M.CancelTask(prdID, taskID, actor, reason)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) EditStory(prdID, storyID, newTitle, newDescription, actor string) (any, error) {
	out, err := a.M.EditStory(prdID, storyID, newTitle, newDescription, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// Structural edits (operator-requested: add/remove a story or task
// without re-running decompose) — distinct from EditStory/EditTask, which
// only change an existing entry's title/spec/description.
func (a *API) AddStory(prdID, title, description, actor string) (any, error) {
	out, err := a.M.AddStory(prdID, title, description, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) RemoveStory(prdID, storyID, actor string) (any, error) {
	out, err := a.M.RemoveStory(prdID, storyID, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) AddTask(prdID, storyID, title, spec, actor string, dependsOn []string) (any, error) {
	out, err := a.M.AddTask(prdID, storyID, title, spec, actor, dependsOn)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) RemoveTask(prdID, storyID, taskID, actor string) (any, error) {
	out, err := a.M.RemoveTask(prdID, storyID, taskID, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// Phase 3 (v5.26.60) — per-story endpoints.
func (a *API) SetStoryProfile(prdID, storyID, profile, actor string) (any, error) {
	out, err := a.M.SetStoryProfile(prdID, storyID, profile, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) ApproveStory(prdID, storyID, actor string) (any, error) {
	out, err := a.M.ApproveStory(prdID, storyID, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// ApproveTask (BL406 Phase 5) — same shape as ApproveStory, one level down.
func (a *API) ApproveTask(prdID, taskID, actor string) (any, error) {
	out, err := a.M.ApproveTask(prdID, taskID, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) RejectStory(prdID, storyID, actor, reason string) (any, error) {
	out, err := a.M.RejectStory(prdID, storyID, actor, reason)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// Phase 4 (v5.26.64) — file association.
func (a *API) SetStoryFiles(prdID, storyID string, files []string, actor string) (any, error) {
	out, err := a.M.SetStoryFiles(prdID, storyID, files, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetTaskFiles(prdID, taskID string, files []string, actor string) (any, error) {
	out, err := a.M.SetTaskFiles(prdID, taskID, files, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// RecordTaskFilesTouched (operator-reported 2026-10-06) exposes the
// pre-existing Manager.RecordTaskFilesTouched for manual correction. It was
// built (Phase 4, v5.26.64) as a "post-session diff callback" hook but
// never actually wired to a caller — this REST action is for backfilling
// Task.FilesTouched on tasks that completed before that wiring existed
// (the verifier now calls Manager.RecordTaskFilesTouched directly for
// every new task). Deliberately no lock-after-approve gate, matching
// Manager.RecordTaskFilesTouched's own doc comment ("fires after the
// worker session ends") — an operator backfilling a completed PRD's
// stale/missing evidence shouldn't have to reset it to draft first.
func (a *API) RecordTaskFilesTouched(prdID, taskID string, files []string) (any, error) {
	if err := a.M.RecordTaskFilesTouched(prdID, taskID, files); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	updated, _ := a.M.Store().GetPRD(prdID)
	return updated, nil
}
func (a *API) InstantiateTemplate(templateID string, vars map[string]string, actor string) (any, error) {
	newPRD, err := a.M.InstantiateTemplate(templateID, vars, actor)
	if err == nil && newPRD != nil {
		a.M.EmitPRDUpdate(newPRD.ID)
	}
	return newPRD, err
}

// BL381 — per-story LLM override.
func (a *API) SetStoryLLM(prdID, storyID, backend, effort, model, actor string) (any, error) {
	out, err := a.M.SetStoryLLM(prdID, storyID, backend, effort, model, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// BL203 (v5.4.0) — flexible LLM overrides at PRD + task level.
func (a *API) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (any, error) {
	out, err := a.M.SetTaskLLM(prdID, taskID, backend, effort, model, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// ResolveTaskReview is the operator's decision on a task held for review (see
// Manager.ResolveTaskReview). The executor of a held Automaton keeps running
// while other tasks are in flight, so the decision never waits for it: when an
// executor is live it is woken to re-scan and no second one is started;
// otherwise the run is relaunched the way Resume relaunches it.
func (a *API) ResolveTaskReview(prdID, taskID string, req ReviewResolution, actor string) (any, error) {
	out, err := a.M.ResolveTaskReview(prdID, taskID, req, actor)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	a.runMu.Lock()
	_, live := a.runCancels[prdID]
	if live {
		a.reviewWake.signal(prdID)
	}
	a.runMu.Unlock()
	if !live && out.Status == PRDRunning {
		if err := a.Run(prdID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// OnQuestionChange is the API half of the question change hook. When a
// decision is answered it releases the tasks parked on it (the decision gate,
// decision_gate_hold.go). For every running Automaton with a task that names
// the decision: a live executor is woken to re-scan, and no second one is
// started; when no executor is live and a task is parked on it, the run is
// relaunched the way ResolveTaskReview relaunches it. Other change kinds are
// ignored: a withdrawn or expired decision keeps its task parked.
func (a *API) OnQuestionChange(c QuestionChange) {
	if c.Kind != QuestionChangeAnswered || c.Question.ID == "" {
		return
	}
	for _, p := range a.M.Store().ListPRDs() {
		referenced, waiting := a.M.decisionRefTasks(p, c.Question.ID)
		if !referenced {
			continue
		}
		if st, ok := a.prdStatus(p.ID); !ok || st != PRDRunning {
			continue
		}
		a.runMu.Lock()
		_, live := a.runCancels[p.ID]
		if live {
			a.reviewWake.signal(p.ID)
		}
		a.runMu.Unlock()
		if !live && waiting {
			if err := a.Run(p.ID); err != nil {
				log.Printf("[autonomous] prd=%s run after decision %s answered: %v", p.ID, c.Question.ID, err)
			}
		}
	}
}

// SetFailureEscalation sets or clears the failure escalation override (see
// Manager.SetFailureEscalation). An empty taskID addresses the Automaton.
func (a *API) SetFailureEscalation(prdID, taskID string, policy *FailureEscalationPolicy, actor string) (any, error) {
	out, err := a.M.SetFailureEscalation(prdID, taskID, policy, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

func (a *API) SetTaskProjectDir(prdID, taskID, dir, actor string) (any, error) {
	dir, err := a.M.CheckProjectDir(dir)
	if err != nil {
		return nil, fmt.Errorf("project_dir refused: %w", err)
	}
	out, err := a.M.SetTaskProjectDir(prdID, taskID, dir, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetPRDLLM(prdID string, backend, effort, model, decompositionProfile, decompositionModel *string, actor string) (any, error) {
	out, err := a.M.SetPRDLLM(prdID, backend, effort, model, decompositionProfile, decompositionModel, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// BL416 — presence-aware guardrail judge LLM overrides at PRD + task level.
func (a *API) SetPRDGuardrailLLM(prdID string, backend, model *string, actor string) (any, error) {
	out, err := a.M.SetPRDGuardrailLLM(prdID, backend, model, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetTaskGuardrailLLM(prdID, taskID string, backend, model *string, actor string) (any, error) {
	out, err := a.M.SetTaskGuardrailLLM(prdID, taskID, backend, model, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// Presence-aware verifier LLM overrides at task, story and Automaton level.
func (a *API) SetTaskVerificationLLM(prdID, taskID string, backend, model, effort *string, actor string) (any, error) {
	out, err := a.M.SetTaskVerificationLLM(prdID, taskID, backend, model, effort, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetStoryVerificationLLM(prdID, storyID string, backend, model, effort *string, actor string) (any, error) {
	out, err := a.M.SetStoryVerificationLLM(prdID, storyID, backend, model, effort, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetPRDVerificationLLM(prdID string, backend, model, effort *string, actor string) (any, error) {
	out, err := a.M.SetPRDVerificationLLM(prdID, backend, model, effort, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// Presence-aware verifier policy overrides at task and Automaton level.
func (a *API) SetTaskVerifierPolicy(prdID, taskID string, mode *string, backends *[]string, actor string) (any, error) {
	out, err := a.M.SetTaskVerifierPolicy(prdID, taskID, mode, backends, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetPRDVerifierPolicy(prdID string, mode *string, backends *[]string, actor string) (any, error) {
	out, err := a.M.SetPRDVerifierPolicy(prdID, mode, backends, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// SetMemorySeed (BL386 Phase 1 + BL387 Phase 2b) updates the PRD's warm-start seed config.
func (a *API) SetMemorySeed(prdID string, enabled bool, maxPerScope int, roleFilter []string, fromPRDs []string, actor string) (any, error) {
	cfg := MemorySeedConfig{Enabled: enabled, MaxPerScope: maxPerScope, RoleFilter: roleFilter, FromPRDs: fromPRDs}
	out, err := a.M.SetMemorySeed(prdID, cfg, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// SetMemoryHarvest (BL386 Phase 2) updates the PRD's harvest-on-completion config.
func (a *API) SetMemoryHarvest(prdID string, enabled bool, promoteTo string, roleFilter []string, max int, actor string) (any, error) {
	cfg := MemoryHarvestConfig{Enabled: enabled, PromoteTo: promoteTo, RoleFilter: roleFilter, Max: max}
	out, err := a.M.SetMemoryHarvest(prdID, cfg, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// SetPermissionMode (GH#172 D75).
func (a *API) SetPermissionMode(prdID, mode, actor string) (any, error) {
	out, err := a.M.SetPermissionMode(prdID, mode, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// SetPRDThinkingLevel — PRD-level pi --thinking override.
func (a *API) SetPRDThinkingLevel(prdID, level, actor string) (any, error) {
	out, err := a.M.SetPRDThinkingLevel(prdID, level, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// SetStoryThinkingLevel — story-level pi --thinking override.
func (a *API) SetStoryThinkingLevel(prdID, storyID, level, actor string) (any, error) {
	out, err := a.M.SetStoryThinkingLevel(prdID, storyID, level, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// SetTaskThinkingLevel — task-level pi --thinking override.
func (a *API) SetTaskThinkingLevel(prdID, taskID, level, actor string) (any, error) {
	out, err := a.M.SetTaskThinkingLevel(prdID, taskID, level, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

func (a *API) ListLearnings() []any {
	src := a.M.Store().ListLearnings()
	out := make([]any, len(src))
	for i, l := range src {
		out[i] = l
	}
	return out
}

// ListChildPRDs (BL191 Q4, v5.9.0) returns every PRD spawned from this
// PRD's SpawnPRD tasks. Empty list when none — same shape as the root
// list endpoint so PWA / chat clients can render uniformly.
func (a *API) ListChildPRDs(prdID string) []any {
	src := a.M.Store().ListChildPRDs(prdID)
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = p
	}
	return out
}

// DeletePRD (v5.19.0) hard-removes a PRD + its SpawnPRD descendants.
// The operator-facing "remove from list" affordance.
func (a *API) DeletePRD(id string) error {
	// v5.26.16 — stop the executor goroutine before mutating store.
	// Manager.DeletePRD will refuse if status==PRDRunning, but the
	// executor goroutine for an already-cancelled PRD might still be
	// finishing up a spawn round-trip. Cancelling forces the loop to
	// bail on next iteration.
	a.cancelRun(id)
	if err := a.M.DeletePRD(id); err != nil {
		return err
	}
	// v5.24.0 — broadcast deletion so PWA drops the row. EmitPRDUpdate
	// marks the event Deleted when the PRD no longer loads.
	a.M.EmitPRDUpdate(id)
	return nil
}

// EditPRDFields (v5.19.0) edits the PRD-level Title + Spec on a non-
// running PRD. Records a Decision audit row. Empty title/spec leaves
// that field unchanged.
func (a *API) EditPRDFields(id, title, spec, actor string) (any, error) {
	out, err := a.M.EditPRDFields(id, title, spec, actor)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// SetPRDProfiles (v5.26.19) attaches F10 project + cluster profiles
// to an existing PRD. Either or both can be empty to leave the
// existing value untouched (use empty string explicitly to clear via
// the future patch endpoint). Manager.SetPRDProfiles validates the
// referenced profiles exist before persisting.
func (a *API) SetPRDProfiles(prdID, projectProfile, clusterProfile string) error {
	if err := a.M.SetPRDProfiles(prdID, projectProfile, clusterProfile); err != nil {
		return err
	}
	a.M.EmitPRDUpdate(prdID)
	return nil
}

// BL221 (v6.2.0) — TemplateStore CRUD wrappers.

func (a *API) ListTemplates() []any {
	src := a.M.Templates().List()
	out := make([]any, len(src))
	for i, t := range src {
		out[i] = t
	}
	return out
}

func (a *API) CreateTemplate(title, description, spec, typ string, tags []string) (any, error) {
	return a.M.Templates().Create(title, description, spec, typ, tags)
}

func (a *API) GetTemplate(id string) (any, bool) {
	return a.M.Templates().Get(id)
}

func (a *API) UpdateTemplate(id, title, description, spec, typ string, tags []string) (any, error) {
	return a.M.Templates().Update(id, title, description, spec, typ, tags, nil)
}

func (a *API) DeleteTemplate(id string) error {
	return a.M.Templates().Delete(id)
}

func (a *API) CloneToTemplate(prdID, description, actor string) (any, error) {
	return a.M.CloneToTemplate(prdID, description, actor)
}

func (a *API) InstantiateFromTemplateStore(templateID string, vars map[string]string, projectDir, backend, effort string) (any, error) {
	projectDir, err := a.M.CheckProjectDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("project_dir refused: %w", err)
	}
	prd, err := a.M.InstantiateFromTemplateStore(templateID, vars, projectDir, backend, Effort(effort))
	if err == nil && prd != nil {
		a.M.EmitPRDUpdate(prd.ID)
	}
	return prd, err
}

// ── BL221 (v6.2.0) Phase 3 — scan API wrappers ───────────────────────────

func (a *API) RunScan(prdID string) (any, error) {
	return a.M.RunScan(prdID)
}

func (a *API) GetScanResult(prdID string) (any, bool) {
	r, ok := a.M.GetScanResult(prdID)
	return r, ok
}

func (a *API) GetScanConfig() any {
	return a.M.ScanConfig()
}

func (a *API) SetScanConfig(body map[string]any) error {
	sc := a.M.ScanConfig()
	if v, ok := body["enabled"].(bool); ok {
		sc.Enabled = v
	}
	if v, ok := body["sast_enabled"].(bool); ok {
		sc.SASTEnabled = v
	}
	if v, ok := body["secrets_enabled"].(bool); ok {
		sc.SecretsEnabled = v
	}
	if v, ok := body["deps_enabled"].(bool); ok {
		sc.DepsEnabled = v
	}
	if v, ok := body["rules_grader_enabled"].(bool); ok {
		sc.RulesGraderEnabled = v
	}
	if v, ok := body["fix_loop_enabled"].(bool); ok {
		sc.FixLoopEnabled = v
	}
	if v, ok := body["fail_on_severity"].(string); ok {
		sc.FailOnSeverity = scanPkg.Severity(v)
	}
	if v, ok := body["max_findings"].(float64); ok {
		sc.MaxFindings = int(v)
	}
	if v, ok := body["fix_loop_max_retries"].(float64); ok {
		sc.FixLoopMaxRetries = int(v)
	}
	// BL406 — project_rules is a full-replace, not a merge: an
	// operator managing rules via a dedicated CRUD surface (Phase 1)
	// should never have this generic config-patch endpoint silently
	// resurrect a deleted rule. Omitting the key entirely leaves the
	// existing list untouched (same semantics as every other field
	// above, which only overwrites when present).
	if raw, ok := body["project_rules"].([]any); ok {
		rules := make([]scanPkg.ProjectRule, 0, len(raw))
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			r := scanPkg.ProjectRule{}
			if v, ok := m["id"].(string); ok {
				r.ID = v
			}
			if v, ok := m["name"].(string); ok {
				r.Name = v
			}
			if v, ok := m["type"].(string); ok {
				r.Type = scanPkg.RuleType(v)
			}
			if v, ok := m["granularity"].(string); ok {
				r.Granularity = scanPkg.RuleGranularity(v)
			}
			if v, ok := m["pattern"].(string); ok {
				r.Pattern = v
			}
			if v, ok := m["severity"].(string); ok {
				r.Severity = scanPkg.Severity(v)
			}
			if v, ok := m["action"].(string); ok {
				r.Action = scanPkg.RuleAction(v)
			}
			if v, ok := m["upstream_target"].(string); ok {
				r.UpstreamTarget = v
			}
			rules = append(rules, r)
		}
		sc.ProjectRules = rules
	}
	a.M.SetScanConfig(sc)
	return nil
}

func (a *API) CreateFixPRD(prdID string) (any, error) {
	prd, err := a.M.CreateFixPRD(prdID)
	if err == nil && prd != nil {
		a.M.EmitPRDUpdate(prd.ID)
	}
	return prd, err
}

func (a *API) ProposeRuleEdits(prdID string) (any, error) {
	diff, err := a.M.ProposeRuleEdits(prdID)
	return map[string]string{"proposed_diff": diff}, err
}

// ── BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills ──────────

func (a *API) ListAutomatonTypes() []any {
	src := a.M.ListTypes()
	out := make([]any, len(src))
	for i, t := range src {
		out[i] = t
	}
	return out
}

func (a *API) RegisterAutomatonType(id, label, description, color string) (any, error) {
	t := AutomatonType{ID: id, Label: label, Description: description, Color: color}
	return t, a.M.RegisterType(t)
}

func (a *API) SetPRDType(prdID, typ string) (any, error) {
	if err := a.M.SetPRDType(prdID, typ); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

func (a *API) SetPRDGuidedMode(prdID string, guided bool) (any, error) {
	if err := a.M.SetPRDGuidedMode(prdID, guided); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

func (a *API) SetPRDSkills(prdID string, skills []string) (any, error) {
	if err := a.M.SetPRDSkills(prdID, skills); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// SetPRDConcurrency (BL370) sets per-PRD max_concurrent_tasks override.
func (a *API) SetPRDConcurrency(prdID string, n int) (any, error) {
	if err := a.M.SetPRDConcurrency(prdID, n); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// SetPRDPriority sets the PRD's capacity-queue priority.
func (a *API) SetPRDPriority(prdID string, n int) (any, error) {
	if err := a.M.SetPRDPriority(prdID, n); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// SetPRDKind sets the explicit kind (feature, bug or other) of an Automaton;
// an empty kind clears it.
func (a *API) SetPRDKind(prdID, kind string) (any, error) {
	if err := a.M.SetPRDKind(prdID, kind); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// SetPRDLanePreference sets which kind of lane suits the Automaton: gpu,
// claude or any.
func (a *API) SetPRDLanePreference(prdID, pref string) (any, error) {
	if err := a.M.SetPRDLanePreference(prdID, pref); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// LaneFillStatus returns the lane fill status with up to limit decisions.
func (a *API) LaneFillStatus(limit int) LaneFillStatus {
	return a.M.LaneFillStatus(limit)
}

// SetPRDDirs sets per-PRD read-only/writable directory scope.
func (a *API) SetPRDDirs(prdID string, readDirs, writeDirs []string) (any, error) {
	if err := a.M.SetPRDDirs(prdID, readDirs, writeDirs); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// SetPRDQualityGates (BL367) sets per-PRD quality gate config.
func (a *API) SetPRDQualityGates(prdID string, enabled bool, testCommand string, timeout int, blockOnRegression bool) (any, error) {
	prd, err := a.M.SetPRDQualityGates(prdID, enabled, testCommand, timeout, blockOnRegression)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return prd, nil
}

// SetPRDContinueOnStoryFailure sets the per-PRD override for whether the
// executor continues into later, independent stories after one story
// fails (default: halt).
func (a *API) SetPRDContinueOnStoryFailure(prdID string, continueOnFailure bool) (any, error) {
	prd, err := a.M.SetPRDContinueOnStoryFailure(prdID, continueOnFailure)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return prd, nil
}

// SetPRDNotifyEvents sets the per-Automaton notify_events list (the
// lifecycle event kinds that produce notifications for this Automaton).
// A nil or empty list clears the field (inherit from the next tier).
// An invalid entry returns the validation error unchanged so the caller
// can show the operator the valid kinds.
func (a *API) SetPRDNotifyEvents(prdID string, kinds []string) (any, error) {
	prd, err := a.M.SetPRDNotifyEvents(prdID, kinds)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	// Return the updated Automaton with its effective notify-events view
	// filled (the stored object itself never carries it).
	return a.M.PRDWithNotifyEffective(prd), nil
}

// SetPRDGit (BL407 Phase 5) sets the per-PRD Git.AutoPR/BaseBranch overrides.
func (a *API) SetPRDGit(prdID string, autoPR bool, baseBranch string) (any, error) {
	prd, err := a.M.SetPRDGit(prdID, autoPR, baseBranch)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return prd, nil
}

// ── BL303 S2 — guardrail library + profiles + per-Automaton override ──────

func (a *API) GuardrailLibrary() []any {
	src := a.M.GuardrailLibrary()
	out := make([]any, len(src))
	for i, e := range src {
		out[i] = e
	}
	return out
}

func (a *API) CreateGuardrailProfile(name, description string, guardrails []string, blockOnFileDrift *bool, notifyEvents []string) (any, error) {
	return a.M.CreateGuardrailProfile(name, description, guardrails, blockOnFileDrift, notifyEvents)
}

func (a *API) UpdateGuardrailProfile(id, name, description string, guardrails []string, blockOnFileDrift *bool, notifyEvents []string) (any, error) {
	return a.M.UpdateGuardrailProfile(id, name, description, guardrails, blockOnFileDrift, notifyEvents)
}

func (a *API) DeleteGuardrailProfile(id string) error {
	return a.M.DeleteGuardrailProfile(id)
}

func (a *API) ListGuardrailProfiles() []any {
	src := a.M.ListGuardrailProfiles()
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = p
	}
	return out
}

func (a *API) GetGuardrailProfile(id string) (any, bool) {
	p, ok := a.M.GetGuardrailProfile(id)
	if !ok {
		return nil, false
	}
	return p, true
}

func (a *API) SetPRDGuardrails(prdID, profile string, perTask, perStory, perPRD []string, blockOnFileDrift *bool) (any, error) {
	prd, err := a.M.SetPRDGuardrails(prdID, profile, perTask, perStory, perPRD, blockOnFileDrift)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return prd, nil
}

// BL410 Phase 1 — retry profiles + per-Automaton retry-policy override.

func (a *API) CreateRetryProfile(name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (any, error) {
	return a.M.CreateRetryProfile(name, description, budgets, ceilingTotal, infraRetryEscalateAfter, contentAutoFixEnabled, autoFixSkipApproval)
}

func (a *API) UpdateRetryProfile(id, name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (any, error) {
	return a.M.UpdateRetryProfile(id, name, description, budgets, ceilingTotal, infraRetryEscalateAfter, contentAutoFixEnabled, autoFixSkipApproval)
}

func (a *API) DeleteRetryProfile(id string) error {
	return a.M.DeleteRetryProfile(id)
}

func (a *API) ListRetryProfiles() []any {
	src := a.M.ListRetryProfiles()
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = p
	}
	return out
}

func (a *API) GetRetryProfile(id string) (any, bool) {
	p, ok := a.M.GetRetryProfile(id)
	if !ok {
		return nil, false
	}
	return p, true
}

// Automation records (data model + CRUD only).

func (a *API) CreateAutomation(in AutomationInput, actor string) (any, error) {
	return a.M.CreateAutomation(in, actor)
}

// ImportGraph converts an orchestrator graph into an Automation draft (or, with
// dryRun, the input a real run would use).
func (a *API) ImportGraph(src ImportGraphSource, dryRun bool) (*ImportGraphResult, error) {
	return a.M.ImportGraph(src, dryRun)
}

func (a *API) UpdateAutomation(id string, expectedVersion int64, in AutomationInput, actor string) (any, error) {
	return a.M.UpdateAutomation(id, expectedVersion, in, actor)
}

func (a *API) DeleteAutomation(id string) error {
	return a.M.DeleteAutomation(id)
}

func (a *API) ListAutomations() []any {
	src := a.M.ListAutomations()
	out := make([]any, len(src))
	for i, x := range src {
		out[i] = a.M.AutomationViewOf(x)
	}
	return out
}

// ListTriageRecords + TriageAlertMinutes — the triage read surface.
// The server's GET /api/autonomous/triage route (and the CLI and MCP
// triage on top of it) resolve these through the interface the daemon
// wires this API as, so the Manager's triage ledger must be reachable
// here — without these two methods the route falls back to its
// 503 "not wired" answer even in a running daemon.
func (a *API) ListTriageRecords() []*TriageRecord {
	return a.M.ListTriageRecords()
}
func (a *API) TriageAlertMinutes() int { return a.M.TriageAlertMinutes() }

// MergeStatus + MergeSafeToDelete — the Automaton merge read surface behind
// GET /api/autonomous/merge and GET /api/autonomous/safe-to-delete. Both only
// read: the status comes from the Coordinator's in-memory cache and the report
// runs read-only git queries.
func (a *API) MergeStatus() MergeStatus { return a.M.MergeStatus() }
func (a *API) MergeSafeToDelete(ctx context.Context) (SafeDeleteReport, error) {
	return a.M.MergeSafeToDelete(ctx)
}

// CleanupStatus — the automation cleanup read surface behind
// GET /api/autonomous/cleanup (and the MCP, CLI and comm reads on top of it).
// Without it the route's interface check fails and the route answers 503 in
// a running daemon.
func (a *API) CleanupStatus() CleanupStatus { return a.M.CleanupStatus() }

func (a *API) GetAutomation(id string) (any, bool) {
	got, ok := a.M.GetAutomation(id)
	if !ok {
		return nil, false
	}
	return a.M.AutomationViewOf(got), true
}

// ActivateAutomation starts a draft Automation (in-process; never via REST).
func (a *API) ActivateAutomation(id, actor string, expectedVersion int64) (any, error) {
	return a.M.ActivateAutomation(id, actor, expectedVersion)
}

func (a *API) SetPRDRetryPolicy(prdID, profile string, policy map[string]int, ceilingTotal int, contentAutoFixEnabled, autoFixSkipApproval *bool) (any, error) {
	prd, err := a.M.SetPRDRetryPolicy(prdID, profile, policy, ceilingTotal, contentAutoFixEnabled, autoFixSkipApproval)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return prd, nil
}

// InvokeGuardrailByName (BL303 S3 T15) — on-demand invocation.
func (a *API) InvokeGuardrailByName(name, projectDir string) (any, error) {
	verdict, err := a.M.InvokeGuardrailByName(name, projectDir)
	if err != nil {
		return nil, err
	}
	return verdict, nil
}

// Automation control operations (in-process adapters for the REST handlers).
// Each returns the updated Automation as any, or a nil any with the error;
// a typed-nil *Automation must never reach the caller as a non-nil any.

// PauseAutomation moves an active Automation to paused.
func (a *API) PauseAutomation(id, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.PauseAutomation(id, actor, expectedVersion)
	return controlResult(got, err)
}

// SetAutomationLLM overrides the Automation's execution and planning defaults.
// A nil pointer leaves a field unchanged; expectedVersion 0 skips the version check.
func (a *API) SetAutomationLLM(id string, backend, model, effort, planningBackend, planningModel *string, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.SetAutomationLLM(id, backend, model, effort, planningBackend, planningModel, actor, expectedVersion)
	return controlResult(got, err)
}

// SetAutomationVerificationLLM overrides the Automation's verification defaults.
func (a *API) SetAutomationVerificationLLM(id string, backend, model, effort *string, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.SetAutomationVerificationLLM(id, backend, model, effort, actor, expectedVersion)
	return controlResult(got, err)
}

// SetAutomationDecisionAssistant sets or clears (nil) the decision assistant of
// an Automation (memberID "") or of one member.
func (a *API) SetAutomationDecisionAssistant(id, memberID, actor string, da *DecisionAssistant, expectedVersion int64) (any, error) {
	got, err := a.M.SetDecisionAssistant(id, memberID, actor, da, expectedVersion)
	return controlResult(got, err)
}

// ResumeAutomation moves a paused Automation back to active.
func (a *API) ResumeAutomation(id, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.ResumeAutomation(id, actor, expectedVersion)
	return controlResult(got, err)
}

// CancelAutomation cancels an Automation. hasChoice reports whether the caller
// supplied cancelInFlight; without it the Manager refuses the cancel.
func (a *API) CancelAutomation(id, actor string, cancelInFlight, hasChoice bool, expectedVersion int64) (any, error) {
	got, err := a.M.CancelAutomation(id, actor, cancelInFlight, hasChoice, expectedVersion)
	return controlResult(got, err)
}

// SkipMember skips an active member along the named edge.
func (a *API) SkipMember(id, memberID, edge, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.SkipMember(id, memberID, edge, actor, expectedVersion)
	return controlResult(got, err)
}

// RemoveMember removes a member that has not started and has no outgoing edges.
func (a *API) RemoveMember(id, memberID, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.RemoveMember(id, memberID, actor, expectedVersion)
	return controlResult(got, err)
}

// ReorderEdges rewires the outgoing edges of a member that has not started.
func (a *API) ReorderEdges(id, memberID, onCompleted, onBlocked, onFailed, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.ReorderEdges(id, memberID, onCompleted, onBlocked, onFailed, actor, expectedVersion)
	return controlResult(got, err)
}

// InsertMember inserts a PRD member into an Automation.
func (a *API) InsertMember(id string, in InsertMemberInput) (any, error) {
	got, err := a.M.InsertMember(id, in)
	return controlResult(got, err)
}

// ApproveInsertedMember promotes a pending insert into the graph.
func (a *API) ApproveInsertedMember(id, memberID, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.ApproveInsertedMember(id, memberID, actor, expectedVersion)
	return controlResult(got, err)
}

// SetAutomationBudgetPolicy sets or clears the budget policy of an Automation
// or one member. The result is the updated record with its computed budget view.
func (a *API) SetAutomationBudgetPolicy(id string, req BudgetSetRequest, actor string, expectedVersion int64) (any, error) {
	got, err := a.M.SetAutomationBudgetPolicy(id, req, actor, expectedVersion)
	if got == nil {
		return nil, err
	}
	return a.M.AutomationViewOf(got), err
}

// controlResult widens a control operation's result. A non-nil Automation is
// returned even alongside an error: a cancel whose in-flight session ends
// failed still leaves the Automation cancelled.
func controlResult(a *Automation, err error) (any, error) {
	if a == nil {
		return nil, err
	}
	return a, err
}

// ProgressSource returns the record source the progress metrics count: the
// manager's store.
func (a *API) ProgressSource() ProgressSource { return a.M.Store() }

// ProgressSnapshots returns the snapshot store behind the trend view, or nil
// when snapshots are not enabled.
func (a *API) ProgressSnapshots() *ProgressSnapshotStore { return a.M.ProgressSnapshotStore() }

// ListRepoCIWatchRecords returns the repo CI watch records currently
// stored, newest first — the whole list for a repository with a clean
// head is empty.
func (a *API) ListRepoCIWatchRecords() []*RepoCIWatchRecord {
	return a.M.ListRepoCIWatchRecords()
}

// RepoCIWatchStatus is the repo CI watch read view in force: the mode
// (off, shadow or on), the configured repository paths, the alert
// threshold in whole minutes (0 while the watch is off), the instant
// the coordinator last polled (the zero time when it never has) and the
// stored records, newest first. Serves GET /api/autonomous/repo-ci-watch.
func (a *API) RepoCIWatchStatus() RepoCIWatchStatus {
	return a.M.RepoCIWatchStatus()
}

// SupervisorWatchStatus is the supervisor watch read view in force: the
// mode (off, shadow or on — an unset mode reads shadow), the instant
// the coordinator last polled (the zero time when it never has), the
// stored findings (open first, most recently seen first) and the
// comparison view (last comparison, days of full agreement, the last
// comparison's one-sided differences). Serves
// GET /api/autonomous/supervisor-watch.
func (a *API) SupervisorWatchStatus() SupervisorWatchStatus {
	return a.M.SupervisorWatchStatus()
}

// SupervisorWatchStats is the numeric monitoring view of the supervisor
// watch (mode, poll age, finding counts, days of full agreement).
// Serves GET /api/autonomous/supervisor-watch/stats.
func (a *API) SupervisorWatchStats() SupervisorWatchStats {
	return a.M.SupervisorWatchStatus().Stats(time.Now())
}
