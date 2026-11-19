package autonomous

import (
	"strings"
	"testing"
)

// planJSON renders the smallest plan with one task whose "verification"
// value is the given raw JSON fragment (inserted verbatim).
func planJSON(verificationRaw string) string {
	return `{"title":"tolerance","stories":[{"id":"s1","title":"S","tasks":[{"title":"the task","spec":"do it","verification":` + verificationRaw + `}]},{"id":"s2","title":"S2","tasks":[{"title":"plain","spec":"no verification field"}]}]}`
}

func TestParsePlanningTaskVerificationStringSummary(t *testing.T) {
	title, stories, err := ParsePlanning(planJSON(`"flaky: needs a rerun"`))
	if err != nil {
		t.Fatalf("ParsePlanning with a string verification: %v", err)
	}
	if title != "tolerance" {
		t.Fatalf("title = %q, want %q", title, "tolerance")
	}
	if len(stories) != 2 || len(stories[0].Tasks) != 1 {
		t.Fatalf("parsed shape = %d stories, want 2", len(stories))
	}
	v := stories[0].Tasks[0].Verification
	if v == nil {
		t.Fatal("Verification is nil, want a summary-only result")
	}
	if v.OK {
		t.Error("Verification.OK = true, want false for a string summary")
	}
	if v.Summary != "flaky: needs a rerun" {
		t.Errorf("Verification.Summary = %q, want %q", v.Summary, "flaky: needs a rerun")
	}
}

func TestParsePlanningTaskVerificationObjectUnchanged(t *testing.T) {
	title, stories, err := ParsePlanning(planJSON(`{"ok":true,"summary":"all green","issues":["none"]}`))
	if err != nil {
		t.Fatalf("ParsePlanning with an object verification: %v", err)
	}
	if title != "tolerance" {
		t.Fatalf("title = %q, want %q", title, "tolerance")
	}
	v := stories[0].Tasks[0].Verification
	if v == nil {
		t.Fatal("Verification is nil, want the decoded object")
	}
	if !v.OK {
		t.Error("Verification.OK = false, want true")
	}
	if v.Summary != "all green" {
		t.Errorf("Verification.Summary = %q, want %q", v.Summary, "all green")
	}
	if len(v.Issues) != 1 || v.Issues[0] != "none" {
		t.Errorf("Verification.Issues = %v, want [none]", v.Issues)
	}
}

func TestParsePlanningTaskVerificationMissing(t *testing.T) {
	// Same plan, but the first task omits "verification" entirely; the
	// second task in story s2 is the "plain" task and also has none.
	raw := `{"title":"tolerance","stories":[{"id":"s1","title":"S","tasks":[{"title":"the task","spec":"do it"}]},{"id":"s2","title":"S2","tasks":[{"title":"plain","spec":"no verification field"}]}]}`
	title, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("ParsePlanning with no verification field: %v", err)
	}
	if title != "tolerance" {
		t.Fatalf("title = %q, want %q", title, "tolerance")
	}
	for si, s := range stories {
		for ti, task := range s.Tasks {
			if task.Verification != nil {
				t.Errorf("story %d task %d: Verification = %+v, want nil", si, ti, task.Verification)
			}
		}
	}
}

func TestParsePlanningTaskVerificationBadTypeNamesField(t *testing.T) {
	for _, bad := range []string{`42`, `true`, `["issues"]`, `1.5`} {
		if _, _, err := ParsePlanning(planJSON(bad)); err == nil {
			t.Errorf("verification %s: got nil error, want a parse error naming the field and task", bad)
			continue
		} else if !strings.Contains(err.Error(), `plan field "verification"`) {
			t.Errorf("verification %s: error %q does not name the field", bad, err)
		} else if !strings.Contains(err.Error(), "the task") {
			t.Errorf("verification %s: error %q does not name the task", bad, err)
		}
	}

	// The number and boolean forms must hit the exact hard-error message.
	_, _, err := ParsePlanning(planJSON(`42`))
	if err == nil {
		t.Fatal("verification 42: got nil error, want the hard error")
	}
	want := `plan field "verification" of task "the task" must be an object or a string`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
