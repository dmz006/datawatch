package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file is the lane utilization read mode: how busy each lane was over a
// window, which Automata ran on it, how long work waited to start, whether a
// lane sat idle with eligible work, the share of busy time per worker
// backend, the lane fill mode with its newest decisions, and plan usage in
// points and tokens. Everything is derived from the stores and the lane fill
// state; nothing here is persisted, starts a goroutine or reads the clock (the
// caller passes the time). No field carries a currency amount: plan usage is
// points and tokens only.

const (
	// LaneUtilizationDefaultWindow is the window used when none is asked for.
	LaneUtilizationDefaultWindow = 24 * time.Hour
	// LaneUtilizationMinWindow and LaneUtilizationMaxWindow clamp the window.
	LaneUtilizationMinWindow = time.Hour
	LaneUtilizationMaxWindow = 30 * 24 * time.Hour
	// LaneUtilizationDefaultBuckets is the bucket count used when none is
	// asked for; the count is clamped to 1..LaneUtilizationMaxBuckets.
	LaneUtilizationDefaultBuckets = 24
	LaneUtilizationMaxBuckets     = 288
	// LaneUtilizationDecisionLimit is how many of the newest lane fill
	// decisions the view carries.
	LaneUtilizationDecisionLimit = 20

	// laneUtilizationOtherType is the lane type of a backend that belongs to
	// no GPU or Claude lane; each such backend gets one lane of its own.
	laneUtilizationOtherType LaneType = "other"
)

// LaneUtilizationUsage is the plan usage a usage source reports for a window:
// the weekly points used and the points the configured plan holds, and the
// tokens spent. There is no currency field by design.
type LaneUtilizationUsage struct {
	Points     float64
	PlanPoints float64
	Tokens     int64
}

// LaneUtilizationUsageSource supplies plan usage for a window. The daemon
// wires it from the usage calculator and the spend ledger.
type LaneUtilizationUsageSource interface {
	LaneUtilizationUsage(start, end time.Time) LaneUtilizationUsage
}

// SetLaneUtilizationUsageSource sets where plan usage comes from. Without one
// the view reports zero usage against no plan.
func (m *Manager) SetLaneUtilizationUsageSource(src LaneUtilizationUsageSource) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.laneUtilUsage = src
	m.mu.Unlock()
}

