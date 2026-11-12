// Audit tests for the model placement decision events (AGENT.md Audit
// Logging Rule): each event must round-trip the JSON-lines format with its
// four fields (node, model, policy, reason) intact, and each must render as
// a CEF line with its own (signatureID, name, severity) triple and correct
// header pipe-escaping and extension '='/newline escaping.

package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type placementEventCase struct {
	name     string
	policy   string
	sigID    int
	cefName  string
	severity int
}

var placementEventCases = []placementEventCase{
	{name: "placement_asked", policy: "ask", sigID: 88, cefName: "PlacementAsked", severity: 3},
	{name: "placement_approved", policy: "ask", sigID: 89, cefName: "PlacementApproved", severity: 2},
	{name: "placement_denied", policy: "ask", sigID: 90, cefName: "PlacementDenied", severity: 4},
	{name: "placement_loaded", policy: "auto_load", sigID: 91, cefName: "PlacementLoaded", severity: 2},
	{name: "placement_refused", policy: "refuse", sigID: 92, cefName: "PlacementRefused", severity: 5},
}

func placementAction(name string) string {
	switch name {
	case "placement_asked":
		return ActionPlacementAsked
	case "placement_approved":
		return ActionPlacementApproved
	case "placement_denied":
		return ActionPlacementDenied
	case "placement_loaded":
		return ActionPlacementLoaded
	case "placement_refused":
		return ActionPlacementRefused
	}
	return ""
}

func TestPlacementEvents_ActionNames(t *testing.T) {
	want := map[string]string{
		"placement_asked":    "placement_asked",
		"placement_approved": "placement_approved",
		"placement_denied":   "placement_denied",
		"placement_loaded":   "placement_loaded",
		"placement_refused":  "placement_refused",
	}
	for name, wantVal := range want {
		if got := placementAction(name); got != wantVal {
			t.Errorf("action constant for %s = %q, want %q", name, got, wantVal)
		}
	}
}

func TestPlacementEvents_JSONLinesRoundTrip(t *testing.T) {
	for _, c := range placementEventCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "placement",
				Action:    placementAction(c.name),
				Details: map[string]any{
					"node":   "node|1=alpha\nx",
					"model":  "gpt\\x=200b",
					"policy": c.policy,
					"reason": "offered: no\nmemory = free|48",
				},
			}
			if err := l.Write(in); err != nil {
				t.Fatal(err)
			}
			_ = l.Close()

			f, err := os.Open(filepath.Join(dir, "audit.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			sc := bufio.NewScanner(f)
			var lines []string
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			if len(lines) != 1 {
				t.Fatalf("want 1 JSON line, got %d: %q", len(lines), lines)
			}
			var out Entry
			if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
				t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
			}
			if out.Action != in.Action || out.Actor != in.Actor || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Details["node"] != "node|1=alpha\nx" {
				t.Errorf("node field lost: %v", out.Details["node"])
			}
			if out.Details["model"] != "gpt\\x=200b" {
				t.Errorf("model field lost: %v", out.Details["model"])
			}
			if out.Details["policy"] != c.policy {
				t.Errorf("policy = %v, want %q", out.Details["policy"], c.policy)
			}
			if out.Details["reason"] != "offered: no\nmemory = free|48" {
				t.Errorf("reason field lost: %v", out.Details["reason"])
			}
		})
	}
}

func TestPlacementEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, c := range placementEventCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			line := FormatCEFLine(Entry{
				Timestamp: time.Now(),
				Actor:     "place|ment=x",
				Action:    placementAction(c.name),
				Details: map[string]any{
					"node":   "node|1",
					"model":  "a=b\nCEF:0|evil",
					"policy": c.policy,
					"reason": "load\nfailed",
				},
			})
			// Header: version escaped, then (signatureID, cefName, severity).
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` +
				strconv.Itoa(c.sigID) + `|` + c.cefName + `|` + strconv.Itoa(c.severity) + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+placementAction(c.name)) {
				t.Errorf("action extension missing: %q", line)
			}
			// Details sort as model, node, policy, reason: first three take
			// cs3..cs5, reason overflows into msg.
			if !strings.Contains(line, `deviceCustomString3Label=model deviceCustomString3=a\=b\nCEF:0|evil`) {
				t.Errorf("model extension missing or not escaped: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString4Label=node deviceCustomString4=node|1`) {
				t.Errorf("node extension missing: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString5Label=policy deviceCustomString5=`+c.policy) {
				t.Errorf("policy extension missing: %q", line)
			}
			if !strings.Contains(line, `msg=reason\=load\nfailed`) {
				t.Errorf("reason overflow into msg missing or not escaped: %q", line)
			}
		})
	}
}
