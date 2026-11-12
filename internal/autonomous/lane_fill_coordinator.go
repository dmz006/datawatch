package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// This file runs the lane fill planner (lane_fill.go) from the Automation
// coordinator: a poll loop, the snapshot it plans over, and the executor that
// carries the decisions out in "on" mode. In "shadow" mode (the default) every
// decision is computed and recorded and nothing is started.

const (
	// laneFillActor is the actor name lane fill uses for every starter call.
	laneFillActor = "lane_fill"
	// laneFillRetryAfter paces the retry of a decision whose execution
	// failed, so a refusing approve or a failing run is not hammered every poll.
	laneFillRetryAfter = 5 * time.Minute
	// laneFillEvidenceTimeout bounds one pool evidence check.
	laneFillEvidenceTimeout = 15 * time.Second
	// laneFillNoEvidence is the evidence error reported until a source is set.
	laneFillNoEvidence = "no evidence source configured"
)

// LoadedModel is one model resident on a pool host.
type LoadedModel struct {
	Host  string `json:"host"`
	Model string `json:"model"`
}

// PoolEvidence is a live reading of the GPU pool.
type PoolEvidence struct {
	// ActiveGPUSessions is how many pool channels are in use, whatever
	// started them.
	ActiveGPUSessions int
	LoadedModels      []LoadedModel
	CheckedAt         time.Time
	// Degraded lists the hosts whose loaded models could not be read but
	// whose failure does not block planning (see NewPoolEvidenceSource).
	Degraded []PoolHostDegraded
}

// PoolEvidenceSource reads the GPU pool. Check returns an error only for a
// blocking fault; a host that is degraded (unreachable past the host-down
// threshold and holding no work) is reported in PoolEvidence.Degraded instead,
// with a nil error. Until one is set with SetLaneFillEvidenceSource the
// evidence is reported unavailable, so no GPU lane is called idle.
type PoolEvidenceSource interface {
	Check(ctx context.Context) (PoolEvidence, error)
}

// MergeProbe confirms that an Automaton's branch has commits ahead of main.
type MergeProbe interface {
	NeedsMerge(ctx context.Context, prdID string) (bool, error)
}

// laneFillStarter is the optional part of a starter lane fill needs beyond
// AutomationStarter.Run. It is checked by type assertion, as nestedStarter is;
// *API satisfies it and tests supply a fake.
type laneFillStarter interface {
	Approve(id, actor, note string) (any, error)
	Decompose(id string) (any, error)
	SetPRDLLM(prdID string, backend, effort, model, decompositionProfile, decompositionModel *string, actor string) (any, error)
}

// LaneFillKindModes is the lane fill mode in effect for each lane kind: the
// kind's own setting when it has one, otherwise the base mode.
type LaneFillKindModes struct {
	GPU    string `json:"gpu"`
	Claude string `json:"claude"`
}

// laneFillNormalizeMode maps any configured value to off, shadow or on; an
// empty or unknown value is shadow, so nothing starts on a typo.
func laneFillNormalizeMode(m string) string {
	switch LaneFillMode(m) {
	case LaneFillModeOff, LaneFillModeOn:
		return m
	}
	return string(LaneFillModeShadow)
}

// laneFillKindModes returns the normalized effective mode of each lane kind.
func laneFillKindModes(cfg LaneFillConfig) LaneFillKindModes {
	return LaneFillKindModes{
		GPU:    laneFillNormalizeMode(cfg.EffectiveModeFor(LaneTypeGPU)),
		Claude: laneFillNormalizeMode(cfg.EffectiveModeFor(LaneTypeClaude)),
	}
}

// For returns the mode for one lane kind.
func (k LaneFillKindModes) For(t LaneType) string {
	if t == LaneTypeGPU {
		return k.GPU
	}
	return k.Claude
}

// allOff reports whether both lane kinds are off.
func (k LaneFillKindModes) allOff() bool {
	return k.GPU == string(LaneFillModeOff) && k.Claude == string(LaneFillModeOff)
}

// LaneFillQueued counts the ready work per lane type.
type LaneFillQueued struct {
	GPU    int `json:"gpu"`
	Claude int `json:"claude"`
}

// LaneFillLaneStatus is one lane as the status shows it.
type LaneFillLaneStatus struct {
	ID   string   `json:"id"`
	Type LaneType `json:"type"`
	// State is idle or occupied; a lane the verifier-wait rule counts
	// available is waiting_verifier.
	State string `json:"state"`

	Occupant *LaneOccupant `json:"occupant"`
	Blocker  string        `json:"blocker"`
	// WaitingOnVerifier, set with WaitReason, says the lane's occupant is
	// blocked waiting for a verifier slot.
	WaitingOnVerifier bool `json:"waiting_on_verifier,omitempty"`
	// WaitReason is the occupant's wait reason, set with WaitingOnVerifier.
	WaitReason      string `json:"wait_reason,omitempty"`
	EligibleWaiting int    `json:"eligible_waiting"`
}

