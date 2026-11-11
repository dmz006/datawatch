// api_dashboard_arc.go — the arc summary: GET /api/dashboard/arc.
//
// The arc summary shows one arc (an Automation) as its stages in order —
// bugs, features, then the final stage — each with total, done, running and
// blocked counts and a percent. Maintenance work is a separate lane that
// reports counts only; it is never a stage. When a stage is blocked the
// summary says so in plain language. It also carries the shared lane usage
// (Claude and pi lanes: in flight, cap, and queued work by priority) and the
// plan usage in points and tokens. No money figure appears anywhere: the
// plan is flat, so only points and tokens are reported.
//
// Like the console, the endpoint is always readable. A subsystem this daemon
// does not have yields available=false with a reason, never an HTTP 5xx, and
// dashboard.console.enabled=false hides the whole summary the same way.
//
//	GET /api/dashboard/arc   dashboard:read   (optional ?arc=<automation id>)
package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// The stage ids of an arc, in the order they run.
const (
	arcStageBugs     = "bugs"
	arcStageFeatures = "features"
	arcStageFinal    = "final"
)

// arcStageOrder is the fixed order the stages are reported in.
var arcStageOrder = []string{arcStageBugs, arcStageFeatures, arcStageFinal}

// arcMember is one member of an arc as the builder reads it.
type arcMember struct {
	Title string
	// Stage is the operator's stage label; Maintenance members carry a
	// label naming maintenance.
	Stage  string
	Status string // pending, running, completed, blocked, failed, skipped, cancelled
	// BlockedReason is the member's own plain-language reason, when it has one.
	BlockedReason string
}

// arcInfo is one arc as the builder reads it.
type arcInfo struct {
	ID      string
	Name    string
	Members []arcMember
}

// arcSource supplies the arcs. Arcs returns every arc known to the daemon.
type arcSource interface {
	Arcs() []arcInfo
}

// arcLane is one lane kind's live numbers.
type arcLane struct {
	Kind     string `json:"kind"` // claude or pi
	InFlight int    `json:"in_flight"`
	Cap      int    `json:"cap"`
}

// arcQueued is queued work of one priority on one lane kind.
type arcQueued struct {
	Lane     string `json:"lane"`
	Priority int    `json:"priority"`
	Count    int    `json:"count"`
}

// arcLaneSource supplies the shared lane usage.
type arcLaneSource interface {
	Lanes() ([]arcLane, error)
	Queued() ([]arcQueued, error)
}

// arcPlanUsage is the plan usage in points, read from the usage calculator.
type arcPlanUsage struct {
	PointsUsed      float64
	PointsAllowance float64
}

// arcUsageSource supplies the plan usage in points.
type arcUsageSource interface {
	PlanUsage(ctx context.Context) (arcPlanUsage, error)
}

// arcTokenSource supplies the tokens used in the plan window.
type arcTokenSource interface {
	WindowTokens(ctx context.Context) (in, out int64, err error)
}

// arcBuilder holds the small interfaces the summary reads. A nil source
// reports its part as unavailable.
type arcBuilder struct {
	Arcs   arcSource
	Lanes  arcLaneSource
	Usage  arcUsageSource
	Tokens arcTokenSource
	Now    func() time.Time
}

// arcStage is one stage of the arc.
type arcStage struct {
	Stage   string  `json:"stage"`
	Total   int     `json:"total"`
	Done    int     `json:"done"`
	Running int     `json:"running"`
	Blocked int     `json:"blocked"`
	Percent float64 `json:"percent"`
}

// arcMaintenance is the maintenance lane: counts only.
type arcMaintenance struct {
	Total   int `json:"total"`
	Done    int `json:"done"`
	Running int `json:"running"`
	Blocked int `json:"blocked"`
}

