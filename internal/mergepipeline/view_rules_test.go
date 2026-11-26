package mergepipeline

import (
	"strings"
	"testing"
)

// The text the CLI, MCP and chat show must not hide a tag limit or a
// conflict rule.
func TestPipelineTextShowsTagSizesAndConflictRules(t *testing.T) {
	got := RenderDefinitionText(Definition{
		Steps:         []Step{{Name: "tag", Kind: KindTag, Action: ActionTagRelease, TagSizes: []string{"minor", "major"}}},
		ConflictRules: []ConflictRule{{Path: "docs/a.md", Rule: ConflictRuleKeepMain}},
	})
	for _, want := range []string{"tags only minor, major releases", "Conflict rules (the first match wins):", "docs/a.md: keep_main"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if plain := RenderDefinitionText(DefaultDefinition()); strings.Contains(plain, "Conflict rules") {
		t.Errorf("a pipeline with no rules lists some:\n%s", plain)
	}
}
