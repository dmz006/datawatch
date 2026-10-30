// The merged-member policy key (autonomous.automation.merged_member_policy) is
// set from chat with configure. The router holds no key list of its own: it
// hands the key and value to the daemon's PUT /api/config, which accepts
// completed, manual or block and answers 400 for anything else. So the tests
// pin that the usage and key list name the key with its default, that the verb
// carries each valid value through intact, and that the daemon's refusal of an
// unknown value reaches the operator with no "Set" confirmation.

package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

const mergedMemberPolicyKey = "autonomous.automation.merged_member_policy"

func TestMergedMemberPolicyConfigDefaultRead(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(string, string) error { return nil })

	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent=%q", sent)
	}
	for i, s := range sent {
		if !strings.Contains(s, mergedMemberPolicyKey+" (completed|manual|block; default completed)") {
			t.Errorf("reply %d does not describe %s with its values and default: %q", i, mergedMemberPolicyKey, s)
		}
	}
}

func TestMergedMemberPolicyConfigSetEachValidValue(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
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

	for _, v := range []string{"completed", "manual", "block"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: mergedMemberPolicyKey + "=" + v})
		wg.Wait()
		mu.Lock()
		if gotKey != mergedMemberPolicyKey || gotValue != v {
			t.Fatalf("configureFn got key=%q value=%q, want %q=%q", gotKey, gotValue, mergedMemberPolicyKey, v)
		}
		if last := sent[len(sent)-1]; !strings.Contains(last, "Set "+mergedMemberPolicyKey+" = "+v) {
			t.Fatalf("reply for %q lacks the apply confirmation: %q", v, last)
		}
		mu.Unlock()
	}
}

func TestMergedMemberPolicyConfigRejectsUnknownValue(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	refusal := mergedMemberPolicyKey + `: must be "completed", "manual" or "block"`
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(string, string) error { return errors.New(refusal) })

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: mergedMemberPolicyKey + "=sometimes"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	last := sent[len(sent)-1]
	if !strings.Contains(last, "Configure failed") || !strings.Contains(last, refusal) {
		t.Fatalf("reply should carry the daemon's refusal, got %q", last)
	}
	if strings.Contains(last, "Set "+mergedMemberPolicyKey) {
		t.Fatalf("a refused value must not be confirmed as set: %q", last)
	}
}
