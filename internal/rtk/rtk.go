// Package rtk integrates with RTK (Rust Token Killer) for token savings tracking.
// RTK is a Rust CLI proxy that compresses AI coding agent output to reduce token usage.
// See: https://github.com/rtk-ai/rtk
package rtk

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// GainSummary holds aggregate token savings from RTK.
type GainSummary struct {
	TotalCommands int     `json:"total_commands"`
	TotalInput    int     `json:"total_input"`
	TotalOutput   int     `json:"total_output"`
	TotalSaved    int     `json:"total_saved"`
	AvgSavingsPct float64 `json:"avg_savings_pct"`
	TotalTimeMs   int     `json:"total_time_ms"`
}

// GainReport is the full output from `rtk gain --all --format json`.
type GainReport struct {
	Summary GainSummary `json:"summary"`
}

// SessionInfo holds per-session RTK data from `rtk session --format json`.
type SessionInfo struct {
	ID        string  `json:"session_id"`
	Date      string  `json:"date"`
	Commands  int     `json:"commands"`
	RTKCount  int     `json:"rtk_count"`
	Adoption  float64 `json:"adoption_pct"`
	OutputLen int     `json:"output_len"`
}

// Status holds the current RTK installation and hook status.
type Status struct {
	Installed    bool   `json:"installed"`
	Version      string `json:"version"`
	HooksActive  bool   `json:"hooks_active"`
	Binary       string `json:"binary"`
}

// binary stores the configured RTK binary path.
var (
	binary   = "rtk"
	binaryMu sync.RWMutex
)

// SetBinary configures the RTK binary path.
func SetBinary(b string) {
	if b != "" {
		binaryMu.Lock()
		binary = b
		binaryMu.Unlock()
	}
}

// getBinary returns the current binary path (thread-safe).
func getBinary() string {
	binaryMu.RLock()
	defer binaryMu.RUnlock()
	return binary
}

// CheckInstalled returns RTK status information.
func CheckInstalled() Status {
	bin := getBinary()
	s := Status{Binary: bin}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return s
	}
	s.Installed = true
	s.Version = strings.TrimSpace(string(out))

	// Check if hooks are installed by looking for the warning
	hookOut, err := exec.Command(bin, "session").CombinedOutput()
	if err == nil {
		s.HooksActive = !strings.Contains(string(hookOut), "No hook installed")
	}
	return s
}

// EnsureInit runs `rtk init -g` if hooks are not installed.
// Returns true if init was run, false if already initialized.
func EnsureInit() (bool, error) {
	status := CheckInstalled()
	if !status.Installed {
		return false, fmt.Errorf("rtk not installed (binary: %s)", getBinary())
	}
	if status.HooksActive {
		return false, nil // already initialized
	}
	out, err := exec.Command(getBinary(), "init", "-g").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("rtk init -g: %w\n%s", err, string(out))
	}
	return true, nil
}

// GetGain returns aggregate token savings data.
func GetGain() (*GainReport, error) {
	out, err := exec.Command(getBinary(), "gain", "--all", "--format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("rtk gain: %w", err)
	}
	var report GainReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("parse rtk gain: %w", err)
	}
	return &report, nil
}

// GetProjectGain returns token savings for a specific project directory.
func GetProjectGain(projectDir string) (*GainReport, error) {
	cmd := exec.Command(getBinary(), "gain", "--project", "--format", "json")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rtk gain --project: %w", err)
	}
	var report GainReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("parse rtk gain: %w", err)
	}
	return &report, nil
}

// DiscoverResult holds a single missed optimization from rtk discover.
type DiscoverResult struct {
	Command          string  `json:"command"`
	Count            int     `json:"count"`
	RTKEquivalent    string  `json:"rtk_equivalent"`
	Category         string  `json:"category"`
	EstSavingsTokens int     `json:"estimated_savings_tokens"`
	EstSavingsPct    float64 `json:"estimated_savings_pct"`
}

// DiscoverReport is the output from rtk discover --format json.
type DiscoverReport struct {
	SessionsScanned int              `json:"sessions_scanned"`
	TotalCommands   int              `json:"total_commands"`
	AlreadyRTK      int              `json:"already_rtk"`
	SinceDays       int              `json:"since_days"`
	Supported       []DiscoverResult `json:"supported"`
}

// GetDiscover returns commands that could benefit from RTK compression.
func GetDiscover(sinceDays int) (*DiscoverReport, error) {
	if sinceDays <= 0 {
		sinceDays = 7
	}
	out, err := exec.Command(getBinary(), "discover", "--all",
		"--since", fmt.Sprintf("%d", sinceDays), "--format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("rtk discover: %w", err)
	}
	var report DiscoverReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("parse rtk discover: %w", err)
	}
	return &report, nil
}

// CollectStats returns RTK data formatted for the stats dashboard.
// Called periodically from the stats collection goroutine.
func CollectStats() map[string]interface{} {
	status := CheckInstalled()
	result := map[string]interface{}{
		"installed":    status.Installed,
		"version":      status.Version,
		"hooks_active": status.HooksActive,
	}
	if !status.Installed {
		return result
	}
	gain, err := GetGain()
	if err == nil && gain != nil {
		result["total_commands"] = gain.Summary.TotalCommands
		result["total_saved"] = gain.Summary.TotalSaved
		result["avg_savings_pct"] = gain.Summary.AvgSavingsPct
		result["total_output"] = gain.Summary.TotalOutput
	}
	return result
}

// StartCollector runs a background goroutine that periodically collects
// RTK stats and calls the provided callback with the data.
func StartCollector(interval time.Duration, callback func(map[string]interface{})) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		// Collect immediately on start
		callback(CollectStats())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			callback(CollectStats())
		}
	}()
}