// arcBlockedStage names a blocked stage and says why in plain language.
type arcBlockedStage struct {
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

type arcArcView struct {
	Available        bool             `json:"available"`
	Reason           string           `json:"reason,omitempty"`
	ID               string           `json:"id,omitempty"`
	Name             string           `json:"name,omitempty"`
	Stages           []arcStage       `json:"stages"`
	MaintenanceQueue *arcMaintenance  `json:"maintenance_queue,omitempty"`
	BlockedStage     *arcBlockedStage `json:"blocked_stage,omitempty"`
}

type arcLanesView struct {
	Available bool        `json:"available"`
	Reason    string      `json:"reason,omitempty"`
	Lanes     []arcLane   `json:"lanes"`
	Queued    []arcQueued `json:"queued_by_priority"`
}

type arcPoints struct {
	Used      float64 `json:"used"`
	Allowance float64 `json:"allowance"`
	Remaining float64 `json:"remaining"`
	Percent   float64 `json:"percent"`
}

type arcTokens struct {
	In    int64 `json:"in"`
	Out   int64 `json:"out"`
	Total int64 `json:"total"`
}

type arcUsageView struct {
	Available    bool       `json:"available"`
	Reason       string     `json:"reason,omitempty"`
	Points       *arcPoints `json:"points,omitempty"`
	TokensReason string     `json:"tokens_reason,omitempty"`
	Tokens       *arcTokens `json:"tokens,omitempty"`
}

// dashboardArcResponse is the body of GET /api/dashboard/arc.
type dashboardArcResponse struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Available   bool         `json:"available"`
	Reason      string       `json:"reason,omitempty"`
	Arc         arcArcView   `json:"arc"`
	Lanes       arcLanesView `json:"lanes"`
	Usage       arcUsageView `json:"usage"`
}

// arcStageOf maps an operator stage label to a stage id or "maintenance".
// An empty or unrecognised label returns "".
func arcStageOf(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	switch {
	case l == "":
		return ""
	case strings.Contains(l, "maint"):
		return "maintenance"
	case strings.Contains(l, "bug"):
		return arcStageBugs
	case strings.Contains(l, "feature"):
		return arcStageFeatures
	case strings.Contains(l, "final"):
		return arcStageFinal
	}
	return ""
}

func arcCount(st string, running, done, blocked *int) {
	switch st {
	case "completed", "skipped":
		*done++
	case "running":
		*running++
	case "blocked", "failed":
		*blocked++
	}
}

// Build assembles the summary. arcID selects an arc; empty picks the first
// arc that has any stage member.
func (b arcBuilder) Build(ctx context.Context, arcID string) dashboardArcResponse {
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	resp := dashboardArcResponse{GeneratedAt: now().UTC(), Available: true}
	resp.Arc = b.buildArc(arcID)
	resp.Lanes = b.buildLanes()
	resp.Usage = b.buildUsage(ctx)
	return resp
}

func (b arcBuilder) buildArc(arcID string) arcArcView {
	view := arcArcView{Stages: []arcStage{}}
	if b.Arcs == nil {
		view.Reason = "the autonomous manager is not wired in on this daemon (autonomous mode off)"
		return view
	}
	var pick *arcInfo
	arcs := b.Arcs.Arcs()
	for i := range arcs {
		a := &arcs[i]
		if arcID != "" {
			if a.ID == arcID {
				pick = a
				break
			}
			continue
		}
		for _, m := range a.Members {
			if arcStageOf(m.Stage) != "" {
				pick = a
				break
			}
		}
		if pick != nil {
			break
		}
	}
	if pick == nil {
		if arcID != "" {
			view.Reason = "no arc with id " + arcID + " was found"
		} else {
			view.Reason = "no arc with staged work was found"
		}
		return view
	}
	view.Available, view.ID, view.Name = true, pick.ID, pick.Name

	stages := map[string]*arcStage{}
	reasons := map[string]string{}
	for _, id := range arcStageOrder {
		stages[id] = &arcStage{Stage: id}
	}
	var maint arcMaintenance
	for _, m := range pick.Members {
		id := arcStageOf(m.Stage)
		switch id {
		case "":
			continue
		case "maintenance":
			maint.Total++
			arcCount(m.Status, &maint.Running, &maint.Done, &maint.Blocked)
		default:
			s := stages[id]
			s.Total++
			arcCount(m.Status, &s.Running, &s.Done, &s.Blocked)
			if (m.Status == "blocked" || m.Status == "failed") && reasons[id] == "" {
				reasons[id] = arcMemberReason(m)
			}
		}
	}
	for _, id := range arcStageOrder {
		s := stages[id]
		if s.Total > 0 {
			s.Percent = float64(int(float64(s.Done)/float64(s.Total)*1000+0.5)) / 10
		}
		view.Stages = append(view.Stages, *s)
		if view.BlockedStage == nil && s.Blocked > 0 {
			reason := reasons[id]
			if reason == "" {
				reason = fmt.Sprintf("%d piece(s) of work in the %s stage are blocked and need attention.", s.Blocked, id)
			}
			view.BlockedStage = &arcBlockedStage{Stage: id, Reason: reason}
		}
	}
	view.MaintenanceQueue = &maint
	return view
}

