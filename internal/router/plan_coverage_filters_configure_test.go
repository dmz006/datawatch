// The four plan-coverage finding-filter keys must be listed on the
// same chat configure surfaces as the two plan-coverage gate keys
// (plan_coverage_check / plan_coverage_min_ratio), so callers know how
// to turn the "file the plan itself creates" suppression, the scoped
// requirement extraction, the required section list and the minimum
// requirement length on and off by chat.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestPlanCoverageFilters_ConfigureHelpAndListMention(t *testing.T) {
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
		for _, key := range []string{
			"autonomous.plan_coverage_suppress_created_files",
			"autonomous.plan_coverage_scoped_requirements",
			"autonomous.plan_coverage_requirement_sections",
			"autonomous.plan_coverage_min_requirement_chars",
		} {
			if !strings.Contains(s, key) {
				t.Errorf("reply %d lacks %s: %q", i, key, s)
			}
		}
	}
}
