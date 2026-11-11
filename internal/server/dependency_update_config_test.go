// Dependency update config — REST surfaces: GET /api/config must expose the
// three autonomous.automation.dependency_update.* keys with their effective
// values, PUT /api/config must store them, and a bad value must be refused
// with 400 and the allowed form, leaving the stored value unchanged.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type dependencyUpdateValues struct {
	Enabled   bool   `json:"enabled"`
	Schedule  string `json:"schedule"`
	MaxMajors int    `json:"max_majors"`
}

func newDependencyUpdateServer(t *testing.T) *Server {
	t.Helper()
	spy := &autonomousConfigPropagationSpy{}
	return &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
}

func getDependencyUpdateSurface(t *testing.T, s *Server) dependencyUpdateValues {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				DependencyUpdate dependencyUpdateValues `json:"dependency_update"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.Automation.DependencyUpdate
}

func putDependencyUpdate(s *Server, patch map[string]interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func TestDependencyUpdateConfigDefaultsSurface(t *testing.T) {
	got := getDependencyUpdateSurface(t, newDependencyUpdateServer(t))
	if !got.Enabled || got.Schedule != config.DependencyUpdateDefaultSchedule || got.MaxMajors != config.DependencyUpdateDefaultMaxMajors {
		t.Errorf("default surface = %+v, want enabled, %q, %d", got, config.DependencyUpdateDefaultSchedule, config.DependencyUpdateDefaultMaxMajors)
	}
}

// PUT then GET round-trips each key on its own, including the explicit
// false and 0 that must not read their defaults.
func TestDependencyUpdateConfigRoundTrip(t *testing.T) {
	s := newDependencyUpdateServer(t)

	if rr := putDependencyUpdate(s, map[string]interface{}{"autonomous.automation.dependency_update.enabled": false}); rr.Code != http.StatusOK {
		t.Fatalf("PUT enabled: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := getDependencyUpdateSurface(t, s); got.Enabled {
		t.Errorf("enabled after PUT false = true, want false")
	}

	if rr := putDependencyUpdate(s, map[string]interface{}{"autonomous.automation.dependency_update.schedule": "friday 23:59"}); rr.Code != http.StatusOK {
		t.Fatalf("PUT schedule: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := getDependencyUpdateSurface(t, s); got.Schedule != "friday 23:59" {
		t.Errorf("schedule after PUT = %q, want friday 23:59", got.Schedule)
	}

	if rr := putDependencyUpdate(s, map[string]interface{}{"autonomous.automation.dependency_update.max_majors": 0}); rr.Code != http.StatusOK {
		t.Fatalf("PUT max_majors 0: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := getDependencyUpdateSurface(t, s)
	if got.MaxMajors != 0 {
		t.Errorf("explicit max_majors 0 = %d, want 0", got.MaxMajors)
	}
	if got.Enabled || got.Schedule != "friday 23:59" {
		t.Errorf("a later PUT disturbed other keys: %+v", got)
	}

	if rr := putDependencyUpdate(s, map[string]interface{}{"autonomous.automation.dependency_update.max_majors": 12}); rr.Code != http.StatusOK {
		t.Fatalf("PUT max_majors 12: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := getDependencyUpdateSurface(t, s); got.MaxMajors != 12 {
		t.Errorf("max_majors after PUT = %d, want 12", got.MaxMajors)
	}
}

// An invalid value is refused with 400 and the allowed form, and the
// stored values stay as they were.
func TestDependencyUpdateConfigRejections(t *testing.T) {
	cases := []struct {
		patch map[string]interface{}
		want  string
	}{
		{map[string]interface{}{"autonomous.automation.dependency_update.schedule": "someday 25:00"}, "monday 05:00"},
		{map[string]interface{}{"autonomous.automation.dependency_update.schedule": "monday"}, "weekday HH:MM"},
		{map[string]interface{}{"autonomous.automation.dependency_update.schedule": 5}, "weekday HH:MM"},
		{map[string]interface{}{"autonomous.automation.dependency_update.max_majors": -1}, "0 to 20"},
		{map[string]interface{}{"autonomous.automation.dependency_update.max_majors": 21}, "0 to 20"},
		{map[string]interface{}{"autonomous.automation.dependency_update.max_majors": 1.5}, "whole number"},
		{map[string]interface{}{"autonomous.automation.dependency_update.max_majors": "many"}, "whole number"},
		{map[string]interface{}{"autonomous.automation.dependency_update.enabled": "yes"}, "true or false"},
	}
	for _, c := range cases {
		s := newDependencyUpdateServer(t)
		seed := map[string]interface{}{
			"autonomous.automation.dependency_update.enabled":    false,
			"autonomous.automation.dependency_update.schedule":   "tuesday 06:30",
			"autonomous.automation.dependency_update.max_majors": 3,
		}
		if rr := putDependencyUpdate(s, seed); rr.Code != http.StatusOK {
			t.Fatalf("seed PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		rr := putDependencyUpdate(s, c.patch)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v: want 400, got %d body=%s", c.patch, rr.Code, rr.Body.String())
		}
		if !bytes.Contains(rr.Body.Bytes(), []byte(c.want)) {
			t.Errorf("PUT %v: error %q does not carry the allowed form %q", c.patch, rr.Body.String(), c.want)
		}
		got := getDependencyUpdateSurface(t, s)
		if got.Enabled || got.Schedule != "tuesday 06:30" || got.MaxMajors != 3 {
			t.Errorf("refused PUT %v changed the stored values: %+v", c.patch, got)
		}
	}
}
