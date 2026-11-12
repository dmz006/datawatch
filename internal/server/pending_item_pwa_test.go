package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// pendingItemSettingsLocaleKeys are the locale keys the Pending items settings
// group and its nine fields use in the PWA.
var pendingItemSettingsLocaleKeys = []string{
	"settings_pending_items_group",
	"settings_automation_pending_item_policy",
	"settings_automation_pending_item_auto_resume",
	"settings_automation_pending_item_source_path",
	"settings_automation_pending_item_source_id_pattern",
	"settings_automation_pending_item_source_id_column",
	"settings_automation_pending_item_source_title_column",
	"settings_automation_pending_item_source_state_column",
	"settings_automation_pending_item_source_done_values",
	"settings_automation_pending_item_source_on_missing_row",
	"pending_item_policy_hold",
	"pending_item_policy_skip",
	"pending_item_on_missing_row_keep",
	"pending_item_on_missing_row_drop",
}

// TestPendingItemSettingsLocaleKeys checks that all five bundles carry the
// pending item settings keys, that the non-English bundles hold the English text
// until translations arrive, that no value says PRD, and that every settings
// label key used by app.js for these fields exists in the bundles.
func TestPendingItemSettingsLocaleKeys(t *testing.T) {
	en := loadLocaleBundle(t, "en")
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range pendingItemSettingsLocaleKeys {
			v, ok := bundle[k]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locale %s is missing %q", lang, k)
				continue
			}
			if lang != "en" && v != en[k] {
				t.Errorf("locale %s key %q is %q, want the English text %q until translations arrive", lang, k, v, en[k])
			}
			if prdLeakRe.MatchString(v) {
				t.Errorf("locale %s key %q says PRD: %q", lang, k, v)
			}
		}
	}

	// Every pending item label key and option label key that app.js references
	// must be one the bundles carry.
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("read app.js from embed: %v", err)
	}
	src := string(raw)
	keyRe := regexp.MustCompile(`'(settings_pending_items_group|settings_automation_pending_item_\w+|pending_item_(?:policy|on_missing_row)_\w+)'`)
	found := map[string]bool{}
	for _, m := range keyRe.FindAllStringSubmatch(src, -1) {
		found[m[1]] = true
	}
	for _, k := range pendingItemSettingsLocaleKeys {
		if !found[k] {
			t.Errorf("app.js does not reference locale key %q", k)
		}
	}
	for k := range found {
		if _, ok := en[k]; !ok {
			t.Errorf("app.js references %q but en.json has no such key", k)
		}
	}
}
