package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// releaseConfigDaemon stands in for the daemon's PUT /api/config: it refuses
// a cadence or scheme that is not available with the daemon's own 400 text,
// and otherwise records the body.
type releaseConfigDaemon struct {
	bodies []map[string]any
}

func startReleaseConfigDaemon(t *testing.T) *releaseConfigDaemon {
	t.Helper()
	d := &releaseConfigDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for k, v := range body {
			s, _ := v.(string)
			switch {
			case strings.HasSuffix(k, ".release_cadence") && (s == "batch" || s == "manual"):
				http.Error(w, k+": release cadence "+s+" is not available yet", http.StatusBadRequest)
				return
			case strings.HasSuffix(k, ".version_scheme") && s != "" && s != "semver":
				http.Error(w, k+": version scheme "+s+" is not available yet", http.StatusBadRequest)
				return
			}
		}
		d.bodies = append(d.bodies, body)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

func TestMergePipelineReleaseCLISetKeys(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.merge_pipeline.major_trigger", "milestone", "milestone"},
		{"autonomous.merge_pipeline.release_cadence", "per_merge", "per_merge"},
		{"autonomous.merge_pipeline.version_scheme", "semver", "semver"},
		{"autonomous.merge_pipeline.release_size_map", "feature=minor,bug=patch", "feature=minor,bug=patch"},
		{"autonomous.merge_pipeline.release_size_map", `{"feature":"major"}`, map[string]any{"feature": "major"}},
		// A per-project override is the same key under projects.<project>.
		{"autonomous.merge_pipeline.projects.demo.major_trigger", "never", "never"},
		{"autonomous.merge_pipeline.projects.demo.release_size_map", "bug=minor", "bug=minor"},
	}
	for _, c := range cases {
		d := startReleaseConfigDaemon(t)
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err != nil {
			t.Errorf("%s=%s: %v", c.key, c.value, err)
			continue
		}
		if len(d.bodies) != 1 || !reflect.DeepEqual(d.bodies[0][c.key], c.want) {
			t.Errorf("%s=%s: daemon got %#v, want %#v", c.key, c.value, d.bodies, c.want)
		}
		if !strings.Contains(out, "Set "+c.key+" = "+c.value) {
			t.Errorf("%s: output %q", c.key, out)
		}
	}
}

// A cadence or scheme that is not available is refused by the daemon with a
// 400; config set shows the daemon's text and exits non-zero.
func TestMergePipelineReleaseCLIRefusals(t *testing.T) {
	cases := []struct{ key, value, want string }{
		{"autonomous.merge_pipeline.release_cadence", "batch", "release cadence batch is not available yet"},
		{"autonomous.merge_pipeline.release_cadence", "manual", "release cadence manual is not available yet"},
		{"autonomous.merge_pipeline.version_scheme", "calver", "version scheme calver is not available yet"},
		{"autonomous.merge_pipeline.projects.demo.release_cadence", "batch", "release cadence batch is not available yet"},
		{"autonomous.merge_pipeline.projects.demo.version_scheme", "calver", "version scheme calver is not available yet"},
	}
	for _, c := range cases {
		d := startReleaseConfigDaemon(t)
		_, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err == nil {
			t.Errorf("%s=%s: want an error (non-zero exit), got none", c.key, c.value)
			continue
		}
		if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%s: error %q lacks the daemon's 400 text %q", c.key, c.value, err, c.want)
		}
		if len(d.bodies) != 0 {
			t.Errorf("%s=%s: a refused value was stored: %v", c.key, c.value, d.bodies)
		}
	}
}

// A JSON-valid value the daemon refuses keeps the daemon's message instead of
// being retried as quoted text.
func TestMergePipelineReleaseCLIJSONRefusalKeepsMessage(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "autonomous.merge_pipeline.release_size_map.feature: \"huge\" is invalid", http.StatusBadRequest)
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	err := runConfigSet(&config.Config{}, "autonomous.merge_pipeline.release_size_map", `{"feature":"huge"}`)
	if err == nil || !strings.Contains(err.Error(), `"huge" is invalid`) {
		t.Errorf("err=%v", err)
	}
	if calls != 1 {
		t.Errorf("daemon called %d times, want 1 (no quoted retry)", calls)
	}
}

func TestIsMergePipelineReleaseKey(t *testing.T) {
	for k, want := range map[string]bool{
		"autonomous.merge_pipeline.release_size_map":            true,
		"autonomous.merge_pipeline.projects.a.b.version_scheme": true,
		"autonomous.merge_pipeline.conflict_policy":             false,
		"autonomous.other.release_cadence":                      false,
	} {
		if got := isMergePipelineReleaseKey(k); got != want {
			t.Errorf("%s: got %v", k, got)
		}
	}
}
