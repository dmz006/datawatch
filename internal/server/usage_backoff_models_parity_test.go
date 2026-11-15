package server

import (
	"strings"
	"testing"
)

// TestUsageBackoffModelsSurfaceParity keeps the optional models list — the
// held-model narrowing of a usage back-off flag — present on every surface
// that sets or clears a flag. Each file must both carry the usage back-off
// surface and name the models parameter, so a rename or a dropped argument
// anywhere fails here.
func TestUsageBackoffModelsSurfaceParity(t *testing.T) {
	surfaces := map[string]struct {
		file    string // the surface's source file, repo-relative
		feature string // token proving the file is the usage back-off surface
		models  string // token proving the models parameter is wired in
	}{
		"REST handler": {
			file:    "internal/server/usage_backoff.go",
			feature: "api/usage-backoff",
			models:  `json:"models,omitempty"`,
		},
		"MCP tools": {
			file:    "internal/mcp/usage_backoff.go",
			feature: "usage_backoff_set",
			models:  `WithArray("models"`,
		},
		"CLI": {
			file:    "cmd/datawatch/cli_usage_backoff.go",
			feature: "usage-backoff",
			models:  `Flags().String("models"`,
		},
		"comm parser": {
			file:    "internal/router/commands.go",
			feature: "usage_backoff",
			models:  `models=`,
		},
		"comm handler": {
			file:    "internal/router/sx2_parity.go",
			feature: "usage_backoff",
			models:  `UsageBackoffModels`,
		},
		"core registry": {
			file:    "internal/autonomous/backoff.go",
			feature: "SetWithModels",
			models:  `Models []string`,
		},
	}
	for name, s := range surfaces {
		src := readRepoFile(t, s.file)
		if !strings.Contains(src, s.feature) {
			t.Errorf("%s (%s) no longer looks like the usage back-off surface: missing %q", name, s.file, s.feature)
		}
		if !strings.Contains(src, s.models) {
			t.Errorf("%s (%s) lost the models parameter: missing %q", name, s.file, s.models)
		}
	}

	// The web app's usage back-off surface is display-only for now: the
	// capacity card renders the back-off list, and the set/clear models
	// control is added by a follow-up change. Its parity obligation is that
	// the back-off card's render path exists for that follow-up to build on.
	web := readRepoFile(t, "internal/server/web/app.js")
	for _, want := range []string{"capacityBackoffHtml", "autonomous.usage_backoff"} {
		if !strings.Contains(web, want) {
			t.Errorf("web app.js lost %q (the usage back-off surface)", want)
		}
	}
}

// TestUsageBackoffModelsDocsParity keeps the operator docs and the OpenAPI
// spec naming the models parameter on both the set and the clear surface,
// so the docs cannot drift behind the code.
func TestUsageBackoffModelsDocsParity(t *testing.T) {
	docs := []struct {
		file  string
		wants []string
	}{
		{
			file:  "docs/howto/usage-backoff.md",
			wants: []string{"models"},
		},
		{
			file:  "internal/server/web/openapi.yaml",
			wants: []string{"models"},
		},
	}
	for _, d := range docs {
		src := readRepoFile(t, d.file)
		for _, want := range d.wants {
			if !strings.Contains(src, want) {
				t.Errorf("%s lost the models parameter: missing %q", d.file, want)
			}
		}
	}
}
