// The request routes over the real store and roles (parts A, B and C
// together). It skips while the store is a stub.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/delivery"
)

type joinSessions struct{}

func (joinSessions) Info(id string) (delivery.SessionInfo, bool) {
	switch id {
	case reqHolder, "sess-other":
		return delivery.SessionInfo{ID: id, Family: "claude-code", Live: true}, true
	}
	return delivery.SessionInfo{}, false
}

func TestReqSurfaceJoinOverTheRealStore(t *testing.T) {
	store, err := delivery.OpenRequests(delivery.RequestOptions{Dir: t.TempDir(), Host: "this-host", Sessions: joinSessions{}})
	if errors.Is(err, delivery.ErrRequestsNotBuilt) {
		t.Skip("the request store is not built in this tree")
	}
	if err != nil {
		t.Fatal(err)
	}
	x := reqCallers(t)
	x.s.requests = store
	h := x.h
	do := func(method, path, who, body string, code int) string {
		t.Helper()
		rr := deliveryCall(h, method, path, x.tokens[who], body, nil)
		if rr.Code != code {
			t.Fatalf("%s %s by %s = %d, want %d: %s", method, path, who, rr.Code, code, rr.Body.String())
		}
		return rr.Body.String()
	}
	clean := func(what, text string) {
		t.Helper()
		if strings.Contains(text, reqMarker) {
			t.Fatalf("%s holds content: %s", what, text)
		}
	}
	create := `{"kind":"ask","role":"session","scope":{"kind":"session","id":"sess-holder"},"key":"join-1","content":{"ask":"` + reqMarker + `"}}`
	var made struct {
		Request delivery.RequestView `json:"request"`
		Created bool                 `json:"created"`
	}
	text := do("POST", "/api/requests", "operator", create, http.StatusOK)
	clean("create", text)
	if err := json.Unmarshal([]byte(text), &made); err != nil || !made.Created {
		t.Fatalf("create = %s", text)
	}
	id := made.Request.ID
	if again := do("POST", "/api/requests", "operator", create, http.StatusOK); !strings.Contains(again, `"created":false`) || !strings.Contains(again, id) {
		t.Fatalf("the same key made another: %s", again)
	}
	do("POST", "/api/requests", "operator", strings.Replace(create, `"key"`, `"host":"elsewhere","key"`, 1), http.StatusUnprocessableEntity)
	do("POST", "/api/requests", "peer", create, http.StatusUnprocessableEntity)
	clean("list", do("GET", "/api/requests", "operator", "", http.StatusOK))
	if mine := do("GET", "/api/requests/mine", "holder", "", http.StatusOK); !strings.Contains(mine, id) {
		t.Fatalf("mine = %s", mine)
	}
	for _, who := range []string{"operator", "other", "unknown"} {
		do("POST", "/api/requests/"+id+"/take", who, `{"claimed_by":"sess-holder","session_id":"sess-holder"}`, http.StatusForbidden)
	}
	var taken delivery.TakeReply
	if err := json.Unmarshal([]byte(do("POST", "/api/requests/"+id+"/take", "holder", `{"via":"tool"}`, http.StatusOK)), &taken); err != nil ||
		!strings.Contains(taken.Content, reqMarker) {
		t.Fatalf("take = %+v %v", taken, err)
	}
	clean("accept", do("POST", "/api/requests/"+id+"/accept", "holder", `{}`, http.StatusOK))
	do("POST", "/api/queue/complete", "other", `{"id":"`+id+`"}`, http.StatusForbidden)
	do("POST", "/api/queue/complete", "holder", `{"id":"`+id+`","result":{"out":"`+reqMarker+`"}}`, http.StatusOK)
	one := do("GET", "/api/requests?id="+id, "operator", "", http.StatusOK)
	clean("show", one)
	var view delivery.RequestView
	if err := json.Unmarshal([]byte(one), &view); err != nil || view.State != delivery.StateReported ||
		delivery.RenderLabel(view) != delivery.LabelNotAccepted {
		t.Fatalf("after the report: %s", one)
	}
	do("POST", "/api/requests/r-invented/take", "holder", "", http.StatusNotFound)
	if audit := x.auditText(t); !strings.Contains(audit, "r-invented") || strings.Contains(audit, reqMarker) {
		t.Fatalf("audit = %s", audit)
	}
	do("POST", "/api/requests/"+id+"/drop", "operator", `{}`, http.StatusBadRequest)
	clean("drop", do("POST", "/api/requests/"+id+"/drop", "operator", `{"reason_code":"obsolete","words":"`+reqMarker+`"}`, http.StatusOK))
	clean("list after", do("GET", "/api/requests?holder=sess-holder", "operator", "", http.StatusOK))
}