// LaneFillEvidence is the pool evidence the last poll planned with.
type LaneFillEvidence struct {
	CheckedAt         time.Time     `json:"checked_at"`
	GPUActiveSessions int           `json:"gpu_active_sessions"`
	LoadedModels      []LoadedModel `json:"loaded_models"`
	Error             string        `json:"error"`
	// Degraded lists pool hosts that could not be read but do not block
	// planning. It is separate from the lane blockers and empty when nothing
	// is degraded.
	Degraded []LaneFillDegradedHost `json:"degraded"`
}

// LaneFillDegradedHost is a pool host that is unreachable past the host-down
// threshold and holds no work, so lanes are judged from the other hosts.
type LaneFillDegradedHost struct {
	Host  string    `json:"host"`
	Error string    `json:"error"`
	Since time.Time `json:"since"`
}

// LaneFillAlert is an idle-lane alert raised by lane fill.
type LaneFillAlert struct {
	At     time.Time `json:"at"`
	LaneID string    `json:"lane_id"`
	// Work names the first eligible item the lane could have taken.
	Work    string `json:"work"`
	Message string `json:"message"`
}

// LaneFillStatus is the lane fill status: the mode, the lanes and evidence of
// the last poll, and the decision history newest first.
type LaneFillStatus struct {
	// Mode is the base mode; Modes is the mode in effect for each lane kind.
	Mode           string               `json:"mode"`
	Modes          LaneFillKindModes    `json:"modes"`
	LastPollAt     time.Time            `json:"last_poll_at"`
	Lanes          []LaneFillLaneStatus `json:"lanes"`
	QueuedEligible LaneFillQueued       `json:"queued_eligible"`
	Evidence       LaneFillEvidence     `json:"evidence"`
	Decisions      []LaneFillDecision   `json:"decisions"`
	Alerts         []LaneFillAlert      `json:"alerts"`
	// LaneCounts: per lane type, the configured count, the count the last
	// poll used and where it came from.
	LaneCounts LaneFillLaneCounts `json:"lane_counts"`
}

// laneFillStart remembers an Automaton lane fill started, until the store
// shows it running, so a start whose status has not flipped yet is neither
// started a second time nor leaves its lane looking idle.
type laneFillStart struct {
	at      time.Time
	title   string
	backend string
}

// laneFillState is the coordinator's lane fill state. None of it is persisted
// except the decision records: after a restart the decisions reload and
// everything else (idle episodes, recent starts, in-flight decomposes, the last
// poll) begins fresh. An Automaton started before the restart shows as running
// in the store, so it is never started a second time.
type laneFillState struct {
	// pollMu serializes polls.
	pollMu sync.Mutex

	mu       sync.Mutex
	records  *laneFillRecords
	evidence PoolEvidenceSource
	probe    MergeProbe
	proposal LaneFillProposalReader
	// caps supplies the LLM caps the lane counts follow (lane_fill_caps.go).
	caps LaneCapSource
	// mainProbe, mainCache and worktreeFS belong to the main-carries and
	// worktree rules (lane_fill_main_worktree.go).
	mainProbe  LaneFillMainProbe
	mainCache  map[string]laneFillMainCached
	worktreeFS SupervisorWatchWorktreeFS
	// proposalWarned remembers which unreadable proposal paths were logged.
	proposalWarned map[string]bool
	status         LaneFillStatus
	idleSince      map[string]time.Time
	// workSince, alerted and alerts are the idle alert state (see
	// lane_fill_alert.go): when each lane was first seen idle with eligible
	// work, which episodes already alerted, and the recent alerts newest first.
	workSince   map[string]time.Time
	alerted     map[string]bool
	alerts      []LaneFillAlert
	started     map[string]laneFillStart
	decomposing map[string]LaneType
	wg          sync.WaitGroup
	// kindModes is the effective mode of each lane kind at the last poll;
	// kindModesSeen is false until the first poll has set it, so a change is
	// audited only against a mode this process saw.
	kindModes     LaneFillKindModes
	kindModesSeen bool
}

func newLaneFillState() laneFillState {
	return laneFillState{
		idleSince:   map[string]time.Time{},
		workSince:   map[string]time.Time{},
		alerted:     map[string]bool{},
		started:     map[string]laneFillStart{},
		decomposing: map[string]LaneType{},
	}
}

