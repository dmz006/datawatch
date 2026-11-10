// Dashboard Timeline card — locale coverage. The card's off-state line
// (dash_card_off_gantt_*) must exist with a non-empty value in all five
// embedded locale bundles, be read by app.js, and carry no tracker IDs or
// "PRD" wording, since it is operator-facing copy.

package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// dashTimelineLocaleKeys are the keys the Timeline card's feature registry
// (DASH_CARD_FEATURES['gantt']) and empty-state line read in app.js.
var dashTimelineLocaleKeys = []string{
	"dash_card_off_gantt_reason",
	"dash_card_off_gantt_link",
}

// dashTimelinePRDWord matches the internal "PRD" term, which must not reach
// operator-facing text (the Automaton/Automata terminology rule).
var dashTimelinePRDWord = regexp.MustCompile(`\bPRDs?\b`)

func TestDashTimelineLocale_AllBundlesHaveKeys(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range dashTimelineLocaleKeys {
			v, ok := bundle[k]
			if !ok {
				t.Errorf("locale %s missing key %q", lang, k)
				continue
			}
			if strings.TrimSpace(v) == "" {
				t.Errorf("locale %s has empty value for %q", lang, k)
			}
			if automataEventsTrackerID.MatchString(v) {
				t.Errorf("locale %s value for %q contains a tracker ID: %q", lang, k, v)
			}
			if dashTimelinePRDWord.MatchString(v) {
				t.Errorf("locale %s value for %q uses the PRD wording: %q", lang, k, v)
			}
		}
	}
}

func TestDashTimelineLocale_AppJSReadsKeys(t *testing.T) {
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	js := string(raw)
	for _, k := range dashTimelineLocaleKeys {
		if !strings.Contains(js, "'"+k+"'") {
			t.Errorf("app.js does not read locale key %q", k)
		}
	}
}
