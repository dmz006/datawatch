// Release settings through `datawatch config set` / `config get`.
//
// runConfigSet has no key allow-list: it forwards any dotted key to the
// daemon's PUT /api/config, the same path git.secret_scan takes. The daemon
// is the authority that validates values, so this fake daemon applies the
// same validators the server applies (config.ValidateReleasePath and
// config.ValidateReleaseCIWindowMinutes) and refuses bad values with HTTP 400.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// releaseSettingsDaemon is a minimal stand-in for the daemon's /api/config.
type releaseSettingsDaemon struct {
	mu     sync.Mutex
	values map[string]any // flat dotted key -> stored value
}

// validateReleaseSettingsKey mirrors the daemon-side validation for the
// release keys exercised here.
func validateReleaseSettingsKey(key string, v any) error {
	switch key {
	case "autonomous.push_and_tag":
		if _, ok := v.(bool); !ok {
			return errString("autonomous.push_and_tag must be true or false")
		}
	case "autonomous.release_path":
		s, ok := v.(string)
		if !ok {
			return errString("autonomous.release_path must be a string")
		}
		return config.ValidateReleasePath(s)
	case "git.release_ci_window_minutes":
		f, ok := v.(float64)
		if !ok || f != float64(int(f)) {
			return errString("git.release_ci_window_minutes must be a whole number of minutes")
		}
		return config.ValidateReleaseCIWindowMinutes(int(f))
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

func (d *releaseSettingsDaemon) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var patch map[string]any
			if err := json.Unmarshal(body, &patch); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			for k, v := range patch {
				if err := validateReleaseSettingsKey(k, v); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			for k, v := range patch {
				d.values[k] = v
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			nested := map[string]any{}
			for k, v := range d.values {
				setReleaseSettingsNested(nested, strings.Split(k, "."), v)
			}
			_ = json.NewEncoder(w).Encode(nested)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func setReleaseSettingsNested(m map[string]any, parts []string, v any) {
	if len(parts) == 1 {
		m[parts[0]] = v
		return
	}
	child, ok := m[parts[0]].(map[string]any)
	if !ok {
		child = map[string]any{}
		m[parts[0]] = child
	}
	setReleaseSettingsNested(child, parts[1:], v)
}

// withReleaseSettingsDaemon points the CLI at a fake daemon for the test and
// returns the daemon so the test can inspect what was stored.
func withReleaseSettingsDaemon(t *testing.T) *releaseSettingsDaemon {
	t.Helper()
	d := &releaseSettingsDaemon{values: map[string]any{}}
	srv := httptest.NewServer(d.handler())
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d
}

// captureReleaseSettingsStdout runs fn and returns what it wrote to stdout.
func captureReleaseSettingsStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = old
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), runErr
}

func TestConfigSetReleaseSettingsKeysReadBack(t *testing.T) {
	withReleaseSettingsDaemon(t)
	cfg := &config.Config{}

	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.push_and_tag", "true", true},
		{"autonomous.push_and_tag", "false", false},
		{"autonomous.release_path", "pull_request", "pull_request"},
		{"autonomous.release_path", "direct_push", "direct_push"},
		{"git.release_ci_window_minutes", "45", float64(45)},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			if _, err := captureReleaseSettingsStdout(t, func() error {
				return runConfigSet(cfg, tc.key, tc.value)
			}); err != nil {
				t.Fatalf("config set %s %s: %v", tc.key, tc.value, err)
			}
			out, err := captureReleaseSettingsStdout(t, func() error {
				return runConfigGet(cfg, tc.key)
			})
			if err != nil {
				t.Fatalf("config get %s: %v", tc.key, err)
			}
			var got any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("config get %s printed non-JSON %q: %v", tc.key, out, err)
			}
			if got != tc.want {
				t.Errorf("config get %s = %v (%T), want %v (%T)", tc.key, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestConfigSetReleaseSettingsRefusesInvalidReleasePath(t *testing.T) {
	d := withReleaseSettingsDaemon(t)
	cfg := &config.Config{}

	_, err := captureReleaseSettingsStdout(t, func() error {
		return runConfigSet(cfg, "autonomous.release_path", "rebase_and_force")
	})
	if err == nil {
		t.Fatal("config set autonomous.release_path rebase_and_force: want refusal, got nil error")
	}
	if !strings.Contains(err.Error(), "release_path") {
		t.Errorf("refusal %q does not name the release_path key", err.Error())
	}

	d.mu.Lock()
	_, stored := d.values["autonomous.release_path"]
	d.mu.Unlock()
	if stored {
		t.Error("an invalid release_path was stored; it must be refused")
	}
}
