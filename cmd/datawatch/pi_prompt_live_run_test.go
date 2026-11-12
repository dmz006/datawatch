package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This test runs a real pi print command in a private tmux pane and watches it
// with hasLiveRunnerProcess, the probe the pi-prompt exit-silence detector
// relies on. It needs a pi binary, a reachable model endpoint and several
// seconds of generation, so it only runs when DATAWATCH_LIVE_PI is 1.

const (
	piPromptLiveEnv      = "DATAWATCH_LIVE_PI"
	piPromptLiveModel    = "ollama/qwen3:8b"
	piPromptLiveTask     = "Reply with the single word ok."
	piPromptLiveInterval = 2 * time.Second
	piPromptLiveTimeout  = 300 * time.Second
)

// piPromptLiveMarkers are the lines the pi-prompt launch command prints when
// the run ends.
var piPromptLiveMarkers = []string{"DATAWATCH_COMPLETE", "DATAWATCH_PI_FAILED"}

// piPromptLiveQuote single-quotes s for a POSIX shell.
func piPromptLiveQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// piPromptLiveCommand builds a pi print command in the shape of the pi-prompt
// launch: pi's output is teed to a file, its status goes to a second file, and
// the command then prints one outcome marker. The unexported builder in the pi
// backend package cannot be called from here. Each marker is written as two
// adjacent shell words, so the echo of this command in the pane never contains
// a marker; only the command's own output does.
func piPromptLiveCommand(projectDir, outPath, statusPath string) string {
	return fmt.Sprintf("cd %s && { pi --print --mode text --approve --model %s %s; echo $? > %s; } | tee %s; "+
		"read dw_status < %s; [ -s %s ]; dw_has_output=$?; "+
		"if [ \"${dw_status:-1}\" -ne 0 ]; then printf 'DATAWATCH_''PI_FAILED: pi exited with status %%s (model %%s)\\n' \"${dw_status:-1}\" %s; "+
		"elif [ \"$dw_has_output\" -ne 0 ]; then echo DATAWATCH_'PI_FAILED: pi produced no output'; "+
		"else echo DATAWATCH_'COMPLETE: pi done'; fi",
		piPromptLiveQuote(projectDir), piPromptLiveQuote(piPromptLiveModel), piPromptLiveQuote(piPromptLiveTask),
		piPromptLiveQuote(statusPath), piPromptLiveQuote(outPath),
		piPromptLiveQuote(statusPath), piPromptLiveQuote(outPath), piPromptLiveQuote(piPromptLiveModel))
}

// piPromptLivePane returns the whole pane text, wrapped lines joined.
func piPromptLivePane(name string) string {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-S", "-", "-t", name).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// piPromptLiveMarkerIn returns the marker line the pane shows, or "".
func piPromptLiveMarkerIn(pane string) string {
	for _, line := range strings.Split(pane, "\n") {
		line = strings.TrimSpace(line)
		for _, marker := range piPromptLiveMarkers {
			if strings.HasPrefix(line, marker) {
				return line
			}
		}
	}
	return ""
}

// TestPiPromptLiveRunWithNodeRunner types a real pi print command into a bash
// pane and polls hasLiveRunnerProcess every two seconds until the pane shows an
// outcome marker or the timeout passes. pi is hosted by node, so the runner is
// seen only through its script name.
func TestPiPromptLiveRunWithNodeRunner(t *testing.T) {
	if os.Getenv(piPromptLiveEnv) != "1" {
		t.Skipf("%s is not 1", piPromptLiveEnv)
	}
	for _, tool := range []string{"pi", "tmux", "pgrep"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")

	const name = "live-pi-run"
	projectDir := t.TempDir()
	scratch := t.TempDir()
	outPath := filepath.Join(scratch, "pi.out")
	statusPath := filepath.Join(scratch, "pi.status")

	piPromptLivenessPaneStart(t, name)
	piPromptLivenessPaneTmux(t, "send-keys", "-t", name, "-l", piPromptLiveCommand(projectDir, outPath, statusPath))
	piPromptLivenessPaneTmux(t, "send-keys", "-t", name, "Enter")

	t.Logf("model=%s task=%q interval=%s timeout=%s", piPromptLiveModel, piPromptLiveTask, piPromptLiveInterval, piPromptLiveTimeout)

	start := time.Now()
	deadline := start.Add(piPromptLiveTimeout)
	aliveSamples := 0
	samples := 0
	marker := ""
	for {
		alive := hasLiveRunnerProcess(name)
		marker = piPromptLiveMarkerIn(piPromptLivePane(name))
		samples++
		if alive {
			aliveSamples++
		}
		t.Logf("sample %d at %.1fs: runner alive=%v marker=%q", samples, time.Since(start).Seconds(), alive, marker)
		if marker != "" || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(piPromptLiveInterval)
	}
	elapsed := time.Since(start)

	pane := piPromptLivePane(name)
	final := piPromptLiveMarkerIn(pane)
	t.Logf("elapsed=%.1fs samples=%d alive samples=%d final marker=%q runner alive after the marker=%v",
		elapsed.Seconds(), samples, aliveSamples, final, hasLiveRunnerProcess(name))

	if aliveSamples == 0 {
		t.Errorf("the runner was never seen alive in %d samples while pi generated", samples)
	}
	if final == "" {
		t.Errorf("no DATAWATCH_COMPLETE or DATAWATCH_PI_FAILED marker within %s; pane tail:\n%s", piPromptLiveTimeout, piPromptLiveTail(pane, 15))
	}
}

// piPromptLiveTail returns the last n lines of s.
func piPromptLiveTail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
