package router_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
)

// TestUsageCalcBurnGuardShowsThePaceHold: the comm reply carries the pace policy's mode, the
// hold it keeps with the rule, and the newest decision with its evidence.
func TestUsageCalcBurnGuardShowsThePaceHold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"state":"idle","level":0,"applied":[],
"pace":{"mode":"on","enforcing":true,"held":true,
 "entries":[{"llm":"claude-code","held":true,"would_hold":false,"rule":"window_pause_line",
  "last":{"rule":"window_pause_line","action":"hold","reason":"window estimate 88.0% is at or over the 85% pause line",
  "window_estimated":88,"window_stop_percent":85,"week_estimated":50,"week_line":60,"week_tolerance":2}}],
 "log":[{"at":"2026-10-30T20:00:00Z","entry":"claude-code","mode":"on","action":"hold","rule":"window_pause_line",
  "reason":"window estimate 88.0% is at or over the 85% pause line"}]}}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	r := router.NewRouter("calc", "g1", nil, nil, 10, nil)
	r.SetWebPort(port)
	out := r.HandleTestMessage("usage burn-guard")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	for _, want := range []string{"pace policy: on", "claude-code held", "window_pause_line", "window estimate 88.0%", "85%"} {
		if !strings.Contains(out[0], want) {
			t.Errorf("reply lacks %q:\n%s", want, out[0])
		}
	}
}

func TestUsageCalcBurnGuardPaceShadowSaysWouldHold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`{"enabled":true,"state":"idle","pace":{"mode":"shadow","enforcing":false,"held":false,
"entries":[{"llm":"claude-code","held":false,"would_hold":true,"rule":"weekly_pace_line",
"last":{"rule":"weekly_pace_line","action":"would_hold","reason":"week estimate 97.0% is over the pace line"}}],"log":[]}}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	r := router.NewRouter("calc", "g1", nil, nil, 10, nil)
	r.SetWebPort(port)
	out := r.HandleTestMessage("usage burn-guard")
	if len(out) == 0 || !strings.Contains(out[0], "pace policy: shadow") || !strings.Contains(out[0], "would hold") || !strings.Contains(out[0], "no new work is held") {
		t.Fatalf("reply = %v", out)
	}
}
