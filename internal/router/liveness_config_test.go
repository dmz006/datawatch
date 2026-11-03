package router

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// livenessConfigStub is an in-process stand-in for the daemon behind
// SetConfigureFunc: it validates with the real ValidateLivenessSettings and
// keeps the stored values so a test can read them back.
type livenessConfigStub struct {
	window    int
	overrides map[string]int
}

func (s *livenessConfigStub) configure(key, value string) error {
	nw, no := s.window, s.overrides
	switch key {
	case "session.liveness_window_seconds":
		if _, err := fmt.Sscanf(value, "%d", &nw); err != nil {
			return fmt.Errorf("session.liveness_window_seconds: %s is invalid; use whole seconds >= 0", value)
		}
	case "session.liveness_idle_timeout_overrides":
		no = map[string]int{}
		for _, part := range strings.Split(value, ",") {
			k, v, found := strings.Cut(part, "=")
			n := 0
			if _, err := fmt.Sscanf(v, "%d", &n); !found || err != nil {
				return fmt.Errorf("session.liveness_idle_timeout_overrides: %s is invalid; use backend=seconds", value)
			}
			no[strings.TrimSpace(k)] = n
		}
	default:
		return fmt.Errorf("unknown key")
	}
	if err := config.ValidateLivenessSettings(nw, no, []string{"claude-code", "pi-prompt", "goose"}); err != nil {
		return err
	}
	s.window, s.overrides = nw, no
	return nil
}

func TestLivenessRouterSetThenGet(t *testing.T) {
	stub := &livenessConfigStub{}
	r := &Router{hostname: "test"}
	r.SetConfigureFunc(stub.configure)

	out := strings.Join(r.HandleTestMessage("configure session.liveness_window_seconds=150"), "\n")
	if !strings.Contains(out, "Set session.liveness_window_seconds = 150") || stub.window != 150 {
		t.Fatalf("window not set: window=%d out=%s", stub.window, out)
	}
	out = strings.Join(r.HandleTestMessage("configure session.liveness_idle_timeout_overrides=pi=300,goose=0"), "\n")
	if !strings.Contains(out, "Set session.liveness_idle_timeout_overrides = pi=300,goose=0") {
		t.Fatalf("overrides not confirmed: %s", out)
	}
	if stub.overrides["pi"] != 300 || stub.overrides["goose"] != 0 || len(stub.overrides) != 2 {
		t.Errorf("stored overrides = %v", stub.overrides)
	}
}

func TestLivenessRouterObjectValueIsPassedWhole(t *testing.T) {
	var gotKey, gotValue string
	r := &Router{hostname: "test"}
	r.SetConfigureFunc(func(k, v string) error { gotKey, gotValue = k, v; return nil })
	r.HandleTestMessage(`configure session.liveness_idle_timeout_overrides={"pi": 300}`)
	if gotKey != "session.liveness_idle_timeout_overrides" || gotValue != `{"pi": 300}` {
		t.Errorf("configureFn got %q = %q", gotKey, gotValue)
	}
}

func TestLivenessRouterRefusesUnknownBackend(t *testing.T) {
	stub := &livenessConfigStub{}
	r := &Router{hostname: "test"}
	r.SetConfigureFunc(stub.configure)
	out := strings.Join(r.HandleTestMessage("configure session.liveness_idle_timeout_overrides=nosuchbackend=60"), "\n")
	if !strings.Contains(out, "Configure failed") || !strings.Contains(out, `unknown backend "nosuchbackend"`) {
		t.Errorf("refusal not shown:\n%s", out)
	}
	if len(stub.overrides) != 0 {
		t.Errorf("refused value was stored: %v", stub.overrides)
	}
}

func TestLivenessRouterHelpListsKeys(t *testing.T) {
	r := &Router{hostname: "test"}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, cmd := range []string{"configure help", "configure list"} {
		out := strings.Join(r.HandleTestMessage(cmd), "\n")
		for _, k := range []string{"session.liveness_window_seconds", "session.liveness_idle_timeout_overrides"} {
			if !strings.Contains(out, k) {
				t.Errorf("%q output lacks %s", cmd, k)
			}
		}
	}
}
