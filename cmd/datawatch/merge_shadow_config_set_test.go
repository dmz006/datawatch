// merge_shadow_config_set_test.go — datawatch config set accepts the three
// autonomous.merge_shadow.* keys, validates them with the daemon API's own
// rules before anything is sent, sends each with its natural JSON type,
// prints the help link, and config get reads each one back.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

const mergeShadowHelpLinkForTest = "https://github.com/dmz006/datawatch/blob/main/docs/datawatch-definitions.md#shadow-merge-plan"

// mergeShadowConfigDaemon is a fake daemon: PUT /api/config refuses anything
// that is not a valid JSON object (like the real one) and keeps the accepted
// keys; GET /api/config serves them back under autonomous.merge_shadow,
// matching the shape runConfigGet walks.
type mergeShadowConfigDaemon struct {
	config map[string]any
	puts   int
}

func startMergeShadowConfigDaemon(t *testing.T) *mergeShadowConfigDaemon {
	t.Helper()
	d := &mergeShadowConfigDaemon{config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if json.Unmarshal(raw, &body) != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			d.puts++
			for k, v := range body {
				d.config[k] = v
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case http.MethodGet:
			shadow := map[string]any{}
			for k, v := range d.config {
				if f, ok := strings.CutPrefix(k, "autonomous.merge_shadow."); ok {
					shadow[f] = v
				}
			}
			b, _ := json.Marshal(map[string]any{"autonomous": map[string]any{"merge_shadow": shadow}})
			_, _ = w.Write(b)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

func mergeShadowSet(t *testing.T, d *mergeShadowConfigDaemon, key, value string) (string, error) {
	t.Helper()
	return captureEscalationStdout(t, func() error {
		return runConfigSet(&config.Config{}, key, value)
	})
}

func mergeShadowGet(t *testing.T, key string) string {
	t.Helper()
	out, err := captureEscalationStdout(t, func() error {
		return runConfigGet(&config.Config{}, key)
	})
	if err != nil {
		t.Fatalf("config get %s: %v", key, err)
	}
	return out
}

// TestMergeShadowConfigSet_EachKeySetAndReadBack: each of the three keys is
// accepted, stored by the daemon with its natural JSON type, announced with
// its value, followed by the help link, and read back unchanged.
func TestMergeShadowConfigSet_EachKeySetAndReadBack(t *testing.T) {
	cases := []struct {
		key, value       string
		stored           any
		readBackContains string
	}{
		{"autonomous.merge_shadow.enabled", "true", true, "true"},
		{"autonomous.merge_shadow.enabled", "false", false, "false"},
		{"autonomous.merge_shadow.gate_timeout", "20m", "20m", `"20m"`},
		{"autonomous.merge_shadow.gate_timeout", "1h30m", "1h30m", `"1h30m"`},
		{"autonomous.merge_shadow.plan_keep", "100", float64(100), "100"},
		{"autonomous.merge_shadow.plan_keep", "1", float64(1), "1"},
	}
	for _, c := range cases {
		d := startMergeShadowConfigDaemon(t)
		out, err := mergeShadowSet(t, d, c.key, c.value)
		if err != nil {
			t.Errorf("%s = %s: %v", c.key, c.value, err)
			continue
		}
		if got := d.config[c.key]; !reflect.DeepEqual(got, c.stored) {
			t.Errorf("%s: daemon stored %#v, want %#v", c.key, got, c.stored)
		}
		if !strings.Contains(out, "Set "+c.key+" = "+c.value+"\n") {
			t.Errorf("%s: output lacks the Set line:\n%s", c.key, out)
		}
		if !strings.Contains(out, "Help: "+mergeShadowHelpLinkForTest+"\n") {
			t.Errorf("%s: output lacks the help link:\n%s", c.key, out)
		}
		if got := mergeShadowGet(t, c.key); !strings.Contains(got, c.readBackContains) {
			t.Errorf("%s: read back %q, want %q in it", c.key, got, c.readBackContains)
		}
	}
}

// TestMergeShadowConfigSet_RefusedByTheAPIsRules: a value the daemon API
// refuses is refused by the CLI with the API's own message, and nothing is
// sent — the fake daemon sees zero PUTs.
func TestMergeShadowConfigSet_RefusedByTheAPIsRules(t *testing.T) {
	cases := []struct {
		key, value  string
		wantInError string
	}{
		{"autonomous.merge_shadow.enabled", "banana", "enabled: must be true or false"},
		{"autonomous.merge_shadow.enabled", "yes", "enabled: must be true or false"},
		{"autonomous.merge_shadow.gate_timeout", "-5m", "gate_timeout: must parse as a positive duration"},
		{"autonomous.merge_shadow.gate_timeout", "0", "gate_timeout: must parse as a positive duration"},
		{"autonomous.merge_shadow.gate_timeout", "garbage", "gate_timeout: must parse as a positive duration"},
		{"autonomous.merge_shadow.plan_keep", "0", "plan_keep: must be a positive whole number"},
		{"autonomous.merge_shadow.plan_keep", "-3", "plan_keep: must be a positive whole number"},
		{"autonomous.merge_shadow.plan_keep", "ten", "plan_keep: must be a positive whole number"},
		{"autonomous.merge_shadow.plan_keep", "2.5", "plan_keep: must be a positive whole number"},
	}
	for _, c := range cases {
		d := startMergeShadowConfigDaemon(t)
		_, err := mergeShadowSet(t, d, c.key, c.value)
		if err == nil {
			t.Errorf("%s = %s: expected a refusal, got none", c.key, c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.wantInError) {
			t.Errorf("%s = %s: error %q lacks %q", c.key, c.value, err, c.wantInError)
		}
		if d.puts != 0 {
			t.Errorf("%s = %s: a refused value may not be sent, saw %d PUTs", c.key, c.value, d.puts)
		}
		if len(d.config) != 0 {
			t.Errorf("%s = %s: the daemon stored %#v", c.key, c.value, d.config)
		}
	}
}

// TestMergeShadowConfigSet_UnknownFieldIsRefusedLocally: a key under the
// prefix that is not one of the three fields is refused with the API's
// message and nothing is sent.
func TestMergeShadowConfigSet_UnknownFieldIsRefusedLocally(t *testing.T) {
	d := startMergeShadowConfigDaemon(t)
	_, err := mergeShadowSet(t, d, "autonomous.merge_shadow.bogus", "1")
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("expected the unknown-key refusal, got %v", err)
	}
	if d.puts != 0 {
		t.Errorf("nothing may be sent, saw %d PUTs", d.puts)
	}
}

// TestMergeShadowConfigSet_OtherKeysUnaffected: a key outside the prefix
// still goes through the generic raw-then-quoted path and prints no
// merge-shadow help link.
func TestMergeShadowConfigSet_OtherKeysUnaffected(t *testing.T) {
	d := startMergeShadowConfigDaemon(t)
	out, err := mergeShadowSet(t, d, "autonomous.merge_pipeline.file_name", ".datawatch/merge-pipeline.yaml")
	if err != nil {
		t.Fatalf("config set: %v", err)
	}
	if d.puts != 1 {
		t.Fatalf("expected exactly one PUT, saw %d", d.puts)
	}
	if !strings.Contains(out, "Set autonomous.merge_pipeline.file_name") {
		t.Errorf("output lacks the Set line:\n%s", out)
	}
	if strings.Contains(out, "Help: "+mergeShadowHelpLinkForTest) {
		t.Errorf("no merge-shadow help link for an unrelated key:\n%s", out)
	}
}

// TestMergeShadowConfigSet_DaemonRefusalPassesThrough: a daemon that still
// rejects an accepted value (the fake never does, a real one can) reports
// the daemon's text.
func TestMergeShadowConfigSet_DaemonRefusalPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/config" {
			http.Error(w, "autonomous.merge_shadow.plan_keep: refused by live config", http.StatusBadRequest)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	_, err := captureEscalationStdout(t, func() error {
		return runConfigSet(&config.Config{}, "autonomous.merge_shadow.plan_keep", "10")
	})
	if err == nil || !strings.Contains(err.Error(), "refused by live config") {
		t.Errorf("expected the daemon refusal to pass through, got %v", err)
	}
}

// TestIsMergeShadowConfigKey — sanity check against the shared helper so the
// CLI and the API agree on the closed key set.
func TestIsMergeShadowConfigKey(t *testing.T) {
	cases := []struct {
		key    string
		field  string
		wantOK bool
	}{
		{"autonomous.merge_shadow.enabled", "enabled", true},
		{"autonomous.merge_shadow.gate_timeout", "gate_timeout", true},
		{"autonomous.merge_shadow.plan_keep", "plan_keep", true},
		{"autonomous.merge_shadow.other", "", false},
		{"autonomous.merge_pipeline.file_name", "", false},
	}
	for _, c := range cases {
		field, ok := server.IsMergeShadowConfigKey(c.key)
		if ok != c.wantOK || (ok && field != c.field) {
			t.Errorf("IsMergeShadowConfigKey(%q) = (%q, %v), want (%q, %v)", c.key, field, ok, c.field, c.wantOK)
		}
	}
}
