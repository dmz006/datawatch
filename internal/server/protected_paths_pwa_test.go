// PWA wiring for protected paths: the card in the Automata overview that polls
// GET /api/autonomous/protected-paths, the chip, task paths and banner on the
// Automaton detail, the Authorised paths editor, the three settings fields, the
// docs links and the locale keys behind every visible string. The markup is
// pinned by app-protected-paths.test.js; this file pins that app.js is wired to
// the route, the settings, the docs links and the bundles, and that all five
// bundles carry the same keys with no PRD wording.

package server

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

const protectedPathsHelpAnchor = "/diagrams.html#docs/datawatch-definitions.md#protected-paths"

var (
	protectedPathsPRDWord       = regexp.MustCompile(`\bPRDs?\b`)
	protectedPathsTrackerIDWord = regexp.MustCompile(`\b(?:BL|F|B|S)\d{1,6}\b`)

	// protectedPathsRequiredKeys are the keys every bundle must carry.
	protectedPathsRequiredKeys = []string{
		"protected_paths_card_title", "protected_paths_read_only", "protected_paths_empty",
		"protected_paths_held_task", "protected_paths_held_automaton", "protected_paths_chip_tip",
		"protected_paths_banner", "protected_paths_help_link", "protected_paths_open_question",
		"protected_paths_authorised_title", "protected_paths_authorised_save",
		"settings_automation_protected_paths_group", "settings_automation_protected_paths",
		"settings_automation_protected_paths_action", "settings_automation_rules_review_source",
		"protected_paths_action_hold", "protected_paths_action_warn", "protected_paths_action_block",
		"protected_paths_action_off", "rules_review_source_base_commit", "rules_review_source_worktree",
	}
)

