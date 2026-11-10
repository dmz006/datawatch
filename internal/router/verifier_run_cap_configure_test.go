// comm `configure autonomous.verifier_max_run_seconds=<n>`: usage text, the
// refusal path for negative or non-whole values, and the accepted forward.

package router

import (
	"strings"
	"sync"
	"testing"
)

// verifierRunCapConfigure runs one configure command through handleConfigure
// and returns the reply text, the key/value handed to the configure function
// (empty when it was never called) and whether it was called at all.
func verifierRunCapConfigure(t *testing.T, text string) (reply, gotKey, gotValue string, called bool) {
	t.Helper()
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
	r.SetConfigureFunc(func(key, value string) error {
		called = true
		gotKey, gotValue = key, value
		return nil
	})
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("sent=%q, want one reply", sent)
	}
	return sent[0], gotKey, gotValue, called
}

func TestVerifierRunCapCommUsageNamesKeyAndDefault(t *testing.T) {
	reply, _, _, called := verifierRunCapConfigure(t, "help")
	if called {
		t.Fatal("help must not call the configure function")
	}
	for _, want := range []string{verifierRunCapKey, "0 = no cap", "default 2700", "negative refused"} {
		if !strings.Contains(reply, want) {
			t.Errorf("usage reply lacks %q: %q", want, reply)
		}
	}
}

func TestVerifierRunCapCommAcceptsWholeSeconds(t *testing.T) {
	for _, value := range []string{"0", "2700", "120"} {
		reply, gotKey, gotValue, called := verifierRunCapConfigure(t, verifierRunCapKey+"="+value)
		if !called {
			t.Fatalf("value %s: configure function not called; reply %q", value, reply)
		}
		if gotKey != verifierRunCapKey || gotValue != value {
			t.Errorf("value %s: configure got key=%q value=%q", value, gotKey, gotValue)
		}
		if !strings.Contains(reply, "Set "+verifierRunCapKey+" = "+value) {
			t.Errorf("value %s: reply %q does not confirm the set", value, reply)
		}
	}
}

func TestVerifierRunCapCommRefusesBadValuesWithoutStoring(t *testing.T) {
	for _, value := range []string{"-1", "-900", "2.5", "soon", ""} {
		reply, _, _, called := verifierRunCapConfigure(t, verifierRunCapKey+"="+value)
		if called {
			t.Errorf("value %q reached the configure function; bad input must be refused in the handler", value)
		}
		if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, verifierRunCapKey) {
			t.Errorf("value %q: reply %q must say the set failed and name the key", value, reply)
		}
	}
}

func TestVerifierRunCapCommRefusalTextIsOperatorFacing(t *testing.T) {
	reply, _, _, _ := verifierRunCapConfigure(t, verifierRunCapKey+"=-1")
	if !strings.Contains(reply, "Automaton") {
		t.Errorf("reply must say Automaton: %q", reply)
	}
	if strings.Contains(reply, "PRD") || strings.Contains(reply, "BL") {
		t.Errorf("reply must carry no PRD or tracker identifier: %q", reply)
	}
}

func TestVerifierRunCapCommCheckValue(t *testing.T) {
	for _, ok := range []string{"0", "1", "2700"} {
		if err := checkVerifierRunCapValue(ok); err != nil {
			t.Errorf("checkVerifierRunCapValue(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"-1", "1.5", "x"} {
		err := checkVerifierRunCapValue(bad)
		if err == nil {
			t.Errorf("checkVerifierRunCapValue(%q) = nil, want error", bad)
			continue
		}
		if !strings.Contains(err.Error(), verifierRunCapKey) {
			t.Errorf("checkVerifierRunCapValue(%q) error %q lacks the key", bad, err)
		}
	}
}
