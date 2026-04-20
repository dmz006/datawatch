// datawatch-agent (BL86) — lightweight stats endpoint for remote
// GPU / system telemetry.
//
// Runs on a remote machine where nvidia-smi + free + df are available
// and exposes:
//
//   GET /stats     JSON: {gpu: [...], cpu, memory, disk, hostname,
//                          timestamp}
//   GET /healthz   200 "ok"
//
// The datawatch parent polls /stats alongside the Ollama API to fill
// the GPU-utilisation columns the Ollama API doesn't surface.
//
// Single binary, no deps. Bind defaults to 0.0.0.0:9877; override
// with --listen <host:port>.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Version is overridden at build time via -ldflags.
var Version = "0.1.0"

type stats struct {
	Hostname  string    `json:"hostname"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	CPU       cpuStats  `json:"cpu"`
	Memory    memStats  `json:"memory"`
	Disk      []disk    `json:"disk,omitempty"`
	GPU       []gpu     `json:"gpu,omitempty"`
	Errors    []string  `json:"errors,omitempty"`
}

type cpuStats struct {
	LoadAverage []float64 `json:"load_average,omitempty"`
	Cores       int       `json:"cores"`
}

type memStats struct {
	TotalMB     int `json:"total_mb"`
	UsedMB      int `json:"used_mb"`
	AvailableMB int `json:"available_mb"`
}

type disk struct {
	Mount    string `json:"mount"`
	UsedPct  int    `json:"used_pct"`
	TotalGB  int    `json:"total_gb"`
}

type gpu struct {
	Index            int    `json:"index"`
	Name             string `json:"name"`
	UtilizationPct   int    `json:"utilization_pct"`
	MemoryTotalMB    int    `json:"memory_total_mb"`
	MemoryUsedMB     int    `json:"memory_used_mb"`
	TemperatureCelsius int  `json:"temperature_c"`
}

func main() {
	listen := flag.String("listen", "0.0.0.0:9877", "host:port to bind")
	showVer := flag.Bool("version", false, "print version + exit")
	flag.Parse()
	if *showVer {
		fmt.Println("datawatch-agent", Version)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/stats", handleStats)

	host, port, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatalf("invalid --listen %q: %v", *listen, err)
	}
	addr := net.JoinHostPort(host, port) // BL1 reuse for IPv6 safety
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("datawatch-agent %s listening on http://%s", Version, addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func handleStats(w http.ResponseWriter, _ *http.Request) {
	s := stats{Version: Version, Timestamp: time.Now()}
	s.Hostname, _ = os.Hostname()
	s.CPU = collectCPU(&s)
	s.Memory = collectMemory(&s)
	s.Disk = collectDisk(&s)
	s.GPU = collectGPU(&s)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s)
}

func collectCPU(s *stats) cpuStats {
	out := cpuStats{Cores: parsedCoreCount()}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		for i := 0; i < 3 && i < len(fields); i++ {
			if v, err := strconv.ParseFloat(fields[i], 64); err == nil {
				out.LoadAverage = append(out.LoadAverage, v)
			}
		}
	} else {
		s.Errors = append(s.Errors, "loadavg: "+err.Error())
	}
	return out
}

func parsedCoreCount() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}
	return count
}

func collectMemory(s *stats) memStats {
	out := memStats{}
	cmd := exec.Command("free", "-m")
	data, err := cmd.Output()
	if err != nil {
		s.Errors = append(s.Errors, "free: "+err.Error())
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Mem:") {
			continue
		}
		fields := strings.Fields(line)
		// Mem: total used free shared buff/cache available
		if len(fields) >= 7 {
			out.TotalMB, _ = strconv.Atoi(fields[1])
			out.UsedMB, _ = strconv.Atoi(fields[2])
			out.AvailableMB, _ = strconv.Atoi(fields[6])
		}
	}
	return out
}

func collectDisk(s *stats) []disk {
	out := []disk{}
	cmd := exec.Command("df", "-h", "--output=target,size,pcent")
	data, err := cmd.Output()
	if err != nil {
		s.Errors = append(s.Errors, "df: "+err.Error())
		return out
	}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mount := fields[0]
		if !strings.HasPrefix(mount, "/") {
			continue
		}
		var d disk
		d.Mount = mount
		d.TotalGB = parseSizeGB(fields[1])
		pct := strings.TrimSuffix(fields[2], "%")
		d.UsedPct, _ = strconv.Atoi(pct)
		out = append(out, d)
	}
	return out
}

func parseSizeGB(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	suffix := s[len(s)-1]
	num, err := strconv.ParseFloat(s[:len(s)-1], 64)
	if err != nil {
		return 0
	}
	switch suffix {
	case 'T':
		return int(num * 1024)
	case 'G':
		return int(num)
	case 'M':
		return int(num / 1024)
	}
	return 0
}

func collectGPU(s *stats) []gpu {
	out := []gpu{}
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return out // no NVIDIA GPU; not an error
	}
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.total,memory.used,temperature.gpu",
		"--format=csv,noheader,nounits")
	data, err := cmd.Output()
	if err != nil {
		s.Errors = append(s.Errors, "nvidia-smi: "+err.Error())
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 6 {
			continue
		}
		var g gpu
		g.Index, _ = strconv.Atoi(strings.TrimSpace(fields[0]))
		g.Name = strings.TrimSpace(fields[1])
		g.UtilizationPct, _ = strconv.Atoi(strings.TrimSpace(fields[2]))
		g.MemoryTotalMB, _ = strconv.Atoi(strings.TrimSpace(fields[3]))
		g.MemoryUsedMB, _ = strconv.Atoi(strings.TrimSpace(fields[4]))
		g.TemperatureCelsius, _ = strconv.Atoi(strings.TrimSpace(fields[5]))
		out = append(out, g)
	}
	return out
}
