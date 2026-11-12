package autonomous

import (
	"reflect"
	"testing"
)

func builtDecisionRecord() DecisionRecord {
	r := validDecisionRecord()
	r.ID = "dr-0123456789abcdef0123"
	r.Label = "decision"
	return r
}

func TestDecisionQuestionBuild_TextLayout(t *testing.T) {
	r := builtDecisionRecord()
	r.Title = "  Pick the storage engine \n"
	r.Situation = "\tThe service loses its state on restart.  "
	r.Deciding = "  Which engine to use.\n"
	q := BuildQuestionFromDecision(r)
	want := "Pick the storage engine\n\nThe service loses its state on restart.\n\nDeciding: Which engine to use."
	if q.Text != want {
		t.Fatalf("text = %q, want %q", q.Text, want)
	}
	if q.Label != "decision" || q.AskedBy != "datawatch" || q.AskerKind != QuestionAskerSystem {
		t.Fatalf("label/asker = %q %q %q", q.Label, q.AskedBy, q.AskerKind)
	}
	if q.Blocking || q.Risk != "" || q.Priority != "" {
		t.Fatalf("blocking=%v risk=%q priority=%q, want all empty", q.Blocking, q.Risk, q.Priority)
	}
	r.Label = "  "
	if got := BuildQuestionFromDecision(r).Label; got != DecisionDefaultLabel {
		t.Fatalf("empty label gave %q, want %q", got, DecisionDefaultLabel)
	}
	r.Label = "design review"
	if got := BuildQuestionFromDecision(r).Label; got != "design review" {
		t.Fatalf("label = %q, want the record label", got)
	}
}

func TestDecisionQuestionBuild_OptionsAndTradeoffsMapped(t *testing.T) {
	r := builtDecisionRecord()
	r.Options = []DecisionOption{
		{ID: "zeta", Label: " Last letter ", Tradeoff: " cheap, slow "},
		{ID: "alpha", Label: "First letter", Tradeoff: ""},
		{ID: "mid", Label: "Middle", Tradeoff: "balanced"},
	}
	r.Default = "hold"
	r.Recommendation = nil
	q := BuildQuestionFromDecision(r)
	want := []QuestionOption{
		{ID: "zeta", Label: "Last letter", Tradeoffs: "cheap, slow"},
		{ID: "alpha", Label: "First letter"},
		{ID: "mid", Label: "Middle", Tradeoffs: "balanced"},
	}
	if !reflect.DeepEqual(q.Options, want) {
		t.Fatalf("options = %+v, want %+v", q.Options, want)
	}
	r.Options[0].ID = "changed"
	if q.Options[0].ID != "zeta" {
		t.Fatal("question options share memory with the record")
	}
}

func TestDecisionQuestionBuild_RecommendationAndDefaultMapped(t *testing.T) {
	r := builtDecisionRecord()
	r.Recommendation = &DecisionRecommendation{OptionID: "files", Reason: " no new dependency "}
	r.Default = "sqlite"
	q := BuildQuestionFromDecision(r)
	if q.Recommendation == nil || q.Recommendation.OptionID != "files" || q.Recommendation.Rationale != "no new dependency" {
		t.Fatalf("recommendation = %+v", q.Recommendation)
	}
	if q.Default != "sqlite" {
		t.Fatalf("default = %q, want sqlite", q.Default)
	}
	r.Recommendation = nil
	r.Default = DecisionRecordHold
	q = BuildQuestionFromDecision(r)
	if q.Recommendation != nil || q.Default != "hold" {
		t.Fatalf("recommendation = %+v default = %q, want nil and hold", q.Recommendation, q.Default)
	}
}

func TestDecisionQuestionBuild_ContextFollowsLinks(t *testing.T) {
	cases := []struct {
		name     string
		links    DecisionLinks
		context  string
		prd      string
		row      string
		proposal string
	}{
		{"all three: automaton wins", DecisionLinks{Doc: "docs/a.md", Automaton: "ee660001", RegistryRow: "row-9"},
			QuestionContextPrd, "ee660001", "", ""},
		{"registry row and doc: row wins", DecisionLinks{Doc: "docs/a.md", RegistryRow: "row-9"},
			QuestionContextRegistryRow, "", "row-9", ""},
		{"doc only", DecisionLinks{Doc: "docs/a.md"},
			QuestionContextProposal, "", "", "docs/a.md"},
		{"no links", DecisionLinks{}, QuestionContextNone, "", "", ""},
		{"blank links count as none", DecisionLinks{Doc: " ", Automaton: "  ", RegistryRow: "\t"},
			QuestionContextNone, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := builtDecisionRecord()
			r.Links = c.links
			q := BuildQuestionFromDecision(r)
			if q.Context != c.context || q.PRDID != c.prd || q.RegistryRowKey != c.row || q.ProposalPath != c.proposal {
				t.Fatalf("got context=%q prd=%q row=%q proposal=%q", q.Context, q.PRDID, q.RegistryRowKey, q.ProposalPath)
			}
			if q.TaskID != "" {
				t.Fatalf("task id = %q, want empty", q.TaskID)
			}
		})
	}
}

func TestDecisionQuestionBuild_CarriesRecordIDAndSource(t *testing.T) {
	r := builtDecisionRecord()
	q := BuildQuestionFromDecision(r)
	if q.DecisionRecordID != r.ID {
		t.Fatalf("decision record id = %q, want %q", q.DecisionRecordID, r.ID)
	}
	if q.Source != QuestionSourceDecisionRecord || QuestionSourceDecisionRecord != "decision_record" {
		t.Fatalf("source = %q", q.Source)
	}
	if q.ID != "" || q.SessionID != "" || q.ReturnPath {
		t.Fatalf("builder set store-owned fields: id=%q session=%q return=%v", q.ID, q.SessionID, q.ReturnPath)
	}
}

func TestDecisionQuestionBuild_ResultValidates(t *testing.T) {
	for _, links := range []DecisionLinks{
		{}, {Doc: "docs/plans/storage.md"}, {RegistryRow: "row-12"}, {Automaton: "ee660001", Doc: "docs/x.md"},
	} {
		r := builtDecisionRecord()
		r.Links = links
		if err := ValidateNewDecisionRecord(&r); err != nil {
			t.Fatalf("record invalid: %v", err)
		}
		q := BuildQuestionFromDecision(r)
		if err := ValidateNewQuestion(&q); err != nil {
			t.Fatalf("question built from %+v does not validate: %v", links, err)
		}
	}
	// Building is a pure function: same record, same question.
	r := builtDecisionRecord()
	if !reflect.DeepEqual(BuildQuestionFromDecision(r), BuildQuestionFromDecision(r)) {
		t.Fatal("two builds of one record differ")
	}
}
