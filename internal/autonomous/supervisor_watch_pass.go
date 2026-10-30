// The supervisor watch pass: one coordinator tick that runs the host,
// work, and worktree supervisor watch checks without a model call,
// upserts their findings into the shared finding store, resolves the
// findings that no longer match, acts on the alert threshold per mode,
// and records first sightings on the owning Automaton's
// triage record.
//
// The pass is report-only in every mode it ships in: shadow (the
// release default) records what it would have raised — the finding
// gains WouldAlert with a timestamp, and the alert sink is never
// called — and this step of the watch in "on" mode raises the alert
// for real, once per episode, once the finding has been open for the
// configured threshold (AlertedAt records it; a re-sighting after a
// resolve is a new episode that may alert again). A check never acts
// by design — the probes read, the store records, the alert informs
// (see supervisor_watch_host.go).

package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// supervisorWatchMinPollInterval — the minimum time between two
// supervisor watch passes. The idle loop ticks every minute; the
// re-reads the pass does (tmux unit, daemon states, worktrees) are
// expensive next to that, so a pass that just ran is skipped. The guard
// is the Coordinator's own last-poll instant, the same shape as the
// repo CI watch's (repoCIWatchLastPoll).
const supervisorWatchMinPollInterval = time.Minute

// supervisorWatchTriageSource names the triage-record annotations the
// pass appends, so a later reader can tell them apart from the other
// sources' notes.
const supervisorWatchTriageSource = "supervisor_watch"

// supervisorWatchAlert is one due alert, ready for the Coordinator's
// alert sink: the level, the one-line title, and the multi-line body.
type supervisorWatchAlert struct {
	level string
	title string
	body  string
}

// supervisorWatchPass is one tick of the supervisor watch. It reads the
// effective mode first — off (never the default: unset reads shadow)
// does nothing at all, before any probe is touched. Otherwise it runs
// the poll once per poll interval and raises the due alerts through the
// Coordinator's alert sink.
func (c *Coordinator) supervisorWatchPass() {
	if c == nil || c.m == nil {
		return
	}
	cfg := c.m.Config().Automation.SupervisorWatch
	if cfg.EffectiveMode() == SupervisorWatchModeOff {
		return
	}

	now := c.now()
	c.mu.Lock()
	if !c.supervisorWatchLastPoll.IsZero() && now.Sub(c.supervisorWatchLastPoll) < supervisorWatchMinPollInterval {
		c.mu.Unlock()
		return
	}
	c.supervisorWatchLastPoll = now
	c.mu.Unlock()

	for _, a := range c.m.supervisorWatchPoll(now, cfg) {
		c.alert(a.level, a.title, a.body)
	}
}

// SupervisorWatchLastPoll returns the instant of the last supervisor
// watch pass (zero if none has run), for inspection and tests.
func (c *Coordinator) SupervisorWatchLastPoll() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.supervisorWatchLastPoll
}

// --- manager side ------------------------------------------------------

// supervisorWatchPRDs returns the PRDs the pass reads its work and
// worktree checks from: the store's list, newest not guaranteed (the
// checks sort what they need). A nil manager or store reads as an empty
// list — with no Automata there is nothing work-scoped to check, and
// the host checks (no Automaton involved) still run.
func (m *Manager) supervisorWatchPRDs() []*PRD {
	if m == nil || m.store == nil {
		return nil
	}
	out := m.store.ListPRDs()
	out = append([]*PRD(nil), out...) // copy: the pass must not alias store slices
	return out
}

