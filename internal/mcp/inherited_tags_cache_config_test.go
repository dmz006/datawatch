// autonomous.inherited_tags_cache_seconds on the MCP surface:
// autonomous_config_set forwards it as the daemon-config dotted key
// autonomous.inherited_tags_cache_seconds on PUT /api/config (the key lives in
// the daemon config, not the autonomous manager's, so it rides the allocation
// PUT, not the /api/autonomous/config PUT), and autonomous_config_get's
// response — proxied GET /api/autonomous/config — includes it.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestInheritedTagsCache_ConfigSetSchemaHasTheArgument(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	prop, ok := tool.InputSchema.Properties["inherited_tags_cache_seconds"].(map[string]any)
	if !ok {
		t.Fatalf("autonomous_config_set lacks the inherited_tags_cache_seconds argument")
	}
	if d, _ := prop["description"].(string); !strings.Contains(d, "autonomous.inherited_tags_cache_seconds") {
		t.Errorf("argument description does not name autonomous.inherited_tags_cache_seconds: %s", d)
	}
	if strings.Contains(tool.Description, "this call does not return it — read it under autonomous.inherited_tags_cache_seconds") {
		t.Errorf("get description is stale: the key is now returned: %s", tool.Description)
	}
}

func TestInheritedTagsCache_ConfigGetDescriptionNamesTheKey(t *testing.T) {
	d := (&Server{}).toolAutonomousConfigGet().Description
	for _, want := range []string{"inherited_tags_cache_seconds", "autonomous.inherited_tags_cache_seconds"} {
		if !strings.Contains(d, want) {
			t.Errorf("autonomous_config_get description does not name %s", want)
		}
	}
}

func TestInheritedTagsCache_ConfigSetSendsDottedKeyToConfig(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"inherited_tags_cache_seconds": float64(60),
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	if len(rec.body) != 1 {
		t.Fatalf("body = %v, want exactly the one key", rec.body)
	}
	if got, ok := rec.body["autonomous.inherited_tags_cache_seconds"]; !ok || got != float64(60) {
		t.Errorf("body = %v, want autonomous.inherited_tags_cache_seconds = 60", rec.body)
	}
}

// 0 (the 15-second default) and negative (cache off) are valid values and
// must be forwarded as given, not dropped by a sentinel check.
func TestInheritedTagsCache_ConfigSetKeepsExplicitZeroAndNegative(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"inherited_tags_cache_seconds": float64(0),
	})); err != nil {
		t.Fatal(err)
	}
	if got := rec.body["autonomous.inherited_tags_cache_seconds"]; got != float64(0) {
		t.Fatalf("explicit 0 was not sent as given: %v", rec.body)
	}

	s, rec = activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"inherited_tags_cache_seconds": float64(-1),
	})); err != nil {
		t.Fatal(err)
	}
	if got := rec.body["autonomous.inherited_tags_cache_seconds"]; got != float64(-1) {
		t.Fatalf("negative (cache off) was not sent as given: %v", rec.body)
	}
}

// Alongside autonomous-manager keys, the key still rides the allocation PUT
// (the last request) while the manager keys go to /api/autonomous/config.
func TestInheritedTagsCache_ConfigSetAlongsideAutonomousBodyKeys(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"poll_interval_seconds":        float64(45),
		"inherited_tags_cache_seconds": float64(120),
	})); err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("last proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	if got := rec.body["autonomous.inherited_tags_cache_seconds"]; got != float64(120) {
		t.Errorf("allocation PUT body = %v, want autonomous.inherited_tags_cache_seconds = 120", rec.body)
	}
}

// autonomous_config_get proxies GET /api/autonomous/config, which now
// includes the key; the tool passes the value through unchanged.
func TestInheritedTagsCache_AutonomousConfigGetShowsTheKey(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"max_concurrent_tasks":2,"inherited_tags_cache_seconds":45}`))
	})
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/config" {
		t.Errorf("proxied %s %s", method, path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"inherited_tags_cache_seconds":45`) {
		t.Errorf("autonomous_config_get dropped inherited_tags_cache_seconds: %s", text)
	}
}
