// Comm-channel configure path for the plan tier inflight table: the
// operator types `configure autonomous.plan_tier_max_inflight=<table>` and
// the value is forwarded verbatim to the daemon (PUT /api/config via the
// configure func), which validates tier keys before applying the patch.
// The table's compact form (tier=N,tier=N) is legal inside the value even
// though top-level configure splits on the FIRST '='.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestCommConfigure_PlanTierTableForwardedToDaemon(t *testing.T) {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	var gotKey, gotValue string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		gotKey, gotValue = key, value
		mu.Unlock()
		return nil
	})

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.plan_tier_max_inflight=max_5x=2,api=0"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "autonomous.plan_tier_max_inflight" {
		t.Fatalf("key = %q, want autonomous.plan_tier_max_inflight", gotKey)
	}
	if gotValue != "max_5x=2,api=0" {
		t.Fatalf("value = %q, want max_5x=2,api=0 (compact table form, split only on first '=')", gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "autonomous.plan_tier_max_inflight") {
		t.Fatalf("sent = %q, want confirmation naming the key", sent)
	}
}

func TestCommConfigureHelp_MentionsPlanTierTable(t *testing.T) {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error { return nil })

	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent = %d messages, want 2", len(sent))
	}
	for i, s := range sent {
		if !strings.Contains(s, "autonomous.plan_tier_max_inflight") {
			t.Errorf("reply %d lacks autonomous.plan_tier_max_inflight: %q", i, s)
		}
	}
}
