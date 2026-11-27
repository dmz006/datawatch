package delivery

import (
	"context"
	"slices"
	"testing"

	"github.com/dmz006/datawatch/internal/acceptance"
)

// doneByFirstDoer has session s1 take an ask, accept it and report "done".
func doneByFirstDoer(t *testing.T, f *cxJoinFx) string {
	t.Helper()
	ctx := context.Background()
	b := f.supervisor("s1", "claude-code")
	f.take("s1", b.HelloID)
	ask := f.ask("reissue-doers", DoneItem{ID: "c1", Statement: "A person saw it", From: "Write the file.", Method: "confirm"},
		DoneItem{ID: "c2", Statement: "A person read the detail", From: "SECRET-CXJ-7.", Method: "confirm"})
	f.take("s1", ask)
	_, err := f.store.Accept(ctx, cxAs("s1"), ask, AcceptInput{})
	cxNoErr(t, err)
	_, err = f.store.Report(ctx, cxAs("s1"), ask, ReportInput{Text: "done"})
	cxNoErr(t, err)
	return ask
}

// reissueTo drops the open request, gives the role to session to, and reissues
// the request there. It returns the new request id.
func reissueTo(t *testing.T, f *cxJoinFx, id, to string) string {
	t.Helper()
	ctx := context.Background()
	_, err := f.store.Drop(ctx, cxOp, id, ReasonInput{ReasonCode: "redo"})
	cxNoErr(t, err)
	f.s.add(to, "claude-code")
	b, err := f.store.GiveRole(ctx, cxOp, RoleInput{Role: RoleSupervisor, Scope: cxProject("p1"), SessionID: to, Force: true, ReasonCode: "swap"})
	cxNoErr(t, err)
	f.take(to, b.HelloID)
	f.tick()
	nv, err := f.store.Reissue(ctx, cxOp, id, ReasonInput{ReasonCode: "again", RepeatOK: true})
	cxNoErr(t, err)
	f.tick()
	return nv.ID
}

// answerAsFormerDoer has session s1 confirm every item of the new list, the way
// the first holder's own answers arrive.
func answerAsFormerDoer(t *testing.T, f *cxJoinFx, id, session string) {
	t.Helper()
	by := acceptance.Caller{Observed: "admin-token via mcp from session:" + session, SessionID: session, ViaTool: true}
	l := f.list(id)
	_, err := f.lists.Confirm(id, "list", true, "yes", "", l.Hash(), by)
	cxNoErr(t, err)
	for _, it := range l.Items {
		if it.Method == "confirm" {
			_, err = f.lists.Confirm(id, it.ID, true, "yes", "", "", by)
			cxNoErr(t, err)
		}
	}
	f.tick()
	f.tick()
}

// assertNoCountedAnswer fails when a former doer's answer counts as independent.
func assertNoCountedAnswer(t *testing.T, l *acceptance.List, session string) {
	t.Helper()
	for id, st := range l.State {
		if st.Confirm != nil && st.Confirm.By.SessionID == session && st.Confirm.Counts {
			t.Errorf("item %s: answer by former doer %s counts as independent", id, session)
		}
	}
}

// A reissued request's list must know the first doer before any answer arrives.
func TestReqReissueKeepsEarlierDoerOnNewList(t *testing.T) {
	f := cxJoin(t, nil)
	ask := doneByFirstDoer(t, f)
	nv := reissueTo(t, f, ask, "s2")
	f.take("s2", nv)
	l := f.list(nv)
	if !slices.Contains(l.FormerHolders, "s1") {
		t.Fatalf("new list former holders = %v, want s1", l.FormerHolders)
	}
	if slices.Contains(l.Holders, "s1") {
		t.Fatalf("new list holders = %v, s1 is a former doer only", l.Holders)
	}
	answerAsFormerDoer(t, f, nv, "s1")
	assertNoCountedAnswer(t, f.list(nv), "s1")
	if v := f.view(nv); v.State == StateDone {
		t.Fatalf("reissued request is done on the first doer's answers alone")
	}
	f.open() // a restart keeps the chain
	if v := f.view(nv); v.State == StateDone {
		t.Fatalf("after restart: reissued request is done on the first doer's answers alone")
	}
	assertNoCountedAnswer(t, f.list(nv), "s1")
}

// A reissue of a reissue keeps every earlier doer of the whole chain.
func TestReqReissueOfReissueKeepsWholeChain(t *testing.T) {
	f := cxJoin(t, nil)
	ask := doneByFirstDoer(t, f)
	nv := reissueTo(t, f, ask, "s2")
	f.take("s2", nv)
	nv2 := reissueTo(t, f, nv, "s3")
	f.take("s3", nv2)
	l := f.list(nv2)
	for _, want := range []string{"s1", "s2"} {
		if !slices.Contains(l.FormerHolders, want) {
			t.Fatalf("second reissue former holders = %v, want %s", l.FormerHolders, want)
		}
	}
	answerAsFormerDoer(t, f, nv2, "s1")
	assertNoCountedAnswer(t, f.list(nv2), "s1")
	answerAsFormerDoer(t, f, nv2, "s2")
	assertNoCountedAnswer(t, f.list(nv2), "s2")
	if v := f.view(nv2); v.State == StateDone {
		t.Fatalf("second reissue is done on former doers' answers alone")
	}
}
