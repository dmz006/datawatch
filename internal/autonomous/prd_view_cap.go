package autonomous

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultDetailTextCap is the per-field byte cap the default PRD detail reply
// applies when autonomous.detail_text_cap_bytes is 0.
const DefaultDetailTextCap = 4096

// TruncatedField names one text field the default detail reply cut short.
// Field is a path such as "spec", "tasks.start_failure_tail[0]" or
// "verification.raw_judge", Bytes is the stored length, Kept is how many
// bytes the reply kept, and Ref is the URL of the full text.
type TruncatedField struct {
	Field string `json:"field"`
	Bytes int    `json:"bytes"`
	Kept  int    `json:"kept"`
	Ref   string `json:"ref"`
}

// ResolveDetailTextCap turns the configured value into a byte cap: 0 is the
// default, a negative value means no cap (returned as 0).
func ResolveDetailTextCap(configured int) int {
	switch {
	case configured < 0:
		return 0
	case configured == 0:
		return DefaultDetailTextCap
	}
	return configured
}

// truncationMarker starts the marker capText appends to a cut field.
const truncationMarker = "\n…[truncated: kept "

// HasTruncationMarker reports whether s ends with the marker of a capped
// field, so a write can refuse text copied from a capped view.
func HasTruncationMarker(s string) bool {
	i := strings.LastIndex(s, truncationMarker)
	return i >= 0 && strings.HasSuffix(s, " bytes]") && !strings.Contains(s[i+1:], "\n")
}

// capText returns s cut to at most max bytes on a rune boundary, with a
// marker appended, and whether it was cut. max <= 0 never cuts.
func capText(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("%s%d of %d bytes]", truncationMarker, cut, len(s)), true
}

// capper accumulates the truncation notes for one record.
type capper struct {
	max  int
	ref  string
	hits []TruncatedField
}

func (c *capper) str(field string, p *string) {
	out, cut := capText(*p, c.max)
	if !cut {
		return
	}
	c.hits = append(c.hits, TruncatedField{Field: field, Bytes: len(*p), Kept: len(out), Ref: c.ref})
	// Kept reports the bytes of original text kept, not the marker.
	c.hits[len(c.hits)-1].Kept = strings.LastIndex(out, truncationMarker)
	*p = out
}

func (c *capper) strs(field string, in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, len(in))
	copy(out, in)
	for i := range out {
		c.str(fmt.Sprintf("%s[%d]", field, i), &out[i])
	}
	return out
}

func (c *capper) verdicts(field string, in []GuardrailVerdict) []GuardrailVerdict {
	if len(in) == 0 {
		return in
	}
	out := make([]GuardrailVerdict, len(in))
	copy(out, in)
	for i := range out {
		c.str(fmt.Sprintf("%s[%d].raw_judge", field, i), &out[i].RawJudge)
		c.str(fmt.Sprintf("%s[%d].summary", field, i), &out[i].Summary)
		out[i].Issues = c.strs(fmt.Sprintf("%s[%d].issues", field, i), out[i].Issues)
	}
	return out
}

// OutputsRef is the URL of the sub-resource that returns the full heavy
// fields of one task, or of the whole Automaton when taskID is empty.
func OutputsRef(prdID, taskID string) string {
	if taskID == "" {
		return "/api/autonomous/prds/" + prdID + "/outputs"
	}
	return "/api/autonomous/prds/" + prdID + "/tasks/" + taskID + "/outputs"
}

