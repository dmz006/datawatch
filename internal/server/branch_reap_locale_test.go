// Guards the branch-reap report copy in every shipped locale. The PWA branch
// reaper card (Automata Events sub-tab) draws its strings from the
// branch_reap_* locale keys; they must carry a non-empty value in all five
// bundles, and every non-English bundle must keep the English positional
// placeholders so t() never renders an empty slot.

package server

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestReapBranchReapLocaleKeysInAllBundles(t *testing.T) {
	reapKeys := []string{
		"branch_reap_card_title",
		"branch_reap_preview_note",
		"branch_reap_computed_at",
		"branch_reap_retention_days",
		"branch_reap_retention_default",
		"branch_reap_reaped_heading",
		"branch_reap_reaped_empty",
		"branch_reap_waiting_heading",
		"branch_reap_waiting_empty",
		"branch_reap_empty",
		"branch_reap_automaton",
		"branch_reap_contained",
		"branch_reap_commits",
		"branch_reap_backup",
		"branch_reap_backups_heading",
		"branch_reap_backups_none",
		"branch_reap_restore_btn",
		"branch_reap_restoring",
		"branch_reap_restore_ok",
		"branch_reap_restore_err",
		"branch_reap_failed",
	}

	placeholderRe := regexp.MustCompile(`%\d+\$[sd]`)
	placeholderSet := func(value string) string {
		found := placeholderRe.FindAllString(value, -1)
		sort.Strings(found)
		return strings.Join(found, ",")
	}

	bundles := map[string]map[string]string{}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundles[lang] = loadLocaleBundle(t, lang)
	}

	for _, key := range reapKeys {
		en, ok := bundles["en"][key]
		if !ok || strings.TrimSpace(en) == "" {
			t.Errorf("en.json: key %s must exist with a non-empty value", key)
		}
		for _, lang := range []string{"de", "es", "fr", "ja"} {
			value, ok := bundles[lang][key]
			if !ok || strings.TrimSpace(value) == "" {
				t.Errorf("%s.json: key %s is missing or empty (English: %q)", lang, key, en)
				continue
			}
			if got, want := placeholderSet(value), placeholderSet(en); got != want {
				t.Errorf("%s.json: key %s placeholders %q; English has %q", lang, key, got, want)
			}
		}
	}
}
