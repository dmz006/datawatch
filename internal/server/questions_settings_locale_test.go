// Locale coverage for the operator-questions-for-Automata settings toggle:
// the label key must exist in every embedded locale bundle with a
// non-empty value, and must never use PRD or the Automation word (the
// Automation word is reserved for automation_ keys and is a different
// object than Automata). The settings entry itself must be wired into
// the General config card in app.js.

package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

const settingsKeyQuestionsEnabled = "settings_automata_questions_enabled"

var prdWord = regexp.MustCompile(`\bPRDs?\b`)

func TestQuestionsSettings_LocaleKeyInAllBundles(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		value, ok := bundle[settingsKeyQuestionsEnabled]
		if !ok {
			t.Errorf("locale %s: missing key %q", lang, settingsKeyQuestionsEnabled)
			continue
		}
		if strings.TrimSpace(value) == "" {
			t.Errorf("locale %s: key %q has an empty value", lang, settingsKeyQuestionsEnabled)
		}
		if prdWord.MatchString(value) {
			t.Errorf("locale %s: key %q value uses PRD: %q", lang, settingsKeyQuestionsEnabled, value)
		}
		if strings.Contains(value, "Automation") {
			t.Errorf("locale %s: key %q value uses the Automation word: %q", lang, settingsKeyQuestionsEnabled, value)
		}
	}

	appJS, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	src := string(appJS)
	for _, want := range []string{"autonomous.questions.enabled", settingsKeyQuestionsEnabled} {
		if !strings.Contains(src, want) {
			t.Errorf("app.js does not contain %q", want)
		}
	}
}
