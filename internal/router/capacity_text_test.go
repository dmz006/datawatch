package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestCapacityText_CommReplyShowsReasons(t *testing.T) {
	autonomous.CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autonomous.CapacityTextClock = time.Now })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capacity" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"pools":[{"name":"llm:claude","limit":3,"held":1,"holders":["t1"]}],"leases":[],"waiting":[],
"backoff":[{"backend":"claude-code","until":"2026-10-22T20:00:00Z","reason":"usage limit","reset_known":true}],
"budgets":[],
"tasks":{"waiting":[{"task_id":"t2","prd_id":"p2","task_title":"Write docs","reason":"waiting for a free llm:claude slot","since":"2026-10-22T16:50:00Z"}],
"running":[{"task_id":"t1","prd_id":"p1","task_title":"Build index","pools":["host","llm:claude"],"since":"2026-10-22T16:55:00Z"}]}}`))
	}))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleCapacity() })
	for _, want := range []string{
		"llm:claude: 1/3 held by t1",
		"Write docs (PRD p2): waiting for a free llm:claude slot - waiting 10m0s",
		"Build index (PRD p1): holds host, llm:claude - running 5m0s",
		"claude-code: resets 2026-10-22T20:00:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in reply:\n%s", want, got)
		}
	}

	// A daemon error surfaces as a failure reply, not a panic.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", 500) }))
	defer bad.Close()
	r2 := loopbackRouter(t, bad)
	if got := captureSends(r2, func() { r2.handleCapacity() }); !strings.Contains(got, "capacity failed") {
		t.Fatalf("want failure reply, got %q", got)
	}
}
