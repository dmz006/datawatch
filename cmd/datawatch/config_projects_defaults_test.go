package main

// `datawatch config set` and `config get` for the projects_defaults keys: a
// value is sent to the daemon and read back through GET /api/config, a refused
// value returns a non-nil error naming the key, and the stored value is left
// unchanged. The fake daemon applies the same shared validator the real
// PUT /api/config handler runs, so the messages match what a user sees.

import (
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

// projectsDefaultsFakeDaemon keeps the projects_defaults block and serves it
// on PUT and GET /api/config.
type projectsDefaultsFakeDaemon struct {
	mu  sync.Mutex
	cfg config.ProjectsDefaultsConfig
}

func (d *projectsDefaultsFakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/config" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		block := map[string]interface{}{
			"branch_retention_days": d.cfg.BranchRetentionDays,
			"weekly_share_percent":  d.cfg.WeeklySharePercent,
			"major_trigger":         d.cfg.EffectiveMajorTrigger(),
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"projects_defaults": block})
	case http.MethodPut:
		raw, _ := io.ReadAll(r.Body)
		var patch map[string]interface{}
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		for k, v := range patch {
			if !strings.HasPrefix(k, config.ProjectsDefaultsSettingPrefix) {
				continue
			}
			if err := config.ValidateProjectsDefaultsSetting(k, v); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		for k, v := range patch {
			if strings.HasPrefix(k, config.ProjectsDefaultsSettingPrefix) {
				_ = d.cfg.ApplyProjectsDefaultsSetting(k, v)
			}
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// captureProjectsDefaultsStdout runs fn and returns what it printed to stdout.
func captureProjectsDefaultsStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

func TestProjectsDefaultsConfigSetThenGetReadsBack(t *testing.T) {
	daemon := &projectsDefaultsFakeDaemon{}
	srv := httptest.NewServer(daemon)
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	cfg := &config.Config{}
	sets := []struct{ key, value string }{
		{"projects_defaults.branch_retention_days", "30"},
		{"projects_defaults.weekly_share_percent", "25"},
		{"projects_defaults.major_trigger", "milestone"},
	}
	for _, s := range sets {
		if err := runConfigSet(cfg, s.key, s.value); err != nil {
			t.Fatalf("runConfigSet(%s, %s): %v", s.key, s.value, err)
		}
	}

	want := map[string]string{
		"projects_defaults.branch_retention_days": "30",
		"projects_defaults.weekly_share_percent":  "25",
		"projects_defaults.major_trigger":         "milestone",
	}
	for key, value := range want {
		var getErr error
		out := captureProjectsDefaultsStdout(t, func() { getErr = runConfigGet(cfg, key) })
		if getErr != nil {
			t.Fatalf("runConfigGet(%s): %v", key, getErr)
		}
		if !strings.Contains(out, value) {
			t.Errorf("runConfigGet(%s) printed %q, want it to contain %q", key, out, value)
		}
	}
}

func TestProjectsDefaultsConfigSetRefusesInvalidValues(t *testing.T) {
	daemon := &projectsDefaultsFakeDaemon{cfg: config.ProjectsDefaultsConfig{MajorTrigger: "never"}}
	srv := httptest.NewServer(daemon)
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	cases := []struct{ key, value string }{
		{"projects_defaults.weekly_share_percent", "150"},
		{"projects_defaults.branch_retention_days", "-3"},
		{"projects_defaults.major_trigger", "weekly"},
	}
	for _, tc := range cases {
		err := runConfigSet(&config.Config{}, tc.key, tc.value)
		if err == nil {
			t.Fatalf("runConfigSet(%s, %s) = nil, want refusal", tc.key, tc.value)
		}
		if !strings.Contains(err.Error(), tc.key) {
			t.Errorf("refusal %q does not name %s", err.Error(), tc.key)
		}
	}
	if daemon.cfg.MajorTrigger != "never" || daemon.cfg.WeeklySharePercent != 0 || daemon.cfg.BranchRetentionDays != 0 {
		t.Errorf("refused sets changed stored value: %+v", daemon.cfg)
	}
}
