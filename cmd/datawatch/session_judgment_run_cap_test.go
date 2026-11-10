package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// runCapFakeClock is the clock the verifier run cap reads. Each read advances
// it by step, so consecutive polls are observed step apart.
type runCapFakeClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *runCapFakeClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.step)
	return c.now
}

// useRunCapFakeClock installs a fake clock stepping by step per poll and
// restores the wall clock when the test ends.
func useRunCapFakeClock(t *testing.T, step time.Duration) {
	t.Helper()
	clock := &runCapFakeClock{now: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), step: step}
	prev := oneShotClock
	oneShotClock = clock.read
	t.Cleanup(func() { oneShotClock = prev })
}

// useRunCapFastPoll shortens the wait loop's poll interval so the test runs
// many polls quickly, and restores it when the test ends.
func useRunCapFastPoll(t *testing.T) {
	t.Helper()
	prev := oneShotPollInterval
	oneShotPollInterval = time.Millisecond
	t.Cleanup(func() { oneShotPollInterval = prev })
}

// runCapDaemon is a fake verifier session behind the daemon endpoints the wait
// loop reads. The session stays running and its pane changes on every poll, so
// it is never idle and never stalls on the no-progress watchdog. When
// outputAfter > 0 the session writes outputFile on that status poll.
type runCapDaemon struct {
	polls atomic.Int32
	kills atomic.Int32
}

func startRunCapDaemon(t *testing.T, outputFile string, outputAfter int32) *runCapDaemon {
	t.Helper()
	d := &runCapDaemon{}
	const sessionID = "st-runcap"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%q}`, sessionID)
	})
	mux.HandleFunc("/api/sessions/"+sessionID, func(w http.ResponseWriter, _ *http.Request) {
		n := d.polls.Add(1)
		if outputAfter > 0 && n == outputAfter {
			_ = os.WriteFile(outputFile, []byte(`{"ok":true}`), 0o600)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%q,"state":"running"}`, sessionID)
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, _ *http.Request) {
		d.kills.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/output", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// The pane text carries the poll count, so it differs on every poll.
		_, _ = fmt.Fprintf(w, "pane output line %d", d.polls.Load())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointLoopbackAt(t, srv.URL)
	return d
}

// runCapFileReader reads the output file from the project directory, the way
// the real judge reader does, without a scratch copy.
func runCapFileReader(projectDir, srcName, _, _ string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(projectDir, srcName))
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}

// runCapConfig builds a config with the given verifier run cap. The no-progress
// limit is set high so only the run cap can end a session in these tests.
func runCapConfig(t *testing.T, capSeconds int) *config.Config {
	t.Helper()
	return &config.Config{
		DataDir: t.TempDir(),
		Autonomous: config.AutonomousConfig{
			VerifierNoProgressTimeoutSeconds: 3600,
			VerifierMaxRunSeconds:            capSeconds,
		},
	}
}

// A verifier session that keeps producing pane changes is progress-blind to
// the run cap: it is ended at the cap, with RunCap set on the error and the
// deferred kill sent.
func TestVerifierRunCapWaitLoopProgressingSessionEndedAtCap(t *testing.T) {
	useRunCapFastPoll(t)
	// Each poll is 10s apart; a 60s cap trips on the seventh observation.
	useRunCapFakeClock(t, 10*time.Second)

	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, "verify-out.json")
	daemon := startRunCapDaemon(t, outputFile, 0)
	cfg := runCapConfig(t, 60)

	content, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "glm-5:755b", "high",
		"autonomous:verify:task-runcap", "verify", outputFile, "verify-runcap.json", 30*time.Second, runCapFileReader, "", launchTags{})
	if err == nil {
		t.Fatalf("a progressing verifier must be ended at the run cap, got success with content %q", content)
	}
	noOutput, ok := err.(*autonomous.OneShotNoOutputError)
	if !ok {
		t.Fatalf("want *autonomous.OneShotNoOutputError, got %T (%v)", err, err)
	}
	if !noOutput.RunCap {
		t.Fatalf("the run-cap end must set RunCap, got %+v", noOutput)
	}
	if !strings.Contains(noOutput.Msg, "run cap") || !strings.Contains(noOutput.Msg, "ran ") {
		t.Fatalf("the message must name the cap and the elapsed time, got %q", noOutput.Msg)
	}
	if strings.Contains(noOutput.Msg, "no-progress watchdog") {
		t.Fatalf("the run cap must not be reported as the watchdog: %q", noOutput.Msg)
	}
	if got := daemon.kills.Load(); got != 1 {
		t.Fatalf("the existing deferred kill must end the session once, got %d kill(s)", got)
	}
}

// A verifier whose output file appears before the cap returns normally with
// the file's content, and RunCap is never set.
func TestVerifierRunCapWaitLoopOutputBeforeCapReturnsNormally(t *testing.T) {
	useRunCapFastPoll(t)
	// Each poll is 10s apart; the output appears on the third poll, far
	// before a one-hour cap could trip.
	useRunCapFakeClock(t, 10*time.Second)

	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, "verify-out.json")
	daemon := startRunCapDaemon(t, outputFile, 3)
	cfg := runCapConfig(t, 3600)

	content, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "glm-5:755b", "high",
		"autonomous:verify:task-runcap-ok", "verify", outputFile, "verify-runcap-ok.json", 30*time.Second, runCapFileReader, "", launchTags{})
	if err != nil {
		t.Fatalf("a verifier that writes its output before the cap must return normally, got %v", err)
	}
	if content != `{"ok":true}` {
		t.Fatalf("content = %q, want the written output", content)
	}
	if polls := daemon.polls.Load(); polls > 10 {
		t.Fatalf("the loop should return once the output appears, polled %d times", polls)
	}
}

// A cap of 0 means no cap: a verifier whose accumulated run time far exceeds
// any finite cap is never ended by the run cap. It keeps running until its
// output appears and then returns normally.
func TestVerifierRunCapWaitLoopZeroCapNeverEnds(t *testing.T) {
	useRunCapFastPoll(t)
	// Each poll is an hour apart, so after 20 polls the run has accumulated
	// about 19 hours of run time.
	useRunCapFakeClock(t, time.Hour)

	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, "verify-out.json")
	daemon := startRunCapDaemon(t, outputFile, 20)
	cfg := runCapConfig(t, 0)

	content, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "glm-5:755b", "high",
		"autonomous:verify:task-runcap-zero", "verify", outputFile, "verify-runcap-zero.json", 30*time.Second, runCapFileReader, "", launchTags{})
	if err != nil {
		t.Fatalf("a cap of 0 must never end the verifier; got %v", err)
	}
	if content != `{"ok":true}` {
		t.Fatalf("content = %q, want the written output", content)
	}
	if polls := daemon.polls.Load(); polls < 20 {
		t.Fatalf("the session should have been polled until its output appeared (20 polls), got %d", polls)
	}
}
