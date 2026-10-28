package usagecalc

import (
	"math"
	"time"
)

// Usage policy ids. The policy is the operator's choice; it changes only the target the
// advice aims at, the headline, the notes, the lane suggestion and the finish block.
const (
	PolicyUseAll      = "use-all"
	PolicyPace        = "pace"
	PolicyFinishEarly = "finish-early"
)

var policyLabels = map[string]string{
	PolicyUseAll:      "Run until used up",
	PolicyPace:        "Pace to the week",
	PolicyFinishEarly: "Run flat out",
}

// Policies lists the policy ids in display order.
func Policies() []string { return []string{PolicyUseAll, PolicyPace, PolicyFinishEarly} }

// PolicyLabel returns the display label of a policy id, or "" for an unknown id.
func PolicyLabel(id string) string { return policyLabels[id] }

// ValidPolicy reports whether id names one of the known policies.
func ValidPolicy(id string) bool { return policyLabels[id] != "" }

// normalizePolicy maps an empty or unknown id to the default, use-all.
func normalizePolicy(id string) string {
	if _, ok := policyLabels[id]; ok {
		return id
	}
	return PolicyUseAll
}

// adviseByPolicy builds the policy-dependent part of the result from the numbers already
// in a. It never reads or changes those numbers.
func adviseByPolicy(cfg settings, a *Advice, readAt time.Time, loc *time.Location) PolicyAdvice {
	pa := PolicyAdvice{Policy: cfg.policy, Notes: []string{operatorChoosesNote}}
	switch cfg.policy {
	case PolicyPace:
		adviseTarget(&pa, cfg, a, a.Ceiling-cfg.safety, true)
	case PolicyFinishEarly:
		adviseFinish(&pa, cfg, a, readAt, loc)
	default:
		adviseTarget(&pa, cfg, a, a.Ceiling, false)
	}
	return pa
}

// largestFitting returns the largest lane count from 1 up whose projected end is within
// the limit, and 1 when none fits: the advice never names zero lanes.
func largestFitting(rows []LaneRow, limit float64) int {
	best := 1
	for _, row := range rows {
		if row.Lanes >= 1 && row.ProjectedEnd <= limit {
			best = row.Lanes
		}
	}
	return best
}

// adviseTarget serves use-all (target: the ceiling) and pace (target: the ceiling minus the
// safety margin).
func adviseTarget(pa *PolicyAdvice, cfg settings, a *Advice, target float64, pace bool) {
	pa.PolicyTarget = &target
	pa.LanesSuggested = largestFitting(a.ByLanes, target)
	row := a.ByLanes[pa.LanesSuggested]
	atMax := a.ByLanes[cfg.maxLanes]
	fits := atMax.ProjectedEnd <= target

	if !pace {
		if fits {
			pa.Headline = "Within budget: " + lanesEnd(cfg.maxLanes) + " the week at " + formatPoints(atMax.ProjectedEnd) +
				"%, " + formatPoints(a.Ceiling-atMax.ProjectedEnd) + " points unused at the reset."
			pa.Notes = append(pa.Notes, "Unused allowance is lost at the reset; there is room for more work.")
		} else {
			pa.Headline = "Over budget at the recent burn: " + lanesWord(cfg.maxLanes) + " run out about " +
				formatPoints(atMax.HoursEarly) + " hours before the reset. " + lanesEnd(pa.LanesSuggested) + " at " +
				formatPoints(row.ProjectedEnd) + "%."
			pa.Notes = append(pa.Notes, "Running out early is allowed under this policy; the alternative is "+lanesWord(pa.LanesSuggested)+" to the reset.")
		}
		return
	}

	if fits {
		pa.Headline = "Within budget: " + lanesEnd(cfg.maxLanes) + " the week at " + formatPoints(atMax.ProjectedEnd) +
			"%, inside the " + formatPoints(target) + "% target."
	} else {
		pa.Headline = "Over budget for the week at " + lanesWord(cfg.maxLanes) + " (" + formatPoints(atMax.ProjectedEnd) +
			"% at the reset). " + lanesEnd(pa.LanesSuggested) + " at " + formatPoints(row.ProjectedEnd) + "%."
	}
	if row.ProjectedEnd > target {
		pa.Notes = append(pa.Notes, "Even "+lanesWord(pa.LanesSuggested)+" does not reach the target; the allowance is the limit.")
	}
	if a.AheadOfPace {
		pa.Notes = append(pa.Notes, "Usage is ahead of the pace line by "+formatPoints(a.Percent-a.PaceLine)+" points.")
	}
}

// adviseFinish serves finish-early: every lane runs, and the check is whether the remaining
// work finishes before the allowance does.
func adviseFinish(pa *PolicyAdvice, cfg settings, a *Advice, readAt time.Time, loc *time.Location) {
	pa.LanesSuggested = cfg.maxLanes
	pa.Notes = append(pa.Notes, "All lanes run; the calculator does not slow them down under this policy.")
	if a.RemainingWork.InUse == WorkInUseNone {
		pa.Headline = "Enter the remaining work to see whether it finishes before the allowance does."
		return
	}
	work := a.RemainingWork.Points
	laneRate := float64(cfg.maxLanes) * a.PerLanePer24h
	if laneRate <= 0 {
		pa.Finish = &Finish{NoBurn: true}
		pa.Headline = "No lane is burning above the idle baseline, so no finish time can be given."
		return
	}
	days := work / laneRate
	endPercent := a.Percent + work + cfg.baseline*days
	spare := a.Ceiling - endPercent
	hours := days * 24
	at := readAt.Add(time.Duration(days * float64(day))).In(loc)
	f := &Finish{
		Hours: &hours, At: &at, EndPercent: &endPercent, SparePoints: &spare,
		BeforeAllowance: spare >= 0 && days <= a.DaysLeft,
	}
	pa.Finish = f
	if f.BeforeAllowance {
		pa.Headline = "Within budget: the work finishes in about " + formatPoints(hours) + " hours with " +
			formatPoints(spare) + " points of allowance to spare."
	} else {
		pa.Headline = "Over budget: the allowance runs out before the work finishes (" + formatPoints(math.Min(spare, 0)) +
			" points short). Consider pacing or running until used up."
	}
}
