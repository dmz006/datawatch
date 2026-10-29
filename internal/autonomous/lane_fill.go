package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file is the pure lane model and the lane fill planner. Nothing here
// starts a goroutine, does I/O or reads the clock: the caller passes a
// snapshot (lanes, candidates, pool evidence, the time) and gets back the
// decisions. The same snapshot always gives the same decisions, in the same
// order, whatever mode is configured.

// LaneType is the kind of an execution lane.
type LaneType string

const (
	// LaneTypeGPU is a lane served by the local model pool. It costs no
	// Claude usage.
	LaneTypeGPU LaneType = "gpu"
	// LaneTypeClaude is a lane served by the Claude backend.
	LaneTypeClaude LaneType = "claude"
)

// LaneFillMode is the lane fill mode. The planner itself does not branch on
// it; the mode is carried on each decision so the coordinator and the status
// views can say what was done with it.
type LaneFillMode string

const (
	LaneFillModeOff    LaneFillMode = "off"
	LaneFillModeShadow LaneFillMode = "shadow"
	LaneFillModeOn     LaneFillMode = "on"
)

// LaneFillAction is what the planner decided for a lane or an Automaton.
type LaneFillAction string

const (
	// LaneFillActionStartPRD: start the Automaton on the lane.
	LaneFillActionStartPRD LaneFillAction = "start_prd"
	// LaneFillActionReportMerge: the Automaton needs a merge. Reported only.
	LaneFillActionReportMerge LaneFillAction = "report_merge"
	// LaneFillActionDecompose: the lane has nothing ready, decompose a draft.
	LaneFillActionDecompose LaneFillAction = "decompose"
	// LaneFillActionDecomposeAhead: the queue behind a lane type is short,
	// decompose a draft now.
	LaneFillActionDecomposeAhead LaneFillAction = "decompose_ahead"
	// LaneFillActionBlocked: an Automaton or a lane cannot be used, with the
	// reason in Blocker.
	LaneFillActionBlocked LaneFillAction = "blocked"
	// LaneFillActionIdle: the lane is idle and nothing usable exists for it;
	// Blocker names the reason.
	LaneFillActionIdle LaneFillAction = "idle"
)

// Lane is one execution lane. Occupant is nil when it is idle. Blocker, when
// set on a lane with no occupant, says why the lane must not be called idle.
type Lane struct {
	ID       string        `json:"id"`
	Type     LaneType      `json:"type"`
	Occupant *LaneOccupant `json:"occupant,omitempty"`
	Blocker  string        `json:"blocker,omitempty"`
}

// LaneOccupant is the work running in a lane. PRDID is empty for a session on
// the pool that belongs to no Automaton.
type LaneOccupant struct {
	PRDID   string `json:"prd_id"`
	Title   string `json:"title"`
	Backend string `json:"backend"`
	// WaitingOnVerifier says the lane is occupied but not advancing: the
	// Automaton has running tasks and every one of them is blocked waiting
	// for a verifier slot (see lane_fill_verifier_wait.go). Zero means the
	// lane is working or holds no running task.
	WaitingOnVerifier bool `json:"waiting_on_verifier,omitempty"`
	// WaitReason is the readable reason text of a blocked waiter, set with
	// WaitingOnVerifier.
	WaitReason string `json:"wait_reason,omitempty"`
}

// LaneFillCandidate is a plain snapshot of one Automaton, as far as lane fill
// cares about it.
type LaneFillCandidate struct {
	ID             string
	Title          string
	Status         PRDStatus
	LanePreference string
	// Backend is the effective worker backend.
	Backend   string
	Priority  int
	CreatedAt time.Time
	// HoldReason is empty when nothing holds the Automaton; see PRDHoldReason.
	HoldReason   string
	AllTasksDone bool
	// NeedsMergeFlag: the finished work still needs a merge.
	NeedsMergeFlag bool
	// InAutomation: a member of an Automation, which starts it.
	InAutomation bool
	// BackendChangeAllowed says whether the set-llm status rules allow
	// changing the worker backend in the Automaton's current status.
	BackendChangeAllowed bool
}