// supervisorWatchRawFindings runs the checks and returns their findings
// sorted for deterministic recording. The test seam
// (supervisorWatchFindingsFn) replaces the whole check run with a fixed
// set of findings, so the pass's own bookkeeping is testable without
// shelling out; the shell probes' behavior is the probes' own tests'
// job.
func (m *Manager) supervisorWatchRawFindings(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
	m.mu.Lock()
	probe := m.supervisorWatchProbe
	nodes := m.supervisorWatchNodes
	baseRepo := m.cfg.WorktreeBaseRepo
	sess := m.supervisorWatchSess
	findingsFn := m.supervisorWatchFindings
	m.mu.Unlock()

	if findingsFn != nil {
		return supervisorWatchSorted(findingsFn(cfg, prds))
	}

	var out []SupervisorWatchFinding
	host := NewSupervisorWatchHost(probe, cfg, nodes)
	host.tmuxBaseline = m.supervisorWatchTmuxSeed(cfg)
	out = append(out, host.CheckTmuxRestart()...)
	m.supervisorWatchTmuxStore(host.StoredTmuxBaseline())
	out = append(out, host.CheckDaemonInactive()...)
	out = append(out, host.CheckModelNotResident()...)
	out = append(out, host.CheckVerifyScriptNotExecutable(SupervisorWatchRunningUnits(prds))...)
	out = append(out, host.CheckMainCheckoutDirty()...)

	work := NewSupervisorWatchWork(
		supervisorWatchPRDLister{prds: prds},
		supervisorWatchLaneFill{m: m},
		supervisorWatchCISnapshot{recs: m.supervisorWatchCIRecords()},
	)
	out = append(out, work.CheckTaskFailed()...)
	out = append(out, work.CheckTaskReviewNeeded()...)
	out = append(out, work.CheckAutomatonNotMerged()...)
	out = append(out, work.CheckIdleLane()...)
	out = append(out, work.CheckCIRed()...)
	out = append(out, work.CheckCISTuck()...)

	if baseRepo != "" {
		wt := NewSupervisorWatchWorktree(
			NewShellSupervisorWatchWorktreeFS(),
			nil, // real clock
			sess,
			cfg,
			baseRepo,
		)
		out = append(out, wt.CheckNoGomod(prds)...)
		out = append(out, wt.CheckNotRegistered(prds)...)
		out = append(out, wt.CheckSilent(prds)...)
	}

	return supervisorWatchSorted(out)
}

// supervisorWatchPRDLister is the *PRD list view the work checks read.
type supervisorWatchPRDLister struct{ prds []*PRD }

func (a supervisorWatchPRDLister) PRDs() []*PRD { return a.prds }

// supervisorWatchLaneFill is the lane fill status view the work checks
// read: the manager's own lane fill status. A manager without a running
// cooldown coordinator — or with lane fill off — reads as no idle lanes,
// so the idle-lane check finds nothing either way.
type supervisorWatchLaneFill struct{ m *Manager }

func (a supervisorWatchLaneFill) LaneFillStat() (LaneFillStatus, bool) {
	if a.m == nil {
		return LaneFillStatus{}, false
	}
	return a.m.LaneFillStatus(0), true
}

// supervisorWatchCISnapshot is the repo CI watch record view the work
// checks read.
type supervisorWatchCISnapshot struct{ recs []*RepoCIWatchRecord }

func (a supervisorWatchCISnapshot) CIWatchRecords() []*RepoCIWatchRecord { return a.recs }

// supervisorWatchCIRecords lists the repo CI watch records the work
// checks read; a nil manager or store reads as none.
func (m *Manager) supervisorWatchCIRecords() []*RepoCIWatchRecord {
	if m == nil || m.repoCIWatch == nil {
		return nil
	}
	recs := m.repoCIWatch.List()
	out := make([]*RepoCIWatchRecord, 0, len(recs))
	out = append(out, recs...)
	return out
}

// supervisorWatchSorted orders the findings deterministically (kind,
// then subject) so recording, notes, and alerts do not depend on check
// order or map iteration.
func supervisorWatchSorted(in []SupervisorWatchFinding) []SupervisorWatchFinding {
	out := make([]SupervisorWatchFinding, 0, len(in))
	for i := range in {
		f := in[i]
		if f.Kind == "" || strings.TrimSpace(f.Subject) == "" {
			continue // malformed findings are dropped, never minted
		}
		out = append(out, f)
	}
	sortSupervisorWatchFindings(out)
	return out
}

func sortSupervisorWatchFindings(out []SupervisorWatchFinding) {
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && supervisorWatchFindingLess(&out[j], &out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
}

func supervisorWatchFindingLess(a, b *SupervisorWatchFinding) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Subject < b.Subject
}

