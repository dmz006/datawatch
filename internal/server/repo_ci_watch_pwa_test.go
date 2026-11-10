// PWA wiring for the repo CI watch in the Automaton view.
// The always-visible repo CI status line under the Automata sub-tab bar
// has been removed. The server endpoint GET /api/autonomous/repo-ci-watch
// and the eight repo_ci_watch settings are kept. This pins down the removal
// on the client side: app.js must no longer poll the endpoint, hold a poll
// interval, or render the status line; the locale keys stay in every bundle
// (they are not deleted in this change); and the eight settings must remain
// in GENERAL_CONFIG_FIELDS.

package server

import (
	"io/fs"
	"strings"
	"testing"
)

// repoCIWatchLocaleKeys are the repo_ci_watch_* keys the removed status
// line used. They stay in the locale bundles for now; this test only checks
// that app.js no longer references them.
var repoCIWatchLocaleKeys = []string{
	"repo_ci_watch_status_title",
	"repo_ci_watch_prefix",
	"repo_ci_watch_mode_off",
	"repo_ci_watch_mode_shadow",
	"repo_ci_watch_mode_on",
	"repo_ci_watch_failing",
	"repo_ci_watch_pending",
	"repo_ci_watch_none_open",
	"repo_ci_watch_last_poll",
	"repo_ci_watch_last_poll_never",
	"repo_ci_watch_age_just_now",
	"repo_ci_watch_age_seconds",
	"repo_ci_watch_age_minutes",
	"repo_ci_watch_age_hours",
	"repo_ci_watch_age_days",
}

// repoCIWatchConfigKeys are the eight repo_ci_watch settings the PWA's
// settings card binds: the autonomous.automation.repo_ci_watch.* family.
var repoCIWatchConfigKeys = []string{
	"autonomous.automation.repo_ci_watch.mode",
	"autonomous.automation.repo_ci_watch.repos",
	"autonomous.automation.repo_ci_watch.poll_seconds",
	"autonomous.automation.repo_ci_watch.alert_minutes",
	"autonomous.automation.repo_ci_watch.tag_count",
	"autonomous.automation.repo_ci_watch.hung_after_minutes",
	"autonomous.automation.repo_ci_watch.security_workflows",
	"autonomous.automation.repo_ci_watch.delete_superseded",
}

func TestRepoCIWatchPWAStatusLineRemoved(t *testing.T) {
	src, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	appJS := string(src)

	// The status line must not poll the read-only endpoint or hold a poll
	// interval, and its helpers and markup id must be gone.
	for _, gone := range []string{
		"'/api/autonomous/repo-ci-watch'",
		"window._repoCIWatchInterval",
		"repoCIWatchStartPolling",
		"repoCIWatchStopPolling",
		"repoCIWatchRefreshStatusLine",
		"repoCIWatchRenderLine",
		"repoCIWatchStatusLine",
	} {
		if strings.Contains(appJS, gone) {
			t.Errorf("app.js still contains removed repo CI status line wiring %q", gone)
		}
	}

	// No t() call may still pass a status-line key (or the mode-prefix
	// family the line built dynamically).
	for _, key := range repoCIWatchLocaleKeys {
		for _, form := range []string{"'" + key + "'", "'repo_ci_watch_mode_'"} {
			if strings.Contains(appJS, form) {
				t.Errorf("app.js still references removed status line locale key %s", form)
			}
		}
	}

	// Locale keys are kept in every bundle in this change. Keys are not
	// deleted here, so they must still be present (non-empty).
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range repoCIWatchLocaleKeys {
			if strings.TrimSpace(bundle[key]) == "" {
				t.Errorf("locale %s: key %q missing or empty (keys are not deleted in this change)", lang, key)
			}
		}
	}

	// The eight settings keys must still be wired into GENERAL_CONFIG_FIELDS.
	for _, key := range repoCIWatchConfigKeys {
		if !strings.Contains(appJS, key) {
			t.Errorf("app.js (GENERAL_CONFIG_FIELDS) does not contain repo_ci_watch config key %q", key)
		}
	}
}
