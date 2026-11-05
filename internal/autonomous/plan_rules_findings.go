// plan_rules_findings.go — advisory plan rule findings layered on the
// structural plan check. Every rule yields a StructuralFinding and is never
// a hard failure: findings go to PlanStructureReport.Advisory, which does not
// affect Pass or Count. The rules run only when the plan_rules check is on or
// shadow (ChecksPolicyFor); in shadow they are recorded, not surfaced.
package autonomous

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	// FindingFeatureWithoutTest — a feature task with no test-bearing task
	// or test file in its files list.
	FindingFeatureWithoutTest = "feature_without_test"
	// FindingTrackerIDInNewName — a new file, function or type name that
	// carries a tracker id (BL/B/GH followed by digits).
	FindingTrackerIDInNewName = "tracker_id_in_new_name"
	// FindingReleaseFixedVersion — a release task that names a fixed
	// version number instead of "next release above main".
	FindingReleaseFixedVersion = "release_fixed_version"
	// FindingMissingModelPin — the plan's rules require a model pin and
	// the task has none.
	FindingMissingModelPin = "missing_model_pin"
	// FindingRulesChecklistGap — a task's rules_applied list does not cover
	// the rules areas its files and spec touch, or is empty although the
	// planning record shows a rules file was present.
	FindingRulesChecklistGap = "rules_checklist_gap"
)

var (
	planRuleTrackerRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:bl|gh|b)[-_ ]?[0-9]+`)
	planRuleCamelRe   = regexp.MustCompile(`(?:BL|GH|B)[0-9]+`)
	planRuleFuncRe    = regexp.MustCompile(`(?i)\b(?:func|type)\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
	planRuleVersionRe = regexp.MustCompile(`\bv?[0-9]+\.[0-9]+\.[0-9]+\b`)
	planRulePinReqRe  = regexp.MustCompile(`(?i)(model pin|pin (?:the |a )?model|pinned model|must (?:name|set) a model)`)
	planRuleFeatureRe = regexp.MustCompile(`(?i)^\s*(add|implement|build|create|introduce|extend|write)\b`)
	planRuleTestRe    = regexp.MustCompile(`(?i)(_test\.go$|\.test\.[a-z]+$|\.spec\.[a-z]+$|(^|/)tests?/|\btests?\b)`)
)

var (
	shadowMu       sync.Mutex
	shadowFindings []StructuralFinding
)

// RecordedShadowPlanRuleFindings returns a copy of the findings recorded in
// shadow mode so far.
func RecordedShadowPlanRuleFindings() []StructuralFinding {
	shadowMu.Lock()
	defer shadowMu.Unlock()
	return append([]StructuralFinding(nil), shadowFindings...)
}

// ResetShadowPlanRuleFindings clears the shadow record.
func ResetShadowPlanRuleFindings() {
	shadowMu.Lock()
	shadowFindings = nil
	shadowMu.Unlock()
}

// ApplyPlanRules runs the plan rules for the PRD's project when plan_rules is
// on or shadow. On: findings land in report.Advisory. Shadow: findings are
// recorded for later inspection and not surfaced. Off or nil report: no-op.
func ApplyPlanRules(prd *PRD, stories []Story, report *PlanStructureReport) {
	dir := ""
	if prd != nil {
		dir = prd.ProjectDir
	}
	applyPlanRulesMode(ChecksPolicyFor(dir).PlanRules, prd, stories, report)
}

func applyPlanRulesMode(mode string, prd *PRD, stories []Story, report *PlanStructureReport) {
	if report == nil {
		return
	}
	switch mode {
	case config.ChecksModeOn:
		report.Advisory = append(report.Advisory, CheckPlanRules(prd, stories)...)
	case config.ChecksModeShadow:
		f := CheckPlanRules(prd, stories)
		if len(f) > 0 {
			shadowMu.Lock()
			shadowFindings = append(shadowFindings, f...)
			shadowMu.Unlock()
		}
	}
}

// CheckPlanRules evaluates the plan rules, ignoring the checks policy. It is
// deterministic and returns an empty (non-nil) slice for an empty plan.
func CheckPlanRules(prd *PRD, stories []Story) []StructuralFinding {
	out := []StructuralFinding{}
	pinRequired := prd != nil && planRulePinReqRe.MatchString(prd.Spec)
	for _, s := range stories {
		// A test-bearing task anywhere in the story covers its feature tasks.
		storyHasTestTask := false
		for _, t := range s.Tasks {
			if taskBearsTests(t) {
				storyHasTestTask = true
			}
		}
		for _, t := range s.Tasks {
			if planRuleFeatureRe.MatchString(t.Title) && !taskBearsTests(t) && !storyHasTestTask {
				out = append(out, StructuralFinding{Kind: FindingFeatureWithoutTest, StoryID: s.ID, TaskID: t.ID,
					Detail: "feature task " + t.ID + " has no test-bearing task or test file in its files list"})
			}
			for _, name := range newNames(t) {
				if m := trackerInName(name); m != "" {
					out = append(out, StructuralFinding{Kind: FindingTrackerIDInNewName, StoryID: s.ID, TaskID: t.ID,
						Detail: "task " + t.ID + " new name " + strconv.Quote(name) + " carries tracker id " + m})
				}
			}
			if isReleaseTask(t) {
				text := t.Title + "\n" + t.Spec
				if v := planRuleVersionRe.FindString(text); v != "" {
					out = append(out, StructuralFinding{Kind: FindingReleaseFixedVersion, StoryID: s.ID, TaskID: t.ID,
						Detail: "release task " + t.ID + " names fixed version " + v + "; say 'next release above main' instead"})
				}
			}
			if f, ok := rulesChecklistGap(prd, s, t); ok {
				out = append(out, f)
			}
			if (pinRequired || planRulePinReqRe.MatchString(t.Spec)) && !hasModelPin(prd, s, t) {
				out = append(out, StructuralFinding{Kind: FindingMissingModelPin, StoryID: s.ID, TaskID: t.ID,
					Detail: "task " + t.ID + " needs a model pin but none is set on the task, story or PRD"})
			}
		}
	}
	return out
}

func taskBearsTests(t Task) bool {
	if strings.Contains(strings.ToLower(t.Title), "test") {
		return true
	}
	for _, f := range append(append([]string{}, t.FilesPlanned...), t.FilesGenerated...) {
		if planRuleTestRe.MatchString(f) {
			return true
		}
	}
	return false
}

func newNames(t Task) []string {
	var names []string
	for _, f := range append(append([]string{}, t.FilesPlanned...), t.FilesGenerated...) {
		base := f
		if i := strings.LastIndex(f, "/"); i >= 0 {
			base = f[i+1:]
		}
		names = append(names, base)
	}
	for _, m := range planRuleFuncRe.FindAllStringSubmatch(t.Spec, -1) {
		names = append(names, m[1])
	}
	return names
}

func trackerInName(name string) string {
	if m := planRuleCamelRe.FindString(name); m != "" {
		return m
	}
	if m := planRuleTrackerRe.FindString(name); m != "" {
		return strings.Trim(m, "-_ ")
	}
	return ""
}

func isReleaseTask(t Task) bool {
	return strings.Contains(strings.ToLower(t.Title), "release") || strings.EqualFold(t.Kind, "release")
}

func hasModelPin(prd *PRD, s Story, t Task) bool {
	return strings.TrimSpace(t.Model) != "" || strings.TrimSpace(s.Model) != "" ||
		(prd != nil && strings.TrimSpace(prd.Model) != "")
}