// LaneEvidence is the live evidence about the GPU pool.
type LaneEvidence struct {
	// GPUBusy is how many pool channels are in use, whatever started them.
	GPUBusy int
	// Err is set when the evidence could not be gathered.
	Err string
}

// LaneModel is the result of BuildLanes.
type LaneModel struct {
	Lanes []Lane
	// Extra are running Automata beyond the lane count of their type. They
	// never create an idle lane.
	Extra []LaneOccupant
	// Unlaned are running Automata whose backend belongs to no lane type.
	// They are only reported.
	Unlaned []LaneOccupant
}

// LaneFillInput is the snapshot PlanLaneFill reads.
type LaneFillInput struct {
	Config     LaneFillConfig
	Lanes      []Lane
	Candidates []LaneFillCandidate
	// GPUBusyByEvidence is the live count of pool channels in use. Lanes
	// built by BuildLanes already include it, so the planner does not read it
	// again; it is carried so the status views can show the evidence used.
	GPUBusyByEvidence int
	// EvidenceErr, when not empty, means the pool evidence is unavailable:
	// no GPU lane is called idle and no GPU work is started.
	EvidenceErr string
	// InFlightDecomposeByType counts decompositions already running per lane
	// type.
	InFlightDecomposeByType map[LaneType]int
	Now                     time.Time
}

// LaneFillDecision is one thing the planner decided. Executed and Error are
// filled by the coordinator, not the planner. RepeatCount and LastSeenAt let
// the coordinator collapse the same decision seen on consecutive polls.
type LaneFillDecision struct {
	At          time.Time      `json:"at"`
	Mode        LaneFillMode   `json:"mode"`
	LaneID      string         `json:"lane_id"`
	LaneType    LaneType       `json:"lane_type"`
	Action      LaneFillAction `json:"action"`
	PRDID       string         `json:"prd_id"`
	Title       string         `json:"title"`
	Reason      string         `json:"reason"`
	Blocker     string         `json:"blocker"`
	Executed    bool           `json:"executed"`
	Error       string         `json:"error"`
	RepeatCount int            `json:"repeat_count"`
	LastSeenAt  time.Time      `json:"last_seen_at"`
}

// LaneTypeForBackend says which lane type a worker backend occupies:
// gpu_backend gives gpu, claude_backend gives claude. ok is false for any
// other backend, which occupies no lane.
func LaneTypeForBackend(cfg LaneFillConfig, backend string) (LaneType, bool) {
	b := strings.TrimSpace(backend)
	switch {
	case b == "":
		return "", false
	case b == cfg.EffectiveGPUBackend():
		return LaneTypeGPU, true
	case b == cfg.EffectiveClaudeBackend():
		return LaneTypeClaude, true
	}
	return "", false
}

