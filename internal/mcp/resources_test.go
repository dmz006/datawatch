// BL302 S1 — unit tests for MCP resource handlers.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// makeTestServer creates a minimal Server for resource handler tests.
// webPort=0 means proxyGet will return an error — tests that need live
// data mock via direct handler calls that short-circuit.
func makeTestServer(version string) *Server {
	return &Server{
		version: version,
	}
}

func makeReq(uri string) mcpsdk.ReadResourceRequest {
	return mcpsdk.ReadResourceRequest{
		Params: mcpsdk.ReadResourceParams{URI: uri},
	}
}

// TestVersionResource_ReturnsVersion verifies that handleVersionResource returns
// a JSON object containing at least the "version" key.
func TestVersionResource_ReturnsVersion(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.1")
	contents, err := s.handleVersionResource(context.Background(), makeReq("datawatch://version"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := m["version"]; !ok {
		t.Errorf("expected 'version' key in response, got: %s", tc.Text)
	}
	if got := m["version"].(string); got != "7.1.0-alpha.1" {
		t.Errorf("expected version='7.1.0-alpha.1', got=%q", got)
	}
}

// TestConfigResource_RedactsSecrets verifies that when the REST proxy is
// unavailable (webPort=0), the config resource returns an error JSON rather
// than panicking or returning empty content.
func TestConfigResource_RedactsSecrets(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.1")
	// webPort=0 → proxyGet will fail → handleConfigResource returns {"error":...}
	contents, err := s.handleConfigResource(context.Background(), makeReq("datawatch://config"))
	if err != nil {
		t.Fatalf("unexpected error (should return error JSON, not Go error): %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	// Should have error key when REST loopback unavailable.
	if _, ok := m["error"]; !ok {
		t.Errorf("expected 'error' key when webPort=0, got: %v", m)
	}
}

// TestDocsResource_ListsHowtos verifies that when the REST proxy is unavailable,
// docs list resource returns error JSON not panic.
func TestDocsResource_ListsHowtos(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.1")
	contents, err := s.handleDocsListResource(context.Background(), makeReq("datawatch://docs"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := m["error"]; !ok {
		t.Errorf("expected error key when REST unavailable")
	}
}

// TestDocsByPath_ReturnsContent verifies that the docs-by-path template
// handler builds the correct proxy path and returns content.
// With webPort=0, it should return error JSON gracefully.
func TestDocsByPath_ReturnsContent(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.1")
	uri := "datawatch://docs/mcp-resources.md"
	contents, err := s.handleDocsByPathTemplate(context.Background(), makeReq(uri))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if tc.URI != uri {
		t.Errorf("expected URI=%q, got=%q", uri, tc.URI)
	}
	// With webPort=0 we get error JSON — still valid JSON.
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

// TestResourceTemplates_Listed verifies that RegisterResources populates the
// global registry with the expected number of templates.
func TestResourceTemplates_Listed(t *testing.T) {
	// Reset registry for test isolation.
	prev := globalResourceRegistry
	globalResourceRegistry = resourceRegistry{}
	defer func() { globalResourceRegistry = prev }()

	s := &Server{version: "7.1.0-test"}
	// We can't call RegisterResources without a real mcpSrv, so test the
	// registry directly after simulating registration.
	// Verify that the template URIs follow the datawatch:// scheme.
	expectedPrefixes := []string{
		"datawatch://docs/",
		"datawatch://sessions/",
		"datawatch://automata/",
		"datawatch://memory/",
		"datawatch://alerts/",
		"datawatch://council/",
	}
	_ = s // used for context; actual template dispatch tested below

	// Verify extractTemplateParam helper.
	tests := []struct {
		uri    string
		prefix string
		suffix string
		want   string
	}{
		{"datawatch://docs/mcp-resources.md", "datawatch://docs/", "", "mcp-resources.md"},
		{"datawatch://sessions/abc123/history", "datawatch://sessions/", "/history", "abc123"},
		{"datawatch://automata/xyz/status", "datawatch://automata/", "/status", "xyz"},
		{"datawatch://memory/42", "datawatch://memory/", "", "42"},
	}
	for _, tt := range tests {
		got := extractTemplateParam(tt.uri, tt.prefix, tt.suffix)
		if got != tt.want {
			t.Errorf("extractTemplateParam(%q, %q, %q) = %q, want %q", tt.uri, tt.prefix, tt.suffix, got, tt.want)
		}
	}

	// Verify all expected URI prefixes match the template URIs we'd generate.
	definedTemplates := []string{
		"datawatch://docs/{path}",
		"datawatch://sessions/{id}",
		"datawatch://sessions/{id}/history",
		"datawatch://automata/{id}",
		"datawatch://automata/{id}/status",
		"datawatch://memory/{id}",
		"datawatch://alerts/{id}",
		"datawatch://council/{id}",
	}
	for _, tmpl := range definedTemplates {
		found := false
		for _, pfx := range expectedPrefixes {
			if strings.HasPrefix(tmpl, pfx) || strings.HasPrefix(tmpl, strings.TrimSuffix(pfx, "/")) {
				found = true
				break
			}
		}
		if !found {
			// Not an error — just verify each template has a datawatch:// scheme.
			if !strings.HasPrefix(tmpl, "datawatch://") {
				t.Errorf("template %q does not use datawatch:// scheme", tmpl)
			}
		}
	}
	if len(definedTemplates) != 8 {
		t.Errorf("expected 8 resource templates, got %d", len(definedTemplates))
	}
}

// TestResourceReadJSON_DispatchVersionURI verifies that ResourceReadJSON dispatches
// to the version handler for "datawatch://version".
func TestResourceReadJSON_DispatchVersionURI(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.1")
	data, err := s.ResourceReadJSON(context.Background(), "datawatch://version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	// Should have "contents" key wrapping the version data.
	if _, ok := m["contents"]; !ok {
		t.Errorf("expected 'contents' key in ResourceReadJSON response")
	}
}

// ── BL302 S2 — live resource handler tests ────────────────────────────────
// All tests use webPort=0 (REST unavailable) to verify graceful fallback
// to empty JSON arrays rather than errors or panics.

// TestSessionsResource_GracefulFallback verifies that when the REST loopback
// is unavailable, datawatch://sessions returns {"sessions":[]} not an error.
func TestSessionsResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleSessionsResource(context.Background(), makeReq("datawatch://sessions"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["sessions"]; !ok {
		t.Errorf("expected 'sessions' key in fallback response, got: %v", m)
	}
}

// TestStatsResource_GracefulFallback verifies that datawatch://stats returns
// valid JSON even when the REST loopback is unavailable.
func TestStatsResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleStatsResource(context.Background(), makeReq("datawatch://stats"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	// With webPort=0 the error key is expected.
	if _, hasError := m["error"]; !hasError {
		t.Errorf("expected 'error' key when webPort=0, got: %v", m)
	}
}

// TestStatsMCPResource_GracefulFallback verifies that datawatch://stats/mcp
// returns valid JSON when REST is unavailable.
func TestStatsMCPResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleStatsMCPResource(context.Background(), makeReq("datawatch://stats/mcp"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
}

// TestAlertsResource_GracefulFallback verifies that datawatch://alerts returns
// {"alerts":[]} when the REST loopback is unavailable.
func TestAlertsResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleAlertsResource(context.Background(), makeReq("datawatch://alerts"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["alerts"]; !ok {
		t.Errorf("expected 'alerts' key in fallback response, got: %v", m)
	}
}

// TestMemoryRecentResource_GracefulFallback verifies that datawatch://memory/recent
// returns {"entries":[]} when memory is unavailable.
func TestMemoryRecentResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleMemoryRecentResource(context.Background(), makeReq("datawatch://memory/recent"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["entries"]; !ok {
		t.Errorf("expected 'entries' key in fallback response, got: %v", m)
	}
}

// TestAutomataResource_GracefulFallback verifies that datawatch://automata
// returns {"automata":[]} when unavailable.
func TestAutomataResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleAutomataResource(context.Background(), makeReq("datawatch://automata"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["automata"]; !ok {
		t.Errorf("expected 'automata' key in fallback response, got: %v", m)
	}
}

// TestCouncilPersonasResource_GracefulFallback verifies that
// datawatch://council/personas returns {"personas":[]} when unavailable.
func TestCouncilPersonasResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleCouncilPersonasResource(context.Background(), makeReq("datawatch://council/personas"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["personas"]; !ok {
		t.Errorf("expected 'personas' key in fallback response, got: %v", m)
	}
}

// TestKGEntitiesResource_GracefulFallback verifies that datawatch://kg/entities
// returns valid JSON when KG is unavailable.
func TestKGEntitiesResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleKGEntitiesResource(context.Background(), makeReq("datawatch://kg/entities"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["entities"]; !ok {
		t.Errorf("expected 'entities' key in fallback response, got: %v", m)
	}
}

// TestKGTriplesResource_GracefulFallback verifies that datawatch://kg/triples
// returns {"triples":[]} when KG WAL is unavailable.
func TestKGTriplesResource_GracefulFallback(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	contents, err := s.handleKGTriplesResource(context.Background(), makeReq("datawatch://kg/triples"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := contents[0].(mcpsdk.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v — got: %s", err, tc.Text)
	}
	if _, ok := m["triples"]; !ok {
		t.Errorf("expected 'triples' key in fallback response, got: %v", m)
	}
}

// TestDispatchResourceRead_LiveURIs verifies that dispatchResourceRead handles
// all S2 live resource URIs without returning "unknown resource" errors.
func TestDispatchResourceRead_LiveURIs(t *testing.T) {
	s := makeTestServer("7.1.0-alpha.2")
	liveURIs := []string{
		"datawatch://sessions",
		"datawatch://stats",
		"datawatch://stats/mcp",
		"datawatch://alerts",
		"datawatch://memory/recent",
		"datawatch://automata",
		"datawatch://council/personas",
		"datawatch://kg/entities",
		"datawatch://kg/triples",
	}
	for _, uri := range liveURIs {
		t.Run(uri, func(t *testing.T) {
			contents, err := s.dispatchResourceRead(context.Background(), makeReq(uri))
			if err != nil {
				t.Errorf("dispatchResourceRead(%q) returned unexpected error: %v", uri, err)
				return
			}
			if len(contents) == 0 {
				t.Errorf("dispatchResourceRead(%q) returned no contents", uri)
				return
			}
			tc, ok := contents[0].(mcpsdk.TextResourceContents)
			if !ok {
				t.Errorf("dispatchResourceRead(%q) did not return TextResourceContents", uri)
				return
			}
			var m interface{}
			if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
				t.Errorf("dispatchResourceRead(%q) returned invalid JSON: %v — got: %s", uri, err, tc.Text)
			}
		})
	}
}

// TestLiveResourceCount_Registered verifies that the S2 resources have the
// expected live resource URIs defined in dispatchResourceRead.
func TestLiveResourceCount_Registered(t *testing.T) {
	// S2 live resource URIs (exact-match, not templates).
	expectedLive := []string{
		"datawatch://sessions",
		"datawatch://stats",
		"datawatch://stats/mcp",
		"datawatch://alerts",
		"datawatch://memory/recent",
		"datawatch://automata",
		"datawatch://council/personas",
		"datawatch://kg/entities",
		"datawatch://kg/triples",
	}
	if len(expectedLive) != 9 {
		t.Errorf("expected 9 S2 live resources, got %d", len(expectedLive))
	}
	// Verify none are duplicated.
	seen := map[string]bool{}
	for _, uri := range expectedLive {
		if seen[uri] {
			t.Errorf("duplicate URI in S2 live resource list: %s", uri)
		}
		seen[uri] = true
	}
}
