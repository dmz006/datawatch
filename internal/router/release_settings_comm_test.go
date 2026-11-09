// comm `configure` for the release settings: autonomous.push_and_tag,
// autonomous.release_path and git.release_ci_window_minutes are set through
// the same configure key handling as the other autonomous keys, read back
// from the stored configuration, and an invalid release_path is refused with
// the config layer's validation message.

package router

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// releaseSettingsStore is a stand-in for the daemon's configuration. It applies
// each key the way the daemon does for these keys (validate, then store) and
// keeps the stored text so a test can read the value back.
type releaseSettingsStore struct {
	values map[string]string
}

func (s *releaseSettingsStore) apply(key, value string) error {
	switch key {
	case "autonomous.push_and_tag":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("autonomous.push_and_tag must be true or false, got %q", value)
		}
	case "autonomous.release_path":
		if err := config.ValidateReleasePath(value); err != nil {
			return err
		}
	case "git.release_ci_window_minutes":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("git.release_ci_window_minutes must be greater than 0 (a whole number of minutes)")
		}
		if err := config.ValidateReleaseCIWindowMinutes(n); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	s.values[key] = value
	return nil
}

func newReleaseSettingsRouter() (*Router, *releaseSettingsStore) {
	store := &releaseSettingsStore{values: map[string]string{}}
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(store.apply)
	return r, store
}

func TestReleaseSettingsCommSetsAndReadsBackEachKey(t *testing.T) {
	for _, tc := range []struct {
		key, value string
	}{
		{"autonomous.push_and_tag", "true"},
		{"autonomous.push_and_tag", "false"},
		{"autonomous.release_path", "direct_push"},
		{"autonomous.release_path", "pull_request"},
		{"git.release_ci_window_minutes", "45"},
	} {
		r, store := newReleaseSettingsRouter()
		out := strings.Join(r.HandleTestMessage("configure "+tc.key+"="+tc.value), "\n")
		if got := store.values[tc.key]; got != tc.value {
			t.Errorf("%s: stored %q, want %q", tc.key, got, tc.value)
		}
		if !strings.Contains(out, "Set "+tc.key+" = "+tc.value) {
			t.Errorf("%s: reply %q does not report the set value", tc.key, out)
		}
	}
}

func TestReleaseSettingsCommRejectsInvalidReleasePath(t *testing.T) {
	r, store := newReleaseSettingsRouter()
	out := strings.Join(r.HandleTestMessage("configure autonomous.release_path=squash_merge"), "\n")
	if _, ok := store.values["autonomous.release_path"]; ok {
		t.Fatalf("invalid release_path was stored")
	}
	want := `autonomous.release_path must be "direct_push" or "pull_request", got "squash_merge"`
	if !strings.Contains(out, "Configure failed") || !strings.Contains(out, want) {
		t.Errorf("reply %q should carry the validation message %q", out, want)
	}
}

func TestReleaseSettingsCommRejectsInvalidCIWindow(t *testing.T) {
	r, store := newReleaseSettingsRouter()
	out := strings.Join(r.HandleTestMessage("configure git.release_ci_window_minutes=0"), "\n")
	if _, ok := store.values["git.release_ci_window_minutes"]; ok {
		t.Fatalf("invalid release_ci_window_minutes was stored")
	}
	if !strings.Contains(out, "Configure failed") {
		t.Errorf("reply %q should report the refusal", out)
	}
}

func TestReleaseSettingsConfigureHelpListsKeys(t *testing.T) {
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, msg := range []string{"configure help", "configure list"} {
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		for _, key := range []string{
			"autonomous.push_and_tag",
			"autonomous.release_path",
			"git.release_ci_window_minutes",
		} {
			if !strings.Contains(out, key) {
				t.Errorf("%q: reply misses %s", msg, key)
			}
		}
	}
}
