package observer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleDCGM = `# HELP DCGM_FI_DEV_FB_USED Framebuffer used in MB.
# TYPE DCGM_FI_DEV_FB_USED gauge
DCGM_FI_DEV_FB_USED{gpu="0",pid="12345"} 8192
DCGM_FI_DEV_FB_USED{gpu="0",pid="22222"} 1024
DCGM_FI_PROF_PROCESS_USAGE{gpu="0",pid="12345"} 73.2
DCGM_FI_PROF_PROCESS_USAGE{gpu="0",pid="22222"} 12.0
DCGM_FI_DEV_GPU_UTIL{gpu="0"} 88.5
`

func TestParseDCGMMetrics_HappyPath(t *testing.T) {
	got, err := parseDCGMMetrics(strings.NewReader(sampleDCGM))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 pids, got %d (%+v)", len(got), got)
	}
	p1 := got[12345]
	if p1.UtilPercent != 73.2 || p1.MemUsedMB != 8192 || p1.GPUIndex != 0 {
		t.Errorf("pid 12345 = %+v", p1)
	}
	p2 := got[22222]
	if p2.UtilPercent != 12.0 || p2.MemUsedMB != 1024 {
		t.Errorf("pid 22222 = %+v", p2)
	}
}

func TestParseDCGMMetrics_IgnoresUnrelatedLines(t *testing.T) {
	got, err := parseDCGMMetrics(strings.NewReader("# comment\n\nrandom_metric{} 1.0\nDCGM_FI_PROF_PROCESS_USAGE{gpu=\"0\",pid=\"5\"} 50.0\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[5].UtilPercent != 50 {
		t.Errorf("got %+v", got)
	}
}

func TestParseDCGMMetrics_RejectsMalformed(t *testing.T) {
	got, err := parseDCGMMetrics(strings.NewReader("DCGM_FI_PROF_PROCESS_USAGE{gpu=\"0\"} 50.0\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing pid label should produce no entry, got %+v", got)
	}
}

func TestNewDCGMScraper_NilOnEmptyURL(t *testing.T) {
	if d := NewDCGMScraper("", time.Second); d != nil {
		t.Errorf("empty URL should produce nil, got %v", d)
	}
}

func TestDCGMScraper_PollsMockExporter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleDCGM))
	}))
	defer srv.Close()
	d := NewDCGMScraper(srv.URL, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)
	defer d.Stop()
	// Wait for first scrape — Start runs one synchronously, but we
	// give the goroutine a moment to update the map.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := d.Latest(); len(got) >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("scraper never produced 2 entries — got %+v", d.Latest())
}

func TestDCGMScraper_HandlesUnreachable(t *testing.T) {
	d := NewDCGMScraper("http://127.0.0.1:1/metrics", 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	d.Start(ctx)
	defer d.Stop()
	time.Sleep(150 * time.Millisecond)
	if got := d.Latest(); len(got) != 0 {
		t.Errorf("unreachable scraper should keep empty map, got %v", got)
	}
}

func TestDCGMScraper_StopIsIdempotent(t *testing.T) {
	d := NewDCGMScraper("http://x", time.Second)
	d.Stop()
	d.Stop() // must not panic
}
