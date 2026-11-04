package autonomous

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDefectTinyFix_Criteria(t *testing.T) {
	cfg := config.DefectQueueConfig{}
	cases := []struct {
		name   string
		change TinyFixChange
		want   bool
		substr string
	}{
		{"at file limit", TinyFixChange{[]string{"a_test.go", "b_test.go", "docs/x.md"}, config.DefectKindTestOnly}, true, ""},
		{"over file limit", TinyFixChange{[]string{"a_test.go", "b_test.go", "c_test.go", "d_test.go"}, config.DefectKindTestOnly}, false, "4 files"},
		{"nested test path", TinyFixChange{[]string{"internal/x/y_test.go"}, config.DefectKindTestOnly}, true, ""},
		{"path outside allowed", TinyFixChange{[]string{"internal/x/y.go"}, config.DefectKindTestOnly}, false, "internal/x/y.go"},
		{"operator visible path", TinyFixChange{[]string{"cmd/datawatch/main.go"}, config.DefectKindTestOnly}, false, "operator-visible"},
		{"kind not allowed", TinyFixChange{[]string{"a_test.go"}, "feature"}, false, `"feature"`},
		{"empty kind", TinyFixChange{[]string{"a_test.go"}, ""}, false, "kind"},
		{"no files", TinyFixChange{nil, config.DefectKindTestOnly}, false, "no files"},
		{"duplicates count once", TinyFixChange{[]string{"a_test.go", "./a_test.go", "a_test.go"}, config.DefectKindTestOnly}, true, ""},
	}
	for _, c := range cases {
		got := ClassifyTinyFix(c.change, cfg, "p")
		if got.Eligible != c.want {
			t.Errorf("%s: eligible=%v want %v (%s)", c.name, got.Eligible, c.want, got.Reason)
		}
		if got.Reason == "" || !strings.Contains(got.Reason, c.substr) {
			t.Errorf("%s: reason %q missing %q", c.name, got.Reason, c.substr)
		}
	}
	off := false
	if ClassifyTinyFix(TinyFixChange{[]string{"a_test.go"}, config.DefectKindTestOnly}, config.DefectQueueConfig{TinyFix: config.DefectQueueTinyFix{Enabled: &off}}, "p").Eligible {
		t.Error("disabled tiny fix must not classify")
	}
}

func TestDefectTinyFix_ProjectOverride(t *testing.T) {
	cfg := config.DefectQueueConfig{
		TinyFix: config.DefectQueueTinyFix{MaxFiles: 1},
		Projects: map[string]config.DefectQueueProject{
			"wide": {TinyFix: config.DefectQueueTinyFix{
				MaxFiles:         2,
				AllowedPathGlobs: []string{"internal/**"},
				AllowedKinds:     []string{config.DefectKindLintTweak},
			}},
		},
	}
	two := TinyFixChange{[]string{"internal/a.go", "internal/b.go"}, config.DefectKindLintTweak}
	if d := ClassifyTinyFix(two, cfg, "wide"); !d.Eligible {
		t.Errorf("project override should admit: %s", d.Reason)
	}
	if d := ClassifyTinyFix(two, cfg, "other"); d.Eligible {
		t.Error("project without override must use the global criteria")
	}
	if d := ClassifyTinyFix(TinyFixChange{[]string{"internal/a.go"}, config.DefectKindTestOnly}, cfg, "wide"); d.Eligible {
		t.Error("override kinds replace the global kinds")
	}
}

func TestDefectTinyFix_TemplateIsSingleTask(t *testing.T) {
	prd, err := BuildTinyFixAutomaton(TinyFixAutomatonSpec{
		Title: "Fix flaky test", Spec: "make it stable", ProjectDir: "/w", VerificationScript: "scripts/verify.sh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prd.Story) != 1 || len(prd.Story[0].Tasks) != 1 {
		t.Fatalf("want 1 story with 1 task, got %+v", prd.Story)
	}
	task := prd.Story[0].Tasks[0]
	if task.VerificationScript != "scripts/verify.sh" || task.Kind != "" {
		t.Errorf("task = %+v", task)
	}
	if task.Status != TaskPending || prd.Status != PRDDraft {
		t.Errorf("statuses: %v %v", task.Status, prd.Status)
	}
	if _, err := BuildTinyFixAutomaton(TinyFixAutomatonSpec{Title: "t"}); err == nil {
		t.Error("missing verification script must be refused")
	}
	if _, err := BuildTinyFixAutomaton(TinyFixAutomatonSpec{VerificationScript: "s.sh"}); err == nil {
		t.Error("missing title must be refused")
	}
}

func TestDefectTinyFix_RefusesOperatorVisibleChange(t *testing.T) {
	cfg := config.DefectQueueConfig{}
	files := []string{"a_test.go", "internal/server/web/app.js"}
	before := append([]string(nil), files...)
	r := CheckOperatorVisible(files, cfg, "p")
	if !r.Refused {
		t.Fatal("operator-visible change must be refused")
	}
	if !strings.Contains(r.Reason, "internal/server/web/app.js") {
		t.Errorf("reason must name the path: %s", r.Reason)
	}
	if strings.Contains(r.Reason, "a_test.go") {
		t.Errorf("reason must not name allowed paths: %s", r.Reason)
	}
	if !strings.Contains(r.Reason, "normal path") || !strings.Contains(r.Reason, "batch") {
		t.Errorf("reason must route to the normal path: %s", r.Reason)
	}
	if !reflect.DeepEqual(files, before) {
		t.Error("input was mutated")
	}
	out := CheckOperatorVisible([]string{"internal/other/thing.go"}, cfg, "p")
	if !out.Refused || !strings.Contains(out.Reason, "internal/other/thing.go") {
		t.Errorf("outside-allowed change must be refused: %+v", out)
	}
	if CheckOperatorVisible([]string{"a_test.go", "docs/x.md"}, cfg, "p").Refused {
		t.Error("allowed files must pass")
	}
}
