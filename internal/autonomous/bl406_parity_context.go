// BL406 Phase 2 — translates PRD/Story data into the scan package's
// generic parity-context type. internal/autonomous/scan can't import
// PRD/Story directly (autonomous already imports scan, so the reverse
// would be a cycle) — same "mirror, don't import" pattern used
// throughout this codebase (see config.ScanConfig's own doc comment).

package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
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
