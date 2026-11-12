package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var rateCeilingKeys = []string{"capacity_rate_requests", "capacity_rate_tokens", "capacity_rate_window_seconds"}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// Every surface that carries the capacity_rate_* keys must name all three.
func TestRateCeilingSurfaceParity(t *testing.T) {
	surfaces := map[string]string{
		"config struct":          "internal/config/config.go",
		"REST":                   "internal/server/api.go",
		"MCP":                    "internal/mcp/autonomous.go",
		"comm help":              "internal/router/router.go",
		"web app.js":             "internal/server/web/app.js",
		"config-reference.yaml":  "docs/config-reference.yaml",
		"implementation.md":      "docs/implementation.md",
		"autonomous manager cfg": "internal/autonomous/manager.go",
	}
	for name, rel := range surfaces {
		src := readRepoFile(t, rel)
		for _, k := range rateCeilingKeys {
			if !strings.Contains(src, k) {
				t.Errorf("%s (%s) lacks %s", name, rel, k)
			}
		}
	}
	// CLI flags use dashes.
	cli := readRepoFile(t, "cmd/datawatch/cli_autonomous.go")
	for _, k := range rateCeilingKeys {
		if !strings.Contains(cli, strings.ReplaceAll(k, "_", "-")) {
			t.Errorf("CLI lacks flag --%s", strings.ReplaceAll(k, "_", "-"))
		}
	}
	// YAML tags on the config struct.
	cfg := readRepoFile(t, "internal/config/config.go")
	for _, k := range rateCeilingKeys {
		if !strings.Contains(cfg, `yaml:"`+k+`,omitempty"`) {
			t.Errorf("config struct lacks yaml tag for %s", k)
		}
	}
}

// All five locale bundles carry the same capacity_rate_ keys, valid JSON,
// non-empty values, and no tracker ids.
func TestRateCeilingLocaleParity(t *testing.T) {
	var ref []string
	for i, lang := range []string{"en", "de", "es", "fr", "ja"} {
		raw := readRepoFile(t, "internal/server/web/locales/"+lang+".json")
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("%s.json: invalid JSON: %v", lang, err)
		}
		var keys []string
		for k, v := range m {
			if !strings.HasPrefix(k, "capacity_rate_") {
				continue
			}
			s, _ := v.(string)
			if strings.TrimSpace(s) == "" {
				t.Errorf("%s.json %s is empty", lang, k)
			}
			if strings.Contains(s, "BL") && strings.ContainsAny(s, "0123456789") {
				t.Errorf("%s.json %s carries a tracker id: %q", lang, k, s)
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) != 6 {
			t.Errorf("%s.json has %d capacity_rate_ keys, want 6 (label+help for each): %v", lang, len(keys), keys)
		}
		if i == 0 {
			ref = keys
			continue
		}
		if strings.Join(keys, ",") != strings.Join(ref, ",") {
			t.Errorf("%s.json capacity_rate_ keys %v differ from en %v", lang, keys, ref)
		}
	}
}
