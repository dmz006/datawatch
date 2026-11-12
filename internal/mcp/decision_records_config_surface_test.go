// Config-parity test for the decision record keys (autonomous.decisions.*) over
// MCP. The keys are set through the generic config_set tool, which PUTs any
// dotted key to /api/config, exactly like the question channel keys. The
// daemon stand-in below refuses what the real daemon refuses (invalid JSON, a
// non-boolean switch, a mode outside its set, a negative minimum, a bad
// follow-up) with the same validators, and stores only what it accepted.
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type decisionRecordMCPDaemon struct {
	mu     sync.Mutex
	config map[string]any
}

func newDecisionRecordMCPServer(t *testing.T) (*decisionRecordMCPDaemon, *Server) {
	t.Helper()
	d := &decisionRecordMCPDaemon{config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for k, v := range body {
			var err error
			switch k {
			case "autonomous.decisions.enabled":
				if _, isBool := v.(bool); !isBool {
					http.Error(w, k+": must be a boolean (true or false)", http.StatusBadRequest)
					return
				}
			case "autonomous.decisions.clarity_mode":
				s, isStr := v.(string)
				if !isStr {
					http.Error(w, k+": must be a string", http.StatusBadRequest)
					return
				}
				err = config.ValidateDecisionClarityMode(s)
			case "autonomous.decisions.doc_status_mode":
				s, isStr := v.(string)
				if !isStr {
					http.Error(w, k+": must be a string", http.StatusBadRequest)
					return
				}
				err = config.ValidateDecisionDocStatusMode(s)
			case "autonomous.decisions.clarity_min_text_chars":
				f, isNum := v.(float64)
				if !isNum {
					http.Error(w, k+": must be a whole number >= 0", http.StatusBadRequest)
					return
				}
				err = config.ValidateDecisionClarityMinTextChars(int(f))
			case "autonomous.decisions.follow_ups":
				var list []config.AutonomousDecisionFollowUp
				b, _ := json.Marshal(v)
				if jerr := json.Unmarshal(b, &list); jerr != nil {
					http.Error(w, k+": expected a list of {name, kind, target}", http.StatusBadRequest)
					return
				}
				err = config.ValidateDecisionFollowUps(list)
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		for k, v := range body {
			d.config[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return d, &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
}

func decisionRecordConfigSet(t *testing.T, s *Server, key, value string) string {
	t.Helper()
	res, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": key, "value": value}))
	if err != nil {
		t.Fatalf("config_set %s = %s: %v", key, value, err)
	}
	return res.Content[0].(mcpsdk.TextContent).Text
}

// TestDecisionRecordConfigSurface_MCPConfigSet: config_set carries a switch as
// a real boolean, a count as a number, a mode as text and the follow-ups as a
// real list; a later value overwrites an earlier one.
func TestDecisionRecordConfigSurface_MCPConfigSet(t *testing.T) {
	d, s := newDecisionRecordMCPServer(t)
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.decisions.enabled", "false", false},
		{"autonomous.decisions.enabled", "true", true},
		{"autonomous.decisions.clarity_mode", "refuse", "refuse"},
		{"autonomous.decisions.clarity_mode", "warn", "warn"},
		{"autonomous.decisions.doc_status_mode", "on", "on"},
		{"autonomous.decisions.clarity_min_text_chars", "0", float64(0)},
		{"autonomous.decisions.folder", "inbox/decisions", "inbox/decisions"},
		{"autonomous.decisions.front_matter_delimiter", "+++", "+++"},
		{"autonomous.decisions.follow_ups", `[{"name":"build","kind":"start_template","target":"t1"}]`,
			[]any{map[string]any{"name": "build", "kind": "start_template", "target": "t1"}}},
	}
	for _, c := range cases {
		if text := decisionRecordConfigSet(t, s, c.key, c.value); strings.Contains(text, "error") || strings.Contains(text, "denied") {
			t.Fatalf("config_set %s = %s refused: %s", c.key, c.value, text)
		}
		d.mu.Lock()
		got, ok := d.config[c.key]
		d.mu.Unlock()
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("stored %s = %#v (present=%v), want %#v", c.key, got, ok, c.want)
		}
	}
}

// TestDecisionRecordConfigSurface_MCPConfigSetRefusesBad: a value outside its
// valid set comes back as an error carrying the daemon's text, and nothing is
// stored.
func TestDecisionRecordConfigSurface_MCPConfigSetRefusesBad(t *testing.T) {
	d, s := newDecisionRecordMCPServer(t)
	cases := []struct{ key, value, named string }{
		{"autonomous.decisions.clarity_mode", "bogus", "clarity_mode"},
		{"autonomous.decisions.clarity_mode", "ON", "clarity_mode"},
		{"autonomous.decisions.doc_status_mode", "apply", "doc_status_mode"},
		{"autonomous.decisions.clarity_min_text_chars", "-4", "clarity_min_text_chars"},
		{"autonomous.decisions.enabled", "maybe", "enabled"},
		// config_set retries a refused value as quoted text, and a quoted list is
		// not valid JSON, so the list's refusal reads as a plain error; the point
		// is that it is refused and nothing is stored.
		{"autonomous.decisions.follow_ups", `[{"name":"a","kind":"run_shell","target":"t"}]`, ""},
		{"autonomous.decisions.follow_ups", `[{"name":"a","kind":"open_automaton","target":"x"},{"name":"a","kind":"open_automaton","target":"y"}]`, ""},
	}
	for _, c := range cases {
		text := decisionRecordConfigSet(t, s, c.key, c.value)
		if !strings.Contains(text, "error") || !strings.Contains(text, c.named) {
			t.Fatalf("config_set %s = %s: want a refusal naming %q, got %q", c.key, c.value, c.named, text)
		}
		d.mu.Lock()
		_, stored := d.config[c.key]
		d.mu.Unlock()
		if stored {
			t.Fatalf("a refused value %s = %s was stored", c.key, c.value)
		}
	}
}
