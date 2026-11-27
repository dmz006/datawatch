package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func acceptanceEntry() Entry {
	return Entry{Timestamp: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC), Actor: "session:w1", Action: ActionAcceptanceChanged, Subject: "r|1",
		Details: map[string]any{"change": "revise", "item": "a=b\nc\\d|e", "resource_type": "acceptance", "resource_id": "r|1"}}
}

func TestAcceptanceAudit_JSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(acceptanceEntry())
	if err != nil || strings.Contains(string(raw), "\n") {
		t.Fatalf("%v %s", err, raw)
	}
	var back Entry
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Action != ActionAcceptanceChanged || back.Details["item"] != "a=b\nc\\d|e" {
		t.Fatalf("%+v", back)
	}
}

func TestAcceptanceAudit_CEF(t *testing.T) {
	line := FormatCEFLine(acceptanceEntry())
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("not one line: %q", line)
	}
	if !strings.Contains(line, "|103|AcceptanceListChanged|3|") {
		t.Fatalf("signature: %s", line)
	}
	for _, want := range []string{`a\=b`, `\nc`, `\\d`} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing escape %q in %s", want, line)
		}
	}
}