// SetLaneFillEvidenceSource sets where pool evidence comes from.
func (c *Coordinator) SetLaneFillEvidenceSource(src PoolEvidenceSource) {
	if c == nil {
		return
	}
	c.lf.mu.Lock()
	c.lf.evidence = src
	c.lf.mu.Unlock()
}

// SetLaneFillMergeProbe sets the optional probe that confirms commits ahead of
// main for finished work.
func (c *Coordinator) SetLaneFillMergeProbe(p MergeProbe) {
	if c == nil {
		return
	}
	c.lf.mu.Lock()
	c.lf.probe = p
	c.lf.mu.Unlock()
}

// LaneFillStatus returns the lane fill status with up to limit decisions
// (all of them when limit is not positive). It is nil-safe: without a running
// coordinator it reports the configured mode with no lanes and no decisions.
func (m *Manager) LaneFillStatus(limit int) LaneFillStatus {
	empty := LaneFillStatus{
		Lanes:     []LaneFillLaneStatus{},
		Decisions: []LaneFillDecision{},
		Alerts:    []LaneFillAlert{},
		Evidence:  LaneFillEvidence{LoadedModels: []LoadedModel{}},
	}
	if m == nil {
		def := DefaultLaneFillConfig()
		empty.Mode = def.EffectiveMode()
		empty.Modes = laneFillKindModes(def)
		return empty
	}
	m.mu.Lock()
	c := m.coordinator
	cfg := m.cfg.LaneFill
	m.mu.Unlock()
	if c == nil {
		empty.Mode = cfg.EffectiveMode()
		empty.Modes = laneFillKindModes(cfg)
		return empty
	}
	return c.laneFillStatus(cfg, limit)
}

func (c *Coordinator) laneFillStatus(cfg LaneFillConfig, limit int) LaneFillStatus {
	c.lf.mu.Lock()
	st := c.lf.status
	rec := c.lf.records
	st.Alerts = append([]LaneFillAlert{}, c.lf.alerts...)
	c.lf.mu.Unlock()
	st.Mode = cfg.EffectiveMode()
	st.Modes = laneFillKindModes(cfg)
	// A kind whose mode is off shows no lanes and no queued work.
	st.Lanes = laneFillKeepLanes(st.Lanes, st.Modes)
	if st.Modes.GPU == string(LaneFillModeOff) {
		st.QueuedEligible.GPU = 0
	}
	if st.Modes.Claude == string(LaneFillModeOff) {
		st.QueuedEligible.Claude = 0
	}
	if st.Lanes == nil {
		st.Lanes = []LaneFillLaneStatus{}
	}
	if st.Evidence.LoadedModels == nil {
		st.Evidence.LoadedModels = []LoadedModel{}
	}
	if st.Alerts == nil {
		st.Alerts = []LaneFillAlert{}
	}
	if st.LaneCounts.GPU.Source == "" {
		// No poll has run yet: show the counts the next poll would use.
		_, st.LaneCounts = laneFillDeriveCounts(cfg, c.laneFillCapSource())
	}
	st.Decisions = []LaneFillDecision{}
	if rec != nil {
		st.Decisions = rec.list(limit)
	}
	return st
}

// laneFillInterval is how long the loop waits between polls, read live from
// the Manager config so a changed poll_seconds applies without a restart.
func (c *Coordinator) laneFillInterval() time.Duration {
	if c.laneFillTick > 0 {
		return c.laneFillTick
	}
	return time.Duration(c.m.Config().LaneFill.EffectivePollSeconds()) * time.Second
}

// laneFillLoop polls until stop is closed.
func (c *Coordinator) laneFillLoop(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		t := time.NewTimer(c.laneFillInterval())
		select {
		case <-stop:
			t.Stop()
			return
		case <-t.C:
		}
		c.laneFillPoll(ctx)
	}
}

