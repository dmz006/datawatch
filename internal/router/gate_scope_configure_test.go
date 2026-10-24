// Gate scope setting (full | touched-plus-smoke | touched) and
// smoke_packages on the comm surfaces:
//   - configure help/list mention the two new default_quality_gates keys
//   - autonomous set-quality-gates forwards scope and smoke_packages to
//     the daemon, and the usage line names both flags
package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestGateScope_ConfigureHelpAndListMentionScopeKeys(t *testing.T) {
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
		t.Fatalf("sent = %d, want 2", len(sent))
	}
	for i, s := range sent {
		for _, want := range []string{
			"autonomous.default_quality_gates.scope",
			"autonomous.default_quality_gates.smoke_packages",
			"touched-plus-smoke",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("configure reply %d lacks %q: %q", i, want, s)
			}
		}
	}
}

func TestGateScope_SetQualityGatesUsageMentionsScopeFlags(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	joined := strings.Join(r.HandleTestMessage("autonomous set-quality-gates"), "\n")
	if !strings.Contains(joined, "[scope=full|touched-plus-smoke|touched]") {
		t.Errorf("usage lacks the scope flag: %q", joined)
	}
	if !strings.Contains(joined, "[smoke_packages=a,b]") {
		t.Errorf("usage lacks the smoke_packages flag: %q", joined)
	}
}

func TestGateScope_SetQualityGatesHelpMentionsScopeFlags(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	joined := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	if !strings.Contains(joined, "set-quality-gates <prd>") {
		t.Fatalf("autonomous help does not mention set-quality-gates:\n%s", joined)
	}
	if !strings.Contains(joined, "scope=full|touched-plus-smoke|touched") {
		t.Errorf("autonomous help lacks the scope flag: %q", joined)
	}
	if !strings.Contains(joined, "smoke_packages=") {
		t.Errorf("autonomous help lacks the smoke_packages flag: %q", joined)
	}
}

func TestGateScope_SetQualityGatesForwardsScopeAndSmokePackages(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanReview(t, &reqs)
	defer closeFn()

	r := &Router{hostname: "h", groupID: "grp1", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	out := r.HandleTestMessage("autonomous set-quality-gates prd_qg enabled=true scope=touched-plus-smoke smoke_packages=internal/pipeline,internal/server")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodPost || reqs[0].path != "/api/autonomous/prds/prd_qg/set_quality_gates" {
		t.Fatalf("request = %v", reqs[0])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, reqs[0].body)
	}
	if body["enabled"] != true {
		t.Errorf("enabled = %v, want true", body["enabled"])
	}
	if body["scope"] != "touched-plus-smoke" {
		t.Errorf("scope = %v, want touched-plus-smoke", body["scope"])
	}
	smoke, _ := body["smoke_packages"].([]any)
	if len(smoke) != 2 || smoke[0] != "internal/pipeline" || smoke[1] != "internal/server" {
		t.Errorf("smoke_packages = %v", body["smoke_packages"])
	}
	if !strings.Contains(strings.Join(out, "\n"), "set-quality-gates") {
		t.Errorf("reply does not confirm the write: %q", out)
	}
}

func TestGateScope_SetQualityGatesUnknownScopeFlagIgnored(t *testing.T) {
	// The arg parser only forwards a fixed key set; unknown flags must not
	// leak into the payload.
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanReview(t, &reqs)
	defer closeFn()

	r := &Router{hostname: "h", groupID: "grp1", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	r.HandleTestMessage("autonomous set-quality-gates prd_qg enabled=true bogus=1")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, reqs[0].body)
	}
	if _, ok := body["bogus"]; ok {
		t.Errorf("unknown flag leaked into payload: %v", body)
	}
	if _, ok := body["scope"]; ok {
		t.Errorf("scope should be absent: %v", body)
	}
}
