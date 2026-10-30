// PWA wiring for the security failure setting in the Settings card (the
// only key of the block: mode). The field must be present in
// GENERAL_CONFIG_FIELDS in app.js and its label key must be present in
// every locale bundle with a non-empty value free of the PRD word — the
// same pattern as the other off/shadow/on mode settings (lane fill,
// repo_ci_watch).

package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var securityFailurePRDWord = regexp.MustCompile(`\bPRDs?\b`)

func TestSecurityFailurePWA(t *testing.T) {
	src, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	appJS := string(src)

	const fieldKey = "autonomous.automation.security_failure.mode"
	if !strings.Contains(appJS, fieldKey) {
		t.Errorf("app.js (GENERAL_CONFIG_FIELDS) does not contain the security failure config key %q", fieldKey)
	}
	const labelKey = "settings_security_failure_mode"
	if !strings.Contains(appJS, labelKey) {
		t.Errorf("app.js does not use the locale label key %q", labelKey)
	}
	// The field is a select over exactly the three legal values, and its
	// field default is the effective default (shadow) — the one default is
	// applied in the EffectiveMode accessor, and the field's default
	// mirrors that same value for the card.
	for _, want := range []string{
		"type: 'select', options: ['off', 'shadow', 'on'], default: 'shadow'",
	} {
		fieldIdx := strings.Index(appJS, "key: '"+fieldKey+"'")
		if fieldIdx < 0 {
			continue
		}
		fieldLine := strings.SplitN(appJS[fieldIdx:], "\n", 2)[0]
		if !strings.Contains(fieldLine, want) {
			t.Errorf("app.js security failure field line does not contain %q (line: %q)", want, fieldLine)
		}
	}

	// The label key must be present in every locale bundle with a
	// non-empty value free of the PRD word.
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		value, ok := bundle[labelKey]
		if !ok {
			t.Errorf("locale %s: missing the security failure label key %q", lang, labelKey)
			continue
		}
		if strings.TrimSpace(value) == "" {
			t.Errorf("locale %s: key %q has an empty value", lang, labelKey)
		}
		if securityFailurePRDWord.MatchString(value) {
			t.Errorf("locale %s: key %q value uses PRD: %q", lang, labelKey, value)
		}
	}
}