// laneFillPoll runs one pass: snapshot, plan, record, and in "on" mode
// execute. Mode off does nothing and records nothing, and nothing is decided
// before boot recovery has finished (Ready), so runs being resumed are never
// read as idle lanes.
func (c *Coordinator) laneFillPoll(ctx context.Context) {
	if c == nil {
		return
	}
	c.lf.pollMu.Lock()
	defer c.lf.pollMu.Unlock()

	cfg := c.m.Config().LaneFill
	// The mode in effect is decided per lane kind: a kind's own setting when
	// it has one, else the base mode. The values are normalized once here and
	// written back, so the planner and the executor read the same modes.
	kinds := laneFillKindModes(cfg)
	cfg.Mode = laneFillNormalizeMode(cfg.EffectiveMode())
	cfg.GPUMode, cfg.ClaudeMode = kinds.GPU, kinds.Claude
	if !c.Ready() {
		return
	}
	c.laneFillAuditModeChange(kinds)
	if kinds.allOff() {
		c.laneFillResetAlerts()
		return
	}
	// One set of lane counts for the whole poll: the lanes, the planner, the
	// idle alert and the decompose-ahead room all read this configuration.
	cfg, counts := laneFillDeriveCounts(cfg, c.laneFillCapSource())
	// A project's policy lanes are a shared-budget floor: when a project's
	// tier sets a lower lane count, the daemon runs the shared budget at no
	// more than that count (never raised). No-op when unwired.
	cfg, counts = c.laneFillApplyProjectCaps(cfg, counts)

	c.lf.mu.Lock()
	rec := c.lf.records
	c.lf.mu.Unlock()
	if rec == nil {
		return
	}

	now := c.now()
	in, status := c.laneFillSnapshot(ctx, cfg, now)
	// A kind whose mode is off plans nothing: its lanes are not handed to the
	// planner and not watched for idle alerts, and no decision of that kind is
	// recorded.
	in.Lanes = laneFillKeepLaneObjects(in.Lanes, kinds)
	decisions := laneFillKeepDecisions(PlanLaneFill(in), kinds)
	c.laneFillCheckAlerts(cfg, in.Lanes, in.Candidates, now)

	candidates := make(map[string]LaneFillCandidate, len(in.Candidates))
	for _, cand := range in.Candidates {
		candidates[cand.ID] = cand
	}
	for _, d := range decisions {
		e, appended := rec.observe(d, now)
		// Execution follows the mode of the decision's lane kind; decisions
		// with no lane kind (merge reports, a blocked Automaton) never run.
		if d.LaneType != "" && kinds.For(d.LaneType) == string(LaneFillModeOn) {
			c.laneFillExecute(ctx, cfg, rec, e, candidates[d.PRDID], now)
		}
		if appended {
			c.laneFillAudit(rec, e)
		}
	}
	rec.endPoll()

	status.Mode = cfg.Mode
	status.Modes = kinds
	status.Lanes = laneFillKeepLanes(status.Lanes, kinds)
	status.LastPollAt = now
	status.LaneCounts = counts
	c.lf.mu.Lock()
	c.lf.status = status
	c.lf.mu.Unlock()
}

// laneFillKeepLanes drops the status rows of lanes whose kind is off.
func laneFillKeepLanes(lanes []LaneFillLaneStatus, kinds LaneFillKindModes) []LaneFillLaneStatus {
	if lanes == nil {
		return nil
	}
	out := make([]LaneFillLaneStatus, 0, len(lanes))
	for _, l := range lanes {
		if kinds.For(l.Type) != string(LaneFillModeOff) {
			out = append(out, l)
		}
	}
	return out
}

