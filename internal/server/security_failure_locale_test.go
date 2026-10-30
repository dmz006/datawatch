// Locale coverage for the read-only security failure card in the Automation
// view: every automation_security_failure_ key the PWA reads through t()
// must exist in all five bundles with a non-empty value, no value may carry
// PRD wording, and the card must not hard-code English.

package server

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var securityFailureKeyRe = regexp.MustCompile(`['"](automation_security_failure_[a-z_]+)['"]`)

// securityFailureAppKeys returns every automation_security_failure_ key the
// PWA source names.
func securityFailureAppKeys(t *testing.T) []string {
	t.Helper()
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("could not read web/app.js from embed: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range securityFailureKeyRe.FindAllStringSubmatch(string(raw), -1) {
		seen[m[1]] = true
	}
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLocalesSecurityFailureKeysInAllBundles(t *testing.T) {
	keys := securityFailureAppKeys(t)
	if len(keys) < 20 {
		t.Fatalf("only %d automation_security_failure_ keys found in app.js", len(keys))
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range keys {
			v, ok := bundle[k]
			if !ok {
				t.Errorf("locale %s missing key %q", lang, k)
				continue
			}
			if strings.TrimSpace(v) == "" {
				t.Errorf("locale %s: key %q has an empty value", lang, k)
			}
		}
	}
}

func TestLocalesSecurityFailureNoPRDWording(t *testing.T) {
	for _, lang := range requiredLocales {
		for k, v := range loadLocaleBundle(t, lang) {
			if !strings.HasPrefix(k, "automation_security_failure_") {
				continue
			}
			if prdWord.MatchString(v) {
				t.Errorf("locale %s: key %q value carries PRD wording: %q", lang, k, v)
			}
		}
	}
}

func TestLocalesSecurityFailureNoOrphanKeys(t *testing.T) {
	used := map[string]bool{}
	for _, k := range securityFailureAppKeys(t) {
		used[k] = true
	}
	for _, lang := range requiredLocales {
		for k := range loadLocaleBundle(t, lang) {
			if strings.HasPrefix(k, "automation_security_failure_") && !used[k] {
				t.Errorf("locale %s: key %q is not read by app.js", lang, k)
			}
		}
	}
}

// TestSecurityFailureCardWiredThroughT pins the card's wiring: the loader
// reads the REST route, the slot sits beside the triage card, and the card
// helper carries no hard-coded English sentence of its own.
func TestSecurityFailureCardWiredThroughT(t *testing.T) {
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("could not read web/app.js from embed: %v", err)
	}
	src := string(raw)
	for _, want := range []string{
		"/api/autonomous/security-failures",
		`id="automationSecurityFailureCard"`,
		"window.securityFailureCardHtml = function",
		"window.securityFailureListHtml = function",
		"_automationSecurityFailureLoader()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("app.js does not contain %q", want)
		}
	}
	start := strings.Index(src, "window.securityFailureCardHtml = function")
	end := strings.Index(src, "window.securityFailureListHtml = function")
	if start < 0 || end < start {
		t.Fatal("security failure card helper not found")
	}
	card := src[start:end]
	for _, english := range []string{"Dependency:", "Advisory:", "Registry entry", "Recommendation:", "Published:", "Would have", "Action taken", "not in the registry"} {
		if strings.Contains(card, english) {
			t.Errorf("security failure card hard-codes English text %q", english)
		}
	}
	// No control that acts: the card helper emits no button, form or handler.
	for _, ctl := range []string{"<button", "<input", "<form", "onclick=", "apiFetch(", "method: 'POST'"} {
		if strings.Contains(card, ctl) {
			t.Errorf("security failure card carries an acting control %q", ctl)
		}
	}
}
