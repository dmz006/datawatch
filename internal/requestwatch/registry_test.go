package requestwatch

import (
	"sync"
	"testing"
)

func TestRegistryEmptyIsNotNil(t *testing.T) {
	got := NewRegistry().Observations()
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil list, got %#v", got)
	}
}

func TestRegistryPutReplacesAndOrders(t *testing.T) {
	r := NewRegistry()
	r.Put(Observation{SessionID: "b", State: StateIdle})
	r.Put(Observation{SessionID: "a", State: StateGenerating})
	r.Put(Observation{SessionID: "b", State: StateUnreachable})
	r.Put(Observation{State: StateIdle}) // no session id: ignored
	got := r.Observations()
	if len(got) != 2 || got[0].SessionID != "a" || got[1].SessionID != "b" {
		t.Fatalf("unexpected list: %#v", got)
	}
	if got[1].State != StateUnreachable {
		t.Fatalf("Put did not replace: %#v", got[1])
	}
}

func TestRegistryForgetEndedSession(t *testing.T) {
	r := NewRegistry()
	r.Put(Observation{SessionID: "a"})
	r.Forget("a")
	r.Forget("missing")
	if n := len(r.Observations()); n != 0 {
		t.Fatalf("want 0 observations, got %d", n)
	}
}

func TestRegistryReturnsCopy(t *testing.T) {
	r := NewRegistry()
	r.Put(Observation{SessionID: "a", Node: "n1"})
	got := r.Observations()
	got[0].Node = "changed"
	if r.Observations()[0].Node != "n1" {
		t.Fatal("caller mutation reached the registry")
	}
}

func TestRegistryConcurrentUse(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i))
			for j := 0; j < 100; j++ {
				r.Put(Observation{SessionID: id})
				_ = r.Observations()
				r.Forget(id)
			}
		}(i)
	}
	wg.Wait()
}