func arcMemberReason(m arcMember) string {
	name := strings.TrimSpace(m.Title)
	if name == "" {
		name = "a piece of work"
	}
	if r := strings.TrimSpace(m.BlockedReason); r != "" {
		return name + " is blocked: " + r
	}
	if m.Status == "failed" {
		return name + " failed and the stage cannot move on until it is dealt with."
	}
	return name + " is blocked and the stage cannot move on until it is unblocked."
}

func (b arcBuilder) buildLanes() arcLanesView {
	view := arcLanesView{Lanes: []arcLane{}, Queued: []arcQueued{}}
	if b.Lanes == nil {
		view.Reason = "neither lane fill nor the capacity ledger is wired in on this daemon"
		return view
	}
	lanes, err := b.Lanes.Lanes()
	if err != nil {
		view.Reason = err.Error()
		return view
	}
	view.Available = true
	view.Lanes = append(view.Lanes, lanes...)
	if q, qerr := b.Lanes.Queued(); qerr != nil {
		view.Reason = "queued work is unavailable: " + qerr.Error()
	} else {
		view.Queued = append(view.Queued, q...)
		sort.SliceStable(view.Queued, func(i, j int) bool {
			if view.Queued[i].Lane != view.Queued[j].Lane {
				return view.Queued[i].Lane < view.Queued[j].Lane
			}
			return view.Queued[i].Priority > view.Queued[j].Priority
		})
	}
	return view
}

func (b arcBuilder) buildUsage(ctx context.Context) arcUsageView {
	var view arcUsageView
	if b.Usage == nil {
		view.Reason = "the usage calculator is not wired in on this daemon"
	} else if pu, err := b.Usage.PlanUsage(ctx); err != nil {
		view.Reason = "plan usage is unavailable: " + err.Error()
	} else {
		view.Available = true
		p := &arcPoints{Used: pu.PointsUsed, Allowance: pu.PointsAllowance}
		p.Remaining = pu.PointsAllowance - pu.PointsUsed
		if pu.PointsAllowance > 0 {
			p.Percent = pu.PointsUsed / pu.PointsAllowance * 100
		}
		view.Points = p
	}
	if b.Tokens == nil {
		view.TokensReason = "token totals are not wired in on this daemon"
	} else if in, out, err := b.Tokens.WindowTokens(ctx); err != nil {
		view.TokensReason = "token totals are unavailable: " + err.Error()
	} else {
		view.Tokens = &arcTokens{In: in, Out: out, Total: in + out}
		view.Available = true
	}
	return view
}

// handleDashboardArc serves GET /api/dashboard/arc.
func (s *Server) handleDashboardArc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed: only GET is supported here", http.StatusNotImplemented)
		return
	}
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}
	if !consoleConfigOf(s.cfg).EffectiveEnabled() {
		writeJSONOK(w, dashboardArcResponse{
			GeneratedAt: time.Now().UTC(),
			Reason:      "the dashboard summary is disabled — set dashboard.console.enabled to true",
			Arc:         arcArcView{Stages: []arcStage{}},
			Lanes:       arcLanesView{Lanes: []arcLane{}, Queued: []arcQueued{}},
		})
		return
	}
	b := s.arcBuilder()
	writeJSONOK(w, b.Build(r.Context(), strings.TrimSpace(r.URL.Query().Get("arc"))))
}

