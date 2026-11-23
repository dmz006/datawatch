package trace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// packKeys are the JSON keys added for pack attribution.
var packKeys = []string{
	"pack_name", "pack_version", "unit", "step", "project", "automation",
	"runner", "tier", "entry", "escalation", "cost", "resolved_inputs",
}

// TestLegacyEventDecodesUnchanged checks that an event written before the
// pack attribution fields existed decodes with those fields zeroed and
// re-encodes to exactly the same JSON.
func TestLegacyEventDecodesUnchanged(t *testing.T) {
	// Key order follows the Event struct, as the pre-pack encoder wrote it.
	legacy := `{"session_id":"s1","task_id":"t1","story_id":"st1","automaton_id":"a1",` +
		`"agent":"worker","backend":"claude-code","kind":"tool_call","name":"Bash",` +
		`"arg_summary":"ls","outcome":"ok","duration_ms":12,"tokens_in":3,"tokens_out":4,` +
		`"timestamp":"2026-11-04T12:00:00Z"}`

	var ev Event
	if err := json.Unmarshal([]byte(legacy), &ev); err != nil {
		t.Fatalf("decode legacy event: %v", err)
	}
	if ev.PackName != "" || ev.PackVersion != "" || ev.Unit != "" || ev.Step != "" ||
		ev.Project != "" || ev.Automation != "" || ev.Runner != "" || ev.Tier != "" ||
		ev.Entry != "" || ev.Escalation || ev.Cost != 0 || ev.ResolvedInputs != nil {
		t.Errorf("pack fields should be zero for a legacy event, got %+v", ev)
	}
	if ev.SessionID != "s1" || ev.Name != "Bash" || ev.TokensOut != 4 {
		t.Errorf("legacy fields not decoded: %+v", ev)
	}

	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("re-encode legacy event: %v", err)
	}
	if string(out) != legacy {
		t.Errorf("legacy event changed on re-encode:\n got  %s\n want %s", out, legacy)
	}
}

// TestEmptyPackFieldsOmitted checks that an event with no pack attribution
// emits none of the new keys.
func TestEmptyPackFieldsOmitted(t *testing.T) {
	ev := Event{Kind: KindStateChange, Timestamp: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range packKeys {
		if strings.Contains(s, `"`+k+`"`) {
			t.Errorf("empty field %q should be omitted, JSON: %s", k, s)
		}
	}

	// An empty but non-nil map is also empty and must be omitted.
	ev.ResolvedInputs = map[string]string{}
	b, err = json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"resolved_inputs"`) {
		t.Errorf("empty resolved_inputs map should be omitted, JSON: %s", b)
	}
}

// TestPackFieldsRoundTrip checks that populated pack fields are encoded
// under their documented keys and survive a decode.
func TestPackFieldsRoundTrip(t *testing.T) {
	in := Event{
		Kind: KindModelRequest, Timestamp: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC),
		PackName: "standard-tasks", PackVersion: "1.2.0", Unit: "build", Step: "compile",
		Project: "datawatch", Automation: "auto-1", Runner: "session", Tier: "large",
		Entry: "main", Escalation: true, Cost: 0.0425,
		ResolvedInputs: map[string]string{"branch": "main", "target": "linux"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range packKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("populated field key %q missing from JSON: %s", k, b)
		}
	}
	if raw["runner"] != "session" || raw["escalation"] != true {
		t.Errorf("unexpected encoding of runner/escalation: %s", b)
	}

	var out Event
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("pack round trip mismatch:\n got  %+v\n want %+v", out, in)
	}
}
