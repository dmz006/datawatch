package autonomous

import "testing"

func contextRefTestLookup() ContextRefLookup {
	return ContextRefLookup{
		AutomatonName: func(id string) (string, bool) {
			if id == "a1b2c3d4" {
				return "Nightly cleanup", true
			}
			return "", false
		},
		TaskName: func(id string) (string, bool) {
			if id == "t-1" {
				return "Wire the route", true
			}
			if id == "t-blank" {
				return "  ", true
			}
			return "", false
		},
		DocExists: func(p string) bool { return p == "docs/plans/plan.md" || p == "docs/a b/x.md" },
	}
}

func TestQuestionContextRefProposal(t *testing.T) {
	q := Question{Context: QuestionContextProposal, ProposalPath: "docs/plans/plan.md"}
	got := QuestionContextRef(q, contextRefTestLookup())
	want := ContextRef{
		Kind:      ContextRefProposal,
		Title:     "plan.md",
		DocPath:   "docs/plans/plan.md",
		ViewerURL: "/diagrams.html#docs/plans/plan.md",
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestQuestionContextRefProposalPathEscaped(t *testing.T) {
	q := Question{Context: QuestionContextProposal, ProposalPath: "docs/a b/x.md"}
	got := QuestionContextRef(q, contextRefTestLookup())
	if got.ViewerURL != "/diagrams.html#docs/a%20b/x.md" {
		t.Fatalf("viewer url %q", got.ViewerURL)
	}
}

func TestQuestionContextRefProposalOutsideDocsTree(t *testing.T) {
	cases := map[string]string{
		"not under docs":        "internal/server/questions.go",
		"docs prefix lookalike": "docsx/plan.md",
		"not markdown":          "docs/plans/plan.png",
		"unclean segment":       "docs/plans//plan.md",
		"dot segment":           "docs/./plan.md",
		"missing file":          "docs/plans/missing.md",
		"backslash":             "docs\\plans\\plan.md",
		"control character":     "docs/plans/pl\nan.md",
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			got := QuestionContextRef(Question{Context: QuestionContextProposal, ProposalPath: p}, contextRefTestLookup())
			if got.Kind != ContextRefProposal {
				t.Fatalf("kind %q", got.Kind)
			}
			if got.ViewerURL != "" {
				t.Fatalf("viewer url %q, want none", got.ViewerURL)
			}
		})
	}
}

func TestQuestionContextRefTraversalPath(t *testing.T) {
	for _, p := range []string{"docs/../../etc/passwd.md", "../docs/plan.md", "/etc/passwd", "docs/plans/../../secret.md"} {
		got := QuestionContextRef(Question{Context: QuestionContextProposal, ProposalPath: p}, ContextRefLookup{})
		if got.ViewerURL != "" || got.DocPath != "" {
			t.Fatalf("%q: got doc_path %q viewer_url %q, want both empty", p, got.DocPath, got.ViewerURL)
		}
		if got.Kind != ContextRefProposal {
			t.Fatalf("%q: kind %q", p, got.Kind)
		}
	}
}

func TestQuestionContextRefProposalWithoutDocsCheck(t *testing.T) {
	q := Question{Context: QuestionContextProposal, ProposalPath: "docs/plans/plan.md"}
	got := QuestionContextRef(q, ContextRefLookup{})
	if got.ViewerURL != "/diagrams.html#docs/plans/plan.md" {
		t.Fatalf("viewer url %q", got.ViewerURL)
	}
}

func TestQuestionContextRefAutomaton(t *testing.T) {
	got := QuestionContextRef(Question{Context: QuestionContextPrd, PRDID: "a1b2c3d4"}, contextRefTestLookup())
	want := ContextRef{Kind: ContextRefAutomaton, Title: "Nightly cleanup", ID: "a1b2c3d4"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// Unresolvable: the id is the title. No lookup at all behaves the same.
	for _, lk := range []ContextRefLookup{contextRefTestLookup(), {}} {
		got = QuestionContextRef(Question{Context: QuestionContextPrd, PRDID: "ffffffff"}, lk)
		if got.Title != "ffffffff" || got.ID != "ffffffff" {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestQuestionContextRefTask(t *testing.T) {
	got := QuestionContextRef(Question{Context: QuestionContextTask, TaskID: "t-1"}, contextRefTestLookup())
	want := ContextRef{Kind: ContextRefTask, Title: "Wire the route", ID: "t-1"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, id := range []string{"t-unknown", "t-blank"} {
		got = QuestionContextRef(Question{Context: QuestionContextTask, TaskID: id}, contextRefTestLookup())
		if got.Title != id {
			t.Fatalf("%s: title %q, want the id", id, got.Title)
		}
	}
}

func TestQuestionContextRefRegistryRow(t *testing.T) {
	got := QuestionContextRef(Question{Context: QuestionContextRegistryRow, RegistryRowKey: "row 12"}, contextRefTestLookup())
	want := ContextRef{Kind: ContextRefRegistryRow, Title: "row 12", ID: "row 12"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestQuestionContextRefMissingLink(t *testing.T) {
	cases := []Question{
		{Context: QuestionContextNone},
		{Context: ""},
		{Context: QuestionContextPrd},
		{Context: QuestionContextTask},
		{Context: QuestionContextProposal},
		{Context: QuestionContextRegistryRow},
		{Context: "unknown", PRDID: "a1b2c3d4"},
	}
	for _, q := range cases {
		got := QuestionContextRef(q, contextRefTestLookup())
		if got != (ContextRef{Kind: ContextRefNone}) {
			t.Fatalf("context %q: got %+v, want the none kind", q.Context, got)
		}
	}
}