// supervisorWatchPoll is one full pass: record the fresh findings,
// resolve the stale ones, then work the alert threshold by mode —
// shadow marks each due finding's stored record WouldAlert with a
// timestamp and returns no alerts (the Coordinator calls the alert
// sink zero times), on returns the due alerts (one per episode). It
// returns the alerts that fell due this pass; the Coordinator raises
// them (shadow: none).
func (m *Manager) supervisorWatchPoll(now time.Time, cfg SupervisorWatchConfig) []supervisorWatchAlert {
	store := m.supervisorWatchStore()
	if store == nil {
		return nil
	}
	prds := m.supervisorWatchPRDs()
	raw := m.supervisorWatchRawFindings(cfg, prds)
	// Cross-check this pass's findings against the oracle script's
	// record file and record the comparison in the same store
	// (supervisor_watch_compare.go). A missing, stale or unparseable
	// record file records nothing — "script output unavailable" is not
	// a disagreement.
	m.supervisorWatchCompare(cfg, raw, now)

	// The open set before this pass: which findings already had a
	// record, and whether that record was already resolved.
	open := make(map[string]*SupervisorWatchFinding, len(raw))
	for _, f := range m.supervisorWatchList() {
		open[f.Key()] = f
	}

	// Record (or re-raise) every fresh finding; a fresh observation
	// re-opens a resolved record in place (store semantics), so a fixed-
	// and-broken-again finding keeps its FirstSeenAt history.
	observed := make(map[string]bool, len(raw))
	for _, f := range raw {
		key := f.Key()
		observed[key] = true
		_, existed := open[key]
		rec, err := m.SaveSupervisorWatchFinding(&f)
		if err != nil {
			continue
		}
		if !existed && supervisorWatchAutomatonScoped(f.Kind) {
			m.supervisorWatchTriageNote(prds, rec, now)
		}
	}

	// Resolve the findings that no longer match: the check no longer
	// reports them, so the record closes. A later re-sighting re-opens it.
	for key := range open {
		if observed[key] {
			continue
		}
		if err := m.ResolveSupervisorWatchFinding(key); err != nil {
			continue
		}
		m.supervisorWatchForgetAlert(key)
	}

	// Work the alert threshold by mode. Both modes skip resolved
	// findings (closing the episode also clears its alert) and the
	// explicit alert_minutes 0 off-switch.
	//
	// Shadow: record what would have been raised — on the finding's
	// stored record set WouldAlert and stamp WouldAlertAt — and return
	// no alerts. The Coordinator therefore calls the alert sink zero
	// times in shadow (distinct from off: off records nothing at all).
	// On: raise for real, at most one alert per episode once the
	// finding is older than the threshold; AlertedAt records it, and a
	// re-sighting after a resolve is a new episode that may alert
	// again.
	mode := cfg.EffectiveMode()
	alertMinutes := cfg.EffectiveAlertMinutes()
	var due []supervisorWatchAlert
	for _, rec := range m.supervisorWatchList() {
		if rec == nil || rec.Resolved {
			continue
		}
		if alertMinutes <= 0 {
			continue
		}
		if rec.FirstSeenAt.IsZero() || now.Sub(rec.FirstSeenAt) < time.Duration(alertMinutes)*time.Minute {
			continue
		}
		if mode == SupervisorWatchModeShadow {
			if rec.WouldAlert {
				continue // already marked this episode
			}
			rec.WouldAlert = true
			rec.WouldAlertAt = now
			// Best effort: a failed save leaves the mark off, so the
			// next pass marks it again.
			_, _ = m.SaveSupervisorWatchFinding(rec)
			continue
		}
		if rec.AlertedAt != nil {
			continue
		}
		key := rec.Key()
		if !m.supervisorWatchTakeAlert(key) {
			continue
		}
		alertedAt := now
		rec.AlertedAt = &alertedAt
		if _, err := m.SaveSupervisorWatchFinding(rec); err != nil {
			// Persisting the alert failed; let the next pass retry the
			// alert instead of dropping it silently.
			m.supervisorWatchForgetAlert(key)
			continue
		}
		due = append(due, supervisorWatchAlert{
			level: "warn",
			title: supervisorWatchAlertTitle(rec),
			body:  supervisorWatchAlertBody(rec, mode, now),
		})
	}
	return due
}

