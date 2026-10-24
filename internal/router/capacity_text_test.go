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

func TestCapacity_CommReplyShowsNodeUsage(t *testing.T) {
	autonomous.CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autonomous.CapacityTextClock = time.Now })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capacity" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"pools":[
{"name":"node:a","limit":1,"held":1,
 "usage":{"state":"ok","checked_at":"2026-10-22T16:59:00Z","models_supported":true,
 "models":[{"name":"qwen3:8b","size_bytes":5368709120,"size_vram_bytes":5162643968,"expires_at":"2026-10-22T17:29:00Z","no_expiry":false}],
 "gpu":[{"name":"NVIDIA A100","util_pct":12.5,"mem_used_bytes":10737418240,"mem_total_bytes":42949672960}]}},
{"name":"host","limit":9,"held":2}],"leases":[],"waiting":[],"budgets":[],
"tasks":{"waiting":[],"running":[]}}`))
	}))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleCapacity() })
	for _, want := range []string{
		"node:a: 1/1",
		"  model: qwen3:8b 5.0 GiB in memory, expires in 29m0s",
		"  GPU NVIDIA A100: 12.5% util, 10.0 of 40.0 GiB memory",
		"host: 2/9",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in reply:\n%s", want, got)
		}
	}
}