// BuildLanes makes gpu_lanes GPU lanes and claude_lanes Claude lanes and
// places the running Automata in them.
//
// A running Automaton takes the first idle lane of the type its backend maps
// to (see LaneTypeForBackend). Working occupants are placed before
// verifier-waiting ones, within each lane type and the same order inside each
// group, so a waiting Automaton never pushes a working one into Extra or
// hides a working lane. Ones beyond the lane count of their type are
// extra occupants, and ones on any other backend occupy no lane; neither ever
// creates an idle lane. GPU occupancy is the larger of the Automaton
// occupants and the live evidence count, so a session on the pool that
// belongs to no Automaton still holds a lane. When the evidence is
// unavailable, no GPU lane is left looking idle: each one gets the blocker
// "pool evidence unavailable: <error>".
func BuildLanes(cfg LaneFillConfig, running []LaneOccupant, evidence LaneEvidence) LaneModel {
	var model LaneModel
	counts := map[LaneType]int{
		LaneTypeGPU:    cfg.EffectiveGPULanes(),
		LaneTypeClaude: cfg.EffectiveClaudeLanes(),
	}
	for _, t := range []LaneType{LaneTypeGPU, LaneTypeClaude} {
		for i := 1; i <= counts[t]; i++ {
			model.Lanes = append(model.Lanes, Lane{ID: fmt.Sprintf("%s-%d", t, i), Type: t})
		}
	}

	ordered := append([]LaneOccupant(nil), running...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].PRDID < ordered[j].PRDID })
	// Working occupants before verifier-waiting ones; the stable sort keeps
	// the order above inside each group.
	sort.SliceStable(ordered, func(i, j int) bool {
		return !ordered[i].WaitingOnVerifier && ordered[j].WaitingOnVerifier
	})

	gpuOccupants := 0
	for _, occ := range ordered {
		t, ok := LaneTypeForBackend(cfg, occ.Backend)
		if !ok {
			model.Unlaned = append(model.Unlaned, occ)
			continue
		}
		if t == LaneTypeGPU {
			gpuOccupants++
		}
		if !placeOccupant(model.Lanes, t, occ) {
			model.Extra = append(model.Extra, occ)
		}
	}

	// Sessions on the pool that no Automaton owns still hold a lane.
	for n := gpuOccupants; n < evidence.GPUBusy; n++ {
		placeOccupant(model.Lanes, LaneTypeGPU, LaneOccupant{
			Title:   "session on the pool that belongs to no Automaton",
			Backend: cfg.EffectiveGPUBackend(),
		})
	}

	if evidence.Err != "" {
		for i := range model.Lanes {
			l := &model.Lanes[i]
			if l.Type == LaneTypeGPU && l.Occupant == nil {
				l.Blocker = evidenceBlocker(evidence.Err)
			}
		}
	}
	return model
}

// laneFree says whether a lane is free for a new start: it has no occupant,
// or its occupant is an Automaton waiting on a verifier slot, the lane is a
// Claude lane and the count_verifier_wait rule is on — a waiting occupant
// holds the lane without spending Claude usage. A GPU lane never frees under
// the rule, because GPU work costs no Claude usage. Both the planner and the
// idle alert test a lane for busy with this one helper.
func laneFree(cfg LaneFillConfig, lane Lane) bool {
	if lane.Occupant == nil {
		return true
	}
	return lane.Occupant.WaitingOnVerifier &&
		lane.Type == LaneTypeClaude &&
		cfg.EffectiveCountVerifierWait()
}

// placeOccupant puts occ in the first idle lane of type t. It reports false
// when every lane of the type is taken.
func placeOccupant(lanes []Lane, t LaneType, occ LaneOccupant) bool {
	for i := range lanes {
		if lanes[i].Type == t && lanes[i].Occupant == nil {
			o := occ
			lanes[i].Occupant = &o
			return true
		}
	}
	return false
}

func evidenceBlocker(err string) string {
	return "pool evidence unavailable: " + err
}

// laneFillOrder sorts candidates: Priority descending, then CreatedAt
// ascending, then ID, so the order never depends on the input order.
func laneFillOrder(list []LaneFillCandidate) {
	sort.SliceStable(list, func(i, j int) bool { return candidateBefore(list[i], list[j]) })
}

func candidateBefore(a, b LaneFillCandidate) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

// laneFillExclusion is queued work that a lane may not take, with the reason.
type laneFillExclusion struct {
	cand   LaneFillCandidate
	reason string
}

func backendFixedReason(c LaneFillCandidate) string {
	return fmt.Sprintf("PRD %s: backend fixed at %s, cannot move to the pool", c.ID, c.Backend)
}

// poolAccepts reports whether the Automaton's worker can be on the GPU pool:
// it is already on the GPU backend, or its backend may still change.
func poolAccepts(cfg LaneFillConfig, c LaneFillCandidate) bool {
	return c.Backend == cfg.EffectiveGPUBackend() || c.BackendChangeAllowed
}

// startReason words the reason for a start_prd decision on a lane.
func startReason(cfg LaneFillConfig, lane Lane, c LaneFillCandidate, pref string) string {
	target := cfg.EffectiveClaudeBackend()
	if lane.Type == LaneTypeGPU {
		target = cfg.EffectiveGPUBackend()
	}
	r := fmt.Sprintf("first eligible work for idle %s lane %s (%s preference, priority %d)",
		lane.Type, lane.ID, pref, c.Priority)
	if c.Backend != target {
		if c.BackendChangeAllowed {
			r += fmt.Sprintf("; backend would change from %s to %s", c.Backend, target)
		} else {
			r += fmt.Sprintf("; backend stays %s", c.Backend)
		}
	}
	return r
}

