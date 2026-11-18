// mcp.resource_subscriptions over the config surfaces: REST round trip, the
// restart-required list, the config template, the reference YAML, the PWA
// field with its help entry, and the five locale bundles.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func putResourceSubscriptions(s *Server, v any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"mcp.resource_subscriptions": v})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func getResourceSubscriptions(t *testing.T, s *Server) any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	var mcpCfg map[string]any
	if err := json.Unmarshal(top["mcp"], &mcpCfg); err != nil {
		t.Fatalf("decode GET mcp: %v", err)
	}
	return mcpCfg["resource_subscriptions"]
}

func TestResourceSubscriptionConfigRoundTrip(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	s := &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}
	if got := getResourceSubscriptions(t, s); got != false {
		t.Fatalf("default GET mcp.resource_subscriptions = %v, want false", got)
	}
	if rr := putResourceSubscriptions(s, true); rr.Code != http.StatusOK {
		t.Fatalf("PUT true: %d %s", rr.Code, rr.Body.String())
	}
	if !s.cfg.MCP.ResourceSubscriptions {
		t.Fatal("PUT true did not set the field")
	}
	if got := getResourceSubscriptions(t, s); got != true {
		t.Fatalf("GET after PUT = %v, want true", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(raw), "resource_subscriptions: true") {
		t.Fatalf("config file lacks resource_subscriptions: true (err=%v):\n%s", err, raw)
	}
	if rr := putResourceSubscriptions(s, false); rr.Code != http.StatusOK || s.cfg.MCP.ResourceSubscriptions {
		t.Fatalf("PUT false: code=%d value=%v", rr.Code, s.cfg.MCP.ResourceSubscriptions)
	}
}

func TestResourceSubscriptionSurfaceParity(t *testing.T) {
	for name, rel := range map[string]string{
		"config struct":         "internal/config/config.go",
		"config template":       "internal/config/template.go",
		"config-reference.yaml": "docs/config-reference.yaml",
		"mcp.md":                "docs/mcp.md",
		"implementation.md":     "docs/implementation.md",
		"mcp README":            "internal/mcp/README.md",
		"CHANGELOG.md":          "CHANGELOG.md",
	} {
		if !strings.Contains(readRepoFile(t, rel), "resource_subscriptions") {
			t.Errorf("%s (%s) lacks resource_subscriptions", name, rel)
		}
	}
	for name, rel := range map[string]string{
		"PUT /api/config":       "internal/server/api.go",
		"restart-required list": "internal/server/reload.go",
		"comm channel help":     "internal/router/router.go",
		"web app.js":            "internal/server/web/app.js",
	} {
		if !strings.Contains(readRepoFile(t, rel), "mcp.resource_subscriptions") {
			t.Errorf("%s (%s) lacks mcp.resource_subscriptions", name, rel)
		}
	}
	app := readRepoFile(t, "internal/server/web/app.js")
	for _, want := range []string{
		"labelKey: 'settings_mcp_resource_subscriptions'",
		"helpKey: 'settings_mcp_resource_subscriptions_help'",
		"helpDocs: 'mcp.md#resource-subscriptions'",
	} {
		if !strings.Contains(app, want) {
			t.Errorf("web app.js: the MCP Server card field lacks %s", want)
		}
	}
	if !strings.Contains(readRepoFile(t, "docs/mcp.md"), "## Resource subscriptions") {
		t.Error("docs/mcp.md lacks the Resource subscriptions section the help link opens")
	}
}

// retiredAutomatonWord is the old name of an Automaton, which user-visible text must not use.
const retiredAutomatonWord = "PR" + "D"

func TestResourceSubscriptionLocaleParity(t *testing.T) {
	en := map[string]string{}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		var m map[string]any
		if err := json.Unmarshal([]byte(readRepoFile(t, "internal/server/web/locales/"+lang+".json")), &m); err != nil {
			t.Fatalf("%s.json: invalid JSON: %v", lang, err)
		}
		for _, key := range []string{"settings_mcp_resource_subscriptions", "settings_mcp_resource_subscriptions_help"} {
			v, ok := m[key].(string)
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s.json: %s missing or empty", lang, key)
				continue
			}
			if lang == "en" {
				en[key] = v
			} else if v == en[key] {
				t.Errorf("%s.json: %s is a copy of the English text", lang, key)
			}
			if strings.Contains(strings.ToUpper(v), retiredAutomatonWord) {
				t.Errorf("%s.json: %s uses the retired word for an Automaton", lang, key)
			}
		}
	}
}