// LaneUtilizationRun is one task run on a lane.
type LaneUtilizationRun struct {
	PRDID   string    `json:"prd_id"`
	Title   string    `json:"title"`
	Stage   string    `json:"stage"`
	Kind    string    `json:"kind"`
	Backend string    `json:"backend"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
}

// LaneUtilizationQueueWait is the wait between queue entry and start for work
// that started on a lane inside the window.
type LaneUtilizationQueueWait struct {
	Count       int     `json:"count"`
	MeanSeconds float64 `json:"mean_seconds"`
	MaxSeconds  float64 `json:"max_seconds"`
}

// LaneUtilizationBucket is the busy share of one slice of the window.
type LaneUtilizationBucket struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	BusyShare float64   `json:"busy_share"`
}

// LaneUtilizationLane is one lane over the window.
type LaneUtilizationLane struct {
	ID          string                   `json:"id"`
	Type        LaneType                 `json:"type"`
	BusySeconds float64                  `json:"busy_seconds"`
	IdleSeconds float64                  `json:"idle_seconds"`
	BusyShare   float64                  `json:"busy_share"`
	IdleShare   float64                  `json:"idle_share"`
	Runs        []LaneUtilizationRun     `json:"runs"`
	QueueWait   LaneUtilizationQueueWait `json:"queue_wait"`
	// IdleWithEligibleWork is set while the lane is idle and work it could
	// take is queued; the duration and the first item go with it.
	IdleWithEligibleWork    bool                    `json:"idle_with_eligible_work"`
	IdleWithEligibleSeconds float64                 `json:"idle_with_eligible_seconds"`
	FirstEligibleItem       string                  `json:"first_eligible_item,omitempty"`
	Buckets                 []LaneUtilizationBucket `json:"buckets"`
}

// LaneUtilizationBackendShare is one backend's share of all busy time.
type LaneUtilizationBackendShare struct {
	Backend     string  `json:"backend"`
	BusySeconds float64 `json:"busy_seconds"`
	Share       float64 `json:"share"`
}

// LaneUtilizationFill is the lane fill mode and its newest decisions.
type LaneUtilizationFill struct {
	Mode      string             `json:"mode"`
	Modes     LaneFillKindModes  `json:"modes"`
	Decisions []LaneFillDecision `json:"decisions"`
}

// LaneUtilizationPlan is plan usage in points and tokens against the plan.
type LaneUtilizationPlan struct {
	PlanPoints    float64 `json:"plan_points"`
	Points        float64 `json:"points"`
	Tokens        int64   `json:"tokens"`
	PercentOfPlan float64 `json:"percent_of_plan"`
}

// LaneUtilization is the read mode result.
type LaneUtilization struct {
	WindowStart   time.Time                     `json:"window_start"`
	WindowEnd     time.Time                     `json:"window_end"`
	WindowSeconds float64                       `json:"window_seconds"`
	BucketCount   int                           `json:"bucket_count"`
	Lanes         []LaneUtilizationLane         `json:"lanes"`
	BackendMix    []LaneUtilizationBackendShare `json:"backend_mix"`
	LaneFill      LaneUtilizationFill           `json:"lane_fill"`
	PlanUsage     LaneUtilizationPlan           `json:"plan_usage"`
}

// laneUtilizationTask is one task run as read from the store, with the
// backend already resolved.
type laneUtilizationTask struct {
	prdID, title, stage, kind, backend string
	start                              time.Time
	// end is zero while the task is still running.
	end time.Time
	// queued is when the work entered the queue (zero when unknown).
	queued time.Time
}

// laneUtilizationInput is everything the builder reads. The builder is a pure
// function of it.
type laneUtilizationInput struct {
	cfg   LaneFillConfig
	tasks []laneUtilizationTask
	// workSince is, per lane id, when the lane was first seen idle with
	// eligible work; firstEligible is the item it could have taken.
	workSince     map[string]time.Time
	firstEligible map[string]string
	status        LaneFillStatus
	usage         LaneUtilizationUsage
}

// ClampLaneUtilizationWindow applies the default, minimum and maximum window.
func ClampLaneUtilizationWindow(w time.Duration) time.Duration {
	switch {
	case w <= 0:
		return LaneUtilizationDefaultWindow
	case w < LaneUtilizationMinWindow:
		return LaneUtilizationMinWindow
	case w > LaneUtilizationMaxWindow:
		return LaneUtilizationMaxWindow
	}
	return w
}

// ClampLaneUtilizationBuckets applies the default and the bucket count range.
func ClampLaneUtilizationBuckets(n int) int {
	switch {
	case n <= 0:
		return LaneUtilizationDefaultBuckets
	case n > LaneUtilizationMaxBuckets:
		return LaneUtilizationMaxBuckets
	}
	return n
}

// LaneUtilization returns how the lanes were used over the window ending at
// now. The window and the bucket count are clamped (see the constants). It is
// nil-safe: without a Manager it reports an empty window of idle default
// lanes.
func (m *Manager) LaneUtilization(window time.Duration, buckets int, now time.Time) LaneUtilization {
	if m == nil {
		return buildLaneUtilization(laneUtilizationInput{cfg: DefaultLaneFillConfig()}, window, buckets, now)
	}
	m.mu.Lock()
	cfg := m.cfg.LaneFill
	execution := m.cfg.ExecutionBackend
	c := m.coordinator
	usageSrc := m.laneUtilUsage
	m.mu.Unlock()

	w := ClampLaneUtilizationWindow(window)
	start := now.Add(-w)
	in := laneUtilizationInput{
		cfg:           cfg,
		workSince:     map[string]time.Time{},
		firstEligible: map[string]string{},
		status:        m.LaneFillStatus(LaneUtilizationDecisionLimit),
	}
	in.tasks = m.laneUtilizationTasks(execution, start, now)
	if c != nil {
		c.lf.mu.Lock()
		for id, t := range c.lf.workSince {
			in.workSince[id] = t
		}
		c.lf.mu.Unlock()
		in.firstEligible = laneUtilizationFirstEligible(in.status)
	}
	if usageSrc != nil {
		in.usage = usageSrc.LaneUtilizationUsage(start, now)
	}
	return buildLaneUtilization(in, window, buckets, now)
}

// laneUtilizationFirstEligible names, per lane, the item it could have taken:
// the newest alert's work, else the newest decision that names an Automaton.
func laneUtilizationFirstEligible(st LaneFillStatus) map[string]string {
	out := map[string]string{}
	for _, a := range st.Alerts {
		if _, ok := out[a.LaneID]; !ok && a.Work != "" {
			out[a.LaneID] = a.Work
		}
	}
	for _, d := range st.Decisions {
		if d.LaneID == "" || d.PRDID == "" {
			continue
		}
		if _, ok := out[d.LaneID]; !ok {
			out[d.LaneID] = fmt.Sprintf("%s (%s)", d.Title, d.PRDID)
		}
	}
	return out
}

// laneUtilizationTasks copies every task that started before now and ended
// (or is still running) after start, resolving its worker backend with the
// resolver the executor uses.
func (m *Manager) laneUtilizationTasks(execution string, start, now time.Time) []laneUtilizationTask {
	if m.store == nil {
		return nil
	}
	type rawTask struct {
		task    Task
		story   Story
		prd     PRD
		stage   string
		queued  time.Time
		running bool
	}
	var raws []rawTask
	prds := m.store.ListPRDs()
	m.readLocked(func() {
		for _, p := range prds {
			for si := range p.Story {
				s := &p.Story[si]
				for ti := range s.Tasks {
					t := &s.Tasks[ti]
					if t.StartedAt == nil || t.StartedAt.After(now) {
						continue
					}
					end := laneUtilizationTaskEnd(t, now)
					if end.Before(start) {
						continue
					}
					q := t.CreatedAt
					if p.ApprovedAt != nil && p.ApprovedAt.After(q) {
						q = *p.ApprovedAt
					}
					raws = append(raws, rawTask{
						task:   Task{Backend: t.Backend, Model: t.Model, AdmittedBackend: t.AdmittedBackend, AdmittedModel: t.AdmittedModel, ID: t.ID, Title: t.Title, Kind: t.Kind, Status: t.Status, StartedAt: t.StartedAt, CompletedAt: t.CompletedAt, UpdatedAt: t.UpdatedAt},
						story:  Story{Backend: s.Backend, Model: s.Model},
						prd:    PRD{ID: p.ID, Title: p.Title, Backend: p.Backend, Model: p.Model, AutomationID: p.AutomationID},
						stage:  s.Title,
						queued: q,
					})
				}
			}
		}
	})
	out := make([]laneUtilizationTask, 0, len(raws))
	for _, r := range raws {
		var auto *Automation
		if r.prd.AutomationID != "" && m.automations != nil {
			auto, _ = m.automations.Get(r.prd.AutomationID)
		}
		task, story, prd := r.task, r.story, r.prd
		backend, _ := admittedWorker(&task, &story, &prd, auto, execution)
		var end time.Time
		if !laneFillRunningTask[task.Status] {
			end = laneUtilizationTaskEnd(&task, now)
		}
		out = append(out, laneUtilizationTask{
			prdID: prd.ID, title: prd.Title, stage: r.stage, kind: task.Kind,
			backend: backend, start: *task.StartedAt, end: end, queued: r.queued,
		})
	}
	return out
}

// laneUtilizationTaskEnd is when a task stopped using its lane: its
// completion time, else (a task that ended without one) its last update, and
// now while it is still running.
func laneUtilizationTaskEnd(t *Task, now time.Time) time.Time {
	if laneFillRunningTask[t.Status] {
		return now
	}
	if t.CompletedAt != nil {
		return *t.CompletedAt
	}
	if !t.UpdatedAt.IsZero() && t.StartedAt != nil && t.UpdatedAt.After(*t.StartedAt) {
		return t.UpdatedAt
	}
	if t.StartedAt != nil {
		return *t.StartedAt
	}
	return now
}

type laneInterval struct{ start, end time.Time }

// mergeIntervals clips intervals to [lo, hi] and joins the ones that touch or
// overlap, so overlapping runs on one lane count once.
func mergeIntervals(in []laneInterval, lo, hi time.Time) []laneInterval {
	clipped := make([]laneInterval, 0, len(in))
	for _, iv := range in {
		if iv.start.Before(lo) {
			iv.start = lo
		}
		if iv.end.After(hi) {
			iv.end = hi
		}
		if iv.end.After(iv.start) {
			clipped = append(clipped, iv)
		}
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].start.Before(clipped[j].start) })
	var out []laneInterval
	for _, iv := range clipped {
		if n := len(out); n > 0 && !iv.start.After(out[n-1].end) {
			if iv.end.After(out[n-1].end) {
				out[n-1].end = iv.end
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

func intervalSeconds(ivs []laneInterval, lo, hi time.Time) float64 {
	var total float64
	for _, iv := range ivs {
		s, e := iv.start, iv.end
		if s.Before(lo) {
			s = lo
		}
		if e.After(hi) {
			e = hi
		}
		if e.After(s) {
			total += e.Sub(s).Seconds()
		}
	}
	return total
}

// buildLaneUtilization is the pure builder.
func buildLaneUtilization(in laneUtilizationInput, window time.Duration, buckets int, now time.Time) LaneUtilization {
	w := ClampLaneUtilizationWindow(window)
	nb := ClampLaneUtilizationBuckets(buckets)
	start := now.Add(-w)
	res := LaneUtilization{
		WindowStart: start, WindowEnd: now, WindowSeconds: w.Seconds(), BucketCount: nb,
		Lanes:      []LaneUtilizationLane{},
		BackendMix: []LaneUtilizationBackendShare{},
	}

	// The lanes: every configured GPU and Claude lane, then one lane per other
	// backend that ran something.
	lanes := BuildLanes(in.cfg, nil, LaneEvidence{}).Lanes
	type laneAcc struct {
		out   LaneUtilizationLane
		ivs   []laneInterval
		waits []float64
	}
	var accs []*laneAcc
	byID := map[string]*laneAcc{}
	addLane := func(id string, t LaneType) *laneAcc {
		a := &laneAcc{out: LaneUtilizationLane{ID: id, Type: t, Runs: []LaneUtilizationRun{}, Buckets: []LaneUtilizationBucket{}}}
		accs = append(accs, a)
		byID[id] = a
		return a
	}
	for _, l := range lanes {
		addLane(l.ID, l.Type)
	}

	tasks := append([]laneUtilizationTask(nil), in.tasks...)
	sort.SliceStable(tasks, func(i, j int) bool {
		if !tasks[i].start.Equal(tasks[j].start) {
			return tasks[i].start.Before(tasks[j].start)
		}
		return tasks[i].prdID < tasks[j].prdID
	})
	// busyEnd tracks when each lane last became free, so a new run takes the
	// first lane of its type that is free at its start; when all are taken it
	// goes on the one that frees earliest (the overlap then counts once).
	busyEnd := map[string]time.Time{}
	backendBusy := map[string]float64{}
	for _, t := range tasks {
		end := t.end
		if end.IsZero() || end.After(now) {
			end = now
		}
		if end.Before(start) || t.start.After(now) {
			continue
		}
		lt, ok := LaneTypeForBackend(in.cfg, t.backend)
		if !ok {
			lt = laneUtilizationOtherType
		}
		var target *laneAcc
		if lt == laneUtilizationOtherType {
			id := "other-" + strings.TrimSpace(t.backend)
			if target = byID[id]; target == nil {
				target = addLane(id, laneUtilizationOtherType)
			}
		} else {
			for _, a := range accs {
				if a.out.Type != lt {
					continue
				}
				if target == nil || busyEnd[a.out.ID].Before(busyEnd[target.out.ID]) {
					target = a
				}
				if !busyEnd[a.out.ID].After(t.start) {
					target = a
					break
				}
			}
		}
		if target == nil {
			// A lane type with no configured lane: it still ran, so give it one.
			target = addLane(fmt.Sprintf("%s-1", lt), lt)
		}
		if end.After(busyEnd[target.out.ID]) {
			busyEnd[target.out.ID] = end
		}
		runStart := t.start
		target.out.Runs = append(target.out.Runs, LaneUtilizationRun{
			PRDID: t.prdID, Title: t.title, Stage: t.stage, Kind: laneUtilizationKind(t.kind),
			Backend: t.backend, Start: runStart, End: end,
		})
		target.ivs = append(target.ivs, laneInterval{runStart, end})
		if !t.start.Before(start) && !t.queued.IsZero() {
			wait := t.start.Sub(t.queued).Seconds()
			if wait < 0 {
				wait = 0
			}
			target.waits = append(target.waits, wait)
		}
	}

	windowSecs := w.Seconds()
	bucketLen := w / time.Duration(nb)
	for _, a := range accs {
		merged := mergeIntervals(a.ivs, start, now)
		busy := intervalSeconds(merged, start, now)
		a.out.BusySeconds = busy
		a.out.IdleSeconds = windowSecs - busy
		a.out.BusyShare = busy / windowSecs
		a.out.IdleShare = a.out.IdleSeconds / windowSecs
		for i := 0; i < nb; i++ {
			bs := start.Add(time.Duration(i) * bucketLen)
			be := bs.Add(bucketLen)
			if i == nb-1 {
				be = now
			}
			a.out.Buckets = append(a.out.Buckets, LaneUtilizationBucket{
				Start: bs, End: be, BusyShare: intervalSeconds(merged, bs, be) / be.Sub(bs).Seconds(),
			})
		}
		if n := len(a.waits); n > 0 {
			var sum, max float64
			for _, x := range a.waits {
				sum += x
				if x > max {
					max = x
				}
			}
			a.out.QueueWait = LaneUtilizationQueueWait{Count: n, MeanSeconds: sum / float64(n), MaxSeconds: max}
		}
		if since, ok := in.workSince[a.out.ID]; ok && !since.After(now) {
			a.out.IdleWithEligibleWork = true
			a.out.IdleWithEligibleSeconds = now.Sub(since).Seconds()
			a.out.FirstEligibleItem = in.firstEligible[a.out.ID]
		}
		res.Lanes = append(res.Lanes, a.out)
	}

	// Backend mix: each backend's share of the busy time, counting a lane's
	// overlapping runs once (time on the lane is split across the backends
	// running at each moment by clipping each run to the lane's busy time in
	// order of start).
	for _, a := range accs {
		covered := []laneInterval{}
		for _, r := range a.out.Runs {
			own := mergeIntervals([]laneInterval{{r.Start, r.End}}, start, now)
			var fresh float64
			for _, iv := range own {
				fresh += intervalSeconds([]laneInterval{iv}, start, now)
			}
			// Subtract what earlier runs on this lane already covered.
			overlap := intervalOverlapSeconds(own, covered)
			backendBusy[r.Backend] += fresh - overlap
			covered = mergeIntervals(append(covered, own...), start, now)
		}
	}
	var totalBusy float64
	for _, v := range backendBusy {
		totalBusy += v
	}
	for b, v := range backendBusy {
		share := 0.0
		if totalBusy > 0 {
			share = v / totalBusy
		}
		res.BackendMix = append(res.BackendMix, LaneUtilizationBackendShare{Backend: b, BusySeconds: v, Share: share})
	}
	sort.Slice(res.BackendMix, func(i, j int) bool {
		if res.BackendMix[i].BusySeconds != res.BackendMix[j].BusySeconds {
			return res.BackendMix[i].BusySeconds > res.BackendMix[j].BusySeconds
		}
		return res.BackendMix[i].Backend < res.BackendMix[j].Backend
	})

	res.LaneFill = LaneUtilizationFill{
		Mode: in.status.Mode, Modes: in.status.Modes, Decisions: in.status.Decisions,
	}
	if res.LaneFill.Decisions == nil {
		res.LaneFill.Decisions = []LaneFillDecision{}
	}
	if res.LaneFill.Mode == "" {
		res.LaneFill.Mode = in.cfg.EffectiveMode()
		res.LaneFill.Modes = laneFillKindModes(in.cfg)
	}
	if len(res.LaneFill.Decisions) > LaneUtilizationDecisionLimit {
		res.LaneFill.Decisions = res.LaneFill.Decisions[:LaneUtilizationDecisionLimit]
	}

	res.PlanUsage = LaneUtilizationPlan{
		PlanPoints: in.usage.PlanPoints, Points: in.usage.Points, Tokens: in.usage.Tokens,
	}
	if in.usage.PlanPoints > 0 {
		res.PlanUsage.PercentOfPlan = in.usage.Points / in.usage.PlanPoints * 100
	}
	return res
}

// intervalOverlapSeconds is the length of time a and b both cover. Both are
// sorted and non-overlapping within themselves.
func intervalOverlapSeconds(a, b []laneInterval) float64 {
	var total float64
	for _, x := range a {
		for _, y := range b {
			s, e := x.start, x.end
			if y.start.After(s) {
				s = y.start
			}
			if y.end.Before(e) {
				e = y.end
			}
			if e.After(s) {
				total += e.Sub(s).Seconds()
			}
		}
	}
	return total
}

func laneUtilizationKind(k string) string {
	if k == "" {
		return "task"
	}
	return k
}