// PlanLaneFill decides, for one snapshot, what each idle lane should take.
// It is the whole lane fill rule set and is independent of the mode.
//
// Per idle GPU lane, then per idle Claude lane, in lane order, it takes the
// first eligible Automaton by source (ready, then draft to decompose),
// Priority descending, CreatedAt ascending, taking the lane's own preference
// first and then any. One Automaton fills one lane and is never used twice in
// a poll. Merge-needed Automata are reported once each and never started. An
// idle lane with nothing usable gets an idle decision whose Blocker names an
// external reason. Last, it asks for a decompose ahead for each lane type
// whose ready queue is short.
//
// Decisions come out in a fixed order: lane decisions (GPU lanes, then Claude
// lanes), blocked Automata, merge reports, decompose-ahead (GPU, then Claude).
func PlanLaneFill(in LaneFillInput) []LaneFillDecision {
	cfg := in.Config
	mode := LaneFillMode(cfg.EffectiveMode())

	// Lanes in a fixed order: GPU first, input order within a type.
	lanes := append([]Lane(nil), in.Lanes...)
	sort.SliceStable(lanes, func(i, j int) bool { return laneTypeRank(lanes[i].Type) < laneTypeRank(lanes[j].Type) })
	laneCount := map[LaneType]int{}
	occupied := map[string]bool{}
	for _, l := range lanes {
		laneCount[l.Type]++
		if l.Occupant != nil && l.Occupant.PRDID != "" {
			occupied[l.Occupant.PRDID] = true
		}
	}

	// Classify every candidate.
	var ready, drafts, merges []LaneFillCandidate
	var exclusions []laneFillExclusion
	for _, c := range in.Candidates {
		if occupied[c.ID] {
			continue
		}
		src, why := ClassifyLaneFillCandidate(c)
		switch src {
		case LaneFillSourceReady:
			ready = append(ready, c)
		case LaneFillSourceDraft:
			drafts = append(drafts, c)
		case LaneFillSourceMerge:
			merges = append(merges, c)
		default:
			if why != "" {
				exclusions = append(exclusions, laneFillExclusion{cand: c, reason: why})
			}
		}
	}
	laneFillOrder(ready)
	laneFillOrder(drafts)
	laneFillOrder(merges)
	sort.SliceStable(exclusions, func(i, j int) bool {
		a, b := exclusions[i].cand, exclusions[j].cand
		ar, br := statusRank(a.Status), statusRank(b.Status)
		if ar != br {
			return ar < br
		}
		return candidateBefore(a, b)
	})

	// Gpu-preference work whose backend cannot move to the pool is blocked
	// and never starts on a Claude lane.
	var blockedPref []LaneFillCandidate
	for _, c := range ready {
		if p, _ := NormalizeLanePreference(c.LanePreference); p == LanePreferenceGPU && !poolAccepts(cfg, c) {
			blockedPref = append(blockedPref, c)
		}
	}

	used := map[string]bool{}
	decomposing := map[LaneType]int{}
	for t, n := range in.InFlightDecomposeByType {
		decomposing[t] = n
	}
	decomposeRoom := func(t LaneType) bool { return decomposing[t] < laneCount[t] }

	var laneDecisions []LaneFillDecision
	// pendingIdle remembers idle lanes so their blocker can be worded after
	// every lane has been assigned.
	type pendingIdle struct {
		index  int
		lane   Lane
		capped *LaneFillCandidate
	}
	var pending []pendingIdle

	newDecision := func(l Lane, a LaneFillAction) LaneFillDecision {
		return LaneFillDecision{
			At: in.Now, Mode: mode, LaneID: l.ID, LaneType: l.Type, Action: a,
			RepeatCount: 1, LastSeenAt: in.Now,
		}
	}

	// A lane is busy by the one rule in laneFree: a Claude lane whose occupant
	// waits on a verifier slot is free for a new start when the rule is on, and
	// every other occupied lane is busy. Free Claude lanes are therefore
	// claude_lanes minus the working Claude occupants minus the starts planned
	// on earlier lanes in this poll, which bounds working Claude Automata;
	// waiting ones never count against the cap because a working occupant is
	// always placed before a waiting one (BuildLanes).
	for _, lane := range lanes {
		if !laneFree(cfg, lane) {
			continue
		}
		if lane.Type == LaneTypeGPU && in.EvidenceErr != "" && lane.Blocker == "" {
			lane.Blocker = evidenceBlocker(in.EvidenceErr)
		}
		if lane.Blocker != "" {
			d := newDecision(lane, LaneFillActionBlocked)
			d.Blocker = lane.Blocker
			d.Reason = "lane cannot be called idle"
			laneDecisions = append(laneDecisions, d)
			continue
		}

		// Source 1: ready work.
		if c, pref, ok := pickReady(cfg, lane, ready, used); ok {
			used[c.ID] = true
			d := newDecision(lane, LaneFillActionStartPRD)
			d.PRDID, d.Title, d.Reason = c.ID, c.Title, startReason(cfg, lane, c, pref)
			laneDecisions = append(laneDecisions, d)
			continue
		}

		// Source 3: a draft to decompose.
		var capped *LaneFillCandidate
		if c, ok := pickDraft(lane.Type, drafts, used); ok {
			if decomposeRoom(lane.Type) {
				used[c.ID] = true
				decomposing[lane.Type]++
				d := newDecision(lane, LaneFillActionDecompose)
				d.PRDID, d.Title = c.ID, c.Title
				d.Reason = fmt.Sprintf("no ready work for idle %s lane %s; decompose the next draft", lane.Type, lane.ID)
				laneDecisions = append(laneDecisions, d)
				continue
			}
			cc := c
			capped = &cc
		}

		d := newDecision(lane, LaneFillActionIdle)
		laneDecisions = append(laneDecisions, d)
		pending = append(pending, pendingIdle{index: len(laneDecisions) - 1, lane: lane, capped: capped})
	}

	// Word the blockers of the idle lanes now that every assignment is made.
	for _, p := range pending {
		d := &laneDecisions[p.index]
		d.Blocker = idleBlocker(cfg, p.lane, exclusions, ready, used, p.capped, decomposing[p.lane.Type])
		d.Reason = fmt.Sprintf("idle %s lane %s has no usable work", p.lane.Type, p.lane.ID)
	}

	var out []LaneFillDecision
	out = append(out, laneDecisions...)

	for _, c := range blockedPref {
		out = append(out, LaneFillDecision{
			At: in.Now, Mode: mode, LaneType: LaneTypeGPU, Action: LaneFillActionBlocked,
			PRDID: c.ID, Title: c.Title,
			Reason:      "gpu preference, but the worker backend cannot move to the pool; never started on a Claude lane",
			Blocker:     backendFixedReason(c),
			RepeatCount: 1, LastSeenAt: in.Now,
		})
	}

	for _, c := range merges {
		out = append(out, LaneFillDecision{
			At: in.Now, Mode: mode, Action: LaneFillActionReportMerge,
			PRDID: c.ID, Title: c.Title,
			Reason:      "all tasks done and the work needs a merge; reported only, lane fill never merges",
			RepeatCount: 1, LastSeenAt: in.Now,
		})
	}

	// Decompose ahead: keep decompose_ahead_min ready items per lane type.
	queued := QueuedEligible(in.Candidates)
	minReady := cfg.EffectiveDecomposeAheadMin()
	for _, t := range []LaneType{LaneTypeGPU, LaneTypeClaude} {
		if laneCount[t] == 0 || queued[t] >= minReady || !decomposeRoom(t) {
			continue
		}
		if t == LaneTypeGPU && in.EvidenceErr != "" {
			// Do not start work on a pool whose state is unknown.
			continue
		}
		c, ok := oldestDraft(t, drafts, used)
		if !ok {
			continue
		}
		used[c.ID] = true
		decomposing[t]++
		out = append(out, LaneFillDecision{
			At: in.Now, Mode: mode, LaneType: t, Action: LaneFillActionDecomposeAhead,
			PRDID: c.ID, Title: c.Title,
			Reason:      fmt.Sprintf("%d ready for %s lanes, fewer than %d; decompose the oldest fitting draft", queued[t], t, minReady),
			RepeatCount: 1, LastSeenAt: in.Now,
		})
	}
	return out
}

