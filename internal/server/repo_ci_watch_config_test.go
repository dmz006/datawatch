// Repo CI watch config — REST surfaces: GET /api/config must expose the
// eight autonomous.automation.repo_ci_watch.* knobs with their effective
// values, and PUT /api/config must set them or refuse a bad value. Every
// default is applied in exactly one place (the Effective* accessors), so
// the default surface reads them from there.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type repoCIWatchValues struct {
	Mode              string   `json:"mode"`
	Repos             []string `json:"repos"`
	PollSeconds       int      `json:"poll_seconds"`
	AlertMinutes      int      `json:"alert_minutes"`
	TagCount          int      `json:"tag_count"`
	HungAfterMinutes  int      `json:"hung_after_minutes"`
	SecurityWorkflows []string `json:"security_workflows"`
	DeleteSuperseded  string   `json:"delete_superseded"`
}

func getRepoCIWatchSurface(t *testing.T, s *Server) repoCIWatchValues {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				RepoCIWatch repoCIWatchValues `json:"repo_ci_watch"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.Automation.RepoCIWatch
}

// The default surface reads every value from the Effective* accessors:
// the unset config shows off, no repos, the 300-second poll, the triage
// alert default (30) minutes, 5 tags, the 60-minute hung threshold, the
// default security workflow list and report.
func TestRepoCIWatchConfigDefaultsSurface(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
	got := getRepoCIWatchSurface(t, s)
	if got.Mode != "off" {
		t.Errorf("default mode = %q, want off", got.Mode)
	}
	if len(got.Repos) != 0 {
		t.Errorf("default repos = %v, want empty (unset watches nothing)", got.Repos)
	}
	if got.PollSeconds != config.RepoCIWatchDefaultPollSeconds {
		t.Errorf("default poll_seconds = %d, want %d", got.PollSeconds, config.RepoCIWatchDefaultPollSeconds)
	}
	if got.AlertMinutes != autonomous.DefaultAutomationTriageAlertMinutes {
		t.Errorf("default alert_minutes = %d, want %d", got.AlertMinutes, autonomous.DefaultAutomationTriageAlertMinutes)
	}
	if got.TagCount != config.RepoCIWatchDefaultTagCount {
		t.Errorf("default tag_count = %d, want %d", got.TagCount, config.RepoCIWatchDefaultTagCount)
	}
	if got.HungAfterMinutes != config.RepoCIWatchDefaultHungAfterMinutes {
		t.Errorf("default hung_after_minutes = %d, want %d", got.HungAfterMinutes, config.RepoCIWatchDefaultHungAfterMinutes)
	}
	if want, gotWorkflows := config.DefaultRepoCIWatchSecurityWorkflows, got.SecurityWorkflows; len(gotWorkflows) != len(want) {
		t.Errorf("default security_workflows = %v, want %v", gotWorkflows, want)
	}
	if got.DeleteSuperseded != "report" {
		t.Errorf("default delete_superseded = %q, want report", got.DeleteSuperseded)
	}
}

// PUT sets every knob; GET returns the values as stored — including the
// explicit zeros (alert off, no tags), which must not read their defaults.
func TestRepoCIWatchConfigRoundTrip(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
	patch := map[string]interface{}{
		"autonomous.automation.repo_ci_watch.mode":               "shadow",
		"autonomous.automation.repo_ci_watch.repos":              []string{"/home/dmz/repo-a", "/home/dmz/repo-b"},
		"autonomous.automation.repo_ci_watch.poll_seconds":       120,
		"autonomous.automation.repo_ci_watch.alert_minutes":      0, // explicit off
		"autonomous.automation.repo_ci_watch.tag_count":          0, // explicit none
		"autonomous.automation.repo_ci_watch.hung_after_minutes": 45,
		"autonomous.automation.repo_ci_watch.security_workflows": []string{"CodeQL", "secret-scan"},
		"autonomous.automation.repo_ci_watch.delete_superseded":  "delete",
	}
	b, _ := json.Marshal(patch)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !regexp.MustCompile(`"status":"ok"`).MatchString(rr.Body.String()) {
		t.Errorf("PUT response = %s, want status ok", rr.Body.String())
	}

	got := getRepoCIWatchSurface(t, s)
	if got.Mode != "shadow" || got.PollSeconds != 120 || got.HungAfterMinutes != 45 || got.DeleteSuperseded != "delete" {
		t.Errorf("surface after PUT = %+v, want mode=shadow poll=120 hung=45 delete=delete", got)
	}
	if len(got.Repos) != 2 || got.Repos[0] != "/home/dmz/repo-a" {
		t.Errorf("repos = %v, want the two configured paths", got.Repos)
	}
	if got.AlertMinutes != 0 {
		t.Errorf("explicit alert_minutes 0 = %d, want 0 (alert off)", got.AlertMinutes)
	}
	if got.TagCount != 0 {
		t.Errorf("explicit tag_count 0 = %d, want 0 (none)", got.TagCount)
	}
	if len(got.SecurityWorkflows) != 2 || got.SecurityWorkflows[1] != "secret-scan" {
		t.Errorf("security_workflows = %v, want [CodeQL secret-scan]", got.SecurityWorkflows)
	}

	// A follow-up PUT of non-zero values stores them (overwriting the
	// explicit zero).
	patch2 := map[string]interface{}{
		"autonomous.automation.repo_ci_watch.alert_minutes": 15,
		"autonomous.automation.repo_ci_watch.tag_count":     3,
	}
	b2, _ := json.Marshal(patch2)
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b2)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT(2): want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got = getRepoCIWatchSurface(t, s)
	if got.AlertMinutes != 15 || got.TagCount != 3 {
		t.Errorf("after second PUT: alert_minutes=%d tag_count=%d, want 15/3", got.AlertMinutes, got.TagCount)
	}
}

// A bad value is refused (400, an error naming the field) and leaves the
// stored value intact.
func TestRepoCIWatchConfigRejections(t *testing.T) {
	cases := []map[string]interface{}{
		{"autonomous.automation.repo_ci_watch.mode": "loud"},
		{"autonomous.automation.repo_ci_watch.mode": 3},
		{"autonomous.automation.repo_ci_watch.delete_superseded": "purge"},
		{"autonomous.automation.repo_ci_watch.repos": []string{"src/repo"}},
		{"autonomous.automation.repo_ci_watch.repos": "not-a-list"},
		{"autonomous.automation.repo_ci_watch.poll_seconds": 10},
		{"autonomous.automation.repo_ci_watch.poll_seconds": "fast"},
		{"autonomous.automation.repo_ci_watch.alert_minutes": -1},
		{"autonomous.automation.repo_ci_watch.tag_count": -1},
		{"autonomous.automation.repo_ci_watch.security_workflows": "not-a-list"},
	}
	for _, patch := range cases {
		spy := &autonomousConfigPropagationSpy{}
		s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
		// A previously-stored value must survive the refused write.
		seed, _ := json.Marshal(map[string]interface{}{"autonomous.automation.repo_ci_watch.mode": "shadow"})
		rrSeed := httptest.NewRecorder()
		s.handlePutConfig(rrSeed, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(seed)))
		if rrSeed.Code != http.StatusOK {
			t.Fatalf("seed PUT: want 200, got %d body=%s", rrSeed.Code, rrSeed.Body.String())
		}

		b, _ := json.Marshal(patch)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v: want 400, got %d body=%s", patch, rr.Code, rr.Body.String())
		}
		if !bytes.Contains(rr.Body.Bytes(), []byte("repo_ci_watch")) {
			t.Errorf("PUT %v: error %q does not name the repo_ci_watch key", patch, rr.Body.String())
		}

		got := getRepoCIWatchSurface(t, s)
		if got.Mode != "shadow" {
			t.Errorf("refused write clobbered stored mode: %q, want shadow", got.Mode)
		}
	}
}
