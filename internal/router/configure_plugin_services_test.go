// The plugins.services.* keys must be settable from chat with `configure`, so
// both the help text and the list text name each of them, configure passes a
// key through to the daemon with its value, and a value the daemon refuses
// comes back as a Configure failure rather than a set confirmation.

package router

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

var pluginServicesTestKeys = []string{
	"plugins.services.health_interval_seconds",
	"plugins.services.backlog_growth_pulls",
	"plugins.services.node_load_ratio",
	"plugins.services.small_model_max_params_billions",
	"plugins.services.small_model_context_tokens",
	"plugins.services.per_service.",
}

func TestPluginServicesConfigure_HelpAndListMentionKeys(t *testing.T) {
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
		for _, key := range pluginServicesTestKeys {
			if !strings.Contains(s, key) {
				t.Errorf("%s reply lacks %s: %q", []string{"help", "list"}[i], key, s)
			}
		}
	}
}

func TestPluginServicesConfigure_SetPassesKeyAndValue(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	var muGot sync.Mutex
	got := map[string]string{}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		muGot.Lock()
		got[key] = value
		muGot.Unlock()
		return nil
	})

	for _, kv := range []string{
		"plugins.services.health_interval_seconds=30",
		"plugins.services.health_interval_seconds=0",
		"plugins.services.backlog_growth_pulls=3",
		"plugins.services.node_load_ratio=1.2",
		"plugins.services.small_model_max_params_billions=4",
		"plugins.services.small_model_context_tokens=4096",
		"plugins.services.per_service.my-svc=managed",
		"plugins.services.per_service.my-svc=attach_only",
		"plugins.services.per_service.my-svc=",
	} {
		parts := strings.SplitN(kv, "=", 2)
		key, value := parts[0], parts[1]
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: kv})
		wg.Wait()
		muGot.Lock()
		if g := got[key]; g != value {
			muGot.Unlock()
			t.Fatalf("configure %s: got %q, want %q", kv, g, value)
		}
		muGot.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 9 {
		t.Fatalf("sent=%q", sent)
	}
	for i, s := range sent {
		if !strings.Contains(s, "Set plugins.services.") {
			t.Errorf("reply %d does not confirm the set: %q", i, s)
		}
	}
}

func TestPluginServicesConfigure_BadValueRejected(t *testing.T) {
	// The daemon refuses a value that would break the block (the same rules a
	// config load runs); configure must surface that as a failure, not a set.
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	var muGot sync.Mutex
	var gotKey, gotValue string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		muGot.Lock()
		gotKey, gotValue = key, value
		muGot.Unlock()
		return fmt.Errorf("plugins.services.health_interval_seconds: must be a whole number of seconds >= 5, got 1")
	})

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "plugins.services.health_interval_seconds=1"})
	wg.Wait()
	muGot.Lock()
	if gotKey != "plugins.services.health_interval_seconds" || gotValue != "1" {
		t.Fatalf("daemon saw %s=%q, want plugins.services.health_interval_seconds=\"1\"", gotKey, gotValue)
	}
	muGot.Unlock()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("sent=%q", sent)
	}
	if !strings.Contains(sent[0], "Configure failed") {
		t.Errorf("reply must report the failure: %q", sent[0])
	}
	if !strings.Contains(sent[0], "plugins.services.health_interval_seconds") {
		t.Errorf("reply must name the refused key: %q", sent[0])
	}
	if strings.Contains(sent[0], "Set plugins.services.health_interval_seconds") {
		t.Errorf("refused value must not be confirmed as set: %q", sent[0])
	}
}

func TestPluginServicesConfigure_RegistryCoversTheSettableKeys(t *testing.T) {
	// Every settable key names a real help line; known-key detection covers the
	// scalar keys and the per-service prefix, and rejects the empty service.
	for _, e := range pluginServicesConfigureEntries {
		if help, ok := PluginServicesConfigureKeyHelp(e.Key); !ok || help == "" {
			t.Errorf("key %s: want a non-empty help line, got %q/%v", e.Key, help, ok)
		}
		if !PluginServicesConfigureIsKnown(e.Key) {
			t.Errorf("key %s must be known", e.Key)
		}
	}
	for _, key := range []string{
		"plugins.services.per_service.my-svc",
		"plugins.services.per_service.",
		"plugins.enabled",
		"plugins.services.per_service",
	} {
		known := PluginServicesConfigureIsKnown(key)
		if key == "plugins.services.per_service." || key == "plugins.enabled" {
			if known {
				t.Errorf("key %s must not be known", key)
			}
		} else {
			if !known {
				t.Errorf("key %s must be known", key)
			}
		}
	}
	if len(PluginServicesConfigureHelpLines()) != len(pluginServicesConfigureEntries) {
		t.Errorf("help lines = %d, want %d", len(PluginServicesConfigureHelpLines()), len(pluginServicesConfigureEntries))
	}
}