// supervisorWatchTmuxSeed returns the tmux_restart baseline the next poll
// starts from: the one the previous poll stored, or the configured one when
// it is the first poll or the configured value changed since (an operator
// who sets or clears tmux_restart_baseline reseeds the check).
func (m *Manager) supervisorWatchTmuxSeed(cfg SupervisorWatchConfig) string {
	cfgBaseline := strings.TrimSpace(cfg.TmuxRestartBaseline)
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfgBaseline != m.supervisorWatchTmuxCfg {
		m.supervisorWatchTmuxCfg = cfgBaseline
		m.supervisorWatchTmuxBaseline = cfgBaseline
	}
	return m.supervisorWatchTmuxBaseline
}

// supervisorWatchTmuxStore remembers the baseline the poll ended with, so a
// restart seen after it raises and the same state does not raise again.
func (m *Manager) supervisorWatchTmuxStore(baseline string) {
	m.mu.Lock()
	m.supervisorWatchTmuxBaseline = baseline
	m.mu.Unlock()
}

// supervisorWatchStore returns the shared finding store.
func (m *Manager) supervisorWatchStore() *SupervisorWatchStore {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.supervisorWatch
}

// supervisorWatchList lists the store's findings (open first), for
// pass-side bookkeeping.
func (m *Manager) supervisorWatchList() []*SupervisorWatchFinding {
	st := m.supervisorWatchStore()
	if st == nil {
		return nil
	}
	return st.List()
}

// SaveSupervisorWatchFinding upserts one finding into the store and
// returns a copy of the recorded state.
func (m *Manager) SaveSupervisorWatchFinding(f *SupervisorWatchFinding) (*SupervisorWatchFinding, error) {
	st := m.supervisorWatchStore()
	if st == nil {
		return nil, errors.New("supervisor watch: no store")
	}
	return st.Upsert(f)
}

// ResolveSupervisorWatchFinding closes the stored finding under its
// kind+subject key.
func (m *Manager) ResolveSupervisorWatchFinding(key string) error {
	st := m.supervisorWatchStore()
	if st == nil {
		return errors.New("supervisor watch: no store")
	}
	_, err := st.Resolve(key)
	return err
}

// SupervisorWatchResetAlerts clears the in-episode alert dedup, so the
// next pass may alert every due finding again. Tests use it between
// episodes; production never does (a restart re-derives dedup from the
// store's AlertedAt, which survives).
func (m *Manager) SupervisorWatchResetAlerts() {
	if m == nil {
		return
	}
	m.supervisorWatchAlertMu.Lock()
	m.supervisorWatchAlerted = map[string]bool{}
	m.supervisorWatchAlertMu.Unlock()
}

// supervisorWatchTakeAlert claims the episode's one alert: true the
// first time a key claims it, false from the second, the same
// per-episode dedupe the lane fill and repo CI watch alerts use.
func (m *Manager) supervisorWatchTakeAlert(key string) bool {
	m.supervisorWatchAlertMu.Lock()
	defer m.supervisorWatchAlertMu.Unlock()
	if m.supervisorWatchAlerted[key] {
		return false
	}
	m.supervisorWatchAlerted[key] = true
	return true
}

func (m *Manager) supervisorWatchForgetAlert(key string) {
	m.supervisorWatchAlertMu.Lock()
	delete(m.supervisorWatchAlerted, key)
	m.supervisorWatchAlertMu.Unlock()
}

// supervisorWatchAutomatonScoped reports whether the finding is about
// one named Automaton (its subject is the Automaton's id), so that the
// pass's first-sighting note lands on that Automaton's triage record.
// Fleet findings (tmux, daemons, lanes, CI runs, the main checkout)
// have no owning Automaton.
func supervisorWatchAutomatonScoped(kind SupervisorWatchKind) bool {
	switch kind {
	case SupervisorWatchTaskFailed,
		SupervisorWatchTaskReviewNeeded,
		SupervisorWatchAutomatonNotMerged,
		SupervisorWatchWorktreeNoGomod,
		SupervisorWatchWorktreeNotRegistered,
		SupervisorWatchWorktreeSilent,
		SupervisorWatchVerifyScriptNotExecutable:
		return true
	}
	return false
}