// CapPRDText returns a copy of prd whose long text fields are cut to max
// bytes each (see TruncatedField). prd itself, and so the stored record, is
// never modified: every slice and pointer the cap touches is copied first.
// max <= 0 returns prd unchanged. A nil prd yields nil.
func CapPRDText(prd *PRD, max int) *PRD {
	if prd == nil || max <= 0 {
		return prd
	}
	out := *prd
	pc := &capper{max: max, ref: OutputsRef(prd.ID, "")}
	pc.str("spec", &out.Spec)
	pc.str("memory_report", &out.MemoryReport)
	out.Truncated = pc.hits

	out.Story = make([]Story, len(prd.Story))
	copy(out.Story, prd.Story)
	for si := range out.Story {
		st := &out.Story[si]
		sc := &capper{max: max, ref: pc.ref}
		sc.str("description", &st.Description)
		st.Verdicts = sc.verdicts("verdicts", st.Verdicts)
		st.Truncated = sc.hits
		if len(st.Tasks) == 0 {
			continue
		}
		st.Tasks = make([]Task, len(prd.Story[si].Tasks))
		copy(st.Tasks, prd.Story[si].Tasks)
		for ti := range st.Tasks {
			capTask(&st.Tasks[ti], max)
		}
	}
	return &out
}

// capTask caps t in place; t must be a copy.
func capTask(t *Task, max int) {
	c := &capper{max: max, ref: OutputsRef(t.PRDID, t.ID)}
	c.str("spec", &t.Spec)
	c.str("error", &t.Error)
	c.str("prior_attempt_hint", &t.PriorAttemptHint)
	t.StartFailureTail = c.strs("start_failure_tail", t.StartFailureTail)
	t.AttemptRefs = c.strs("attempt_refs", t.AttemptRefs)
	t.Verdicts = c.verdicts("verdicts", t.Verdicts)
	if v := t.Verification; v != nil {
		vc := *v
		c.str("verification.raw_judge", &vc.RawJudge)
		c.str("verification.summary", &vc.Summary)
		vc.Issues = c.strs("verification.issues", vc.Issues)
		t.Verification = &vc
	}
	t.Truncated = c.hits
}

// TaskOutputs holds the heavy text fields of one task, uncapped.
type TaskOutputs struct {
	TaskID           string   `json:"task_id"`
	StoryID          string   `json:"story_id"`
	Spec             string   `json:"spec"`
	Error            string   `json:"error,omitempty"`
	PriorAttemptHint string   `json:"prior_attempt_hint,omitempty"`
	StartFailureTail []string `json:"start_failure_tail,omitempty"`
	AttemptRefs      []string `json:"attempt_refs,omitempty"`
	// Verification text, copied from Task.Verification.
	VerifierSummary string             `json:"verifier_summary,omitempty"`
	VerifierIssues  []string           `json:"verifier_issues,omitempty"`
	RawJudge        string             `json:"raw_judge,omitempty"`
	Verdicts        []GuardrailVerdict `json:"verdicts,omitempty"`
}

// OutputsView is the reply of the outputs sub-resource.
type OutputsView struct {
	PRDID string        `json:"prd_id"`
	Tasks []TaskOutputs `json:"tasks"`
}

// BuildOutputsView returns the full heavy fields of the tasks of prd, or of
// the single task taskID when it is non-empty. The second result is false
// when taskID names no task of prd.
func BuildOutputsView(prd *PRD, taskID string) (OutputsView, bool) {
	view := OutputsView{Tasks: []TaskOutputs{}}
	if prd == nil {
		return view, taskID == ""
	}
	view.PRDID = prd.ID
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if taskID != "" && t.ID != taskID {
				continue
			}
			o := TaskOutputs{
				TaskID: t.ID, StoryID: t.StoryID, Spec: t.Spec, Error: t.Error,
				PriorAttemptHint: t.PriorAttemptHint, StartFailureTail: t.StartFailureTail,
				AttemptRefs: t.AttemptRefs, Verdicts: t.Verdicts,
			}
			if v := t.Verification; v != nil {
				o.VerifierSummary, o.VerifierIssues, o.RawJudge = v.Summary, v.Issues, v.RawJudge
			}
			view.Tasks = append(view.Tasks, o)
		}
	}
	return view, taskID == "" || len(view.Tasks) > 0
}
