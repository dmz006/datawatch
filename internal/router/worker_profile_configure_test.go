package router

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
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

// A bare worker-profile key read adds the profile in force: one line per role
// and task kind, built from the live values the read function returns.
func TestWorkerProfileConfigure_BareKeyReadAddsInForce(t *testing.T) {
	r, sent, mu, wg := newWorkerProfileRouter(t, func(string, string) error {
		t.Fatal("a bare key must not write")
		return nil
	})
	r.SetWorkerProfileInForceFunc(func() ([]workerprofile.InForceEntry, error) {
		return []workerprofile.InForceEntry{
			{Scope: workerprofile.ScopeRole, Key: "worker", Profile: "full", Tier: "role"},
			{Scope: workerprofile.ScopeKind, Key: "note", Profile: "", Tier: workerprofile.TierNone},
		}, nil
	})
	for _, key := range workerProfileKeys {
		reply := runConfigure(t, r, key, sent, mu, wg)
		if !strings.Contains(reply, workerProfileHowto) {
			t.Errorf("reply for %s lacks the howto path: %q", key, reply)
		}
		for _, want := range []string{
			"profile in force:",
			"role worker profile=full tier=role",
			"kind note profile=none tier=none",
		} {
			if !strings.Contains(reply, want) {
				t.Errorf("reply for %s lacks %q: %q", key, want, reply)
			}
		}
	}
}

// The in-force lines come after the help text, one per line, in the order the
// read function returns them.
func TestWorkerProfileConfigure_InForceLinesFollowHelp(t *testing.T) {
	r, sent, mu, wg := newWorkerProfileRouter(t, func(string, string) error { return nil })
	r.SetWorkerProfileInForceFunc(func() ([]workerprofile.InForceEntry, error) {
		return []workerprofile.InForceEntry{
			{Scope: workerprofile.ScopeRole, Key: "worker", Profile: "full", Tier: "role"},
			{Scope: workerprofile.ScopeRole, Key: "judge", Profile: "minimal", Tier: "built-in"},
		}, nil
	})
	reply := runConfigure(t, r, "autonomous.worker_profile", sent, mu, wg)
	helpAt := strings.Index(reply, workerProfileHowto)
	workerAt := strings.Index(reply, "role worker profile=full")
	judgeAt := strings.Index(reply, "role judge profile=minimal tier=built-in")
	if helpAt < 0 || workerAt < helpAt || judgeAt < workerAt {
		t.Errorf("reply order wrong (help=%d worker=%d judge=%d): %q", helpAt, workerAt, judgeAt, reply)
	}
	if !strings.Contains(reply, "\n    role worker profile=full tier=role") {
		t.Errorf("in-force line is not indented under the header: %q", reply)
	}
}

// A failed read names the failure and still sends the help text.
func TestWorkerProfileConfigure_InForceReadErrorIsNamed(t *testing.T) {
	r, sent, mu, wg := newWorkerProfileRouter(t, func(string, string) error { return nil })
	r.SetWorkerProfileInForceFunc(func() ([]workerprofile.InForceEntry, error) {
		return nil, errors.New("daemon not reachable")
	})
	reply := runConfigure(t, r, "autonomous.worker_profile", sent, mu, wg)
	if !strings.Contains(reply, workerProfileHowto) {
		t.Errorf("help text missing on read error: %q", reply)
	}
	if !strings.Contains(reply, "profile in force: unavailable (daemon not reachable)") {
		t.Errorf("read error not named: %q", reply)
	}
}

// With no read function wired, the bare key still returns its help and says the
// profile in force is not available.
func TestWorkerProfileConfigure_NoInForceFuncSaysUnavailable(t *testing.T) {
	r, sent, mu, wg := newWorkerProfileRouter(t, func(string, string) error { return nil })
	reply := runConfigure(t, r, "autonomous.worker_profile", sent, mu, wg)
	if !strings.Contains(reply, "profile in force: not available") {
		t.Errorf("unwired read not reported: %q", reply)
	}
}