// arcBuilder wires the live subsystems into the builder; a subsystem the
// daemon does not have stays nil.
func (s *Server) arcBuilder() arcBuilder {
	b := arcBuilder{}
	if s.autonomousMgr != nil {
		b.Arcs = serverArcSource{s: s}
		b.Lanes = serverArcLanes{s: s}
	} else if s.capacityLedger != nil {
		b.Lanes = serverArcLanes{s: s}
	}
	if s.usageCalc != nil {
		b.Usage = serverArcUsage{svc: s.usageCalc}
	}
	if t, ok := s.autonomousMgr.(arcTokenSource); ok && s.autonomousMgr != nil {
		b.Tokens = t
	}
	return b
}

type serverArcSource struct{ s *Server }

func (a serverArcSource) Arcs() []arcInfo {
	var out []arcInfo
	for _, raw := range a.s.autonomousMgr.ListAutomations() {
		am, ok := raw.(*autonomous.Automation)
		if !ok || am == nil {
			continue
		}
		info := arcInfo{ID: am.ID, Name: am.Name}
		for _, m := range am.Members {
			mem := arcMember{Stage: m.Stage, Status: string(m.Status)}
			if m.PRDID != "" {
				if v, found := a.s.autonomousMgr.GetPRD(m.PRDID); found {
					if prd, isPRD := v.(*autonomous.PRD); isPRD && prd != nil {
						mem.Title, mem.BlockedReason = prd.Title, prd.BlockedReason
					}
				}
			}
			info.Members = append(info.Members, mem)
		}
		out = append(out, info)
	}
	return out
}

type serverArcLanes struct{ s *Server }

func (l serverArcLanes) Lanes() ([]arcLane, error) {
	if l.s.autonomousMgr == nil {
		return nil, fmt.Errorf("lane fill is not wired in on this daemon")
	}
	st := l.s.autonomousMgr.LaneFillStatus(0)
	byKind := map[string]*arcLane{
		"claude": {Kind: "claude"},
		"pi":     {Kind: "pi"},
	}
	for _, lane := range st.Lanes {
		k := "pi"
		if lane.Type == autonomous.LaneTypeClaude {
			k = "claude"
		}
		byKind[k].Cap++
		if lane.State != "idle" {
			byKind[k].InFlight++
		}
	}
	return []arcLane{*byKind["claude"], *byKind["pi"]}, nil
}

func (l serverArcLanes) Queued() ([]arcQueued, error) {
	if l.s.capacityLedger == nil {
		return nil, fmt.Errorf("the capacity ledger is not wired in on this daemon")
	}
	counts := map[arcQueued]int{}
	for _, w := range l.s.capacityLedger.Snapshot().Waiting {
		kind := "pi"
		for _, p := range w.Pools {
			if strings.Contains(strings.ToLower(p), "claude") {
				kind = "claude"
			}
		}
		counts[arcQueued{Lane: kind, Priority: w.Priority}]++
	}
	out := make([]arcQueued, 0, len(counts))
	for k, n := range counts {
		k.Count = n
		out = append(out, k)
	}
	return out, nil
}

type serverArcUsage struct{ svc *usagecalc.Service }

func (u serverArcUsage) PlanUsage(ctx context.Context) (arcPlanUsage, error) {
	res, err := u.svc.Advice(ctx, usagecalc.Options{})
	if err != nil {
		return arcPlanUsage{}, err
	}
	// The plan is measured in points: the weekly allowance is 100 points
	// plus any bought extra, and Percent is the points used so far.
	return arcPlanUsage{PointsUsed: res.Advice.Percent, PointsAllowance: res.Advice.Ceiling}, nil
}
