// PWA wiring for the repo CI watch status line in the Automaton view.
// The line is read-only: it polls GET /api/autonomous/repo-ci-watch and
// renders the mode in effect, the failing/pending count of the watched
// runs, and how long ago the watch last polled — all through localized
// key. When the endpoint is missing the line hides instead of showing a
// stale state, so this pins down the wiring end to end on the client
// side: every repo_ci_watch_* key used by this line must be present in
// every locale bundle with a non-empty value free of the PRD word, app.js
// must be the one reading the read-only endpoint (and keeping its poll
// interval on window so it can never stack), and the eight repo_ci_watch
// settings must be present in GENERAL_CONFIG_FIELDS.

package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// repoCIWatchLocaleKeys are the repo_ci_watch_* keys the status line
// passes to t() in app.js (the repo_ci_watch settings card labels use
// the parallel settings_repo_ci_watch_* family next to them in the same
// bundle).
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

var repoCIWatchPRDWord = regexp.MustCompile(`\bPRDs?\b`)

func TestRepoCIWatchPWA(t *testing.T) {
	src, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	appJS := string(src)

	// The line reads the read-only endpoint and holds its poll
	// interval on window (the PWA poll-stacking rule): re-render and
	// view-change must clear before setting it.
	for _, want := range []string{
		"'/api/autonomous/repo-ci-watch'",
		"window._repoCIWatchInterval",
		"clearInterval(window._repoCIWatchInterval)",
		"repoCIWatchStartPolling",
	} {
		if !strings.Contains(appJS, want) {
			t.Errorf("app.js does not contain %q", want)
		}
	}

	// Every key the line t()s must be wired in app.js itself (literal
	// key in a t() call; the mode keys are built as the
	// repo_ci_watch_mode_ + off/shadow/on prefix the same way the other
	// dynamic status families are) and present in every bundle with a
	// non-empty value free of the PRD word.
	for _, key := range repoCIWatchLocaleKeys {
		var want string
		if strings.HasPrefix(key, "repo_ci_watch_mode_") {
			want = "'repo_ci_watch_mode_'"
		} else {
			want = "'" + key + "'"
		}
		if !strings.Contains(appJS, want) {
			t.Errorf("app.js does not use locale key %q (looking for %q)", key, want)
		}
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range repoCIWatchLocaleKeys {
			value, ok := bundle[key]
			if !ok {
				t.Errorf("locale %s: missing repo_ci_watch key %q", lang, key)
				continue
			}
			if strings.TrimSpace(value) == "" {
				t.Errorf("locale %s: key %q has an empty value", lang, key)
			}
			if repoCIWatchPRDWord.MatchString(value) {
				t.Errorf("locale %s: key %q value uses PRD: %q", lang, key, value)
			}
		}
		title, ok := bundle["repo_ci_watch_status_title"]
		if !ok || strings.TrimSpace(title) == "" {
			t.Errorf("locale %s: status line title key repo_ci_watch_status_title missing or empty", lang)
		}
	}

	// The eight repo_ci_watch settings keys must be wired into
	// GENERAL_CONFIG_FIELDS in app.js — all of them, each one, so the
	// PWA's settings card can bind them (autonomous section, alongside
	// the other autonomous.automation.* entries).
	for _, key := range repoCIWatchConfigKeys {
		if !strings.Contains(appJS, key) {
			t.Errorf("app.js (GENERAL_CONFIG_FIELDS) does not contain repo_ci_watch config key %q", key)
		}
	}
}
