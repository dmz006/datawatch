// Comm-channel configure path for the per-LLM billing mode:
// `configure llm.<name>.billing_mode=<mode>`, validated by the shared
// validator so the refusal matches REST.

package router

import (
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// runConfigure sends one configure line and returns the reply plus the key and
// value that reached the daemon (empty when the daemon was never called).
func runBillingConfigure(t *testing.T, text string) (reply, gotKey, gotValue string) {
	t.Helper()
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) {
		mu.Lock()
		reply = s
		mu.Unlock()
		wg.Done()
	}}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		gotKey, gotValue = key, value
		mu.Unlock()
		return nil
	})
	r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return reply, gotKey, gotValue
}

func TestConfigureLLMBillingMode_Valid(t *testing.T) {
	reply, key, value := runBillingConfigure(t, "llm.worker.billing_mode=allowance")
	if key != "llms.worker.billing_mode" || value != "allowance" {
		t.Fatalf("daemon got %q=%q, want llms.worker.billing_mode=allowance", key, value)
	}
	if !strings.Contains(reply, "Set") {
		t.Errorf("reply = %q, want a confirmation", reply)
	}
}

func TestConfigureLLMBillingMode_Invalid(t *testing.T) {
	reply, key, _ := runBillingConfigure(t, "llm.worker.billing_mode=bogus")
	if key != "" {
		t.Errorf("daemon called with %q, want no call", key)
	}
	if want := config.ValidateBillingMode("bogus").Error(); !strings.Contains(reply, want) {
		t.Errorf("reply = %q, want it to contain %q", reply, want)
	}
}

func TestConfigureHelp_NamesBillingModes(t *testing.T) {
	for _, text := range []string{"help", "list"} {
		reply, _, _ := runBillingConfigure(t, text)
		for _, w := range []string{"billing_mode", "api", "allowance", "local"} {
			if !strings.Contains(reply, w) {
				t.Errorf("configure %s reply missing %q", text, w)
			}
		}
	}
}
