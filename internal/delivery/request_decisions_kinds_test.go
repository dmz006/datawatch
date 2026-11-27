// The operator's decision of 2026-11-27: only datawatch itself creates the
// request kinds that close on receipt.

package delivery

import (
	"net/http"
	"strings"
	"testing"
)

func TestReqKeyOnlyDatawatchCreatesReceiptKinds(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	var closing []RequestKind
	for kind, rule := range kindRules {
		if rule.DeliveryOnly {
			closing = append(closing, kind)
		}
	}
	if len(closing) < 3 {
		t.Fatalf("hello, answer and rules close on receipt: %v", closing)
	}
	toA := Scope{Kind: ScopeSession, ID: "a"}
	before := len(f.s.Requests(RequestFilter{}))
	n := 0
	for _, kind := range closing {
		for _, surface := range []Surface{SurfaceREST, SurfaceMCP, SurfaceCLI, SurfaceChat} {
			for _, c := range []Caller{
				{Kind: CallerOperator}, {Kind: CallerSession, SessionID: "a"}, {Kind: CallerSessionUnknown},
			} {
				c.Surface = surface
				n++
				_, created, err := f.s.Create(bg, c, CreateInput{Kind: kind, Role: RoleSession, Scope: toA,
					Key: "operator:closing-" + itoa(n), Content: RequestContent{Ask: "x"}})
				if err == nil || created {
					t.Fatalf("%s by %s over %s was created", kind, c.Observed(), surface)
				}
				if HTTPStatus(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "only datawatch") ||
					!strings.Contains(err.Error(), "closes on receipt") {
					t.Fatalf("%s: the refusal must give the reason: %d %v", kind, HTTPStatus(err), err)
				}
			}
		}
	}
	if got := len(f.s.Requests(RequestFilter{})); got != before {
		t.Fatalf("a refused create saved something: %d, was %d", got, before)
	}
	// datawatch itself still may.
	if _, created, err := f.s.Create(bg, Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon},
		CreateInput{Kind: ReqHello, Role: RoleSession, Scope: toA, Key: "hello:a:own"}); err != nil || !created {
		t.Fatalf("datawatch: %v %v", created, err)
	}
	// Other kinds are unchanged.
	if _, created, err := f.s.Create(bg, opCaller, CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope,
		Key: "operator:work", Content: RequestContent{Ask: "x"}}); err != nil || !created {
		t.Fatalf("an ask: %v %v", created, err)
	}
}

// A reissue makes a new request, so it is refused for these kinds too.
func TestReqKeyAReceiptKindIsNotReissuedFromASurface(t *testing.T) {
	f := newReqFx(t, nil)
	f.sessions.add("s", "claude-code")
	v, _, err := f.s.Create(bg, Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon}, CreateInput{
		Kind: ReqHello, Role: RoleSession, Scope: Scope{Kind: ScopeSession, ID: "s"}, Key: "hello:s:1"})
	if err != nil {
		t.Fatal(err)
	}
	f.end("s")
	f.want(v.ID, StateFailed)
	before := len(f.s.Requests(RequestFilter{}))
	_, err = f.s.Reissue(bg, opCaller, v.ID, ReasonInput{ReasonCode: "again", RepeatOK: true})
	if err == nil || !strings.Contains(err.Error(), "only datawatch") || HTTPStatus(err) != http.StatusBadRequest {
		t.Fatalf("reissue: %v", err)
	}
	r := f.want(v.ID, StateFailed)
	if len(f.s.Requests(RequestFilter{})) != before || r.ReissuedAs != "" || r.SettledAt != nil {
		t.Fatalf("a refused reissue changed something: %+v", r)
	}
	// It can still be dropped, which takes it off the list.
	if _, err := f.s.Drop(bg, opCaller, v.ID, ReasonInput{ReasonCode: "not_needed"}); err != nil {
		t.Fatal(err)
	}
}
