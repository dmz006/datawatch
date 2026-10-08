// v8.73.25 (datawatch#195) — every literal t('some_key') call site in
// app.js must have a matching key in the EN locale bundle. Without this,
// a key can be referenced in the PWA and silently never added to any
// locale bundle: t() returns the raw key string (not undefined), so the
// `t('key') || 'fallback'` pattern used throughout app.js never falls
// through to its English fallback — the operator sees the literal key
// name rendered (uppercased by CSS) instead of real text. Found live via
// lifecycle_hint_needs_review, which shipped missing from all 5 bundles.
package server

import (
	"io/fs"
	"regexp"
	"sort"
	"testing"
)

// Matches t('key') / t("key") call sites, excluding identifiers that merely
// end in "t(" (format(, await(, etc.) by requiring the preceding character
// not be a letter, digit, underscore or dollar sign.
var tCallKeyRe = regexp.MustCompile(`(?:^|[^a-zA-Z0-9_$])t\(\s*['"]([a-zA-Z0-9_]+)['"]`)

func TestLocales_AllAppJSKeysExistInEnglishBundle(t *testing.T) {
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("could not read web/app.js from embed: %v", err)
	}
	en := loadLocaleBundle(t, "en")

	// Call sites that build the real key by concatenating a literal prefix
	// with a runtime value (e.g. t('automata_status_' + s)) match the regex
	// as a truncated literal -- that's a dynamic key, not a missing one.
	dynamicPrefixes := map[string]bool{
		"automata_status_":  true, // + prd/story/task status string
		"chat_quick_reply_": true, // + lowercased quick-reply label
	}

	seen := map[string]bool{}
	for _, m := range tCallKeyRe.FindAllStringSubmatch(string(raw), -1) {
		seen[m[1]] = true
	}
	if len(seen) < 100 {
		t.Fatalf("only found %d t('...') call sites -- regex likely broken, not that app.js shrank", len(seen))
	}

	var missing []string
	for key := range seen {
		if dynamicPrefixes[key] {
			continue
		}
		if _, ok := en[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d key(s) used via t('...') in app.js have no entry in locales/en.json (will render as the literal, uppercased key name instead of any fallback text): %v", len(missing), missing)
	}
}
