// BL173 task 2 — DCGM (NVIDIA Data Center GPU Manager) scrape for
// per-process GPU telemetry on Shape C cluster containers.
//
// Operators run NVIDIA's official DCGM exporter as a sidecar; it
// exposes Prometheus metrics on (default) :9400/metrics. We scrape
// `DCGM_FI_PROF_PROCESS_USAGE` family fields and feed per-pid
// utilisation + framebuffer usage into the StatsResponse v2 envelope.
//
// Falls back cleanly when the exporter isn't reachable — Shape A/B
// hosts without DCGM see zero per-process GPU metrics rather than an
// error. NOT yet validated against a live GPU; structure + parser are
// exercised by tests.

package observer

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DCGMProcessGPU is the per-pid GPU usage snapshot fed back into the
// observer envelope from a DCGM exporter scrape.
type DCGMProcessGPU struct {
	PID         int     `json:"pid"`
	GPUIndex    int     `json:"gpu_index"`
	UtilPercent float64 `json:"util_pct"`
	MemUsedMB   uint64  `json:"mem_used_mb"`
}

// DCGMScraper polls a DCGM exporter at a configured URL and returns
// the latest per-pid snapshot. Refresh interval defaults to 5 s.
type DCGMScraper struct {
	URL      string
	Interval time.Duration
	client   *http.Client

	mu     sync.RWMutex
	latest map[int]DCGMProcessGPU
	stopCh chan struct{}
}

// NewDCGMScraper returns a scraper pointed at url. url empty → nil
// scraper (caller treats nil as "DCGM not configured").
func NewDCGMScraper(url string, interval time.Duration) *DCGMScraper {
	if url == "" {
		return nil
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &DCGMScraper{
		URL:      url,
		Interval: interval,
		client:   &http.Client{Timeout: 3 * time.Second},
		latest:   map[int]DCGMProcessGPU{},
		stopCh:   make(chan struct{}),
	}
}

// Start begins scraping in a background goroutine; cheap when the
// exporter is unreachable.
func (d *DCGMScraper) Start(ctx context.Context) {
	if d == nil {
		return
	}
	go func() {
		t := time.NewTicker(d.Interval)
		defer t.Stop()
		// Run one immediate scrape so the first Read isn't empty.
		d.scrape(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.stopCh:
				return
			case <-t.C:
				d.scrape(ctx)
			}
		}
	}()
}

// Stop cancels the scraper goroutine. Idempotent.
func (d *DCGMScraper) Stop() {
	if d == nil {
		return
	}
	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}
}

// Latest returns the most recent per-pid snapshot. Empty map when
// the exporter hasn't responded successfully yet.
func (d *DCGMScraper) Latest() map[int]DCGMProcessGPU {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[int]DCGMProcessGPU, len(d.latest))
	for k, v := range d.latest {
		out[k] = v
	}
	return out
}

// scrape pulls /metrics from the DCGM exporter and parses the
// per-process labels into d.latest. Errors are swallowed (the
// scraper's job is to be best-effort).
func (d *DCGMScraper) scrape(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	parsed, err := parseDCGMMetrics(resp.Body)
	if err != nil {
		return
	}
	d.mu.Lock()
	d.latest = parsed
	d.mu.Unlock()
}

// parseDCGMMetrics consumes Prometheus-format text and extracts the
// DCGM_FI_PROF_PROCESS_USAGE family. Only handles the two metrics
// we care about today; ignores everything else.
//
// Example lines we parse:
//
//   DCGM_FI_PROF_PROCESS_USAGE{gpu="0",pid="12345"} 73.2
//   DCGM_FI_DEV_FB_USED{gpu="0",pid="12345"} 8192
func parseDCGMMetrics(body io.Reader) (map[int]DCGMProcessGPU, error) {
	out := map[int]DCGMProcessGPU{}
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "DCGM_FI_PROF_PROCESS_USAGE"):
			pid, gpu, val, ok := parseDCGMLine(line)
			if !ok {
				continue
			}
			e := out[pid]
			e.PID = pid
			e.GPUIndex = gpu
			e.UtilPercent = val
			out[pid] = e
		case strings.HasPrefix(line, "DCGM_FI_DEV_FB_USED"):
			pid, gpu, val, ok := parseDCGMLine(line)
			if !ok {
				continue
			}
			e := out[pid]
			e.PID = pid
			e.GPUIndex = gpu
			e.MemUsedMB = uint64(val)
			out[pid] = e
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseDCGMLine pulls (pid, gpu, value) from one Prometheus-format
// metric line. Returns ok=false when the line doesn't have the labels
// we need (which is fine — many DCGM lines are device-level, not
// per-process).
func parseDCGMLine(line string) (pid, gpu int, val float64, ok bool) {
	open := strings.Index(line, "{")
	close := strings.Index(line, "}")
	if open < 0 || close < 0 || close < open {
		return 0, 0, 0, false
	}
	labels := line[open+1 : close]
	rest := strings.TrimSpace(line[close+1:])
	pidStr := labelValue(labels, "pid")
	gpuStr := labelValue(labels, "gpu")
	if pidStr == "" {
		return 0, 0, 0, false
	}
	p, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, 0, 0, false
	}
	g, _ := strconv.Atoi(gpuStr)
	v, err := strconv.ParseFloat(strings.Fields(rest)[0], 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return p, g, v, true
}

func labelValue(labels, key string) string {
	for _, kv := range strings.Split(labels, ",") {
		kv = strings.TrimSpace(kv)
		eq := strings.Index(kv, "=")
		if eq < 0 {
			continue
		}
		if kv[:eq] != key {
			continue
		}
		v := strings.Trim(kv[eq+1:], `"`)
		return v
	}
	return ""
}

// errDCGMUnreachable is reserved for future explicit error surfacing.
var errDCGMUnreachable = errors.New("dcgm exporter unreachable")
