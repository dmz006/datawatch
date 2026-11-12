package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// gate_disagreement — the autonomous_config_set tool schema must list the
// param (block|warn) and forward it to the REST write; the
// autonomous_config_get description must name it.

func TestAutonomousConfigSet_SchemaListsWithString(t *testing.T) {
	s := &Server{}
	var tool struct {
		InputSchema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"inputSchema"`
		Description string `json:"description"`
	}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	p, ok := tool.InputSchema.Properties["gate_disagreement"]
	if !ok {
		t.Fatalf("autonomous_config_set schema lacks gate_disagreement property: %s", b)
	}
	if p.Type != "string" {
		t.Fatalf("gate_disagreement schema type = %q, want string", p.Type)
	}
	for _, word := range []string{"\"warn\"", "\"block\"", "quality gate"} {
		if !strings.Contains(p.Description, word) {
			t.Errorf("gate_disagreement description must contain %s; got %q", word, p.Description)
		}
	}
}

func TestAutonomousConfigGet_DescriptionNamesGateDisagreement(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigGet())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "gate_disagreement") {
		t.Fatal("autonomous_config_get description lacks gate_disagreement")
	}
	if !strings.Contains(string(b), "quality gate") {
		t.Error("gate_disagreement description should mention the quality gate")
	}
}

func TestAutonomousConfigSet_ForwardsGateDisagreement(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"gate_disagreement": "block"})); err != nil {
		t.Fatal(err)
	}
	if got["gate_disagreement"] != "block" {
		t.Fatalf("gate_disagreement not forwarded: %v", got)
	}

	// Absent when the caller never mentioned it (partial-merge semantics).
	got = nil
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["gate_disagreement"]; ok {
		t.Fatalf("gate_disagreement present when the caller never mentioned it: %v", got)
	}
}
