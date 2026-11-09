package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// workerProfileKeys are the three settable worker-profile keys, as the daemon
// stores them (the autonomous. prefix is the config path).
var workerProfileKeys = []string{
	"autonomous.worker_profiles",
	"autonomous.worker_profile_defaults",
	"autonomous.worker_profile",
}

const workerProfileHowto = "docs/howto/worker-session-profiles.md"

// runConfigure sends one configure command through handleConfigure and
// returns the single reply it produced.
func runConfigure(t *testing.T, r *Router, text string, sent *[]string, mu *sync.Mutex, wg *sync.WaitGroup) string {
	t.Helper()
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(*sent) == 0 {
		t.Fatalf("no reply for %q", text)
	}
	reply := (*sent)[len(*sent)-1]
	return reply
}

func newWorkerProfileRouter(t *testing.T, fn func(key, value string) error) (*Router, *[]string, *sync.Mutex, *sync.WaitGroup) {
	t.Helper()
	var mu sync.Mutex
	var wg sync.WaitGroup
	sent := &[]string{}
	capture := func(s string) {
		mu.Lock()
		*sent = append(*sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(fn)
	return r, sent, &mu, &wg
}

// Reading each key: a bare key returns its help text, which names the howto.
func TestWorkerProfileConfigure_ReadEachKeyReturnsHelp(t *testing.T) {
	r, sent, mu, wg := newWorkerProfileRouter(t, func(string, string) error {
		t.Fatal("a bare key must not write")
		return nil
	})
	for _, key := range workerProfileKeys {
		reply := runConfigure(t, r, key, sent, mu, wg)
		if !strings.Contains(reply, key) {
			t.Errorf("reply for %s lacks the key: %q", key, reply)
		}
		if !strings.Contains(reply, workerProfileHowto) {
			t.Errorf("reply for %s lacks the howto path: %q", key, reply)
		}
	}
}

// The help and list surfaces name every worker-profile key and the howto.
func TestWorkerProfileConfigure_HelpAndListNameEachKey(t *testing.T) {
	r, sent, mu, wg := newWorkerProfileRouter(t, func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		reply := runConfigure(t, r, text, sent, mu, wg)
		for _, key := range workerProfileKeys {
			if !strings.Contains(reply, key) {
				t.Errorf("%s reply lacks %s: %q", text, key, reply)
			}
		}
		if !strings.Contains(reply, workerProfileHowto) {
			t.Errorf("%s reply lacks the howto path", text)
		}
	}
}

// A valid write goes through the configure function with the key and value,
// and the reply confirms it.
func TestWorkerProfileConfigure_ValidWriteIsApplied(t *testing.T) {
	stored := map[string]string{}
	fn := func(key, value string) error {
		stored[key] = value
		return nil
	}
	r, sent, mu, wg := newWorkerProfileRouter(t, fn)

	cases := map[string]string{
		"autonomous.worker_profile":          "datawatch-tools",
		"autonomous.worker_profile_defaults": `{"role":{"coder":"full"}}`,
		"autonomous.worker_profiles":         `{"mine":{"name":"mine","skills":true}}`,
	}
	for key, value := range cases {
		reply := runConfigure(t, r, key+"="+value, sent, mu, wg)
		if strings.Contains(reply, "failed") {
			t.Errorf("valid write of %s refused: %q", key, reply)
		}
		if !strings.Contains(reply, "Set "+key) {
			t.Errorf("reply does not confirm %s: %q", key, reply)
		}
		if stored[key] != value {
			t.Errorf("stored[%s]=%q, want %q", key, stored[key], value)
		}
	}
}

// An invalid write is refused: the reply carries the daemon's message and the
// value is not stored. The stand-in configure function applies the same
// refusal the daemon gives (a non-object table, or a non-string name).
func TestWorkerProfileConfigure_InvalidWriteIsRefused(t *testing.T) {
	stored := map[string]string{}
	fn := func(key, value string) error {
		switch key {
		case "autonomous.worker_profiles", "autonomous.worker_profile_defaults":
			if !strings.HasPrefix(value, "{") {
				return errors.New(key + ": must be a JSON object")
			}
		}
		stored[key] = value
		return nil
	}
	r, sent, mu, wg := newWorkerProfileRouter(t, fn)

	for _, key := range []string{"autonomous.worker_profiles", "autonomous.worker_profile_defaults"} {
		reply := runConfigure(t, r, key+"=not-json", sent, mu, wg)
		if !strings.Contains(reply, "Configure failed") {
			t.Errorf("invalid %s not refused: %q", key, reply)
		}
		if !strings.Contains(reply, "must be a JSON object") {
			t.Errorf("refusal for %s lacks the daemon message: %q", key, reply)
		}
		if _, ok := stored[key]; ok {
			t.Errorf("refused write of %s was stored", key)
		}
	}
}