// protectedPathsKeys returns the sorted protected-paths keys of one bundle.
func protectedPathsKeys(t *testing.T, lang string) []string {
	t.Helper()
	var keys []string
	for k := range loadLocaleBundle(t, lang) {
		if strings.Contains(k, "protected_paths") || strings.HasPrefix(k, "rules_review_source_") || k == "settings_automation_rules_review_source" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func TestProtectedPathsPWA(t *testing.T) {
	appJS := uncommittedAppJS(t)

	t.Run("card, loader and detail wiring", func(t *testing.T) {
		for _, want := range []string{
			// the card reads the route through the shared list loader
			"url: '/api/autonomous/protected-paths'",
			"key: 'automation_protected_paths'",
			"el: 'automationProtectedPathsCard'",
			"function _automationProtectedPathsLoader()",
			"window.protectedPathsCardHtml(status)",
			// the slot is in the Housekeeping body and the loader is mounted and stopped with its neighbours
			`id="automationProtectedPathsCard"`,
			"_automationUncommittedWorkLoader(), _automationProtectedPathsLoader()",
			"'automation_uncommitted_work', 'automation_protected_paths'",
			// the Automaton detail: task chip, task paths, Automaton banner, authorised editor
			"window.protectedPathsChipHtml(task)",
			"window.protectedPathsTaskHtml(task)",
			"window.protectedPathsBannerHtml(prd)",
			"window.protectedPathsAuthorisedHtml(prd)",
			"window.protectedPathsSaveAuthorised",
			"/set_authorized_paths",
			// the question button is the uncommitted-work helper
			"_uncommittedWorkQuestionButton(h.question_id, 'protected_paths_open_question')",
			protectedPathsHelpAnchor,
		} {
			if !strings.Contains(appJS, want) {
				t.Errorf("app.js does not contain %q", want)
			}
		}

		// A poll belongs to the list loader's registry, never to a bare
		// interval of its own (the PWA poll-stacking rule).
		start := strings.Index(appJS, "// ── Automata protected-paths card")
		end := strings.Index(appJS, "// ── Automata uncommitted-work card")
		if start < 0 || end < start {
			t.Fatal("app.js has no protected-paths card section")
		}
		if section := appJS[start:end]; strings.Contains(section, "setInterval") {
			t.Error("the protected-paths card must poll through listLoader, not its own setInterval")
		}
	})

	t.Run("DOCS_LINKS_BY_KEY entries", func(t *testing.T) {
		for key, anchor := range map[string]string{
			"settings_protected_paths_group":  "#protected-paths",
			"settings_protected_paths":        "#protected-paths",
			"settings_protected_paths_action": "#protected-paths-action",
			"settings_rules_review_source":    "#rules-review-source",
			"automaton_authorised_paths":      "#authorised-paths",
		} {
			want := key + ": DOCS_DEFINITIONS + '" + anchor + "'"
			if !strings.Contains(appJS, want) {
				t.Errorf("DOCS_LINKS_BY_KEY does not contain %q", want)
			}
		}
	})

	t.Run("settings fields", func(t *testing.T) {
		for _, want := range []string{
			"labelKey: 'settings_automation_protected_paths_group', docsKey: 'settings_protected_paths_group', type: 'group'",
			"key: 'autonomous.automation.protected_paths', ",
			"labelKey: 'settings_automation_protected_paths', docsKey: 'settings_protected_paths', type: 'text', csv: true",
			"key: 'autonomous.automation.protected_paths_action'",
			"labelKey: 'settings_automation_protected_paths_action', docsKey: 'settings_protected_paths_action'",
			"options: ['hold', 'warn', 'block', 'off'], default: 'hold'",
			"key: 'autonomous.automation.rules_review_source'",
			"labelKey: 'settings_automation_rules_review_source', docsKey: 'settings_rules_review_source'",
			"options: ['base_commit', 'worktree'], default: 'base_commit'",
		} {
			if !strings.Contains(appJS, want) {
				t.Errorf("app.js (GENERAL_CONFIG_FIELDS) does not contain %q", want)
			}
		}

		// Each field's fallback label equals its English bundle text, as the
		// other settings fields do.
		en := loadLocaleBundle(t, "en")
		for _, k := range []string{
			"settings_automation_protected_paths_group",
			"settings_automation_protected_paths",
			"settings_automation_protected_paths_action",
			"settings_automation_rules_review_source",
		} {
			v, ok := en[k]
			if !ok {
				t.Errorf("en.json missing %q", k)
				continue
			}
			if !strings.Contains(appJS, "label: '"+v+"'") {
				t.Errorf("app.js fallback label for %q does not equal the English text %q", k, v)
			}
		}
	})

	t.Run("locale keys in every bundle", func(t *testing.T) {
		enKeys := protectedPathsKeys(t, "en")
		for _, k := range protectedPathsRequiredKeys {
			if _, ok := loadLocaleBundle(t, "en")[k]; !ok {
				t.Errorf("en.json missing required key %q", k)
			}
		}
		if len(enKeys) < len(protectedPathsRequiredKeys) {
			t.Fatalf("en.json carries %d protected-paths keys, want at least %d", len(enKeys), len(protectedPathsRequiredKeys))
		}

		en := loadLocaleBundle(t, "en")
		for _, lang := range requiredLocales {
			keys := protectedPathsKeys(t, lang)
			if strings.Join(keys, "\n") != strings.Join(enKeys, "\n") {
				t.Errorf("locale %s protected-paths key set differs from en: got %v, want %v", lang, keys, enKeys)
			}
			bundle := loadLocaleBundle(t, lang)
			for _, k := range enKeys {
				v := bundle[k]
				if strings.TrimSpace(v) == "" {
					t.Errorf("locale %s: key %q is empty", lang, k)
				}
				if protectedPathsTrackerIDWord.MatchString(v) {
					t.Errorf("locale %s: key %q carries a tracker id: %q", lang, k, v)
				}
				// Until translations arrive the other bundles carry the English text.
				if lang != "en" && v != en[k] {
					t.Errorf("locale %s: key %q differs from the English text", lang, k)
				}
			}
		}

		// Every key is used by the PWA as a literal, so none is orphaned.
		for _, k := range enKeys {
			if !strings.Contains(appJS, "'"+k+"'") {
				t.Errorf("app.js never uses locale key %q", k)
			}
		}
	})

	t.Run("no visible string says PRD", func(t *testing.T) {
		for _, lang := range requiredLocales {
			bundle := loadLocaleBundle(t, lang)
			for _, k := range protectedPathsKeys(t, lang) {
				if protectedPathsPRDWord.MatchString(bundle[k]) {
					t.Errorf("locale %s: key %q mentions PRD: %q", lang, k, bundle[k])
				}
			}
		}

		// The PWA code of this feature builds visible text only through t(key) and
		// the bundle, so no string literal in its section may carry the word either.
		start := strings.Index(appJS, "// ── Automata protected-paths card")
		end := strings.Index(appJS, "// ── Automata uncommitted-work card")
		if start < 0 || end < start {
			t.Fatal("app.js has no protected-paths card section")
		}
		literal := regexp.MustCompile("(?s)'[^'\\n]*'|\"[^\"\\n]*\"|`[^`]*`")
		for _, lit := range literal.FindAllString(appJS[start:end], -1) {
			// A JSON key or identifier such as prd (a data attribute, a request path)
			// is not visible text; only a capitalised PRD word is.
			if protectedPathsPRDWord.MatchString(lit) {
				t.Errorf("a string literal in the protected-paths section mentions PRD: %s", lit)
			}
		}
	})
}
