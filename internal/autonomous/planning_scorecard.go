package autonomous

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// PlanningUnclassified names the rule class of a record that matched no
// configured risk class.
const PlanningUnclassified = "unclassified"

// PlannerClassKey identifies one scorecard row: a planner (backend plus
// model) and a rule class.
type PlannerClassKey struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Class   string `json:"class"`
}

// ScorecardRow holds counts only for one planner and rule class. A decompose
// whose record carries several classes counts once in each class's row.
// Every count is per decompose: Errors is the number of decomposes in which
// at least one planner attempt errored, and Escalations the number in which
// the ladder used more than one distinct planner.
type ScorecardRow struct {
	PlannerClassKey
	Decomposes           int     `json:"decomposes"`
	PassedFirstAttempt   int     `json:"passed_first_attempt"`
	PassedAfterTouchUp   int     `json:"passed_after_touch_up"`
	PassedAfterReplan    int     `json:"passed_after_replan"`
	Escalations          int     `json:"escalations"`
	Errors               int     `json:"errors"`
	MeanFindings         float64 `json:"mean_findings"`
	MeanSupervisorEdits  float64 `json:"mean_supervisor_edits"`
	findingSum, editsSum int
}

// WouldRouteRow counts shadow records for one planner and rule.
type WouldRouteRow struct {
	Backend    string `json:"backend"`
	Model      string `json:"model"`
	Rule       string `json:"rule"`
	WouldRoute int    `json:"would_route"`
}

