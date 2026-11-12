// Tests for the webhook and peer sinks on the MCP subscription tools: the
// secret_ref argument reaches the REST body for both subscribe and update, is
// omitted when not given, and the tool schema documents the new sink types.
// The daemon's own validation (allow-list, enablement, literal secret refusal)
// is covered in internal/server and internal/autonomous.

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestSubscriptionSinkArgs_SubscribeSendsSecretRef checks the webhook shape of
// autonomous_subscribe: the URL target and the reference go to the body.
func TestSubscriptionSinkArgs_SubscribeSendsSecretRef(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-hook"}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	res, err := s.handleAutonomousSubscribe(t.Context(), subUpdateReq("autonomous_subscribe", map[string]any{
		"kinds":       "task.completed",
		"sink_type":   "webhook",
		"sink_target": "https://hooks.example.net/datawatch",
		"secret_ref":  "${secret:lifecycle-hook}",
	}))
	if err != nil || res.IsError {
		t.Fatalf("subscribe: res=%+v err=%v", res, err)
	}
	sink := mapField(t, gotBody, "sink")
	if sink["type"] != "webhook" || sink["target"] != "https://hooks.example.net/datawatch" {
		t.Errorf("sink = %v, want webhook to the URL", sink)
	}
	if sink["secret_ref"] != "${secret:lifecycle-hook}" {
		t.Errorf("secret_ref = %v, want the reference text", sink["secret_ref"])
	}
}

// TestSubscriptionSinkArgs_SubscribeOmitsSecretRefWhenUnset checks that no
// secret_ref key is sent for a sink that does not take one.
func TestSubscriptionSinkArgs_SubscribeOmitsSecretRefWhenUnset(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-peer"}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	if _, err := s.handleAutonomousSubscribe(t.Context(), subUpdateReq("autonomous_subscribe", map[string]any{
		"kinds":       "prd.blocked",
		"sink_type":   "peer",
		"sink_target": "laptop",
	})); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sink := mapField(t, gotBody, "sink")
	if sink["type"] != "peer" || sink["target"] != "laptop" {
		t.Errorf("sink = %v, want peer laptop", sink)
	}
	if _, present := sink["secret_ref"]; present {
		t.Errorf("secret_ref sent for a peer sink: %v", sink)
	}
}

// TestSubscriptionSinkArgs_UpdateSendsSecretRef checks the same argument on the
// update tool's PUT body.
func TestSubscriptionSinkArgs_UpdateSendsSecretRef(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{"id":"sub-9"}`)
	if _, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", map[string]any{
		"id":          "sub-9",
		"kinds":       "task.completed",
		"sink_type":   "webhook",
		"sink_target": "https://hooks.example.net/datawatch",
		"secret_ref":  "${secret:lifecycle-hook-prod}",
	})); err != nil {
		t.Fatalf("update: %v", err)
	}
	if c.method != http.MethodPut || c.path != "/api/autonomous/subscriptions/sub-9" {
		t.Fatalf("route = %s %s, want PUT .../sub-9", c.method, c.path)
	}
	sink := mapField(t, c.body, "sink")
	if sink["secret_ref"] != "${secret:lifecycle-hook-prod}" || sink["type"] != "webhook" {
		t.Errorf("sink = %v, want webhook with the reference", sink)
	}
}

// TestSubscriptionSinkArgs_SchemaDocumentsNewSinks checks the tool schemas
// advertise secret_ref and both new sink types, so an MCP client can discover
// them without reading the source.
func TestSubscriptionSinkArgs_SchemaDocumentsNewSinks(t *testing.T) {
	s := &Server{}
	for _, tool := range []mcpsdk.Tool{s.toolAutonomousSubscribe(), s.toolAutonomousSubscriptionUpdate()} {
		props := tool.InputSchema.Properties
		if _, ok := props["secret_ref"]; !ok {
			t.Errorf("%s: no secret_ref argument in its schema", tool.Name)
		}
		sinkType, _ := props["sink_type"].(map[string]any)
		desc, _ := sinkType["description"].(string)
		for _, want := range []string{`"webhook"`, `"peer"`, "allow-list", "autonomous:write"} {
			if !strings.Contains(desc, want) {
				t.Errorf("%s: sink_type description lacks %q: %s", tool.Name, want, desc)
			}
		}
		if secret, _ := props["secret_ref"].(map[string]any); !strings.Contains(secret["description"].(string), "${secret:name}") {
			t.Errorf("%s: secret_ref description does not show the reference form", tool.Name)
		}
	}
}
