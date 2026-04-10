package pipeline

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// QualityGateConfig controls test regression detection.
type QualityGateConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	TestCommand      string `yaml:"test_command" json:"test_command"` // e.g. "go test ./..."
	Timeout          int    `yaml:"timeout" json:"timeout"`          // seconds, default 300
	BlockOnRegression bool  `yaml:"block_on_regression" json:"block_on_regression"`
}

// TestResult holds the result of running a test command.
type TestResult struct {
	Command    string    `json:"command"`
	ExitCode   int       `json:"exit_code"`
	Output     string    `json:"output"`
	PassCount  int       `json:"pass_count"`
	FailCount  int       `json:"fail_count"`
	Duration   float64   `json:"duration_sec"`
	RunAt      time.Time `json:"run_at"`
}

// QualityGateResult is the comparison between baseline and current test runs.
type QualityGateResult struct {
	Baseline    TestResult `json:"baseline"`
	Current     TestResult `json:"current"`
	NewFailures int        `json:"new_failures"` // failures not in baseline
	Regression  bool       `json:"regression"`   // true if new failures found
	Improved    bool       `json:"improved"`     // true if fewer failures
	Summary     string     `json:"summary"`
}

// RunTests executes the test command and parses results.
func RunTests(command, projectDir string, timeout int) TestResult {
	if timeout <= 0 {
		timeout = 300
	}

	start := time.Now()
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return TestResult{Command: command, ExitCode: -1, Output: "empty command"}
	}

	cmd := exec.Command(parts[0], parts[1:]...) //nolint:gosec
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()

	result := TestResult{
		Command:  command,
		Output:   string(out),
		Duration: time.Since(start).Seconds(),
		RunAt:    time.Now(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	// Parse pass/fail counts from output (Go test format)
	for _, line := range strings.Split(result.Output, "\n") {
		if strings.Contains(line, "PASS") {
			result.PassCount++
		}
		if strings.Contains(line, "FAIL") {
			result.FailCount++
		}
	}

	return result
}

// CompareResults compares baseline and current test results.
func CompareResults(baseline, current TestResult) QualityGateResult {
	result := QualityGateResult{
		Baseline: baseline,
		Current:  current,
	}

	if current.FailCount > baseline.FailCount {
		result.NewFailures = current.FailCount - baseline.FailCount
		result.Regression = true
		result.Summary = fmt.Sprintf("REGRESSION: %d new test failures (%d → %d)",
			result.NewFailures, baseline.FailCount, current.FailCount)
	} else if current.FailCount < baseline.FailCount {
		result.Improved = true
		result.Summary = fmt.Sprintf("IMPROVED: %d fewer failures (%d → %d)",
			baseline.FailCount-current.FailCount, baseline.FailCount, current.FailCount)
	} else {
		result.Summary = fmt.Sprintf("STABLE: %d pass, %d fail (unchanged)",
			current.PassCount, current.FailCount)
	}

	return result
}
