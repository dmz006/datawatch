package delivery

import (
	"strings"
	"testing"
	"time"
)

func renderView(id string, state RequestState, label, proofAs string) RequestView {
	return RequestView{Request: Request{
		ID: id, Kind: ReqAsk, Class: ClassAsk, Role: RoleSupervisor, Scope: Scope{Kind: ScopeProject, ID: "web"},
		State: state, ProofAs: proofAs, Created: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
	}, Label: label}
}

func TestReqRenderListShowsContractLabelsAndCodes(t *testing.T) {
	v := renderView("r-aaaa", StateWaiting, LabelFetched, "")
	v.ReasonCode, v.Overdue, v.MayRunTwice = WhyTakeNotProven, true, true
	v.Holder = &Holder{SessionID: "host-1a2b", Family: "pi"}
	out := RenderRequests([]RequestView{v}, 0)
	for _, want := range []string{"r-aaaa", "ask", "supervisor@project:web", LabelFetched, WhyTakeNotProven, "overdue", "may run twice", "host-1a2b"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if got := RenderRequests(nil, 0); got != "no requests" {
		t.Errorf("empty list = %q", got)
	}
}

// Only proven done reads "done", whatever label arrives.
func TestReqRenderOnlyProvenDoneReadsDone(t *testing.T) {
	proven := renderView("r-1", StateDone, LabelDone, ProofDone)
	if got := RenderLabel(proven); got != LabelDone {
		t.Fatalf("proven done reads %q", got)
	}
	for _, v := range []RequestView{
		renderView("r-2", StateReported, LabelDone, ""),
		renderView("r-3", StateDone, LabelDone, ProofDelivered),
		renderView("r-4", StateDone, LabelDone, ProofAcceptedUnproven),
		renderView("r-5", StateAccepted, LabelDone, ProofDone),
		renderView("r-6", StateReported, "", ""),
		renderView("r-7", StateReported, "all done, trust me", ""),
	} {
		got := RenderLabel(v)
		if got == LabelDone || got == "all done, trust me" || got == "" {
			t.Errorf("%s (%s/%s %q) reads %q", v.ID, v.State, v.ProofAs, v.Label, got)
		}
	}
	for _, l := range []string{LabelDeliveredOnly, LabelAcceptedUnproven} {
		proof := ProofDelivered
		if l == LabelAcceptedUnproven {
			proof = ProofAcceptedUnproven
		}
		if got := RenderLabel(renderView("r-8", StateDone, l, proof)); got != l {
			t.Errorf("%q reads %q", l, got)
		}
	}
}

func TestReqRenderListIsBounded(t *testing.T) {
	views := []RequestView{renderView("r-1", StateWaiting, LabelWaiting, ""), renderView("r-2", StateWaiting, LabelWaiting, ""), renderView("r-3", StateWaiting, LabelWaiting, "")}
	out := RenderRequests(views, 2)
	if strings.Contains(out, "r-3") || !strings.Contains(out, "1 more") {
		t.Fatalf("bounded list:\n%s", out)
	}
}

// A view holds no content, and rendering one never shows a claimed word: the
// call history's claimed names are not part of it.
func TestReqRenderOneAndRolesAndMine(t *testing.T) {
	v := renderView("r-9", StateAccepted, LabelAccepted, "")
	v.Calls = []CallRecord{{Verb: "take", Observed: "admin-token", Refused: "not_holder"}}
	v.NonOperatorActions = 2
	one := RenderRequest(v)
	for _, want := range []string{"r-9", LabelAccepted, "take", "not_holder", "admin-token", "2"} {
		if !strings.Contains(one, want) {
			t.Errorf("one lacks %q:\n%s", want, one)
		}
	}
	roles := RenderRoles([]RoleBinding{{Role: RoleSupervisor, Scope: Scope{Kind: ScopeProject, ID: "web"}, SessionID: "host-1a2b", SelfGiven: true, GivenBy: "session:host-1a2b"}})
	for _, want := range []string{"supervisor@project:web", "host-1a2b", "self-given", "not yet proven"} {
		if !strings.Contains(roles, want) {
			t.Errorf("roles lack %q:\n%s", want, roles)
		}
	}
	if got := RenderRoles(nil); got != "no roles are held" {
		t.Errorf("no roles = %q", got)
	}
	mine := RenderMine(MineReply{SessionID: "host-1a2b", Items: []RequestView{v}}, 0)
	if !strings.Contains(mine, "r-9") || !strings.Contains(mine, "host-1a2b") {
		t.Errorf("mine:\n%s", mine)
	}
}
