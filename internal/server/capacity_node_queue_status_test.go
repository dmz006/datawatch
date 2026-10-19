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

// TestHandleCapacity_NodePoolQueueDepthAndHolder verifies a node:<name> pool
// reports queue_depth (waiters on that pool) and holder (comma-separated
// holder IDs occupying its slots) in the /api/capacity pool JSON.
func TestHandleCapacity_NodePoolQueueDepthAndHolder(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	l.SetLimit("node:gpu", 1)
	s.SetCapacity(l)

	// One holder takes the only node:gpu slot.
	held := make(chan struct{})
	go func() {
		_ = l.Acquire(context.Background(), capacity.Request{Holder: "task-holder", PRDID: "prd-a", Pools: []string{"node:gpu"}}, 0, nil, nil)
		close(held)
	}()
	<-held

	// One waiter queues behind it.
	go func() {
		_ = l.Acquire(context.Background(), capacity.Request{Holder: "task-waiter", PRDID: "prd-b", Pools: []string{"node:gpu"}}, time.Second, nil, nil)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(l.Snapshot().Waiting) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Pools []struct {
			Name       string  `json:"name"`
			QueueDepth *int    `json:"queue_depth"`
			Holder     *string `json:"holder"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range resp.Pools {
		if p.Name != "node:gpu" {
			continue
		}
		found = true
		if p.QueueDepth == nil || *p.QueueDepth != 1 {
			t.Errorf("node:gpu queue_depth = %v, want 1", p.QueueDepth)
		}
		if p.Holder == nil || *p.Holder != "task-holder" {
			t.Errorf("node:gpu holder = %v, want task-holder", p.Holder)
		}
	}
	if !found {
		t.Fatalf("node:gpu missing from /api/capacity: %s", rec.Body.String())
	}
}