// supervisorWatchTriageNote records the first sighting of an
// Automaton-scoped finding on the owning Automaton's triage record:
// an existing record gains an annotation; a failed or blocked Automaton
// with no record gains a minimal one carrying the note. A running
// Automaton has no triage record by convention — its finding store
// record is its record — so the pass writes nothing for it.
func (m *Manager) supervisorWatchTriageNote(prds []*PRD, rec *SupervisorWatchFinding, now time.Time) {
	if m == nil || m.store == nil || rec == nil {
		return
	}
	var prd *PRD
	for _, p := range prds {
		if p != nil && p.ID == rec.Subject {
			prd = p
			break
		}
	}
	text := strings.TrimSpace(rec.Text)
	if text == "" {
		text = string(rec.Kind)
	}
	if prd != nil {
		if m.AppendTriageNote(prd.ID, supervisorWatchTriageSource, text) == nil {
			return
		}
	}
	if prd == nil || (prd.Status != PRDFailed && prd.Status != PRDBlocked) {
		return // nothing to annotate, and nothing to mint a record for
	}
	fresh := &TriageRecord{
		PRDID:       prd.ID,
		PRDTitle:    prd.Title,
		Status:      string(prd.Status),
		FirstSeenAt: now,
		UpdatedAt:   now,
		Notes:       []TriageNote{{At: now, Source: supervisorWatchTriageSource, Text: text}},
	}
	if err := m.SaveTriageRecord(prd.ID, fresh); err != nil {
		// The finding store record stays authoritative; the daemon
		// surfaces the save failure through the audit sink when one is
		// wired.
		if m.auditFn != nil {
			m.auditFn("supervisor_watch", map[string]any{
				"error":  "create triage record",
				"prd_id": prd.ID,
				"detail": err.Error(),
			})
		}
	}
}

// --- wiring (production views + test seams, guarded by the manager lock) ---

// SetSupervisorWatchProbe installs the host probe the pass runs the
// host checks through. Production wires the shell probe over the
// compute registry (cmd/datawatch); tests wire a fake. Nil restores
// "no view" — the checks then read unknown and find nothing.
func (m *Manager) SetSupervisorWatchProbe(p SupervisorWatchHostProbe) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.supervisorWatchProbe = p
	m.mu.Unlock()
}

// SetSupervisorWatchNodeView installs the compute-node registry view
// the daemon_inactive and model_not_resident checks iterate. Nil
// restores "no nodes": both checks find nothing (there is no fleet to
// be inactive on).
func (m *Manager) SetSupervisorWatchNodeView(n SupervisorWatchNodeLister) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.supervisorWatchNodes = n
	m.mu.Unlock()
}

// SetSupervisorWatchSessionLookup wires the worktree silence check's
// view of the session records' log files (the adapter in
// supervisor_watch_worktree.go). Nil restores "no session logs".
func (m *Manager) SetSupervisorWatchSessionLookup(fn func(id string) (*session.Session, bool)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if fn == nil {
		m.supervisorWatchSess = nil
	} else {
		m.supervisorWatchSess = SessionSupervisorWatchSessionLogger{GetSession: fn}
	}
	m.mu.Unlock()
}

// SetSupervisorWatchFindingsFn installs the test seam: when set, the
// pass takes its findings from fn instead of running the checks. nil
// restores the real checks. Production never sets it.
func (m *Manager) SetSupervisorWatchFindingsFn(fn func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.supervisorWatchFindings = fn
	m.mu.Unlock()
}

// Alert text (supervisor_watch_text.go owns the finding's own line;
// the alert needs the episode shape too: how long it has been open and
// in which mode the watch is running). The operator-facing strings
// name Automaton/Automata and the check, never a tracker id.

func supervisorWatchAlertTitle(rec *SupervisorWatchFinding) string {
	return fmt.Sprintf("supervisor watch: %s — %s", rec.Kind, rec.Subject)
}

func supervisorWatchAlertBody(rec *SupervisorWatchFinding, mode string, now time.Time) string {
	open := 0
	if !rec.FirstSeenAt.IsZero() {
		open = int(now.Sub(rec.FirstSeenAt) / time.Minute)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The supervisor watch (mode %s) has an open finding:\n", mode)
	if t := strings.TrimSpace(rec.Text); t != "" {
		fmt.Fprintf(&b, "%s\n", t)
	}
	fmt.Fprintf(&b, "open for %d minute(s), first seen %s.", open, rec.FirstSeenAt.Format("2006-01-02 15:04:05"))
	return b.String()
}
