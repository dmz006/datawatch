// v8.73.33 (GH#193) — guards the AGENT.md Terminology Rule ("Automata"/
// "Automaton", never "PRD", never left untranslated, never swapped for a
// different word like "Automation"): no de/es/fr/ja locale value may
// contain the literal English words "Automaton"/"Automata", nor the
// "Automation"/"automatización"/"automatisation"/"オートメーション" mistranslation
// for this feature. Found live: 123 strings across the 4 non-English
// bundles still carried the untranslated or mistranslated word.
package server

import (
	"regexp"
	"testing"
)

var automatonLeakRe = regexp.MustCompile(`(?i)\bAutomat(a|on)s?\b`)

// perLocaleWrongWord catches the feature mistranslated as a *different*
// word (automation/automatización/automatisation/オートメーション) rather than
// left untranslated.
var perLocaleWrongWord = map[string]*regexp.Regexp{
	"de": regexp.MustCompile(`Automatisierung`),
	"es": regexp.MustCompile(`automatización`),
	"fr": regexp.MustCompile(`automatisation`),
	"ja": regexp.MustCompile(`オートメーション`),
}

func TestLocales_AutomatonNeverUntranslatedOrMistranslated(t *testing.T) {
	for _, lang := range []string{"de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		wrongWord := perLocaleWrongWord[lang]
		for key, val := range bundle {
			if automatonLeakRe.MatchString(val) {
				t.Errorf("locale %s key %q still has the untranslated English word: %q", lang, key, val)
			}
			if wrongWord != nil && wrongWord.MatchString(val) {
				t.Errorf("locale %s key %q uses the wrong word for Automaton/Automata (automation, a different concept): %q", lang, key, val)
			}
		}
	}
}
