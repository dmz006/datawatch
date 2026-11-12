// Automata Events tab — locale coverage. Every automata_events_* key the
// PWA reads (internal/server/web/app.js) and every '?' help entry for the
// Events tab, its filters and the moved cards must exist in all five embedded
// locale bundles, with no tracker IDs and no "PRD" wording in operator text.

package server

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// automataEventsLocaleKeys is the full set the Events tab and its help
// anchors use. app.js is also scanned, so a new key added there fails the
// test until it is listed here and translated.
var automataEventsLocaleKeys = []string{
	"automata_events_detail",
	"automata_events_empty",
	"automata_events_error_fix_default",
	"automata_events_error_happened",
	"automata_events_failed",
	"automata_events_filter_all_projects",
	"automata_events_filter_kind",
	"automata_events_filter_project",
	"automata_events_filter_search",
	"automata_events_label_automation",
	"automata_events_label_automaton",
	"automata_events_loading",
	"automata_events_load_more",
	"automata_events_next_action",
	"automata_events_project",
	"automata_events_reason",
	"automata_events_tab",
	"automata_events_help_tip",
	"automata_events_help_filters",
	"automata_events_help_card_supervisor_watch",
	"automata_events_help_card_recent_decisions",
	"automata_events_help_card_infra_retries",
	"automata_events_help_card_automata_checks",
	"automata_events_help_card_safe_to_delete",
	"automata_events_help_card_cleanup_status",
	"automata_events_config_in_settings",
}

var automataEventsKeyRef = regexp.MustCompile(`automata_events_[a-z0-9_]+`)

// automataEventsTrackerID matches tracker IDs (BL###, B###, GH###, SEC-###,
// HLLM-###) that must never appear in operator-facing strings.
var automataEventsTrackerID = regexp.MustCompile(`\b(BL|GH|HLLM|SEC-|B)[0-9]+\b`)

func TestAutomataEventsLocale_AllBundlesHaveKeys(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range automataEventsLocaleKeys {
			v, ok := bundle[k]
			if !ok {
				t.Errorf("locale %s missing key %q", lang, k)
				continue
			}
			if strings.TrimSpace(v) == "" {
				t.Errorf("locale %s has empty value for %q", lang, k)
			}
		}
	}
}

func TestAutomataEventsLocale_SameKeySetInEveryBundle(t *testing.T) {
	want := map[string]bool{}
	for _, k := range automataEventsLocaleKeys {
		want[k] = true
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for k := range bundle {
			if strings.HasPrefix(k, "automata_events_") && !want[k] {
				t.Errorf("locale %s has automata_events key %q not in the expected set", lang, k)
			}
		}
	}
}

func TestAutomataEventsLocale_AppJSKeysAreCovered(t *testing.T) {
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	js := string(raw)
	// Every automata_events_ token app.js references, whatever the quoting.
	used := map[string]bool{}
	for _, m := range automataEventsKeyRef.FindAllString(js, -1) {
		used[m] = true
	}
	listed := map[string]bool{}
	for _, k := range automataEventsLocaleKeys {
		listed[k] = true
	}
	// Every key app.js uses must be listed here, so a new key added in app.js
	// fails the test until it is listed and translated.
	var unlisted []string
	for k := range used {
		if !listed[k] {
			unlisted = append(unlisted, k)
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Errorf("app.js uses automata_events keys not in the locale key list: %v", unlisted)
	}
	// ...and every key listed here must be used by app.js, so a key that is
	// never rendered (dead string) fails the test instead of passing unnoticed.
	var unused []string
	for _, k := range automataEventsLocaleKeys {
		if !used[k] {
			unused = append(unused, k)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("locale key list names automata_events keys app.js never uses: %v", unused)
	}
}

func TestAutomataEventsLocale_NoTrackerIDsOrPRDWording(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range automataEventsLocaleKeys {
			v := bundle[k]
			if automataEventsTrackerID.MatchString(v) {
				t.Errorf("locale %s key %q contains a tracker ID: %q", lang, k, v)
			}
			if strings.Contains(v, "PRD") {
				t.Errorf("locale %s key %q uses PRD wording: %q", lang, k, v)
			}
		}
	}
}
