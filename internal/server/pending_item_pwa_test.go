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

// pendingItemPWABlock returns the part of app.js that draws the pending item
// card, its add form, attach control and hold badge: the text between the
// pending item card header and the progress card header.
func pendingItemPWABlock(t *testing.T) string {
	t.Helper()
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("read app.js from embed: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "// ── Automata pending item card")
	end := strings.Index(src, "// ── Automata progress card")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("app.js has no pending item card block (start=%d end=%d)", start, end)
	}
	return src[start:end]
}

// pendingItemPWAKeys lists every pending_item_* locale key app.js names as a
// string literal.
func pendingItemPWAKeys(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("read app.js from embed: %v", err)
	}
	found := map[string]bool{}
	for _, m := range regexp.MustCompile(`'(pending_item_[a-z_]+)'`).FindAllStringSubmatch(string(raw), -1) {
		found[m[1]] = true
	}
	return found
}

// pendingItemPWACardKeys are the keys the card, the add form, the attach
// control, the hold badge and the insert picker need. A key listed here must
// be named in app.js and carried by all five bundles.
var pendingItemPWACardKeys = []string{
	"pending_item_card_title", "pending_item_help_link", "pending_item_policy_line", "pending_item_auto_resume_line",
	"pending_item_on", "pending_item_off", "pending_item_registry_none", "pending_item_registry_line",
	"pending_item_refresh_never", "pending_item_refresh_line", "pending_item_refresh_error", "pending_item_refresh_button",
	"pending_item_refreshed_toast", "pending_item_refresh_failed", "pending_item_held_heading", "pending_item_held_row",
	"pending_item_since", "pending_item_reason", "pending_item_items_heading", "pending_item_empty", "pending_item_ref",
	"pending_item_state_waiting", "pending_item_state_attached", "pending_item_state_done",
	"pending_item_source_internal", "pending_item_source_file", "pending_item_attached_to", "pending_item_in_automations",
	"pending_item_attach_label", "pending_item_attach_placeholder", "pending_item_attach_button", "pending_item_attach_required",
	"pending_item_attached_toast", "pending_item_attach_failed", "pending_item_add_heading", "pending_item_add_title_label",
	"pending_item_add_ref_label", "pending_item_add_button", "pending_item_add_title_required", "pending_item_added_toast",
	"pending_item_add_failed", "pending_item_failed", "pending_item_badge", "pending_item_badge_tip",
	"pending_item_member_fallback", "pending_item_kind_label", "pending_item_field_title", "pending_item_field_ref",
	"pending_item_field_state", "pending_item_insert_heading", "pending_item_insert_none", "pending_item_insert_reason_in_use",
}

// TestPendingItemPWAEveryStringIsALocaleKey checks that the card, the add form,
// the attach control, the hold badge and the insert picker take every word they
// show from a locale key: each key app.js names exists in all five bundles, each
// key the PWA needs is named in app.js, and the drawing code carries no English
// text of its own (no literal text between tags and no literal title,
// placeholder or label attribute).
func TestPendingItemPWAEveryStringIsALocaleKey(t *testing.T) {
	used := pendingItemPWAKeys(t)
	for _, k := range pendingItemPWACardKeys {
		if !used[k] {
			t.Errorf("app.js does not name locale key %q", k)
		}
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for k := range used {
			if v, ok := bundle[k]; !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locale %s is missing %q, which app.js uses", lang, k)
			}
		}
		for _, k := range pendingItemPWACardKeys {
			if v, ok := bundle[k]; !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locale %s is missing %q", lang, k)
			}
		}
	}
	// The non-English bundles hold the English text until translations arrive.
	en := loadLocaleBundle(t, "en")
	for _, lang := range []string{"de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range pendingItemPWACardKeys {
			if bundle[k] != en[k] {
				t.Errorf("locale %s key %q is %q, want the English text %q until translations arrive", lang, k, bundle[k], en[k])
			}
		}
	}

	block := pendingItemPWABlock(t)
	// Drop every ${...} expression, then look for letters left as markup text.
	stripped := regexp.MustCompile(`\$\{[^{}]*\}`).ReplaceAllString(block, "")
	for _, m := range regexp.MustCompile(`>([^<>\n]*[A-Za-z]{2,}[^<>\n]*)<`).FindAllStringSubmatch(stripped, -1) {
		t.Errorf("the card code draws literal text %q; take it from a locale key", m[1])
	}
	for _, m := range regexp.MustCompile(`(?:placeholder|title|aria-label|alt)="([^"$]*[A-Za-z][^"]*)"`).FindAllStringSubmatch(stripped, -1) {
		t.Errorf("the card code draws the literal attribute text %q; take it from a locale key", m[1])
	}
}

// pendingItemTrackerIDRe matches a tracker id such as BL123 or GH#45.
var pendingItemTrackerIDRe = regexp.MustCompile(`\b(?:BL|GH#?)\d{2,}\b`)

// TestPendingItemPWATerminologyNoPRD checks that nothing the pending item PWA
// shows says PRD or carries a tracker id: not a locale value of the keys it
// uses, in any of the five bundles, and not a word of the drawing code.
func TestPendingItemPWATerminologyNoPRD(t *testing.T) {
	keys := pendingItemPWAKeys(t)
	for _, k := range pendingItemPWACardKeys {
		keys[k] = true
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for k := range keys {
			v := bundle[k]
			if prdLeakRe.MatchString(v) {
				t.Errorf("locale %s key %q says PRD: %q", lang, k, v)
			}
			if pendingItemTrackerIDRe.MatchString(v) {
				t.Errorf("locale %s key %q carries a tracker id: %q", lang, k, v)
			}
		}
	}
	block := pendingItemPWABlock(t)
	if loc := prdLeakRe.FindStringIndex(block); loc != nil {
		t.Errorf("the pending item card code says PRD near %q", block[max(0, loc[0]-30):min(len(block), loc[1]+30)])
	}
	if loc := pendingItemTrackerIDRe.FindStringIndex(block); loc != nil {
		t.Errorf("the pending item card code carries a tracker id near %q", block[max(0, loc[0]-30):min(len(block), loc[1]+30)])
	}
}
