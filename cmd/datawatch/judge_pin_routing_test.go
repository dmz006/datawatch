package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// pinRoutingConfig is the production-shaped live config: pi-prompt is the
// verification backend and the judge policy lists pi-prompt before claude-code.
func pinRoutingConfig() autonomouspkg.Config {
	return autonomouspkg.Config{
		VerificationBackend: "pi-prompt",
		VerificationModel:   "qwen3.8-64k:27b",
		JudgePolicy:         []string{"pi-prompt", "claude-code"},
	}
}

// unpinnedInvocation is a call with no task or Automaton override: it carries
// the resolved global backend but is not pinned.
func unpinnedInvocation() autonomouspkg.GuardrailInvocation {
	return autonomouspkg.GuardrailInvocation{PRDID: "p1", UnitID: "u1", GuardrailBackend: "pi-prompt"}
}

// pinnedInvocation is a call whose backend came from a task or Automaton override.
func pinnedInvocation(backend string) autonomouspkg.GuardrailInvocation {
	return autonomouspkg.GuardrailInvocation{PRDID: "p1", UnitID: "u1", GuardrailBackend: backend, GuardrailPinned: true}
}

// pinRoutingChooser builds the production chooser for req over cfg and size and
// returns it with a recorder holding the decision log reasons it wrote. The
// chooser writes its picks through the daemon's decision log, which prints the
// reason to the standard logger, so the reasons are read back from there.
func pinRoutingChooser(t *testing.T, req autonomouspkg.GuardrailInvocation, size judgeSize) (judgeBackendChooser, *pickRecorder) {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })
	choose := judgeBackendChooserForSize(context.Background(), nil, req, pinRoutingConfig(), req.GuardrailBackend, size)
	rec := &pickRecorder{}
	return func(tried []string) (string, bool) {
		before := buf.Len()
		backend, ok := choose(tried)
		for _, line := range strings.Split(buf.String()[before:], "\n") {
			const marker = "judge: policy: "
			i := strings.Index(line, marker)
			if i < 0 {
				continue
			}
			reason := line[i+len(marker):]
			if j := strings.LastIndex(reason, " (prd "); j >= 0 {
				reason = reason[:j]
			}
			rec.record(backend, "", reason)
		}
		return backend, ok
	}, rec
}

// lastReason is the reason of the recorder's last pick.
func lastReason(t *testing.T, rec *pickRecorder) string {
	t.Helper()
	if len(rec.picks) == 0 {
		t.Fatal("decision log has no picks")
	}
	return strings.SplitN(rec.picks[len(rec.picks)-1], "|", 3)[2]
}

func TestPinRoutingUnpinnedFirstPick(t *testing.T) {
	cases := []struct {
		name        string
		size        judgeSize
		wantBackend string
		wantPrefix  string
		wantHas     string
	}{
		{
			name:        "large diff goes to claude-code by the size rule",
			size:        judgeSize{Threshold: 400, Lines: 1943},
			wantBackend: "claude-code",
			wantPrefix:  "size rule",
			wantHas:     "1943 changed lines",
		},
		{
			name:        "small diff keeps the default order",
			size:        judgeSize{Threshold: 400, Lines: 212},
			wantBackend: "pi-prompt",
			wantPrefix:  "default order",
			wantHas:     "212 changed lines",
		},
		{
			name:        "unknown line count skips size routing",
			size:        judgeSize{Threshold: 400, Err: errors.New("git diff failed")},
			wantBackend: "pi-prompt",
			wantPrefix:  "default order",
			wantHas:     "changed line count unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			choose, rec := pinRoutingChooser(t, unpinnedInvocation(), c.size)
			backend, ok := choose(nil)
			if !ok || backend != c.wantBackend {
				t.Fatalf("first pick = %q %v, want %s", backend, ok, c.wantBackend)
			}
			reason := lastReason(t, rec)
			if !strings.HasPrefix(reason, c.wantPrefix) {
				t.Fatalf("reason %q does not start with %q", reason, c.wantPrefix)
			}
			if !strings.Contains(reason, c.wantHas) {
				t.Fatalf("reason %q lacks %q", reason, c.wantHas)
			}
			if strings.Contains(reason, "pinned unit backend") {
				t.Fatalf("an unpinned call logged a pin: %q", reason)
			}
		})
	}
}

func TestPinRoutingUnknownCountSaysSizeRoutingSkipped(t *testing.T) {
	choose, rec := pinRoutingChooser(t, unpinnedInvocation(), judgeSize{Threshold: 400, Err: errors.New("git diff failed")})
	if _, ok := choose(nil); !ok {
		t.Fatal("no backend chosen")
	}
	reason := lastReason(t, rec)
	if !strings.Contains(reason, "size routing skipped") || strings.Contains(reason, "size rule") {
		t.Fatalf("reason %q should say the count is unknown and size routing was skipped", reason)
	}
}

