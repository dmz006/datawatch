// Supervisor watch working_model — REST write path: PUT /api/config must
// store the key, GET /api/config must show it back, an empty value turns
// the model_not_resident check off, and a value that is not text is
// refused (400) with the stored value left intact.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const supervisorWatchWorkingModelKey = "autonomous.automation.supervisor_watch.working_model"

func putSupervisorWatchWorkingModel(s *Server, v interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]interface{}{supervisorWatchWorkingModelKey: v})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func getSupervisorWatchWorkingModel(t *testing.T, s *Server) string {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				SupervisorWatch struct {
					WorkingModel string `json:"working_model"`
				} `json:"supervisor_watch"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.Automation.SupervisorWatch.WorkingModel
}

func TestSupervisorWatchWorkingModelRESTWritePath(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}

	if got := getSupervisorWatchWorkingModel(t, s); got != "" {
		t.Fatalf("default surface working_model = %q, want empty (check off)", got)
	}

	if rr := putSupervisorWatchWorkingModel(s, "  qwen3.8  "); rr.Code != http.StatusOK {
		t.Fatalf("PUT working_model: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := getSupervisorWatchWorkingModel(t, s); got != "qwen3.8" {
		t.Errorf("surface working_model = %q, want the trimmed value", got)
	}

	for _, bad := range []interface{}{3, true, []string{"qwen3.8"}} {
		rr := putSupervisorWatchWorkingModel(s, bad)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT working_model=%v: want 400, got %d", bad, rr.Code)
		}
		if got := getSupervisorWatchWorkingModel(t, s); got != "qwen3.8" {
			t.Errorf("after refused PUT working_model=%v: surface = %q, want the stored value intact", bad, got)
		}
	}

	if rr := putSupervisorWatchWorkingModel(s, ""); rr.Code != http.StatusOK {
		t.Fatalf("PUT empty working_model: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := getSupervisorWatchWorkingModel(t, s); got != "" {
		t.Errorf("after clearing, surface working_model = %q, want empty", got)
	}
}