func laneTypeRank(t LaneType) int {
	if t == LaneTypeGPU {
		return 0
	}
	return 1
}

func statusRank(s PRDStatus) int {
	if NormalizePRDStatus(s) == PRDDraft {
		return 1
	}
	return 0
}

// pickReady chooses the ready Automaton for an idle lane: the lane's own
// preference first, then any. pref names the preference group it came from.
func pickReady(cfg LaneFillConfig, lane Lane, ready []LaneFillCandidate, used map[string]bool) (LaneFillCandidate, string, bool) {
	own := LanePreferenceGPU
	if lane.Type == LaneTypeClaude {
		own = LanePreferenceClaude
	}
	for _, group := range []string{own, LanePreferenceAny} {
		for _, c := range ready {
			if used[c.ID] {
				continue
			}
			p, _ := NormalizeLanePreference(c.LanePreference)
			if effectiveLanePreference(p) != group {
				continue
			}
			if lane.Type == LaneTypeGPU && !poolAccepts(cfg, c) {
				continue
			}
			return c, group, true
		}
	}
	return LaneFillCandidate{}, "", false
}

// pickDraft chooses the draft an idle lane decomposes: the lane's own
// preference first, then any, in the usual order.
func pickDraft(t LaneType, drafts []LaneFillCandidate, used map[string]bool) (LaneFillCandidate, bool) {
	own := LanePreferenceGPU
	if t == LaneTypeClaude {
		own = LanePreferenceClaude
	}
	for _, group := range []string{own, LanePreferenceAny} {
		for _, c := range drafts {
			if used[c.ID] {
				continue
			}
			p, _ := NormalizeLanePreference(c.LanePreference)
			if effectiveLanePreference(p) == group {
				return c, true
			}
		}
	}
	return LaneFillCandidate{}, false
}

