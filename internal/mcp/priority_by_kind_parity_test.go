// priority_by_kind + defect_intake_automation over MCP: the tool schema
// lists both fields with the daemon defaults and the integer-only rule, the
// forwarding body carries the parsed table and the Automation name, and bad
// values are refused locally with a clear message before anything is
// written.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func kindTable(v any) map[string]int {
	m, _ := v.(map[string]any)
	out := make(map[string]int, len(m))
	for k, val := range m {
		if f, ok := val.(float64); ok {
			out[k] = int(f)
		}
	}
	return out
}

func TestAutonomousConfigSet_SchemaListsPriorityByKindAndDefectIntake(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var doc struct {
		InputSchema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	pkb, ok := doc.InputSchema.Properties["priority_by_kind"]
	if !ok {
		t.Fatal("schema has no priority_by_kind property")
	}
	for _, frag := range []string{"bug=80", "integer values only", "priority_source"} {
		if !strings.Contains(pkb.Description, frag) {
			t.Errorf("priority_by_kind description should name %q:\n%q", frag, pkb.Description)
		}
	}
	di, ok := doc.InputSchema.Properties["defect_intake_automation"]
	if !ok {
		t.Fatal("schema has no defect_intake_automation property")
	}
	if !strings.Contains(di.Description, "kind:bug") {
		t.Errorf("defect_intake_automation description should name the kind:bug filings:\n%q", di.Description)
	}
}

func TestAutonomousConfigGet_DescriptionNamesBothSurfaces(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigGet())
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	doc := string(b)
	for _, frag := range []string{"priority_by_kind", "defect_intake_automation", "bug=80", "priority_source"} {
		if !strings.Contains(doc, frag) {
			t.Errorf("autonomous_config_get description should name %q", frag)
		}
	}
}

func TestAutonomousConfigSet_ForwardsPriorityTableAndDefectIntake(t *testing.T) {
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/autonomous/config" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := context.Background()

	// JSON-object form: the parsed table and the Automation name go over the wire.
	body = nil
	if res, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{
		"priority_by_kind":         `{"bug":90,"epic":50}`,
		"defect_intake_automation": "my-auto",
	})); err != nil || res.IsError {
		t.Fatalf("set object form: %+v / %v", res, err)
	}
	if got := kindTable(body["priority_by_kind"]); got["bug"] != 90 || got["epic"] != 50 || len(got) != 2 {
		t.Fatalf("forwarded priority_by_kind = %v, want the parsed {bug:90 epic:50} table", body["priority_by_kind"])
	}
	if got := body["defect_intake_automation"]; got != "my-auto" {
		t.Fatalf("forwarded defect_intake_automation = %v", got)
	}

	// Pair form: same table.
	body = nil
	if res, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{
		"priority_by_kind": "bug=90,maintenance=50",
	})); err != nil || res.IsError {
		t.Fatalf("set pair form: %+v / %v", res, err)
	}
	if got := kindTable(body["priority_by_kind"]); got["bug"] != 90 || got["maintenance"] != 50 || len(got) != 2 {
		t.Fatalf("forwarded pair-form table = %v", body["priority_by_kind"])
	}

	// Omitted: neither field rides in the body (the stored values stay).
	body = nil
	if res, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"gate_disagreement": "block"})); err != nil || res.IsError {
		t.Fatalf("set other key: %+v / %v", res, err)
	}
	if _, ok := body["priority_by_kind"]; ok {
		t.Errorf("omitted priority_by_kind should not be forwarded, got %v", body["priority_by_kind"])
	}
	if _, ok := body["defect_intake_automation"]; ok {
		t.Errorf("omitted defect_intake_automation should not be forwarded, got %v", body["defect_intake_automation"])
	}
}

func TestAutonomousConfigSet_RefusesBadPriorityByKindLocally(t *testing.T) {
	var forwarded int
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		forwarded++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := context.Background()

	cases := []struct {
		name  string
		value any
		frag  string
	}{
		{"fraction", `{"bug":1.5}`, "integer"},
		{"word", `{"bug":"high"}`, "integer"},
		{"negative", `{"bug":-1}`, "integer"},
		{"non-string", map[string]int{"bug": 90}, "must be a string"},
	}
	for _, tc := range cases {
		res, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"priority_by_kind": tc.value}))
		if err != nil {
			t.Fatalf("%s: err = %v", tc.name, err)
		}
		if !res.IsError {
			t.Errorf("%s: expected a tool error, got %+v", tc.name, res)
			continue
		}
		if !strings.Contains(resContentText(t, res), tc.frag) {
			t.Errorf("%s: message %q should say %q", tc.name, resContentText(t, res), tc.frag)
		}
	}
	// defect_intake_automation: a present non-string is refused locally.
	res, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"defect_intake_automation": 42}))
	if err != nil {
		t.Fatalf("defect non-string: err = %v", err)
	}
	if !res.IsError {
		t.Errorf("defect non-string: expected a tool error, got %+v", res)
	}
	if forwarded != 0 {
		t.Errorf("rejected values must not be forwarded, but %d PUTs went over the wire", forwarded)
	}
}