func TestPinRoutingTaskOverrideStaysOnItsBackend(t *testing.T) {
	// A task override pins the call, so a large diff does not move it.
	choose, rec := pinRoutingChooser(t, pinnedInvocation("pi-prompt"), judgeSize{Threshold: 400, Lines: 1943})
	backend, ok := choose(nil)
	if !ok || backend != "pi-prompt" {
		t.Fatalf("first pick = %q %v, want pi-prompt", backend, ok)
	}
	if b, ok := choose([]string{"pi-prompt"}); ok {
		t.Fatalf("pinned call offered %q after pi-prompt, want none", b)
	}
	reason := lastReason(t, rec)
	if !strings.Contains(reason, "pinned unit backend") {
		t.Fatalf("reason %q should name the pinned unit backend", reason)
	}
	if strings.Contains(reason, "size rule") || strings.Contains(reason, "1943") {
		t.Fatalf("reason %q mentions the size rule for a pinned call", reason)
	}
}

func TestPinRoutingPRDOverrideToClaudeCodeIsNeverMoved(t *testing.T) {
	choose, rec := pinRoutingChooser(t, pinnedInvocation("claude-code"), judgeSize{Threshold: 400, Lines: 1943})
	backend, ok := choose(nil)
	if !ok || backend != "claude-code" {
		t.Fatalf("first pick = %q %v, want claude-code", backend, ok)
	}
	if b, ok := choose([]string{"claude-code"}); ok {
		t.Fatalf("pinned call offered %q after claude-code, want none (never moved to pi-prompt)", b)
	}
	for _, p := range rec.picks {
		if strings.HasPrefix(p, "pi-prompt|") {
			t.Fatalf("pinned claude-code call picked pi-prompt: %v", rec.picks)
		}
	}
}

func TestPinRoutingTimeoutFallback(t *testing.T) {
	cases := []struct {
		name         string
		req          autonomouspkg.GuardrailInvocation
		wantAttempts []string
		wantHold     bool
	}{
		{
			name:         "unpinned moves to the next backend and is not held",
			req:          unpinnedInvocation(),
			wantAttempts: []string{"pi-prompt", "claude-code"},
			wantHold:     false,
		},
		{
			name:         "pinned runs once and is held",
			req:          pinnedInvocation("pi-prompt"),
			wantAttempts: []string{"pi-prompt"},
			wantHold:     true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Small diff, so only the timeout fallback decides the second attempt.
			choose, _ := pinRoutingChooser(t, c.req, judgeSize{Threshold: 400, Lines: 212})
			var ran []string
			ok := autonomouspkg.GuardrailVerdict{Outcome: "pass", Summary: "second backend verdict"}
			verdict, err := runJudgeAttempts(c.req, choose, time.Minute,
				func(string, string, string) {}, nil, nil, nil,
				func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
					ran = append(ran, backend)
					if len(ran) == 1 {
						att.TimedOut = true
						return autonomouspkg.GuardrailVerdict{}, nil
					}
					return ok, nil
				})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if strings.Join(ran, ",") != strings.Join(c.wantAttempts, ",") {
				t.Fatalf("attempts = %v, want %v", ran, c.wantAttempts)
			}
			if got := autonomouspkg.IsJudgeTimeoutHold(verdict); got != c.wantHold {
				t.Fatalf("timeout hold = %v, want %v (verdict %+v)", got, c.wantHold, verdict)
			}
			if !c.wantHold && (verdict.Outcome != ok.Outcome || verdict.Summary != ok.Summary) {
				t.Fatalf("verdict = %+v, want the second backend's verdict", verdict)
			}
		})
	}
}

func TestPinRoutingJudgeSizeForMeasuresOnlyUnpinned(t *testing.T) {
	prev := judgeDiffLines
	t.Cleanup(func() { judgeDiffLines = prev })
	calls := 0
	judgeDiffLines = func(context.Context, *autonomouspkg.PRD, string) (int, error) {
		calls++
		return 1943, nil
	}
	find := func(string) (*autonomouspkg.PRD, bool) { return &autonomouspkg.PRD{}, true }

	if got := judgeSizeFor(context.Background(), 400, pinnedInvocation("pi-prompt"), find); got != (judgeSize{}) || calls != 0 {
		t.Fatalf("pinned: size %+v after %d measurements, want zero size and none", got, calls)
	}
	got := judgeSizeFor(context.Background(), 400, unpinnedInvocation(), find)
	if calls != 1 || got.Lines != 1943 || got.Threshold != 400 || got.Err != nil {
		t.Fatalf("unpinned: size %+v after %d measurements, want 1943 lines at threshold 400 from one measurement", got, calls)
	}
}
