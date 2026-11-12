package stats

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUIPromptDismiss_StatsJSON: the daemon's ui_prompt dismissal
// counters land in SystemStats as ui_prompts (total + per-kind breakout)
// when a getter is wired, and the field stays absent from the JSON when
// the getter reports nothing (omitempty — a fresh daemon shows no
// empty object).
func TestUIPromptDismiss_StatsJSON(t *testing.T) {
	c := NewCollector(t.TempDir())
	c.SetUIPromptsFunc(func() *UIPromptStats {
		return &UIPromptStats{
			Dismissed:  3,
			NotCleared: 1,
			Refused:    2,
			ByKind: map[string]UIPromptKindStats{
				"autocompact-threshold": {Dismissed: 3, Refused: 2},
				"theme-picker":          {NotCleared: 1},
			},
		}
	})
	c.collect()

	s := c.Latest()
	if s.UIPrompts == nil {
		t.Fatal("ui_prompts nil after a wired getter returned stats")
	}
	if s.UIPrompts.Dismissed != 3 || s.UIPrompts.NotCleared != 1 || s.UIPrompts.Refused != 2 {
		t.Fatalf("ui_prompts totals = %+v, want dismissed=3 not_cleared=1 refused=2", s.UIPrompts)
	}
	if got := s.UIPrompts.ByKind["autocompact-threshold"]; got.Dismissed != 3 || got.Refused != 2 || got.NotCleared != 0 {
		t.Fatalf("autocompact-threshold by_kind = %+v, want dismissed=3 refused=2 not_cleared=0", got)
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal SystemStats: %v", err)
	}
	for _, want := range []string{
		`"ui_prompts"`,
		`"dismissed":3`,
		`"not_cleared":1`,
		`"refused":2`,
		`"by_kind"`,
		`"autocompact-threshold"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("stats JSON missing %s: %s", want, b)
		}
	}

	// A getter that reports nothing (fresh daemon) must leave the field
	// out of the JSON entirely — no empty objects.
	c2 := NewCollector(t.TempDir())
	c2.SetUIPromptsFunc(func() *UIPromptStats { return nil })
	c2.collect()
	if got := c2.Latest().UIPrompts; got != nil {
		t.Fatalf("ui_prompts = %+v for a nil getter, want nil", got)
	}
	b2, err := json.Marshal(c2.Latest())
	if err != nil {
		t.Fatalf("marshal SystemStats: %v", err)
	}
	if strings.Contains(string(b2), `"ui_prompts"`) {
		t.Errorf(`fresh daemon JSON must omit "ui_prompts": %s`, b2)
	}
}
