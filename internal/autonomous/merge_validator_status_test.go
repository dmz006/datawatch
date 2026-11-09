package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validatorRun(id string, v *MergeValidatorResult) MergePipelineRun {
	return MergePipelineRun{ID: id, Result: "ok", Validator: v}
}

func TestLatestMergeValidatorNoneGivesNilKeyAndNoLine(t *testing.T) {
	runs := []MergePipelineRun{validatorRun("r2", nil), validatorRun("r1", nil)}
	if got := latestMergeValidator(runs); got != nil {
		t.Fatalf("latestMergeValidator = %+v, want nil", got)
	}
	if got := latestMergeValidator(nil); got != nil {
		t.Fatalf("latestMergeValidator(nil) = %+v, want nil", got)
	}

	ms := MergeStatus{Mode: "on", Automata: []MergeStatusEntry{}, PipelineRuns: runs}
	ms.LastValidator = latestMergeValidator(ms.PipelineRuns)
	body, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "last_validator") {
		t.Fatalf("body carries last_validator with no result: %s", body)
	}
	if txt := RenderMergeState(ms); strings.Contains(txt, "release-bump validation") {
		t.Fatalf("render shows a validator line with no result:\n%s", txt)
	}
}

func TestLatestMergeValidatorPicksNewestRunWithResult(t *testing.T) {
	older := &MergeValidatorResult{Pass: true, Version: "1.0.0"}
	newest := &MergeValidatorResult{Pass: false, Version: "1.1.0"}
	runs := []MergePipelineRun{
		validatorRun("new-no-result", nil),
		validatorRun("newest", newest),
		validatorRun("older", older),
	}
	got := latestMergeValidator(runs)
	if got == nil || got.Version != "1.1.0" || got.Pass {
		t.Fatalf("latestMergeValidator = %+v, want the newest failing result 1.1.0", got)
	}
	got.Version = "changed"
	if newest.Version != "1.1.0" {
		t.Fatalf("latestMergeValidator must return a copy, source changed to %q", newest.Version)
	}
}

func TestMergeStatusPassingValidatorText(t *testing.T) {
	ms := MergeStatus{
		Mode:         "on",
		Automata:     []MergeStatusEntry{},
		PipelineRuns: []MergePipelineRun{validatorRun("r1", &MergeValidatorResult{Pass: true, Version: "2.3.4", CheckedAt: time.Now()})},
	}
	ms.LastValidator = latestMergeValidator(ms.PipelineRuns)
	txt := RenderMergeState(ms)
	if !strings.Contains(txt, "Last release-bump validation: passed (version 2.3.4)") {
		t.Fatalf("passing line missing:\n%s", txt)
	}
	if strings.Contains(txt, "failing check") {
		t.Fatalf("passing result must list no failing checks:\n%s", txt)
	}
}

func TestMergeStatusFailingValidatorText(t *testing.T) {
	res := &MergeValidatorResult{
		Pass:    false,
		Version: "2.4.0",
		Checks: []MergeValidatorCheck{
			{Name: "changelog", Pass: true, Detail: "heading present"},
			{Name: "version-order", Pass: false, Detail: "2.4.0 is not above 2.3.4"},
			{Name: "tag", Pass: false, Detail: "tag v2.4.0\nalready exists"},
		},
	}
	ms := MergeStatus{Mode: "shadow", Automata: []MergeStatusEntry{}, PipelineRuns: []MergePipelineRun{validatorRun("r1", res)}}
	ms.LastValidator = latestMergeValidator(ms.PipelineRuns)
	txt := RenderMergeState(ms)
	for _, want := range []string{
		"Last release-bump validation: failed (version 2.4.0)",
		"  failing check version-order: 2.4.0 is not above 2.3.4",
		"  failing check tag: tag v2.4.0 already exists",
	} {
		if !strings.Contains(txt, want) {
			t.Fatalf("render missing %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "failing check changelog") {
		t.Fatalf("passing check must not be listed as failing:\n%s", txt)
	}
}

func TestMergeStatusValidatorRoundTrip(t *testing.T) {
	when := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	ms := MergeStatus{
		Mode:     "on",
		Automata: []MergeStatusEntry{},
		LastValidator: &MergeValidatorResult{
			Pass:      false,
			Version:   "3.0.1",
			CheckedAt: when,
			Checks:    []MergeValidatorCheck{{Name: "bump", Pass: false, Detail: "no bump"}},
		},
	}
	body, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"last_validator"`) {
		t.Fatalf("body lacks last_validator: %s", body)
	}
	got, err := ParseMergeStatus(body)
	if err != nil {
		t.Fatalf("ParseMergeStatus: %v", err)
	}
	if got.LastValidator == nil {
		t.Fatalf("round trip lost LastValidator")
	}
	v := got.LastValidator
	if v.Pass || v.Version != "3.0.1" || !v.CheckedAt.Equal(when) || len(v.Checks) != 1 || v.Checks[0].Detail != "no bump" {
		t.Fatalf("round trip mismatch: %+v", v)
	}
}
