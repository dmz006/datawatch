// Bridges PRD/Story data into the scan package's project-rules engine
// (BL406 Phases 2-4): translates PRD/Story into the generic parity-
// context type the parity rule needs (internal/autonomous/scan can't
// import PRD/Story directly — autonomous already imports scan, so the
// reverse would be a cycle — same "mirror, don't import" pattern used
// throughout this codebase; see config.ScanConfig's own doc comment),
// runs the PRD-completion checkpoint's project-rules check, and fires
// a rule's file_upstream_issue action when its finding fires.

package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/git"
)

// buildPRDParityContext builds the scan package's PRDParityContext
// from a PRD's own spec and decomposed stories.
func buildPRDParityContext(prd *PRD) *scan.PRDParityContext {
	if prd == nil {
		return nil
	}
	stories := make([]scan.StoryParityInfo, 0, len(prd.Story))
	for _, st := range prd.Story {
		stories = append(stories, scan.StoryParityInfo{ID: st.ID, Title: st.Title, Description: st.Description})
	}
	return &scan.PRDParityContext{
		ParentSurfaceText: extractMarkdownSection(prd.Spec, "Parity surface"),
		Stories:           stories,
	}
}

// extractMarkdownSection returns the text under a "## <heading>" line
// (case-insensitive substring match against the heading text) up to
// the next "## " heading or end of document. Empty string if no
// matching heading exists.
func extractMarkdownSection(markdown, heading string) string {
	lines := strings.Split(markdown, "\n")
	headingLower := strings.ToLower(heading)
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && strings.Contains(strings.ToLower(trimmed), headingLower) {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// runPRDCompletionProjectRulesCheck (BL406 Phase 3) is the third
// completion checkpoint — task and story already ride the existing
// PerTask/PerStoryGuardrails lists for free via the "project-rules-scan"
// guardrail entry (guardrail_registry.go); there is no equivalent
// PRD-level guardrail list, so this is a direct, self-gating call: a
// no-op (zero cost) when no rule is declared at prd_complete
// granularity, otherwise a real scan.Run using the same Pass/
// FailOnSeverity logic every other scan already uses.
func (m *Manager) runPRDCompletionProjectRulesCheck(prd *PRD) (bool, error) {
	m.mu.Lock()
	sc := m.cfg.Scan
	m.mu.Unlock()

	filtered := scan.FilterRulesByGranularity(sc.ProjectRules, scan.GranularityPRD)
	if len(filtered) == 0 {
		return false, nil
	}
	scanners := []scan.Scanner{scan.NewProjectRulesScanner(filtered, buildPRDParityContext(prd))}
	result := scan.Run(prd.ProjectDir, sc, scanners, nil)
	if result.Error != "" {
		return false, fmt.Errorf("prd-completion project-rules check: %s", result.Error)
	}
	if !result.Pass {
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: "project_rules_block", Actor: "autonomous",
			Note: fmt.Sprintf("%d finding(s) at or above fail_on_severity blocked PRD completion", len(result.Findings)),
		})
	}
	m.fireUpstreamIssueActions(filtered, result.Findings)
	return !result.Pass, nil
}

// fireUpstreamIssueActions (BL406 Phase 4) inspects findings for rules
// whose Action is ActionFileUpstreamIssue, resolves each one's
// UpstreamTarget against the configured autonomous.upstream_repos, and
// files a GitHub issue via the existing git.Provider (the same
// OpenPR-shaped CreateIssue sibling used by internal/agents' own
// PostSessionPRHook — reused, not a second GitHub client). Best-effort
// throughout: a filing failure is logged and never blocks the scan or
// its caller. At most one issue is filed per rule per call, even if
// that rule produced multiple findings.
func (m *Manager) fireUpstreamIssueActions(rules []scan.ProjectRule, findings []scan.Finding) {
	if len(findings) == 0 {
		return
	}
	ruleByID := make(map[string]scan.ProjectRule, len(rules))
	for _, r := range rules {
		if r.Action == scan.ActionFileUpstreamIssue {
			ruleByID[r.ID] = r
		}
	}
	if len(ruleByID) == 0 {
		return
	}
	m.mu.Lock()
	repos := m.cfg.UpstreamRepos
	m.mu.Unlock()
	repoByName := make(map[string]scan.UpstreamRepo, len(repos))
	for _, r := range repos {
		repoByName[r.Name] = r
	}

	resolve := m.gitProviderFn
	if resolve == nil {
		resolve = git.Resolve
	}
	provider := resolve("github")
	filed := map[string]bool{}
	for _, f := range findings {
		if f.Scanner != "project-rules" || filed[f.RuleID] {
			continue
		}
		rule, ok := ruleByID[f.RuleID]
		if !ok {
			continue // this finding's rule has no file_upstream_issue action
		}
		filed[rule.ID] = true
		target, ok := repoByName[rule.UpstreamTarget]
		if !ok {
			log.Printf("[project-rules] rule %s: file_upstream_issue target %q not found in autonomous.upstream_repos", rule.ID, rule.UpstreamTarget)
			continue
		}
		url, err := provider.CreateIssue(context.Background(), git.IssueOptions{
			Repo:  target.OwnerRepo,
			Title: "project rule: " + rule.Name,
			Body:  f.Message,
		})
		if err != nil {
			log.Printf("[project-rules] rule %s: file_upstream_issue to %s failed: %v", rule.ID, target.OwnerRepo, err)
			continue
		}
		log.Printf("[project-rules] rule %s: filed upstream issue %s", rule.ID, url)
	}
}
