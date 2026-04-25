package observer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captured 2026-04-25 from a real PKS / Tanzu k8s 1.33 cluster with
// metrics-server v0.8.1 — three worker nodes, no GPU.
const realNodeMetrics = `{"kind":"NodeMetricsList","apiVersion":"metrics.k8s.io/v1beta1","metadata":{},"items":[
  {"metadata":{"name":"node-a"},"timestamp":"2026-04-25T17:20:42Z","window":"1m0.729s","usage":{"cpu":"168064370n","memory":"1948212Ki"}},
  {"metadata":{"name":"node-b"},"timestamp":"2026-04-25T17:20:42Z","window":"1m0.392s","usage":{"cpu":"107274740n","memory":"1949744Ki"}},
  {"metadata":{"name":"node-c"},"timestamp":"2026-04-25T17:20:42Z","window":"1m1.237s","usage":{"cpu":"125100100n","memory":"2287000Ki"}}
]}`

// Mirrors the shape /api/v1/nodes returns (only the bits the scraper
// reads). Allocatable: 2 cores, 8 GiB memory. node-c carries the
// MemoryPressure flag for the parser test.
const realNodeList = `{"items":[
  {"metadata":{"name":"node-a"},"status":{
    "allocatable":{"cpu":"2","memory":"8126204Ki"},
    "conditions":[
      {"type":"Ready","status":"True"},
      {"type":"MemoryPressure","status":"False"},
      {"type":"DiskPressure","status":"False"}
    ]}},
  {"metadata":{"name":"node-b"},"status":{
    "allocatable":{"cpu":"2000m","memory":"8126204Ki"},
    "conditions":[
      {"type":"Ready","status":"True"}
    ]}},
  {"metadata":{"name":"node-c"},"status":{
    "allocatable":{"cpu":"2","memory":"8Gi"},
    "conditions":[
      {"type":"Ready","status":"True"},
      {"type":"MemoryPressure","status":"True"},
      {"type":"DiskPressure","status":"False"},
      {"type":"PIDPressure","status":"True"}
    ]}}
]}`

func TestParseNodeMetricsResponse_RealPayload(t *testing.T) {
	got, err := parseNodeMetricsResponse([]byte(realNodeMetrics))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(got))
	}
	// 168064370n / 1_000_000 = 168 millicores
	if got[0].Name != "node-a" || got[0].CPUMilli != 168 {
		t.Errorf("node-a: %+v want CPUMilli=168", got[0])
	}
	if got[0].MemKi != 1948212 {
		t.Errorf("node-a MemKi = %d want 1948212", got[0].MemKi)
	}
}

func TestParseCPUtoMilli_AllUnits(t *testing.T) {
	cases := map[string]int64{
		"":            0,
		"168064370n":  168,
		"500m":        500,
		"2":           2000,
		"500u":        0, // 500 microcores → 0 millicores rounded
	}
	for in, want := range cases {
		if got := parseCPUtoMilli(in); got != want {
			t.Errorf("parseCPUtoMilli(%q) = %d want %d", in, got, want)
		}
	}
}

func TestParseMemoryToKi_AllUnits(t *testing.T) {
	cases := map[string]int64{
		"":          0,
		"1948212Ki": 1948212,
		"8Mi":       8 * 1024,
		"8Gi":       8 * 1024 * 1024,
		"1048576":   1024, // raw bytes → KiB
	}
	for in, want := range cases {
		if got := parseMemoryToKi(in); got != want {
			t.Errorf("parseMemoryToKi(%q) = %d want %d", in, got, want)
		}
	}
}

func TestParseNodeListResponse_PressureFlags(t *testing.T) {
	got, err := parseNodeListResponse([]byte(realNodeList))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(got))
	}
	a := got["node-a"]
	if !a.Ready || a.CPUMilli != 2000 || a.MemKi != 8126204 {
		t.Errorf("node-a: %+v", a)
	}
	if len(a.Pressure) != 0 {
		t.Errorf("node-a should have no pressure: %v", a.Pressure)
	}
	b := got["node-b"]
	// "2000m" should also parse to 2000 millicores
	if b.CPUMilli != 2000 {
		t.Errorf("node-b CPUMilli = %d want 2000 (from 2000m)", b.CPUMilli)
	}
	c := got["node-c"]
	if !c.Ready {
		t.Errorf("node-c should be Ready=true (the pressure conditions don't toggle Ready)")
	}
	wantPressure := map[string]bool{"memory": false, "pid": false}
	for _, p := range c.Pressure {
		if _, ok := wantPressure[p]; ok {
			wantPressure[p] = true
		}
	}
	for k, v := range wantPressure {
		if !v {
			t.Errorf("node-c missing %s pressure: %v", k, c.Pressure)
		}
	}
	// 8Gi allocatable → 8 * 1024 * 1024 KiB
	if c.MemKi != 8*1024*1024 {
		t.Errorf("node-c MemKi = %d want %d", c.MemKi, 8*1024*1024)
	}
}

func TestK8sMetricsScraper_EndToEnd(t *testing.T) {
	// Mock API server that returns the captured payloads. Ensures
	// the cluster.nodes population end-to-end matches expectations:
	// percentages computed from allocatable, pressure flags propagated.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(realNodeList))
		case "/apis/metrics.k8s.io/v1beta1/nodes":
			_, _ = w.Write([]byte(realNodeMetrics))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	k := &K8sMetricsScraper{
		APIServerURL: srv.URL,
		Interval:     50 * time.Millisecond,
		stopCh:       make(chan struct{}),
		nodeAlloc:    map[string]nodeCapacity{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k.Start(ctx)
	defer k.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := k.Latest(); len(got) >= 3 {
			// node-a: 168 milli / 2000 milli ≈ 8.4%
			var a *ClusterNode
			for i := range got {
				if got[i].Name == "node-a" {
					a = &got[i]
					break
				}
			}
			if a == nil {
				t.Fatalf("node-a missing: %+v", got)
			}
			if a.CPUPct < 7 || a.CPUPct > 10 {
				t.Errorf("node-a CPU pct = %.1f want ~8.4", a.CPUPct)
			}
			if !a.Ready {
				t.Errorf("node-a should be Ready")
			}
			// node-c should have memory + pid pressure flags
			var c *ClusterNode
			for i := range got {
				if got[i].Name == "node-c" {
					c = &got[i]
					break
				}
			}
			if c == nil {
				t.Fatalf("node-c missing")
			}
			if !contains(strings.Join(c.Pressure, ","), "memory") {
				t.Errorf("node-c missing memory pressure: %v", c.Pressure)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("scraper never produced 3 nodes; latest = %+v", k.Latest())
}

func TestNewK8sMetricsScraper_NilOutsidePod(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	if k := NewK8sMetricsScraper(time.Second); k != nil {
		t.Errorf("expected nil outside pod, got %+v", k)
	}
}

func TestK8sMetricsScraper_StopIdempotent(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "1.2.3.4")
	k := NewK8sMetricsScraper(time.Second)
	if k == nil {
		t.Fatal("expected scraper")
	}
	k.Stop()
	k.Stop()
}
