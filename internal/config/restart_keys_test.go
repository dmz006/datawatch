package config

import (
	"reflect"
	"testing"
)

func TestKeyRequiresRestart(t *testing.T) {
	for key, want := range map[string]bool{
		"server.port":                    true,
		"server.host":                    true,
		"mcp.sse_port":                   true,
		"acme.dns01.provider":            true,
		"database.path":                  true,
		"agents.image_prefix":            true,
		"pin_check_seconds":              true,
		"autonomous.tmux_kill_guard":     false,
		"autonomous.capacity_enabled":    false,
		"session.schedule_settle_ms":     false,
		"server.suppress_active_toasts":  false,
		"server.portal":                  false, // a prefix match must stop at a dot
		"autonomous.automation.max_runs": false,
	} {
		if got := KeyRequiresRestart(key); got != want {
			t.Errorf("KeyRequiresRestart(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestSplitRestartKeys(t *testing.T) {
	now, later := SplitRestartKeys([]string{"server.port", "autonomous.tmux_kill_guard", "agents.x", "a.b"})
	if want := []string{"a.b", "autonomous.tmux_kill_guard"}; !reflect.DeepEqual(now, want) {
		t.Errorf("immediate = %v, want %v", now, want)
	}
	if want := []string{"agents.x", "server.port"}; !reflect.DeepEqual(later, want) {
		t.Errorf("restart = %v, want %v", later, want)
	}
}

func TestPutConfigApplyNote(t *testing.T) {
	cases := map[string]string{
		`{"status":"ok","applied":["a.b"]}`:                                          "applied now",
		`{"status":"ok","requires_restart":["server.port"]}`:                         "saved; restart the daemon to apply: server.port",
		`{"status":"ok","applied":["a.b"],"requires_restart":["server.port","x.y"]}`: "applied now: a.b; restart the daemon to apply: server.port, x.y",
		`{"status":"ok"}`: "",
		`not json`:        "",
	}
	for body, want := range cases {
		if got := PutConfigApplyNote([]byte(body)); got != want {
			t.Errorf("PutConfigApplyNote(%s) = %q, want %q", body, got, want)
		}
	}
}
