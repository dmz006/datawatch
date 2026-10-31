// PWA wiring for the uncommitted-work check: the card in the Automata overview
// that polls GET /api/autonomous/uncommitted-work, the chip and banner on the
// Automaton detail, the two settings fields and the locale keys behind every
// visible string. The markup itself is pinned by app-uncommitted-work.test.js;
// this file pins that app.js is wired to the route, the settings and the
// bundles, and that all five bundles carry one key set.

package server

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const uncommittedWorkHelpAnchor = "/diagrams.html#docs/datawatch-definitions.md#uncommitted-work-check"

var (
	uncommittedPRDWord        = regexp.MustCompile(`\bPRDs?\b`)
	uncommittedTrackerIDWord  = regexp.MustCompile(`\b(?:BL|F|B|S)\d{1,6}\b`)
	uncommittedBundleKeyCount = 8
)

func uncommittedAppJS(t *testing.T) string {
	t.Helper()
	src, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	return string(src)
}

// uncommittedKeys returns the sorted uncommitted_work keys of one bundle.
func uncommittedKeys(t *testing.T, lang string) []string {
	t.Helper()
	var keys []string
	for k := range loadLocaleBundle(t, lang) {
		if strings.Contains(k, "uncommitted_work") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func TestUncommittedPWAHasCardAndPoll(t *testing.T) {
	appJS := uncommittedAppJS(t)

	for _, want := range []string{
		// the card reads the route through the shared list loader
		"url: '/api/autonomous/uncommitted-work'",
		"key: 'automation_uncommitted_work'",
		"el: 'automationUncommittedWorkCard'",
		"function _automationUncommittedWorkLoader()",
		"window.uncommittedWorkCardHtml(status)",
		// the slot is in the Overview body and the loader is mounted and stopped with its neighbours
		`id="automationUncommittedWorkCard"`,
		"_automationCleanupLoader(), _automationUncommittedWorkLoader()]",
		"'automation_cleanup', 'automation_uncommitted_work'",
		// the Automaton detail: task chip, task file list, Automaton banner
		"window.uncommittedWorkChipHtml(task)",
		"window.uncommittedWorkTaskHtml(task)",
		"window.uncommittedWorkBannerHtml(prd)",
		// the link that opens the question
		"window.uncommittedWorkOpenQuestion",
		"window.openQuestion(qid)",
		// the help link target agrees with the documentation heading
		uncommittedWorkHelpAnchor,
		"settings_uncommitted_work_check: DOCS_DEFINITIONS + '#uncommitted-work-check'",
	} {
		if !strings.Contains(appJS, want) {
			t.Errorf("app.js does not contain %q", want)
		}
	}

	// A poll belongs to the list loader's registry, never to a bare interval
	// of its own (the PWA poll-stacking rule).
	start := strings.Index(appJS, "// ── Automata uncommitted-work card")
	end := strings.Index(appJS, "// ── Automata progress card")
	if start < 0 || end < start {
		t.Fatal("app.js has no uncommitted-work card section")
	}
	if section := appJS[start:end]; strings.Contains(section, "setInterval") {
		t.Error("the uncommitted-work card must poll through listLoader, not its own setInterval")
	}
}

func TestUncommittedPWASettingsFields(t *testing.T) {
	appJS := uncommittedAppJS(t)

	for _, want := range []string{
		"key: 'autonomous.automation.uncommitted_work_policy'",
		"labelKey: 'settings_automation_uncommitted_work_policy'",
		"options: ['hold', 'warn', 'off'], default: 'hold'",
		"key: 'autonomous.automation.uncommitted_work_scope'",
		"labelKey: 'settings_automation_uncommitted_work_scope'",
		"options: ['task_files', 'worktree'], default: 'task_files'",
		// the settings group carries the help link
		"labelKey: 'settings_automation_uncommitted_work_group'",
		"docsKey: 'settings_uncommitted_work_check'",
	} {
		if !strings.Contains(appJS, want) {
			t.Errorf("app.js (GENERAL_CONFIG_FIELDS) does not contain %q", want)
		}
	}

	// Each field's fallback label equals its English bundle text, as the
	// other settings fields do.
	en := loadLocaleBundle(t, "en")
	for _, k := range []string{
		"settings_automation_uncommitted_work_policy",
		"settings_automation_uncommitted_work_scope",
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
}

func TestUncommittedLocaleKeysInAllBundles(t *testing.T) {
	enKeys := uncommittedKeys(t, "en")
	if len(enKeys) < uncommittedBundleKeyCount {
		t.Fatalf("en.json carries %d uncommitted_work keys, want at least %d", len(enKeys), uncommittedBundleKeyCount)
	}
	appJS := uncommittedAppJS(t)

	for _, lang := range requiredLocales {
		keys := uncommittedKeys(t, lang)
		if strings.Join(keys, "\n") != strings.Join(enKeys, "\n") {
			t.Errorf("locale %s uncommitted_work key set differs from en: got %v, want %v", lang, keys, enKeys)
		}
		bundle := loadLocaleBundle(t, lang)
		for _, k := range enKeys {
			v := bundle[k]
			if strings.TrimSpace(v) == "" {
				t.Errorf("locale %s: key %q is empty", lang, k)
			}
			if uncommittedPRDWord.MatchString(v) {
				t.Errorf("locale %s: key %q mentions PRD: %q", lang, k, v)
			}
			if uncommittedTrackerIDWord.MatchString(v) {
				t.Errorf("locale %s: key %q carries a tracker id: %q", lang, k, v)
			}
			// Until translations arrive the other bundles carry the English text.
			if lang != "en" && v != loadLocaleBundle(t, "en")[k] {
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

}
