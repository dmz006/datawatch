package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/packs"
)

// TestPromptSpecTextDropsHeading: the spec is the prompt body; the leading
// "# " heading and the blank lines around it are not part of the spec.
func TestPromptSpecTextDropsHeading(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"heading then body": {"# Docs set\n\nBring the docs into shape.\n", "Bring the docs into shape."},
		"crlf":              {"# T\r\n\r\nBody line.\r\n", "Body line."},
		"no heading":        {"Plain body\n", "Plain body"},
		"only heading":      {"# Only\n", ""},
		"multi-line body":   {"# H\n\nfirst\nsecond\n", "first\nsecond"},
	}
	for name, c := range cases {
		if got := promptSpecText(c.in); got != c.want {
			t.Errorf("%s: promptSpecText = %q, want %q", name, got, c.want)
		}
	}
}

// TestBuiltInTaskTemplatesFollowBuiltInPack: the defs are one per resolved
// built-in task, in pack order, and are fresh on every call so a caller
// cannot mutate the shared pack.
func TestBuiltInTaskTemplatesFollowBuiltInPack(t *testing.T) {
	pack, err := packs.LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	resolved, err := packs.ResolveTaskList(PRDKindFeature, []*packs.Pack{pack})
	if err != nil {
		t.Fatalf("ResolveTaskList: %v", err)
	}
	defs := BuiltInTaskTemplates()
	if len(defs) != len(resolved) {
		t.Fatalf("got %d defs, want %d (one per built-in task)", len(defs), len(resolved))
	}
	for i, rt := range resolved {
		if defs[i].ID != rt.ID || defs[i].Title != rt.Title {
			t.Errorf("def %d = (%q, %q), want (%q, %q)", i, defs[i].ID, defs[i].Title, rt.ID, rt.Title)
		}
		if defs[i].Spec == "" {
			t.Errorf("def %q has an empty spec", defs[i].ID)
		}
	}
	defs[0].Title = "mutated"
	if again := BuiltInTaskTemplates(); again[0].Title == "mutated" {
		t.Error("BuiltInTaskTemplates shares its slice with an earlier caller")
	}
}
