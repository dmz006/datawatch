// rules_checklist_lint.go — the rules-checklist gap rule: compares each
// task's rules_applied list with the rules areas its files and spec touch.
// Advisory only; findings carry the task title and area names, never file
// content.
package autonomous

import (
	"sort"
	"strconv"
	"strings"
)

// rulesAreaMatchers maps each rules area to the substrings (lower-case) that
// satisfy it in a rules_applied entry.
var rulesAreaMatchers = map[string][]string{
	"localization":             {"locali", "i18n", "locale"},
	"parity and configuration": {"parity", "config"},
	"documentation":            {"document", "docs"},
	"tests":                    {"test"},
	"versioning":               {"version", "release"},
}

// rulesFilePresent reports whether the PRD's planning record shows a rules
// file that was present at capture time.
func rulesFilePresent(prd *PRD) bool {
	if prd == nil || prd.RulesContext == nil {
		return false
	}
	for _, f := range prd.RulesContext.Files {
		if f.Kind == "rules" && f.Present {
			return true
		}
	}
	return false
}

// touchedRulesAreas derives the rules areas a task touches, sorted.
func touchedRulesAreas(t Task) []string {
	set := map[string]bool{}
	title := strings.ToLower(t.Title)
	for _, f := range append(append([]string{}, t.FilesPlanned...), t.FilesGenerated...) {
		lf := strings.ToLower(strings.ReplaceAll(f, "\\", "/"))
		has := func(frag string) bool { return strings.HasPrefix(lf, frag) || strings.Contains(lf, "/"+frag) }
		if has("internal/server/web") || has("locales/") || strings.Contains(lf, "/locales/") {
			set["localization"] = true
		}
		for _, d := range []string{"internal/mcp", "internal/router", "cmd/datawatch", "internal/server", "internal/config"} {
			if has(d) {
				set["parity and configuration"] = true
			}
		}
		if has("docs/") {
			set["documentation"] = true
		}
		if (strings.HasSuffix(lf, ".go") || strings.HasSuffix(lf, ".js")) && !planRuleTestRe.MatchString(lf) {
			set["tests"] = true
		}
	}
	if strings.Contains(title, "setting") || strings.Contains(title, "config") {
		set["parity and configuration"] = true
	}
	if strings.HasPrefix(strings.TrimSpace(title), "document") {
		set["documentation"] = true
	}
	if isReleaseTask(t) || strings.Contains(title, "version") {
		set["versioning"] = true
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func rulesAreaCovered(area string, applied []string) bool {
	for _, a := range applied {
		la := strings.ToLower(a)
		for _, m := range rulesAreaMatchers[area] {
			if strings.Contains(la, m) {
				return true
			}
		}
	}
	return false
}

// rulesChecklistGap returns the gap finding for one task, if any.
func rulesChecklistGap(prd *PRD, s Story, t Task) (StructuralFinding, bool) {
	if t.ReadOnly {
		return StructuralFinding{}, false
	}
	applied := make([]string, 0, len(t.RulesApplied))
	for _, a := range t.RulesApplied {
		if strings.TrimSpace(a) != "" {
			applied = append(applied, a)
		}
	}
	if len(applied) == 0 {
		if rulesFilePresent(prd) {
			return StructuralFinding{Kind: FindingRulesChecklistGap, StoryID: s.ID, TaskID: t.ID,
				Detail: "task " + strconv.Quote(t.Title) + " has an empty rules_applied list although a rules file was present"}, true
		}
		return StructuralFinding{}, false
	}
	var missing []string
	for _, area := range touchedRulesAreas(t) {
		if !rulesAreaCovered(area, applied) {
			missing = append(missing, area)
		}
	}
	if len(missing) == 0 {
		return StructuralFinding{}, false
	}
	return StructuralFinding{Kind: FindingRulesChecklistGap, StoryID: s.ID, TaskID: t.ID,
		Detail: "task " + strconv.Quote(t.Title) + " rules_applied is missing: " + strings.Join(missing, ", ")}, true
}
