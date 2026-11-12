// priority_by_kind + defect_intake_automation over the comm channel:
// `autonomous config-set` carries both settings to the daemon with the same
// rules as the other surfaces (integer-only table values with a clear
// refusal, any Automation name, empty value clears the stored table), and
// the parser hands them to the dispatcher intact.

package router

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPriorityByKindComm_ConfigSetForwardsParsedTable(t *testing.T) {
	d := &scopeLintDaemon{}
	port, closeFn := d.serve(t)
	defer closeFn()

	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	r.HandleTestMessage("autonomous config-set priority_by_kind=bug=90,feature=25")

	if len(d.puts) != 1 {
		t.Fatalf("want one PUT, got %d", len(d.puts))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(d.puts[0]), &body); err != nil {
		t.Fatalf("PUT body is not JSON: %v", err)
	}
	table, ok := body["priority_by_kind"].(map[string]any)
	if !ok {
		t.Fatalf("PUT body has no priority_by_kind object: %s", d.puts[0])
	}
	if table["bug"] != float64(90) || table["feature"] != float64(25) || len(table) != 2 {
		t.Fatalf("priority_by_kind table = %v, want {bug:90 feature:25}", table)
	}
}

func TestPriorityByKindComm_BothSpellingAndDefectIntake(t *testing.T) {
	d := &scopeLintDaemon{}
	port, closeFn := d.serve(t)
	defer closeFn()

	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	r.HandleTestMessage("autonomous config-set autonomous.priority_by_kind=bug=90 defect_intake_automation=my-auto")

	if len(d.puts) != 1 {
		t.Fatalf("want one PUT, got %d", len(d.puts))
	}
	if !strings.Contains(d.puts[0], `"priority_by_kind":{`) || strings.Contains(d.puts[0], "autonomous.priority_by_kind") {
		t.Fatalf("autonomous.priority_by_kind not normalised: %s", d.puts[0])
	}
	if !strings.Contains(d.puts[0], `"defect_intake_automation":"my-auto"`) {
		t.Fatalf("defect_intake_automation missing: %s", d.puts[0])
	}
}

func TestPriorityByKindComm_ClearAndReject(t *testing.T) {
	d := &scopeLintDaemon{}
	port, closeFn := d.serve(t)
	defer closeFn()

	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)

	// An empty value is a valid "clear the table" edit.
	replies := r.HandleTestMessage("autonomous config-set priority_by_kind=")
	if len(d.puts) != 1 || !strings.Contains(d.puts[0], `"priority_by_kind":{}`) {
		t.Fatalf("clear: PUTs=%q replies=%q, want one PUT of {\"priority_by_kind\":{}}", d.puts, replies)
	}

	// A non-integer value is refused with a clear message, over the wire and
	// on the screen, and nothing is sent.
	d.puts = nil
	replies = r.HandleTestMessage("autonomous config-set priority_by_kind=bug=high")
	if len(d.puts) != 0 {
		t.Fatalf("rejected value was still sent: %v", d.puts)
	}
	joined := strings.Join(replies, " ")
	if !strings.Contains(joined, "not a number") && !strings.Contains(joined, "integer") {
		t.Fatalf("rejection reply %q should say the value must be an integer", joined)
	}
}

func TestPriorityByKindComm_Parse(t *testing.T) {
	got := Parse("autonomous config-set priority_by_kind=bug=90 defect_intake_automation=my-auto")
	if got.Text != "config-set priority_by_kind=bug=90 defect_intake_automation=my-auto" {
		t.Fatalf("Text = %q", got.Text)
	}
}
