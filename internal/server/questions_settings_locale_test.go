// Locale coverage for the operator-questions-for-Automata settings toggles:
// every label key must exist in every embedded locale bundle with a
// non-empty value, and must never use PRD or the Automation word (the
// Automation word is reserved for automation_ keys and is a different
// object than Automata). The settings entries themselves must be wired
// into the General config card in app.js, and the question_raised
// WebSocket frame must be acknowledged (an explicit no-op) rather than
// falling through silently.

package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// All four operator-questions settings keys in the General config card.
var questionSettingsLocaleKeys = []string{
	"settings_automata_questions_enabled",
	"settings_automata_questions_notify_messaging",
	"settings_automata_questions_notify_alert",
	"settings_automata_questions_return_path",
}

// The three notification/return-path keys added alongside the master
// switch.
var questionNotifySettingsLocaleKeys = []string{
	"settings_automata_questions_notify_messaging",
	"settings_automata_questions_notify_alert",
	"settings_automata_questions_return_path",
}

// The config keys the General card toggles bind to.
var questionSettingsConfigKeys = []string{
	"autonomous.questions.enabled",
	"autonomous.questions.notify_messaging",
	"autonomous.questions.notify_alert",
	"autonomous.questions.return_path",
}

var prdWord = regexp.MustCompile(`\bPRDs?\b`)

func checkQuestionSettingValue(t *testing.T, lang, key, value string) {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		t.Errorf("locale %s: key %q has an empty value", lang, key)
	}
	if prdWord.MatchString(value) {
		t.Errorf("locale %s: key %q value uses PRD: %q", lang, key, value)
	}
	if strings.Contains(value, "Automation") {
		t.Errorf("locale %s: key %q value uses the Automation word: %q", lang, key, value)
	}
}

func TestQuestionsSettings_LocaleKeyInAllBundles(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range questionSettingsLocaleKeys {
			value, ok := bundle[key]
			if !ok {
				t.Errorf("locale %s: missing key %q", lang, key)
				continue
			}
			checkQuestionSettingValue(t, lang, key, value)
		}
	}

	appJS, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	src := string(appJS)
	for _, want := range append(append([]string{}, questionSettingsConfigKeys...), questionSettingsLocaleKeys...) {
		if !strings.Contains(src, want) {
			t.Errorf("app.js does not contain %q", want)
		}
	}
	// The question_raised frame must be an explicit no-op in the message
	// handler, never an error or an accidental render.
	if !strings.Contains(src, "'question_raised'") {
		t.Errorf("app.js does not handle the 'question_raised' message type")
	}
}

func TestQuestionNotifySettingsLocale(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range questionNotifySettingsLocaleKeys {
			value, ok := bundle[key]
			if !ok {
				t.Errorf("locale %s: missing key %q", lang, key)
				continue
			}
			checkQuestionSettingValue(t, lang, key, value)
		}
	}

	appJS, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	src := string(appJS)
	for _, want := range append([]string{
		"autonomous.questions.notify_messaging",
		"autonomous.questions.notify_alert",
		"autonomous.questions.return_path",
	}, questionNotifySettingsLocaleKeys...) {
		if !strings.Contains(src, want) {
			t.Errorf("app.js does not contain %q", want)
		}
	}
}
