package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// The capacity read shows an operator lease granted over a pool's limit (class
// and overage on the lease, over and reserve on the pool) and the subscription
// backends that resolve to no pool.
func TestHandleCapacity_ShowsOperatorOverageReserveAndUnmapped(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	l.SetLimit("llm:claude-code", 1)
	l.SetReserve("llm:claude-code", 1)
	_ = l.Acquire(context.Background(), capacity.Request{Holder: "w1", Pools: []string{"llm:claude-code"}}, time.Second, nil, nil)
	_ = l.Acquire(context.Background(), capacity.Request{Holder: "op1", Class: capacity.ClassOperator, Pools: []string{"llm:claude-code"}}, time.Second, nil, nil)
	s.SetCapacity(l)
	s.SetCapacityUnmapped(func() []CapacityUnmapped {
		return []CapacityUnmapped{{Backend: "claude-code", Count: 2, LastSeen: time.Now()}}
	})
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Pools []struct {
			Name    string `json:"name"`
			Held    int    `json:"held"`
			Over    int    `json:"over"`
			Reserve int    `json:"reserve"`
		} `json:"pools"`
		Leases []struct {
			Holder  string             `json:"holder"`
			Class   string             `json:"class"`
			Overage []capacity.Overage `json:"overage"`
		} `json:"leases"`
		Unmapped []CapacityUnmapped `json:"unmapped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Pools) != 1 || got.Pools[0].Held != 2 || got.Pools[0].Over != 1 || got.Pools[0].Reserve != 1 {
		t.Fatalf("pools = %+v, want held 2 over 1 reserve 1", got.Pools)
	}
	var op bool
	for _, ls := range got.Leases {
		if ls.Holder == "op1" && ls.Class == "operator" && len(ls.Overage) == 1 && ls.Overage[0].Over == 1 {
			op = true
		}
	}
	if !op {
		t.Fatalf("leases = %+v, want the operator lease with its overage", got.Leases)
	}
	if len(got.Unmapped) != 1 || got.Unmapped[0].Backend != "claude-code" {
		t.Fatalf("unmapped = %+v", got.Unmapped)
	}
}
