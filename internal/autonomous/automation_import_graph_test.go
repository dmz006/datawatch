package autonomous

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func importTestManager(t *testing.T, n int) (*Manager, []string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ids := make([]string, n)
	for i := range ids {
		p, err := m.CreatePRD("prd", "/p", "", "", "")
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		ids[i] = p.ID
	}
	return m, ids
}

func memberByID(a []AutomationMember, id string) AutomationMember {
	for _, m := range a {
		if m.ID == id {
			return m
		}
	}
	return AutomationMember{}
}

func TestImportGraphLinear(t *testing.T) {
	m, p := importTestManager(t, 3)
	src := ImportGraphSource{ID: "g1", Title: "Chain", Nodes: []ImportGraphNode{
		{ID: "c", Kind: "prd", PRDID: p[2], DependsOn: []string{"b"}},
		{ID: "b", Kind: "prd", PRDID: p[1], DependsOn: []string{"a"}},
		{ID: "a", Kind: "prd", PRDID: p[0]},
	}}
	res, err := m.ImportGraph(src, false)
	if err != nil {
		t.Fatalf("ImportGraph: %v", err)
	}
	a := res.Automation
	if a == nil || res.DryRun {
		t.Fatalf("want a stored automation, got %+v", res)
	}
	if a.Name != "Chain" || !strings.Contains(a.Description, "mported") {
		t.Fatalf("name/description = %q / %q", a.Name, a.Description)
	}
	if len(a.Members) != 3 || a.EntryMemberID != a.Members[0].ID {
		t.Fatalf("members = %+v entry %s", a.Members, a.EntryMemberID)
	}
	for i, want := range []string{p[0], p[1], p[2]} {
		mem := a.Members[i]
		if mem.PRDID != want || mem.Status != MemberPending || mem.AddedBy != "import:graph:g1" {
			t.Fatalf("member %d = %+v", i, mem)
		}
	}
	if a.Members[0].OnComplete != a.Members[1].ID || a.Members[1].OnComplete != a.Members[2].ID || a.Members[2].OnComplete != "" {
		t.Fatalf("edges wrong: %+v", a.Members)
	}
}

func TestImportGraphFanOutFanIn(t *testing.T) {
	m, p := importTestManager(t, 4)
	src := ImportGraphSource{ID: "g2", Title: "Diamond", Nodes: []ImportGraphNode{
		{ID: "A", Kind: "prd", PRDID: p[0]},
		{ID: "B", Kind: "prd", PRDID: p[1], DependsOn: []string{"A"}},
		{ID: "C", Kind: "prd", PRDID: p[2], DependsOn: []string{"A"}},
		{ID: "D", Kind: "prd", PRDID: p[3], DependsOn: []string{"B", "C"}},
	}}
	in, _, err := ImportGraphToInput(src)
	if err != nil {
		t.Fatalf("ImportGraphToInput: %v", err)
	}
	if err := validateAutomation(&Automation{Name: in.Name, Status: AutomationDraft, EntryMemberID: in.EntryMemberID, Members: in.Members}); err != nil {
		t.Fatalf("validateAutomation: %v", err)
	}
	res, err := m.ImportGraph(src, false)
	if err != nil {
		t.Fatalf("ImportGraph: %v", err)
	}
	var fork, join AutomationMember
	for _, mem := range res.Automation.Members {
		switch mem.Kind {
		case MemberKindFork:
			fork = mem
		case MemberKindJoin:
			join = mem
		}
	}
	if fork.ID == "" || join.ID == "" {
		t.Fatalf("missing fork/join: %+v", res.Automation.Members)
	}
	if !sameMemberSet(fork.ForkMembers, join.JoinMembers) || len(fork.ForkMembers) != 2 {
		t.Fatalf("fork %v join %v", fork.ForkMembers, join.JoinMembers)
	}
	if join.OnBranchFailure != "" {
		t.Fatalf("on_branch_failure = %q, want default", join.OnBranchFailure)
	}
	entry := memberByID(res.Automation.Members, res.Automation.EntryMemberID)
	if entry.PRDID != p[0] || entry.OnComplete != fork.ID {
		t.Fatalf("entry = %+v", entry)
	}
	last := memberByID(res.Automation.Members, join.OnComplete)
	if last.PRDID != p[3] {
		t.Fatalf("join leads to %+v", last)
	}
	for _, b := range fork.ForkMembers {
		br := memberByID(res.Automation.Members, b)
		if br.OnComplete != "" || br.OnBlocked != "" || br.OnFailed != "" {
			t.Fatalf("branch %+v carries edges", br)
		}
	}
}

func TestImportGraphEntryFork(t *testing.T) {
	m, p := importTestManager(t, 3)
	src := ImportGraphSource{ID: "g3", Title: "Roots", Nodes: []ImportGraphNode{
		{ID: "a", Kind: "prd", PRDID: p[0]},
		{ID: "b", Kind: "prd", PRDID: p[1]},
		{ID: "c", Kind: "prd", PRDID: p[2], DependsOn: []string{"a", "b"}},
	}}
	res, err := m.ImportGraph(src, false)
	if err != nil {
		t.Fatalf("ImportGraph: %v", err)
	}
	if memberByID(res.Automation.Members, res.Automation.EntryMemberID).Kind != MemberKindFork {
		t.Fatalf("entry is not a fork: %+v", res.Automation.Members)
	}
}

