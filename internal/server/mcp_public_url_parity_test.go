package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every surface that carries the MCP SSE public URL setting names it.
func TestMCPPublicURLSurfaceParity(t *testing.T) {
	// Files that use the YAML field name (`public_url`) inside the mcp block.
	yamlForm := map[string]string{
		"config struct":           "internal/config/config.go",
		"config template":         "internal/config/template.go",
		"config-reference.yaml":   "docs/config-reference.yaml",
		"mcp.md":                  "docs/mcp.md",
		"operations.md":           "docs/operations.md",
		"implementation.md":       "docs/implementation.md",
		"mcp README":              "internal/mcp/README.md",
		"GET and PUT /api/config": "internal/server/api.go",
	}
	for name, rel := range yamlForm {
		if !strings.Contains(readRepoFile(t, rel), "public_url") {
			t.Errorf("%s (%s) lacks public_url", name, rel)
		}
	}
	// Files that use the dotted key.
	dotted := map[string]string{
		"PUT /api/config":       "internal/server/api.go",
		"restart-required list": "internal/server/reload.go",
		"comm channel help":     "internal/router/router.go",
		"web app.js":            "internal/server/web/app.js",
		"operations.md":         "docs/operations.md",
		"implementation.md":     "docs/implementation.md",
		"CHANGELOG.md":          "CHANGELOG.md",
	}
	for name, rel := range dotted {
		if !strings.Contains(readRepoFile(t, rel), "mcp.public_url") {
			t.Errorf("%s (%s) lacks mcp.public_url", name, rel)
		}
	}
	// The PWA field is both restart-required and localized.
	app := readRepoFile(t, "internal/server/web/app.js")
	if !strings.Contains(app, "labelKey: 'settings_mcp_public_url'") {
		t.Error("web app.js: the MCP Server card field lacks the settings_mcp_public_url label key")
	}
	if !strings.Contains(readRepoFile(t, "internal/server/reload.go"), `"mcp.sse_port", "mcp.public_url"`) {
		t.Error("reload.go: mcp.public_url is not in the restart-required list beside mcp.sse_port")
	}
}

// All five locale bundles define the label for the PWA field.
func TestMCPPublicURLLocaleParity(t *testing.T) {
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		var m map[string]any
		if err := json.Unmarshal([]byte(readRepoFile(t, "internal/server/web/locales/"+lang+".json")), &m); err != nil {
			t.Fatalf("%s.json: invalid JSON: %v", lang, err)
		}
		v, ok := m["settings_mcp_public_url"].(string)
		if !ok || strings.TrimSpace(v) == "" {
			t.Errorf("%s.json: settings_mcp_public_url missing or empty", lang)
		}
	}
}
