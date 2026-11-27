package delivery

import (
	"context"
	"errors"
	"testing"
)

type cxRoleFx struct {
	b *cxBook
	s cxSessions
	k RoleKeeper
}

func cxRoles() cxRoleFx {
	s := cxSessions{}
	s.add("s1", "claude-code")
	s.add("s2", "pi")
	return cxRoleFx{b: cxNewBook(), s: s, k: NewRoleKeeper(s)}
}

var cxOp = Caller{Kind: CallerOperator, Surface: SurfaceREST}

func cxGive(session string) RoleInput {
	return RoleInput{Role: RoleSupervisor, Scope: cxProject("p1"), SessionID: session}
}

// give does what part A does: save the hello and its ID on the binding.
func (f cxRoleFx) give(t *testing.T, c Caller, in RoleInput) (RoleBinding, *CreateInput) {
	t.Helper()
	n, hello, err := f.k.Give(context.Background(), f.b, c, in)
	cxNoErr(t, err)
	if hello != nil {
		id := RequestIDPrefix + "hello-" + hello.Key
		f.b.put(Request{ID: id, Kind: ReqHello, Role: hello.Role, Scope: hello.Scope, Source: hello.Source,
			State: StateWaiting}, hello.Content)
		n.HelloID = id
		cxNoErr(t, f.b.PutBinding(n))
	}
	return n, hello
}

func TestReqRoleGiveMakesAHello(t *testing.T) {
	f := cxRoles()
	n, hello := f.give(t, cxOp, cxGive("s1"))
	if n.SessionID != "s1" || n.GivenBy != "admin-token" || n.SelfGiven || n.Generation != 1 || n.CanTakeAt != nil {
		t.Fatalf("binding: %+v", n)
	}
	if hello == nil || hello.Kind != ReqHello || hello.Role != RoleSession ||
		hello.Scope != (Scope{Kind: ScopeSession, ID: "s1"}) || hello.Key != "hello:s1:1" {
		t.Fatalf("hello: %+v", hello)
	}
	if got, ok := f.k.Resolve(f.b, RoleSupervisor, cxProject("p1")); !ok || got.SessionID != "s1" {
		t.Fatalf("resolve: %+v %v", got, ok)
	}
}

func TestReqRoleSelfAssignIsRecorded(t *testing.T) {
	f := cxRoles()
	n, _ := f.give(t, Caller{Kind: CallerSession, SessionID: "s1", Surface: SurfaceMCP}, cxGive("s1"))
	if !n.SelfGiven || n.GivenBy != "session:s1" {
		t.Fatalf("self-assign must be recorded: %+v", n)
	}
}

func TestReqRoleOneHolderForceNeedsAReason(t *testing.T) {
	f := cxRoles()
	f.give(t, cxOp, cxGive("s1"))
	if _, _, err := f.k.Give(context.Background(), f.b, cxOp, cxGive("s2")); !errors.Is(err, ErrRoleHeld) {
		t.Fatalf("a held role: %v", err)
	}
	in := cxGive("s2")
	in.Force = true
	if _, _, err := f.k.Give(context.Background(), f.b, cxOp, in); !errors.Is(err, ErrReasonNeeded) {
		t.Fatalf("force without a reason: %v", err)
	}
	in.ReasonCode = "Not A Code"
	if _, _, err := f.k.Give(context.Background(), f.b, cxOp, in); !errors.Is(err, ErrReasonCode) {
		t.Fatalf("force with a sentence: %v", err)
	}
	in.ReasonCode = "replaced"
	n, hello := f.give(t, cxOp, in)
	if n.SessionID != "s2" || n.Generation != 2 || hello == nil {
		t.Fatalf("forced: %+v %+v", n, hello)
	}
	if len(f.b.bindings) != 1 {
		t.Fatalf("one holder per role and scope: %+v", f.b.bindings)
	}
}

