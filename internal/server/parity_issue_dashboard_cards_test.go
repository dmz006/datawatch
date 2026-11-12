package server

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The parity issue file for the dashboard cards must name only locale keys
// that exist in all five bundles, and must keep the operator-facing rules
// (no tracker ids, no PRD wording).
func TestParityIssueDashboardCards_LocaleKeysExistEverywhere(t *testing.T) {
	raw, err := os.ReadFile("../../docs/plans/parity-issues/dashboard-cards-data-fix.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	keyRe := regexp.MustCompile("`((?:dash|websearch|cost_report|lane_pulse)_[a-z0-9_]+)`")
	bundles := map[string]map[string]string{}
	for _, l := range []string{"en", "de", "es", "fr", "ja"} {
		b, err := os.ReadFile("web/locales/" + l + ".json")
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		bundles[l] = m
	}
	seen := 0
	for _, m := range keyRe.FindAllStringSubmatch(doc, -1) {
		k := m[1]
		if strings.HasSuffix(k, "_") || strings.Contains(k, "<") {
			continue
		}
		// Only keys written as table rows are contract keys.
		if !strings.Contains(doc, "| `"+k+"` |") {
			continue
		}
		seen++
		for l, b := range bundles {
			if _, ok := b[k]; !ok {
				t.Errorf("locale key %s listed in the parity issue is missing from %s.json", k, l)
			}
		}
	}
	if seen < 150 {
		t.Fatalf("expected the issue to list the new locale keys, found %d", seen)
	}
	if regexp.MustCompile(`(?i)\bPRDs?\b|BL\d{3}|\b646b[0-9a-f]{4}\b`).MatchString(doc) {
		t.Error("parity issue contains a tracker id or PRD wording")
	}
}
