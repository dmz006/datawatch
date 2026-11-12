// REST surface for autonomous.judge_prompt_file_threshold_lines and
// autonomous.judge_delivery_wait_per_thousand_lines_seconds: GET shows the
// stored values, PUT accepts whole numbers in range and refuses anything else
// with a 400 that names the key.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var judgePromptKeys = []struct {
	key string
	max int
}{
	{"judge_prompt_file_threshold_lines", 100000},
	{"judge_delivery_wait_per_thousand_lines_seconds", 3600},
}

func TestJudgePromptSurface_RESTRoundTrip(t *testing.T) {
	for _, k := range judgePromptKeys {
		for _, val := range []int{0, 123, k.max} {
			s, _, _ := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous." + k.key: val})
			want, _ := json.Marshal(val)
			if got := string(getAutonomousConfigMap(t, s)[k.key]); got != string(want) {
				t.Errorf("%s: GET after PUT %d = %q", k.key, val, got)
			}
		}
	}
}

func TestJudgePromptSurface_RESTGetPresent(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	m := getAutonomousConfigMap(t, s)
	for _, k := range judgePromptKeys {
		if _, ok := m[k.key]; !ok {
			t.Errorf("GET /api/config autonomous lacks %s", k.key)
		}
	}
}

func TestJudgePromptSurface_RESTPutRejectsInvalid(t *testing.T) {
	for _, k := range judgePromptKeys {
		for _, bad := range []any{-1, k.max + 1, 2.5, "10", true} {
			s, _, _ := newMaxConcurrentTasksServer(t)
			before := string(getAutonomousConfigMap(t, s)[k.key])
			b, _ := json.Marshal(map[string]any{"autonomous." + k.key: bad})
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("PUT %s: want 400, got %d body=%s", b, rr.Code, rr.Body.String())
				continue
			}
			if !strings.Contains(rr.Body.String(), "autonomous."+k.key) {
				t.Errorf("PUT %s: error %q does not name the key", b, rr.Body.String())
			}
			if got := string(getAutonomousConfigMap(t, s)[k.key]); got != before {
				t.Errorf("PUT %s: rejected value changed %s from %s to %s", b, k.key, before, got)
			}
		}
	}
}