func TestReqRoleAGoneHolderNeedsNoForce(t *testing.T) {
	f := cxRoles()
	f.give(t, cxOp, cxGive("s1"))
	f.s["s1"] = SessionInfo{ID: "s1", Family: "claude-code"}
	if _, ok := f.k.Resolve(f.b, RoleSupervisor, cxProject("p1")); ok {
		t.Fatal("an ended session holds no role")
	}
	if n, _ := f.give(t, cxOp, cxGive("s2")); n.SessionID != "s2" || n.Generation != 2 {
		t.Fatalf("%+v", n)
	}
}

func TestReqRoleOnlyTheProjectSupervisorCanBeGiven(t *testing.T) {
	f := cxRoles()
	f.s["dead"] = SessionInfo{ID: "dead"}
	for name, in := range map[string]RoleInput{
		"worker":     {Role: RoleWorker, Scope: cxProject("p1"), SessionID: "s1"},
		"host scope": {Role: RoleSupervisor, Scope: Scope{Kind: ScopeHost, ID: "h"}, SessionID: "s1"},
		"a path":     {Role: RoleSupervisor, Scope: cxProject("/srv/x"), SessionID: "s1"},
		"no session": {Role: RoleSupervisor, Scope: cxProject("p1")},
		"unknown":    {Role: RoleSupervisor, Scope: cxProject("p1"), SessionID: "nobody"},
		"ended":      {Role: RoleSupervisor, Scope: cxProject("p1"), SessionID: "dead"},
	} {
		if _, _, err := f.k.Give(context.Background(), f.b, cxOp, in); !errors.Is(err, ErrRequestInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.b.bindings) != 0 {
		t.Fatalf("refused gives saved %+v", f.b.bindings)
	}
}

func TestReqRoleSaveFailureGivesNothing(t *testing.T) {
	f := cxRoles()
	f.b.failSave = true
	_, hello, err := f.k.Give(context.Background(), f.b, cxOp, cxGive("s1"))
	if !errors.Is(err, ErrCannotRecord) || hello != nil || len(f.b.bindings) != 0 {
		t.Fatalf("%v %+v", err, hello)
	}
}

func TestReqRoleHelloCountsOnlyByTheWorkerPolicy(t *testing.T) {
	at := cxStart
	for _, tc := range []struct {
		name, session, family string
		d                     DeliveryRecord
		proven                bool
	}{
		{"claude take", "s1", "claude-code", DeliveryRecord{FetchedAt: &at}, true},
		{"claude typed", "s1", "claude-code", DeliveryRecord{ContentTypedAt: &at, NoticeAt: &at}, false},
		{"pi fetch only", "s2", "pi", DeliveryRecord{FetchedAt: &at}, false},
		{"pi seen", "s2", "pi", DeliveryRecord{FetchedAt: &at, InModelCallAt: &at}, true},
		{"unproven worker", "s2", "opencode", DeliveryRecord{FetchedAt: &at, InModelCallAt: &at}, false},
	} {
		f := cxRoles()
		f.s.add(tc.session, tc.family)
		n, _ := f.give(t, cxOp, cxGive(tc.session))
		r, _ := f.b.Request(n.HelloID)
		r.Holder, r.Delivery = &Holder{SessionID: tc.session, Family: tc.family}, tc.d
		cxNoErr(t, f.k.Delivered(context.Background(), f.b, r))
		got, _ := f.k.Resolve(f.b, RoleSupervisor, cxProject("p1"))
		if (got.CanTakeAt != nil) != tc.proven {
			t.Errorf("%s: can take %v", tc.name, got.CanTakeAt)
		}
	}
}

func TestReqRoleOnlyTheSessionsOwnHelloProves(t *testing.T) {
	f := cxRoles()
	f.give(t, cxOp, cxGive("s1"))
	at := cxStart
	for name, r := range map[string]Request{
		"not a hello": {Kind: ReqRules, Scope: Scope{Kind: ScopeSession, ID: "s1"},
			Holder: &Holder{SessionID: "s1", Family: "claude-code"}, Delivery: DeliveryRecord{FetchedAt: &at}},
		"another's hello": {Kind: ReqHello, Scope: Scope{Kind: ScopeSession, ID: "s2"},
			Holder: &Holder{SessionID: "s1", Family: "claude-code"}, Delivery: DeliveryRecord{FetchedAt: &at}},
		"no holder": {Kind: ReqHello, Scope: Scope{Kind: ScopeSession, ID: "s1"}},
	} {
		cxNoErr(t, f.k.Delivered(context.Background(), f.b, r))
		if f.b.bindings[0].CanTakeAt != nil {
			t.Fatalf("%s proved a take", name)
		}
	}
}

func TestReqRoleAProvenSessionNeedsNoSecondHello(t *testing.T) {
	f := cxRoles()
	n, _ := f.give(t, cxOp, cxGive("s1"))
	at := cxStart
	r, _ := f.b.Request(n.HelloID)
	r.Holder, r.Delivery = &Holder{SessionID: "s1", Family: "claude-code"}, DeliveryRecord{FetchedAt: &at}
	cxNoErr(t, f.k.Delivered(context.Background(), f.b, r))
	other := cxGive("s1")
	other.Scope = cxProject("p2")
	second, h := f.give(t, cxOp, other)
	if h != nil || second.CanTakeAt == nil || second.HelloID != n.HelloID {
		t.Fatalf("proof is per session: %+v %+v", second, h)
	}
	// With no role left (the session ended, or released it) it proves again:
	// a session started again may have lost what let it take.
	cxNoErr(t, f.k.Ended(context.Background(), f.b, "s1"))
	f.b.reqs[n.HelloID].State = StateDone
	if again, h := f.give(t, cxOp, cxGive("s1")); h == nil || again.CanTakeAt != nil || h.Key == "hello:s1:1" {
		t.Fatalf("after the end: %+v %+v", again, h)
	}
}

func TestReqRoleKeysNeverRepeatAndAnOpenHelloIsReused(t *testing.T) {
	f := cxRoles()
	keys := map[string]bool{}
	give := func(in RoleInput) (RoleBinding, *CreateInput) {
		n, h := f.give(t, cxOp, in)
		if h != nil {
			if keys[h.Key] {
				t.Fatalf("the key %s was made twice", h.Key)
			}
			keys[h.Key] = true
		}
		return n, h
	}
	n, _ := give(cxGive("s1"))
	if same, h := give(cxGive("s1")); h != nil || same.Generation != n.Generation || same.HelloID != n.HelloID {
		t.Fatalf("the same give again: %+v %+v", same, h)
	}
	cxNoErr(t, f.k.Release(context.Background(), f.b, cxOp, cxGive("s1")))
	if again, h := give(cxGive("s1")); h != nil || again.HelloID != n.HelloID {
		t.Fatalf("an open hello is reused: %+v %+v", again, h)
	}
	// Its hello expired: a new give is a new occurrence.
	for i := 0; i < 3; i++ {
		cxNoErr(t, f.k.Release(context.Background(), f.b, cxOp, cxGive("s1")))
		for _, r := range f.b.reqs {
			r.State = StateExpired
		}
		if _, h := give(cxGive("s1")); h == nil {
			t.Fatal("no new hello after the old one expired")
		}
	}
}

func TestReqRoleReleaseAndEnded(t *testing.T) {
	f := cxRoles()
	f.give(t, cxOp, cxGive("s1"))
	other := cxGive("s1")
	other.Scope = cxProject("p2")
	f.give(t, cxOp, other)
	if err := f.k.Release(context.Background(), f.b, cxOp, cxGive("s2")); !errors.Is(err, ErrRequestState) {
		t.Fatalf("releasing for a session that does not hold it: %v", err)
	}
	if err := f.k.Release(context.Background(), f.b, cxOp, RoleInput{Role: RoleSupervisor, Scope: cxProject("none")}); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("releasing nothing: %v", err)
	}
	cxNoErr(t, f.k.Ended(context.Background(), f.b, "s1"))
	if len(f.b.bindings) != 0 {
		t.Fatalf("an ended session keeps %+v", f.b.bindings)
	}
}