// oldestDraft returns the oldest draft that fits lane type t.
func oldestDraft(t LaneType, drafts []LaneFillCandidate, used map[string]bool) (LaneFillCandidate, bool) {
	var best LaneFillCandidate
	found := false
	for _, c := range drafts {
		if used[c.ID] || !preferenceFitsLane(c.LanePreference, t) {
			continue
		}
		if !found || c.CreatedAt.Before(best.CreatedAt) ||
			(c.CreatedAt.Equal(best.CreatedAt) && c.ID < best.ID) {
			best, found = c, true
		}
	}
	return best, found
}

// idleBlocker words why an idle lane got nothing: the first queued Automaton
// that was kept out and why, else a decompose that has to wait, else that
// there is no eligible work. It never names the planner's own review backlog.
func idleBlocker(cfg LaneFillConfig, lane Lane, exclusions []laneFillExclusion, ready []LaneFillCandidate,
	used map[string]bool, capped *LaneFillCandidate, inFlight int) string {
	// Excluded (held, chained) work that fits the lane type.
	for _, e := range exclusions {
		if preferenceFitsLane(e.cand.LanePreference, lane.Type) {
			return e.reason
		}
	}
	// Ready work the GPU pool cannot take and no Claude lane took.
	if lane.Type == LaneTypeGPU {
		for _, c := range ready {
			if !used[c.ID] && !poolAccepts(cfg, c) && preferenceFitsLane(c.LanePreference, LaneTypeGPU) {
				return backendFixedReason(c)
			}
		}
	}
	if capped != nil {
		return fmt.Sprintf("PRD %s: decompose waits, %d already in flight for %s lanes", capped.ID, inFlight, lane.Type)
	}
	return "no eligible work"
}
