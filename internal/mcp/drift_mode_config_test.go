package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// drift_mode — the autonomous_config_set tool must list the key in its
// schema (block|warn) and forward it to the REST write only when the caller
// supplied it; autonomous_config_get must name it.

func TestAutonomousConfigSet_SchemaListsDriftMode(t *testing.T) {
	s := &Server{}
	var tool struct {
		InputSchema struct {
			Properties map[string]struct {
				Type        string   `json:"type"`
				Description string   `json:"description"`
				Enum        []string `json:"enum"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	p, ok := tool.InputSchema.Properties["drift_mode"]
	if !ok {
		t.Fatalf("autonomous_config_set schema lacks drift_mode property")
	}
	if p.Type != "string" {
		t.Errorf("drift_mode schema type = %q, want string", p.Type)
	}
	if len(p.Enum) != 2 || p.Enum[0] != "block" || p.Enum[1] != "warn" {
		t.Errorf("drift_mode enum = %v, want [block warn]", p.Enum)
	}
	for _, word := range []string{"block", "warn", "pre-existing"} {
		if !strings.Contains(p.Description, word) {
			t.Errorf("drift_mode description must contain %q; got %q", word, p.Description)
		}
	}
}

func TestAutonomousConfigGet_DescriptionNamesDriftMode(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigGet())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "drift_mode") {
		t.Fatal("autonomous_config_get description lacks drift_mode")
	}
}

func TestAutonomousConfigSet_ForwardsDriftMode(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	for _, want := range []string{"block", "warn"} {
		got = nil
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"drift_mode": want})); err != nil {
			t.Fatal(err)
		}
		if v, ok := got["drift_mode"]; !ok || v != want {
			t.Fatalf("drift_mode=%q not forwarded: %v", want, got)
		}
	}
	// Not mentioning the field must not add it to the body.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["drift_mode"]; ok {
		t.Fatalf("drift_mode present when the caller never mentioned it: %v", got)
	}
}
