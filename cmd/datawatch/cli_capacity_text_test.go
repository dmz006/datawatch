package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/router"
)

const capacityTextCLIFixture = `{"pools":[{"name":"host","limit":2,"held":1,"holders":["t1"]}],"leases":[],"waiting":[],
"backoff":[{"backend":"claude-code","until":"2026-10-22T20:00:00Z","reason":"usage limit","reset_known":false}],
"budgets":[],
"tasks":{"waiting":[{"task_id":"t2","prd_id":"p2","reason":"backend claude-code is backing off","since":"2026-10-22T16:00:00Z"}],
"running":[{"task_id":"t1","prd_id":"p1","pools":["host"],"since":"2026-10-22T16:30:00Z"}]}}`

func TestCapacityText_CLIMatchesCommReply(t *testing.T) {
	autonomous.CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autonomous.CapacityTextClock = time.Now })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capacity" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(capacityTextCLIFixture))
	}))
	defer srv.Close()

	var cli bytes.Buffer
	if err := renderCapacity(&cli, []byte(capacityTextCLIFixture), false); err != nil {
		t.Fatal(err)
	}

	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	rt := router.NewRouter("h", "g", nil, nil, 0, nil)
	rt.SetWebPort(port)
	replies := rt.HandleTestMessage("capacity")
	if len(replies) != 1 {
		t.Fatalf("want one reply, got %q", replies)
	}
	want := "[h] Capacity:\n" + strings.TrimRight(cli.String(), "\n")
	if replies[0] != want {
		t.Fatalf("comm reply differs from CLI output\ncomm:\n%s\ncli:\n%s", replies[0], want)
	}
	for _, s := range []string{"backend claude-code is backing off", "reset time unknown", "holds host"} {
		if !strings.Contains(cli.String(), s) {
			t.Fatalf("CLI output missing %q:\n%s", s, cli.String())
		}
	}
}

func TestCapacityText_CLIRendersNodeUsage(t *testing.T) {
	autonomous.CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autonomous.CapacityTextClock = time.Now })

	body := `{"pools":[{"name":"node:a","limit":1,"held":1,"usage":{"state":"ok","checked_at":"2026-10-22T16:59:00Z","models_supported":true,"models":[{"name":"qwen3:8b","size_bytes":5368709120,"size_vram_bytes":5162643968,"expires_at":"2026-10-22T17:29:00Z","no_expiry":false}],"gpu":[{"name":"NVIDIA A100","util_pct":12.5,"mem_used_bytes":10737418240,"mem_total_bytes":42949672960}]}},{"name":"host","limit":9,"held":2}],"leases":[],"waiting":[],"budgets":[],"tasks":{"waiting":[],"running":[]}}`

	var cli bytes.Buffer
	if err := renderCapacity(&cli, []byte(body), false); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"node:a: 1/1",
		"  model: qwen3:8b 5.0 GiB in memory, expires in 29m0s",
		"  GPU NVIDIA A100: 12.5% util, 10.0 of 40.0 GiB memory",
	} {
		if !strings.Contains(cli.String(), s) {
			t.Fatalf("CLI output missing %q:\n%s", s, cli.String())
		}
	}
}
