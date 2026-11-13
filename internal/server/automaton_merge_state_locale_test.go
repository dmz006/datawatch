// Automaton merge_state badge — locale coverage. The read-only merge_state
// field the daemon derives (internal/autonomous/archive_merge_guard.go) must
// have its operator key in all five embedded locale bundles, with no tracker
// IDs and no "PRD" wording in the operator-facing text.

package server

import (
	"regexp"
	"testing"
)

var mergeStateLocaleKeys = []string{
	"prd_merge_state_badge",
}

var mergeStateTrackerID = regexp.MustCompile(`\b(BL|GH|HLLM|SEC-|B)[0-9]+\b`)

func TestMergeStateLocale_AllBundlesHaveKey(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range mergeStateLocaleKeys {
			v, ok := bundle[k]
			if !ok {
				t.Errorf("locale %s missing key %q", lang, k)
				continue
			}
			if v == "" {
				t.Errorf("locale %s key %q is empty", lang, k)
			}
			if mergeStateTrackerID.MatchString(v) {
				t.Errorf("locale %s key %q value carries a tracker ID: %q", lang, k, v)
			}
			if regexp.MustCompile(`\bPRD\b`).MatchString(v) {
				t.Errorf("locale %s key %q value says PRD: %q", lang, k, v)
			}
		}
	}
}
