package autonomous

import (
	"context"
	"strings"
	"testing"
)

type fakeLookup map[string]string

func (f fakeLookup) ResolveKind(name string) (string, bool) { k, ok := f[name]; return k, ok }

// The merge conflict model is the named entry and only it: the prompt reaches
// it unchanged and the planner is never asked.
func TestMergeConflictProposalGoesToTheNamedEntryOnly(t *testing.T) {
	planned := 0
	m, err := NewManager(t.TempDir(), DefaultConfig(), func(DecomposeRequest) (string, error) {
		planned++
		return "planner reply", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ProposeMergeConflict("the conflict prompt", "sonnet-entry"); err == nil || !strings.Contains(err.Error(), "no conflict model") {
		t.Errorf("with nothing wired: err = %v, want a refusal", err)
	}
	var entry, prompt string
	m.SetMergeConflictModel(fakeLookup{"sonnet-entry": "claude-code"}, func(_ context.Context, e, p string) (string, error) {
		entry, prompt = e, p
		return "model reply", nil
	})
	got, err := m.ProposeMergeConflict("the conflict prompt", "sonnet-entry")
	if err != nil || got != "model reply" || entry != "sonnet-entry" || prompt != "the conflict prompt" {
		t.Errorf("got %q err %v, entry %q prompt %q", got, err, entry, prompt)
	}
	if planned != 0 {
		t.Errorf("the planner was called %d time(s)", planned)
	}
	if l := NewAPI(m).MergeModelLookup(); l == nil {
		t.Error("the API does not hand on the model lookup")
	} else if _, ok := l.ResolveKind("sonnet-entry"); !ok {
		t.Error("the lookup does not know the entry")
	}
}

func TestMergeReleaseFactsFromTheAutomaton(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		kind, size string
		breaking   bool
		want       string
		major      bool
	}{
		"feature":          {kind: "feature", want: "minor"},
		"bug":              {kind: "bug", want: "patch"},
		"explicit major":   {kind: "bug", size: "major", want: "major"},
		"breaking change":  {kind: "bug", breaking: true, want: "patch", major: true},
		"other makes none": {kind: "other", want: "none"},
		"explicit none":    {kind: "feature", size: "none", want: "none"},
	} {
		p, err := m.CreatePRD("spec for "+name, "", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		p.Title, p.Kind, p.ReleaseSize, p.BreakingChange = "Title "+name+"\nsecond line", c.kind, c.size, c.breaking
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
		f, ok := NewAPI(m).MergeReleaseFacts(p.ID, map[string]string{"feature": "minor", "bug": "patch"}, "manual")
		if !ok || f.Size != c.want || f.RaisesMajorApproval != c.major || f.Title != "Title "+name {
			t.Errorf("%s: %+v ok=%v, want size %s major=%v", name, f, ok, c.want, c.major)
		}
	}
	if _, ok := m.MergeReleaseFacts("nope", nil, ""); ok {
		t.Error("an unknown Automaton has no facts")
	}
}
