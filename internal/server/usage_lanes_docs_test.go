package server

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// yamlSub parses src into nodes (which, unlike a map decode, accepts the older duplicate
// keys elsewhere in the file), follows keys to one subtree and decodes only that subtree.
func yamlSub(t *testing.T, where, src string, keys ...string) any {
	t.Helper()
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(src), &root); err != nil {
		t.Fatalf("%s does not parse: %v", where, err)
	}
	n := &root
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	for _, k := range keys {
		var next *yaml.Node
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == k {
					next = n.Content[i+1]
					break
				}
			}
		}
		if next == nil {
			t.Fatalf("%s: no %q on the path %v", where, k, keys)
		}
		n = next
	}
	var out any
	if err := n.Decode(&out); err != nil {
		t.Fatalf("%s: decode %v: %v", where, keys, err)
	}
	return out
}

// yamlBlock cuts out the mapping entry that starts at the line head: that line and every
// following line indented deeper than it.
func yamlBlock(t *testing.T, where, src, head string) string {
	t.Helper()
	depth := len(head) - len(strings.TrimLeft(head, " "))
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if len(out) == 0 {
			if line == head {
				out = append(out, line)
			}
			continue
		}
		if trimmed := strings.TrimLeft(line, " "); trimmed != "" && len(line)-len(trimmed) <= depth {
			break
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no block %q", where, head)
	}
	return strings.Join(out, "\n")
}

// field returns m[key] as a map, or fails the test.
func field(t *testing.T, where string, m any, keys ...string) any {
	t.Helper()
	cur := m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%s: %q is not under a map", where, k)
		}
		if cur, ok = mm[k]; !ok {
			t.Fatalf("%s: no %q", where, k)
		}
	}
	return cur
}

// TestUsageLanesOpenAPIBothCopies: the usage lane contract is in the docs copy and in the
// copy embedded in the binary, with the lane fields where the JSON puts them (under
// `advice`, and on the reading).
func TestUsageLanesOpenAPIBothCopies(t *testing.T) {
	for _, rel := range []string{"docs/api/openapi.yaml", "internal/server/web/openapi.yaml"} {
		src := readRepoFile(t, rel)
		// Only the usage blocks are parsed: the embedded copy has older YAML errors in
		// unrelated blocks, so it does not parse as one document.
		block := func(head string) string { return yamlBlock(t, rel, src, head) }

		advicePath := yamlSub(t, rel, block("  /api/usage/advice:"), "/api/usage/advice")
		advice := field(t, rel, advicePath, "get", "responses", "200", "content", "application/json", "schema", "properties", "advice", "properties").(map[string]any)
		for _, f := range []string{"lanes_running_now", "lanes_stamped", "what_if_lanes"} {
			if _, ok := advice[f]; !ok {
				t.Errorf("%s: advice response lacks %s", rel, f)
			}
		}

		reading := yamlSub(t, rel, block("    UsageReading:"), "UsageReading", "properties").(map[string]any)
		for _, f := range []string{"lanes", "lanes_source"} {
			if _, ok := reading[f]; !ok {
				t.Errorf("%s: UsageReading lacks %s", rel, f)
			}
		}
		var got []string
		for _, v := range field(t, rel, reading, "lanes_source", "enum").([]any) {
			got = append(got, v.(string))
		}
		if want := "live,recorded,current,unavailable,typed"; strings.Join(got, ",") != want {
			t.Errorf("%s: lanes_source values %v; want %s", rel, got, want)
		}

		// The reading request still names lanes, as deprecated.
		post := yamlSub(t, rel, block("  /api/usage/readings:"), "/api/usage/readings", "post")
		body, err := json.Marshal(post)
		if err != nil {
			t.Fatalf("%s: marshal readings post: %v", rel, err)
		}
		if !strings.Contains(string(body), `"deprecated":true`) {
			t.Errorf("%s: the lanes field on the reading request is not marked deprecated", rel)
		}
	}
}

// TestUsageLanesOperatorDocsNoReadingLanes: the operator docs no longer offer a lanes input on
// a reading, and the locale key of the removed input is gone from every bundle.
func TestUsageLanesOperatorDocsNoReadingLanes(t *testing.T) {
	stale := map[string][]string{
		"docs/commands.md":               {"usage reading <percent> [lanes=N]", "usage reading <pct> [lanes=N]", "usage reading 72.5 lanes=3"},
		"docs/mcp.md":                    {"session_percent=33, lanes=2", "the lanes that were in play when the reading was taken"},
		"docs/cursor-mcp.md":             {"session percent, lanes) or delete"},
		"docs/api-mcp-mapping.md":        {"`session_percent` (0–100), `lanes` (>= 0)"},
		"docs/howto/usage-calculator.md": {"usage reading <percent> [lanes=N]", "Without it the advice uses the lanes running now"},
	}
	for rel, needles := range stale {
		src := readRepoFile(t, rel)
		for _, n := range needles {
			if strings.Contains(src, n) {
				t.Errorf("%s still says %q", rel, n)
			}
		}
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		var m map[string]any
		if err := json.Unmarshal([]byte(readRepoFile(t, "internal/server/web/locales/"+lang+".json")), &m); err != nil {
			t.Fatalf("%s bundle: %v", lang, err)
		}
		if _, ok := m["usage_calc_lanes_label"]; ok {
			t.Errorf("%s bundle still carries usage_calc_lanes_label, which nothing references", lang)
		}
		for _, k := range []string{"usage_calc_whatif_label", "usage_calc_whatif_hint", "usage_calc_running_now", "usage_calc_running_unknown"} {
			if _, ok := m[k]; !ok {
				t.Errorf("%s bundle lacks %s", lang, k)
			}
		}
	}
}