func TestImportGraphUnrepresentable(t *testing.T) {
	prd := func(id string, deps ...string) ImportGraphNode {
		return ImportGraphNode{ID: id, Kind: "prd", PRDID: "p" + id, DependsOn: deps}
	}
	cases := []struct {
		name   string
		nodes  []ImportGraphNode
		reason string
	}{
		{"cycle", []ImportGraphNode{prd("a", "b"), prd("b", "a")}, "cycle"},
		{"unknown dependency", []ImportGraphNode{prd("a", "zz")}, "unknown node zz"},
		{"empty prd id", []ImportGraphNode{{ID: "a", Kind: "prd"}}, "empty prd id"},
		{"no prd nodes", []ImportGraphNode{{ID: "g", Kind: "guardrail", Guardrail: "rules"}}, "no prd nodes"},
		{"branch with successors", []ImportGraphNode{
			prd("a"), prd("b", "a"), prd("c", "a"), prd("e", "b"), prd("d", "e", "c"),
		}, "before the join"},
		{"partial join", []ImportGraphNode{
			prd("a"), prd("b", "a"), prd("c", "a"), prd("e", "a"), prd("d", "b", "c"),
		}, "partial join"},
		{"join with extra dependency", []ImportGraphNode{
			prd("a"), prd("b", "a"), prd("c", "a"), prd("d", "b", "c", "a"),
		}, "partial join"},
		{"nested fork", []ImportGraphNode{
			prd("a"), prd("b", "a"), prd("c", "a"), prd("d", "b"), prd("e", "b"), prd("f", "c", "d", "e"),
		}, "nested fork"},
		{"two components", []ImportGraphNode{prd("a"), prd("b")}, "disconnected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ImportGraphToInput(ImportGraphSource{ID: "g", Title: "t", Nodes: tc.nodes})
			if !errors.Is(err, ErrGraphNotRepresentable) {
				t.Fatalf("err = %v, want ErrGraphNotRepresentable", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err = %q, want substring %q", err, tc.reason)
			}
		})
	}
}

func TestImportGraphDryRun(t *testing.T) {
	m, p := importTestManager(t, 2)
	src := ImportGraphSource{ID: "g4", Title: "Dry", Nodes: []ImportGraphNode{
		{ID: "a", Kind: "prd", PRDID: p[0]},
		{ID: "b", Kind: "prd", PRDID: p[1], DependsOn: []string{"a"}},
	}}
	dry, err := m.ImportGraph(src, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || dry.Input == nil || dry.Automation != nil {
		t.Fatalf("dry result = %+v", dry)
	}
	if n := len(m.ListAutomations()); n != 0 {
		t.Fatalf("dry run stored %d automations", n)
	}
	if prd, _ := m.Store().GetPRD(p[0]); prd.AutomationID != "" {
		t.Fatalf("dry run stamped prd with %q", prd.AutomationID)
	}
	real, err := m.ImportGraph(src, false)
	if err != nil {
		t.Fatalf("real run: %v", err)
	}
	if len(m.ListAutomations()) != 1 {
		t.Fatalf("real run did not store")
	}
	got := real.Automation
	want := dry.Input
	if got.Name != want.Name || got.Description != want.Description || got.EntryMemberID != want.EntryMemberID {
		t.Fatalf("header mismatch: %+v vs %+v", got, want)
	}
	if len(got.Members) != len(want.Members) {
		t.Fatalf("member count %d vs %d", len(got.Members), len(want.Members))
	}
	for i := range want.Members {
		g, w := got.Members[i], want.Members[i]
		if g.ID != w.ID || g.PRDID != w.PRDID || g.OnComplete != w.OnComplete || g.AddedBy != w.AddedBy ||
			g.Kind != w.Kind || !reflect.DeepEqual(g.ForkMembers, w.ForkMembers) || !reflect.DeepEqual(g.JoinMembers, w.JoinMembers) {
			t.Fatalf("member %d: %+v vs %+v", i, g, w)
		}
	}
}

func TestImportGraphUnknownPRDSurfacesStoreError(t *testing.T) {
	m, _ := importTestManager(t, 0)
	_, err := m.ImportGraph(ImportGraphSource{ID: "g5", Title: "x", Nodes: []ImportGraphNode{
		{ID: "a", Kind: "prd", PRDID: "nosuchprd"},
	}}, false)
	if err == nil || errors.Is(err, ErrGraphNotRepresentable) {
		t.Fatalf("err = %v, want the store's own error", err)
	}
	if len(m.ListAutomations()) != 0 {
		t.Fatalf("failed import stored an automation")
	}
}

func TestImportGraphGuardrailNodes(t *testing.T) {
	m, p := importTestManager(t, 2)
	src := ImportGraphSource{ID: "g6", Title: "Guarded", Nodes: []ImportGraphNode{
		{ID: "a", Kind: "prd", PRDID: p[0]},
		{ID: "ga", Kind: "guardrail", Guardrail: "security", DependsOn: []string{"a"}},
		{ID: "b", Kind: "prd", PRDID: p[1], DependsOn: []string{"ga"}},
		{ID: "gb", Kind: "guardrail", Guardrail: "docs", DependsOn: []string{"b"}},
	}}
	res, err := m.ImportGraph(src, false)
	if err != nil {
		t.Fatalf("ImportGraph: %v", err)
	}
	if len(res.Automation.Members) != 2 {
		t.Fatalf("members = %+v", res.Automation.Members)
	}
	if res.Automation.Members[0].OnComplete != res.Automation.Members[1].ID {
		t.Fatalf("chain not kept through the guardrail: %+v", res.Automation.Members)
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	for i, want := range [][2]string{{"security", "node a"}, {"docs", "node b"}} {
		if !strings.Contains(res.Warnings[i], want[0]) || !strings.Contains(res.Warnings[i], want[1]) {
			t.Fatalf("warning %d = %q", i, res.Warnings[i])
		}
	}
}