// laneFillKeepDecisions drops the decisions of a lane kind whose mode is off.
// Decisions with no lane kind stay.
func laneFillKeepDecisions(ds []LaneFillDecision, kinds LaneFillKindModes) []LaneFillDecision {
	out := ds[:0:0]
	for _, d := range ds {
		if d.LaneType != "" && kinds.For(d.LaneType) == string(LaneFillModeOff) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// laneFillKeepLaneObjects drops the lanes whose kind is off.
func laneFillKeepLaneObjects(lanes []Lane, kinds LaneFillKindModes) []Lane {
	out := make([]Lane, 0, len(lanes))
	for _, l := range lanes {
		if kinds.For(l.Type) != string(LaneFillModeOff) {
			out = append(out, l)
		}
	}
	return out
}

// laneFillAuditModeChange writes one lane_fill_mode audit entry for each lane
// kind whose effective mode differs from the previous poll. The first poll
// after start only records the modes, so a restart writes nothing.
func (c *Coordinator) laneFillAuditModeChange(now LaneFillKindModes) {
	c.lf.mu.Lock()
	prev, seen := c.lf.kindModes, c.lf.kindModesSeen
	c.lf.kindModes, c.lf.kindModesSeen = now, true
	c.lf.mu.Unlock()
	if !seen {
		return
	}
	for _, t := range []LaneType{LaneTypeGPU, LaneTypeClaude} {
		if prev.For(t) == now.For(t) {
			continue
		}
		c.audit(audit.ActionLaneFillMode, map[string]any{
			"kind":     string(t),
			"old_mode": prev.For(t),
			"new_mode": now.For(t),
		})
	}
}

// laneFillAudit sends the audit record for an appended decision.
func (c *Coordinator) laneFillAudit(rec *laneFillRecords, e *laneFillEntry) {
	var d LaneFillDecision
	rec.read(e, func(cur LaneFillDecision, _ time.Time) { d = cur })
	details := map[string]any{
		"lane_id":  d.LaneID,
		"action":   string(d.Action),
		"prd_id":   d.PRDID,
		"reason":   d.Reason,
		"blocker":  d.Blocker,
		"mode":     string(d.Mode),
		"executed": d.Executed,
	}
	if d.Error != "" {
		details["error"] = d.Error
	}
	if d.EligibilityReason != "" {
		details["eligibility_reason"] = d.EligibilityReason
	}
	if d.WaitReason != "" {
		// The verifier-wait basis of the decision, when it rests on one: the
		// shadow record says why the planner thought the lane was free, not
		// only what it would have started.
		details["wait_reason"] = d.WaitReason
	}
	c.audit("lane_fill", details)
}

// laneFillRaw is what the snapshot copies out of one Automaton under the
// store lock, so the rest of the work happens outside it.
type laneFillRaw struct {
	id, title, spec string
	status          PRDStatus
	pref            string
	priority        int
	created         time.Time
	updated         time.Time
	automationID    string
	backend, model  string
	// worker is the task whose backend stands for the Automaton's worker: one
	// that is running, else an empty one that inherits.
	worker             Task
	workerStoryBackend string
	workerStoryModel   string
	tasks, tasksDone   int
	template           bool
	typ                string
	// branch and worktreeDir are the recorded Automaton branch and, for an
	// Automaton that runs in its own worktree, that worktree directory.
	branch, worktreeDir string
	// reviewOutcome is the Automaton's last blocked rules review record, if any.
	reviewOutcome *RulesReviewOutcome
}

var laneFillRunningTask = map[TaskStatus]bool{
	TaskInProgress: true, TaskRunningTests: true, TaskVerifying: true,
	TaskQueued: true, TaskWaitingCapacity: true,
}

// laneFillTerminal says the Automaton has ended for good.
func laneFillTerminal(s PRDStatus) bool {
	switch NormalizePRDStatus(s) {
	case PRDCompleted, PRDRejected, PRDCancelled, PRDArchived:
		return true
	}
	return false
}

func laneFillRunning(s PRDStatus) bool {
	s = NormalizePRDStatus(s)
	return s == PRDRunning || s == PRDActive
}

// readLaneFillRaw copies the facts lane fill needs from every Automaton under
// the store lock.
func (c *Coordinator) readLaneFillRaw() []laneFillRaw {
	prds := c.m.store.ListPRDs()
	out := make([]laneFillRaw, 0, len(prds))
	c.m.readLocked(func() {
		for _, p := range prds {
			r := laneFillRaw{
				id: p.ID, title: p.Title, spec: p.Spec, status: p.Status,
				pref: p.LanePreference, priority: p.Priority, created: p.CreatedAt,
				updated: p.UpdatedAt, automationID: p.AutomationID, backend: p.Backend, model: p.Model,
				template: p.IsTemplate, typ: p.Type, branch: p.Git.Branch,
				reviewOutcome: p.RulesReviewOutcome,
			}
			if p.Git.Branch != "" && p.ProjectDir != "" && p.ProjectProfile == "" && p.ClusterProfile == "" {
				r.worktreeDir = p.ProjectDir
			}
			picked := false
			for si := range p.Story {
				s := &p.Story[si]
				for ti := range s.Tasks {
					t := &s.Tasks[ti]
					if t.Status != TaskCancelled {
						r.tasks++
						if t.Status == TaskCompleted {
							r.tasksDone++
						}
					}
					if !picked && laneFillRunningTask[t.Status] {
						picked = true
						r.worker = Task{Backend: t.Backend, Model: t.Model}
						r.workerStoryBackend, r.workerStoryModel = s.Backend, s.Model
					}
				}
			}
			out = append(out, r)
		}
	})
	return out
}

// workerBackend resolves the Automaton's effective worker backend with the
// resolver the executor uses.
func (c *Coordinator) laneFillWorkerBackend(r laneFillRaw, execution string) string {
	var story *Story
	if r.workerStoryBackend != "" || r.workerStoryModel != "" {
		story = &Story{Backend: r.workerStoryBackend, Model: r.workerStoryModel}
	}
	task := r.worker
	prd := &PRD{ID: r.id, Backend: r.backend, Model: r.model}
	var auto *Automation
	if r.automationID != "" {
		auto, _ = c.m.automations.Get(r.automationID)
	}
	backend, _ := admittedWorker(&task, story, prd, auto, execution)
	return backend
}

// laneFillRefLookup resolves a "sequenced after" reference against the
// snapshot: an id, else a title fragment. When several Automata match, one that
// is not completed is the answer, so an ambiguous reference holds.
func laneFillRefLookup(raws []laneFillRaw) PRDRefLookup {
	return func(ref string) (PRDStatus, bool) {
		lower := strings.ToLower(ref)
		var found, ok bool
		var status PRDStatus
		for _, r := range raws {
			if r.id != ref && !strings.Contains(strings.ToLower(r.title), lower) {
				continue
			}
			s := NormalizePRDStatus(r.status)
			if !found || (status == PRDCompleted && s != PRDCompleted) {
				status, found, ok = s, true, true
			}
		}
		return status, ok
	}
}

// laneFillSnapshot builds the planner input from the stores and the pool
// evidence. It holds no Manager or store lock while it calls the evidence
// source or the merge probe.
func (c *Coordinator) laneFillSnapshot(ctx context.Context, cfg LaneFillConfig, now time.Time) (LaneFillInput, LaneFillStatus) {
	execution := c.m.Config().ExecutionBackend
	raws := c.readLaneFillRaw()
	lookup := laneFillRefLookup(raws)

	c.lf.mu.Lock()
	probe, src, proposalRd := c.lf.probe, c.lf.evidence, c.lf.proposal
	started := make(map[string]laneFillStart, len(c.lf.started))
	for id, s := range c.lf.started {
		started[id] = s
	}
	decomposing := make(map[string]LaneType, len(c.lf.decomposing))
	for id, t := range c.lf.decomposing {
		decomposing[id] = t
	}
	c.lf.mu.Unlock()
	grace := 3 * time.Duration(cfg.EffectivePollSeconds()) * time.Second

	var running []LaneOccupant
	var cands []LaneFillCandidate
	branches := map[string]string{}
	worktrees := map[string]string{}
	inFlight := map[string]LaneType{}
	for _, r := range raws {
		if r.template {
			continue
		}
		backend := c.laneFillWorkerBackend(r, execution)
		switch {
		case laneFillRunning(r.status):
			running = append(running, c.laneFillVerifierWait(LaneOccupant{PRDID: r.id, Title: r.title, Backend: backend}))
			c.laneFillForgetStart(r.id)
			continue
		case laneFillTerminal(r.status):
			continue
		}
		if s, ok := started[r.id]; ok && now.Sub(s.at) < grace {
			running = append(running, c.laneFillVerifierWait(LaneOccupant{PRDID: r.id, Title: s.title, Backend: s.backend}))
			continue
		}
		if NormalizePRDStatus(r.status) == PRDPlanning {
			if t, ok := LaneTypeForBackend(cfg, backend); ok {
				inFlight[r.id] = t
			}
		}
		if _, busy := decomposing[r.id]; busy {
			continue // a decompose lane fill started is still running
		}
		prd := &PRD{ID: r.id, Title: r.title, Spec: r.spec, Status: r.status, RulesReviewOutcome: r.reviewOutcome}
		holdCode, holdText := PRDHoldCode(prd, lookup)
		cand := LaneFillCandidate{
			ID: r.id, Title: r.title, Status: NormalizePRDStatus(r.status),
			LanePreference: r.pref, Backend: backend, Priority: r.priority, CreatedAt: r.created,
			UpdatedAt:            r.updated,
			HoldReason:           holdText,
			HoldCode:             holdCode,
			AllTasksDone:         r.tasks > 0 && r.tasks == r.tasksDone,
			InAutomation:         r.automationID != "",
			BackendChangeAllowed: prdLLMOverrideAllowed(r.status),
		}
		cand.NeedsMergeFlag = c.laneFillNeedsMerge(ctx, probe, cand)
		if cfg.EffectiveRequireConfirmedProposal() && laneFillQueuedStatus(cand.Status) {
			cand.ProposalState, cand.ProposalPath = CheckLaneFillProposal(r.typ, r.template, r.spec, proposalRd, c.laneFillProposalWarnOnce)
		}
		branches[r.id] = r.branch
		if r.worktreeDir != "" {
			worktrees[r.id] = r.worktreeDir
		}
		cands = append(cands, cand)
	}
	// The git and filesystem facts of the last two rules, gathered here with
	// no store lock held.
	c.laneFillProbeMain(ctx, cfg, cands, branches, now)
	c.laneFillCheckWorktrees(cfg, cands, worktrees, c.m.Config().WorktreeBaseRepo)
	// Apply the refusal rules once, here, with no store lock held: every later
	// reader (the planner, the queued count, the alerts) honours the result.
	for i := range cands {
		code, text := EvaluateLaneFillEligibility(cfg, cands[i], now)
		cands[i].Refusal = LaneFillRefusal{Code: code, Text: text}
	}
	for id, t := range decomposing {
		inFlight[id] = t
	}
	inFlightByType := map[LaneType]int{}
	for _, t := range inFlight {
		inFlightByType[t]++
	}

	ev, evErr := c.laneFillEvidence(ctx, src, now)
	model := BuildLanes(cfg, running, LaneEvidence{GPUBusy: ev.ActiveGPUSessions, Err: evErr})
	in := LaneFillInput{
		Config: cfg, Lanes: model.Lanes, Candidates: cands,
		GPUBusyByEvidence: ev.ActiveGPUSessions, EvidenceErr: evErr,
		InFlightDecomposeByType: inFlightByType, Now: now,
	}

	queued := QueuedEligible(cands)
	status := LaneFillStatus{
		QueuedEligible: LaneFillQueued{GPU: queued[LaneTypeGPU], Claude: queued[LaneTypeClaude]},
		Evidence: LaneFillEvidence{
			CheckedAt: ev.CheckedAt, GPUActiveSessions: ev.ActiveGPUSessions,
			LoadedModels: append([]LoadedModel{}, ev.LoadedModels...), Error: evErr,
			Degraded: laneFillDegradedView(ev.Degraded),
		},
		Lanes: make([]LaneFillLaneStatus, 0, len(model.Lanes)),
	}
	c.lf.mu.Lock()
	seen := map[string]bool{}
	for _, l := range model.Lanes {
		if l.Occupant == nil {
			seen[l.ID] = true
			if _, ok := c.lf.idleSince[l.ID]; !ok {
				c.lf.idleSince[l.ID] = now
			}
		}
		status.Lanes = append(status.Lanes, laneFillLaneStatusView(cfg, l, queued[l.Type]))
	}
	for id := range c.lf.idleSince {
		if !seen[id] {
			delete(c.lf.idleSince, id)
		}
	}
	c.lf.mu.Unlock()
	return in, status
}

func (c *Coordinator) laneFillForgetStart(id string) {
	c.lf.mu.Lock()
	delete(c.lf.started, id)
	c.lf.mu.Unlock()
}

// laneFillNeedsMerge says whether finished work still needs a merge flag.
// Blocked or failed work with every task done counts; a probe, when one is
// set, must also confirm commits ahead of main. A probe that errors is
// ignored, so the status rule alone decides.
func (c *Coordinator) laneFillNeedsMerge(ctx context.Context, probe MergeProbe, cand LaneFillCandidate) bool {
	if !cand.AllTasksDone || (cand.Status != PRDBlocked && cand.Status != PRDFailed) {
		return false
	}
	if probe == nil {
		return true
	}
	ahead, err := probe.NeedsMerge(ctx, cand.ID)
	if err != nil {
		log.Printf("[autonomous] lane fill: merge probe for %s: %v", cand.ID, err)
		return true
	}
	return ahead
}

// laneFillEvidence reads the pool. Without a source, or when the check fails,
// the evidence is reported unavailable.
func (c *Coordinator) laneFillEvidence(ctx context.Context, src PoolEvidenceSource, now time.Time) (PoolEvidence, string) {
	if src == nil {
		return PoolEvidence{CheckedAt: now}, laneFillNoEvidence
	}
	cctx, cancel := context.WithTimeout(ctx, laneFillEvidenceTimeout)
	defer cancel()
	ev, err := src.Check(cctx)
	if ev.CheckedAt.IsZero() {
		ev.CheckedAt = now
	}
	if err != nil {
		// A blocking fault blocks every GPU lane as before; the degraded
		// list is still reported next to the error.
		return PoolEvidence{CheckedAt: ev.CheckedAt, Degraded: ev.Degraded}, err.Error()
	}
	// Only degraded faults, or none: the readable hosts' evidence stands.
	return ev, ""
}

// laneFillDegradedView converts the degraded hosts for the status; the result
// is never nil so the JSON list is empty, not null.
func laneFillDegradedView(in []PoolHostDegraded) []LaneFillDegradedHost {
	out := make([]LaneFillDegradedHost, 0, len(in))
	for _, d := range in {
		out = append(out, LaneFillDegradedHost(d))
	}
	return out
}

// laneFillLaneBackend is the backend and model the lane type runs.
func laneFillLaneBackend(cfg LaneFillConfig, t LaneType) (backend, model string) {
	if t == LaneTypeGPU {
		return cfg.EffectiveGPUBackend(), cfg.GPUModel
	}
	return cfg.EffectiveClaudeBackend(), ""
}

// laneFillExecute carries out one decision in "on" mode, unless it was already
// carried out or failed too recently to try again. report_merge, blocked and
// idle decisions are record-only.
func (c *Coordinator) laneFillExecute(ctx context.Context, cfg LaneFillConfig, rec *laneFillRecords,
	e *laneFillEntry, cand LaneFillCandidate, now time.Time) {
	var d LaneFillDecision
	var attempted time.Time
	rec.read(e, func(cur LaneFillDecision, at time.Time) { d, attempted = cur, at })
	switch d.Action {
	case LaneFillActionStartPRD, LaneFillActionDecompose, LaneFillActionDecomposeAhead:
	default:
		return
	}
	if d.Executed {
		return
	}
	if !attempted.IsZero() && (d.Error == "" || now.Sub(attempted) < laneFillRetryAfter) {
		return
	}
	rec.update(e, func(cur *LaneFillDecision, at *time.Time) { *at = now })

	switch d.Action {
	case LaneFillActionStartPRD:
		c.laneFillStartPRD(cfg, rec, e, d, cand, now)
	default:
		c.laneFillDecompose(rec, e, d)
	}
}

// laneFillFail records a failed execution as the decision's error and blocker.
func laneFillFail(rec *laneFillRecords, e *laneFillEntry, msg string) {
	rec.update(e, func(d *LaneFillDecision, _ *time.Time) {
		d.Executed, d.Error, d.Blocker = false, msg, msg
	})
}

// laneFillStartPRD starts an Automaton on its lane: change the worker backend
// if it must move, approve it if it still awaits review, then Run it through
// the normal admission path.
func (c *Coordinator) laneFillStartPRD(cfg LaneFillConfig, rec *laneFillRecords, e *laneFillEntry,
	d LaneFillDecision, cand LaneFillCandidate, now time.Time) {
	ls, ok := c.starter.(laneFillStarter)
	if !ok {
		laneFillFail(rec, e, "starter cannot approve, decompose or change the backend")
		return
	}
	target, model := laneFillLaneBackend(cfg, d.LaneType)
	if cand.Backend != target && cand.BackendChangeAllowed {
		if _, err := ls.SetPRDLLM(d.PRDID, &target, nil, &model, nil, nil, laneFillActor); err != nil {
			laneFillFail(rec, e, fmt.Sprintf("set backend to %s refused: %v", target, err))
			return
		}
	}
	if cand.Status == PRDNeedsReview {
		note := fmt.Sprintf("approved by lane fill to fill lane %s", d.LaneID)
		if _, err := ls.Approve(d.PRDID, laneFillActor, note); err != nil {
			laneFillFail(rec, e, fmt.Sprintf("approve refused: %v", err))
			return
		}
	}
	if err := c.starter.Run(d.PRDID); err != nil {
		laneFillFail(rec, e, fmt.Sprintf("run failed: %v", err))
		return
	}
	backend := cand.Backend
	if cand.BackendChangeAllowed {
		backend = target
	}
	c.lf.mu.Lock()
	c.lf.started[d.PRDID] = laneFillStart{at: now, title: d.Title, backend: backend}
	c.lf.mu.Unlock()
	rec.update(e, func(cur *LaneFillDecision, _ *time.Time) { cur.Executed, cur.Error, cur.Blocker = true, "", "" })
}

// laneFillDecompose starts a decompose in the background, since it can take
// many minutes and a poll must not wait for it. The record shows it executed
// once the call is made; a failure is written to the record when it returns.
func (c *Coordinator) laneFillDecompose(rec *laneFillRecords, e *laneFillEntry, d LaneFillDecision) {
	ls, ok := c.starter.(laneFillStarter)
	if !ok {
		laneFillFail(rec, e, "starter cannot approve, decompose or change the backend")
		return
	}
	c.lf.mu.Lock()
	if _, busy := c.lf.decomposing[d.PRDID]; busy {
		c.lf.mu.Unlock()
		return
	}
	c.lf.decomposing[d.PRDID] = d.LaneType
	c.lf.wg.Add(1)
	c.lf.mu.Unlock()
	rec.update(e, func(cur *LaneFillDecision, _ *time.Time) { cur.Executed, cur.Error, cur.Blocker = true, "", "" })

	go func() {
		defer c.lf.wg.Done()
		_, err := ls.Decompose(d.PRDID)
		c.lf.mu.Lock()
		delete(c.lf.decomposing, d.PRDID)
		c.lf.mu.Unlock()
		if err != nil {
			laneFillFail(rec, e, fmt.Sprintf("decompose failed: %v", err))
		}
	}()
}

// laneFillWait blocks until every decompose lane fill started has returned.
// Tests use it.
func (c *Coordinator) laneFillWait() { c.lf.wg.Wait() }

// openLaneFill loads the decision records beside the other autonomous stores.
func (c *Coordinator) openLaneFill() {
	rec := newLaneFillRecords(c.m.store.dir)
	c.lf.mu.Lock()
	c.lf.records = rec
	c.lf.mu.Unlock()
}