// PlanningScorecard is the result of Scorecard.
type PlanningScorecard struct {
	Window     time.Duration   `json:"window"`
	At         time.Time       `json:"at"`
	Rows       []ScorecardRow  `json:"rows"`
	WouldRoute []WouldRouteRow `json:"would_route"`
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// Scorecard aggregates the PlanningRecords of prds that fall inside the
// window ending at now(): a record counts when now-window <= RecordedAt <=
// now, so a record exactly at the boundary counts. A window <= 0 means no
// lower bound. Shadow records never count as decomposes; they are tallied
// separately as would_route per planner and rule.
func Scorecard(prds []*PRD, window time.Duration, now func() time.Time) PlanningScorecard {
	at := now()
	sc := PlanningScorecard{Window: window, At: at, Rows: []ScorecardRow{}, WouldRoute: []WouldRouteRow{}}
	rows := map[PlannerClassKey]*ScorecardRow{}
	routes := map[[3]string]*WouldRouteRow{}
	for _, p := range prds {
		if p == nil || p.PlanningRecord == nil {
			continue
		}
		rec := p.PlanningRecord
		if rec.RecordedAt.After(at) || (window > 0 && rec.RecordedAt.Before(at.Add(-window))) {
			continue
		}
		if rec.Shadow {
			rule := rec.RuleName
			if rule == "" {
				rule = PlanningUnclassified
			}
			k := [3]string{rec.PlannerBackend, rec.PlannerModel, rule}
			r := routes[k]
			if r == nil {
				r = &WouldRouteRow{Backend: k[0], Model: k[1], Rule: k[2]}
				routes[k] = r
			}
			r.WouldRoute++
			continue
		}
		classes := rec.Classes
		if len(classes) == 0 {
			classes = []string{PlanningUnclassified}
		}
		seen := map[string]bool{}
		for _, c := range classes {
			if c == "" {
				c = PlanningUnclassified
			}
			if seen[c] {
				continue
			}
			seen[c] = true
			k := PlannerClassKey{rec.PlannerBackend, rec.PlannerModel, c}
			r := rows[k]
			if r == nil {
				r = &ScorecardRow{PlannerClassKey: k}
				rows[k] = r
			}
			addDecompose(r, rec)
		}
	}
	for _, r := range rows {
		r.MeanFindings = round1(float64(r.findingSum) / float64(r.Decomposes))
		r.MeanSupervisorEdits = round1(float64(r.editsSum) / float64(r.Decomposes))
		sc.Rows = append(sc.Rows, *r)
	}
	sort.Slice(sc.Rows, func(i, j int) bool {
		a, b := sc.Rows[i].PlannerClassKey, sc.Rows[j].PlannerClassKey
		if a.Backend != b.Backend {
			return a.Backend < b.Backend
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Class < b.Class
	})
	for _, r := range routes {
		sc.WouldRoute = append(sc.WouldRoute, *r)
	}
	sort.Slice(sc.WouldRoute, func(i, j int) bool {
		a, b := sc.WouldRoute[i], sc.WouldRoute[j]
		if a.Backend != b.Backend {
			return a.Backend < b.Backend
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Rule < b.Rule
	})
	return sc
}

// addDecompose folds one real decompose record into row r.
func addDecompose(r *ScorecardRow, rec *PlanningRecord) {
	r.Decomposes++
	r.findingSum += rec.FindingCount
	r.editsSum += rec.SupervisorEdits
	planners := map[string]bool{}
	errored := false
	for _, a := range rec.Attempts {
		planners[a.Backend+"\x00"+a.Model] = true
		if a.Outcome == LadderOutcomeError {
			errored = true
		}
	}
	if errored {
		r.Errors++
	}
	if len(planners) > 1 {
		r.Escalations++
	}
	for _, a := range rec.Attempts {
		if a.Outcome != LadderOutcomePassed {
			continue
		}
		switch a.Kind {
		case LadderAttemptTouchUp:
			r.PassedAfterTouchUp++
		case LadderAttemptReplan:
			r.PassedAfterReplan++
		default:
			r.PassedFirstAttempt++
		}
		break
	}
}

// PlanningGateSummary is the counts-only summary behind the rendered gate
// text. It holds no spec text, task titles or finding strings.
type PlanningGateSummary struct {
	Window             time.Duration   `json:"window"`
	Rows               []ScorecardRow  `json:"rows"`
	WouldRoute         []WouldRouteRow `json:"would_route"`
	Decomposes         int             `json:"decomposes"`
	PassedFirstAttempt int             `json:"passed_first_attempt"`
	PassedAfterTouchUp int             `json:"passed_after_touch_up"`
	PassedAfterReplan  int             `json:"passed_after_replan"`
	Escalations        int             `json:"escalations"`
	Errors             int             `json:"errors"`
	WouldRouteTotal    int             `json:"would_route_total"`
}

// PlanningGateReport returns the counts-only gate summary for sc and its
// rendered text. Only counts, planner identifiers (backend, model) and the
// configured class and rule identifiers appear. Class rows of one decompose
// that carries several classes are summed, so totals are row totals.
func PlanningGateReport(sc PlanningScorecard) (PlanningGateSummary, string) {
	g := PlanningGateSummary{Window: sc.Window, Rows: sc.Rows, WouldRoute: sc.WouldRoute}
	for _, r := range sc.Rows {
		g.Decomposes += r.Decomposes
		g.PassedFirstAttempt += r.PassedFirstAttempt
		g.PassedAfterTouchUp += r.PassedAfterTouchUp
		g.PassedAfterReplan += r.PassedAfterReplan
		g.Escalations += r.Escalations
		g.Errors += r.Errors
	}
	for _, w := range sc.WouldRoute {
		g.WouldRouteTotal += w.WouldRoute
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Planning gate report (window %s)\n", sc.Window)
	fmt.Fprintf(&b, "decomposes=%d first_attempt=%d touch_up=%d replan=%d escalations=%d errors=%d would_route=%d\n",
		g.Decomposes, g.PassedFirstAttempt, g.PassedAfterTouchUp, g.PassedAfterReplan, g.Escalations, g.Errors, g.WouldRouteTotal)
	for _, r := range sc.Rows {
		fmt.Fprintf(&b, "planner=%s/%s class=%s decomposes=%d first_attempt=%d touch_up=%d replan=%d escalations=%d errors=%d mean_findings=%.1f mean_edits=%.1f\n",
			r.Backend, r.Model, r.Class, r.Decomposes, r.PassedFirstAttempt, r.PassedAfterTouchUp, r.PassedAfterReplan, r.Escalations, r.Errors, r.MeanFindings, r.MeanSupervisorEdits)
	}
	for _, w := range sc.WouldRoute {
		fmt.Fprintf(&b, "would_route planner=%s/%s rule=%s count=%d\n", w.Backend, w.Model, w.Rule, w.WouldRoute)
	}
	return g, b.String()
}
