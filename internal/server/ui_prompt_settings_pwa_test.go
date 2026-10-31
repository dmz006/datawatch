// PWA coverage for the UI prompt settings in the Settings → Sessions card:
// the four settings rows (the dismiss toggle plus the dismiss delay, the
// clear bound and the max attempts), each carrying its labelKey, its help
// link table entry into the definitions manual and the manual heading that
// entry points at, the client-side whole-number guard, and the locale keys
// in all five bundles. app.js and the manual are read as text through the
// web embed.

package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const uiPromptDocsAnchor = "claude-code-interstitial-prompts"

// uiPromptSettings pairs each config key with the label key and the control
// type its settings row must carry.
var uiPromptSettings = []struct {
	configKey string
	labelKey  string
	rowType   string
}{
	{"session.dismiss_ui_prompts", "settings_dismiss_ui_prompts", "toggle"},
	{"session.ui_prompt_dismiss_delay_seconds", "settings_ui_prompt_dismiss_delay_seconds", "number"},
	{"session.ui_prompt_clear_bound_seconds", "settings_ui_prompt_clear_bound_seconds", "number"},
	{"session.ui_prompt_max_attempts", "settings_ui_prompt_max_attempts", "number"},
}

func TestUIPromptSettingsPWA_DocsLinksPointAtTheManualSection(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	start := strings.Index(src, "const DOCS_LINKS_BY_KEY = {")
	if start < 0 {
		t.Fatal("app.js has no DOCS_LINKS_BY_KEY table")
	}
	end := strings.Index(src[start:], "\n};")
	if end < 0 {
		t.Fatal("DOCS_LINKS_BY_KEY table does not close")
	}
	table := src[start : start+end]
	for _, s := range uiPromptSettings {
		want := s.labelKey + ": DOCS_DEFINITIONS + '#" + uiPromptDocsAnchor + "'"
		if !strings.Contains(table, want) {
			t.Errorf("DOCS_LINKS_BY_KEY has no entry %q", want)
		}
	}

	manual, err := os.ReadFile("../../docs/datawatch-definitions.md")
	if err != nil {
		t.Fatalf("read the definitions manual: %v", err)
	}
	nonSlug := regexp.MustCompile(`[^a-z0-9]+`)
	found := false
	for _, line := range strings.Split(string(manual), "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-") == uiPromptDocsAnchor {
			found = true
		}
	}
	if !found {
		t.Errorf("datawatch-definitions.md has no heading for anchor #%s", uiPromptDocsAnchor)
	}
}

func TestUIPromptSettingsPWA_SessionSectionCarriesTheFourRows(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	for _, s := range uiPromptSettings {
		marker := "{ key: '" + s.configKey + "',"
		line := settingsRowLine(t, src, s.configKey)
		if !strings.Contains(line, "labelKey: '"+s.labelKey+"'") {
			t.Errorf("row %q has no labelKey %q", marker, s.labelKey)
		}
		if !strings.Contains(line, "docsKey: '"+s.labelKey+"'") {
			t.Errorf("row %q has no docsKey %q", marker, s.labelKey)
		}
		if !strings.Contains(line, "type: '"+s.rowType+"'") {
			t.Errorf("row %q has no type %q", marker, s.rowType)
		}
		if !strings.Contains(line, "helpKey: '"+s.labelKey+"_help'") {
			t.Errorf("row %q has no helpKey %s_help", marker, s.labelKey)
		}
	}
}

func TestUIPromptSettingsPWA_NumberRowsGuardsUseTheDaemonFloors(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	for _, guard := range []string{
		"function savePromptNumberSetting(key, rawValue, min)",
		"Number.isInteger(n)",
		"window.savePromptNumberSetting = savePromptNumberSetting",
		"savePromptNumberSetting('session.ui_prompt_dismiss_delay_seconds', this.value, 0)",
		"savePromptNumberSetting('session.ui_prompt_clear_bound_seconds', this.value, 1)",
		"savePromptNumberSetting('session.ui_prompt_max_attempts', this.value, 1)",
	} {
		if !strings.Contains(src, guard) {
			t.Errorf("app.js has no %q", guard)
		}
	}

	delay := settingsRowLine(t, src, "session.ui_prompt_dismiss_delay_seconds")
	if !strings.Contains(delay, "min: 0") {
		t.Errorf("the dismiss delay row does not carry min: 0")
	}
	for _, configKey := range []string{"session.ui_prompt_clear_bound_seconds", "session.ui_prompt_max_attempts"} {
		row := settingsRowLine(t, src, configKey)
		if !strings.Contains(row, "min: 1") {
			t.Errorf("row %q does not carry min: 1", configKey)
		}
	}
}

func TestUIPromptSettingsPWA_AllFiveBundlesCarryTheEightKeys(t *testing.T) {
	en := loadLocaleBundle(t, "en")
	for _, s := range uiPromptSettings {
		for _, key := range []string{s.labelKey, s.labelKey + "_help"} {
			if val, ok := en[key]; !ok || val == "" {
				t.Errorf("en bundle has no %q", key)
			}
		}
	}
	for _, lang := range requiredLocales {
		if lang == "en" {
			continue
		}
		bundle := loadLocaleBundle(t, lang)
		for _, s := range uiPromptSettings {
			for _, key := range []string{s.labelKey, s.labelKey + "_help"} {
				if val, ok := bundle[key]; !ok || val == "" {
					t.Errorf("locale %s has no %q", lang, key)
				} else if val == en[key] {
					t.Errorf("locale %s %q is an English copy", lang, key)
				}
			}
		}
	}
}

func TestUIPromptSettingsPWA_NoTrackerIDsInTheNewStrings(t *testing.T) {
	en := loadLocaleBundle(t, "en")
	identifiers := regexp.MustCompile(`V\d+\.\d+[0-9.]*|[A-Z]{2,3}-?\d{3,}|\bBL\d+\b`)
	for _, s := range uiPromptSettings {
		for _, key := range []string{s.labelKey, s.labelKey + "_help"} {
			val := en[key]
			if identifiers.MatchString(val) {
				t.Errorf("en %q looks like a tracker id: %q", key, val)
			}
		}
	}
}
