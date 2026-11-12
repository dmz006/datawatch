package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// TestSupervisorWakeConfigHelp: config_set lists every wake key.
func TestSupervisorWakeConfigHelp(t *testing.T) {
	desc := (&Server{}).toolConfigSet().Description
	for _, k := range supervisorloop.WakeKeys() {
		if !strings.Contains(desc, k) {
			t.Errorf("config_set description does not list %q", k)
		}
	}
}

// TestSupervisorWakeStatsCarriesWake: the stats tool returns the stats plus
// the wake record under "wake".
func TestSupervisorWakeStatsCarriesWake(t *testing.T) {
	var paths []string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/supervisor-loop/wake" {
			_, _ = w.Write([]byte(`{"enabled":true,"coalesce_seconds":30,"event_count":4}`))
			return
		}
		_, _ = w.Write([]byte(`{"enabled":true,"jobs":11}`))
	})
	res, err := s.handleSupervisorLoopStats(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	var out struct {
		Jobs int `json:"jobs"`
		Wake struct {
			Enabled    bool `json:"enabled"`
			EventCount int  `json:"event_count"`
		} `json:"wake"`
	}
	if err := json.Unmarshal([]byte(automationText(t, res)), &out); err != nil {
		t.Fatal(err)
	}
	if out.Jobs != 11 || !out.Wake.Enabled || out.Wake.EventCount != 4 {
		t.Errorf("merged result wrong: %+v", out)
	}
	if len(paths) != 2 {
		t.Errorf("proxied %v", paths)
	}
}

// TestSupervisorWakeStatsWithoutWake: a failing wake read leaves stats as is.
func TestSupervisorWakeStatsWithoutWake(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/supervisor-loop/wake" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"jobs":3}`))
	})
	res, _ := s.handleSupervisorLoopStats(context.Background(), callReq(nil))
	if res == nil || res.IsError || automationText(t, res) != `{"jobs":3}` {
		t.Errorf("stats changed: %+v", res)
	}
}
