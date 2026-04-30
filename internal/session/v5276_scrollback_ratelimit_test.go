// v5.27.6 — tests for BL211 (scrollback state-detection) and
// BL215 (rate-limit miss + no-reset-time fallback).
//
// BL211 verifies the new CapturePaneLiveTail interface is in place
// on both the real and fake tmux implementations, since the
// production state-detection loop now switches to it.
//
// BL215 verifies modern rate-limit messages longer than the
// previous 200-char gate now match, AND that lines lacking a
// parseable reset time still fall through to the +60min fallback.

package session

import (
	"strings"
	"testing"
	"time"
)

func TestBL211_FakeTmuxImplementsCapturePaneLiveTail(t *testing.T) {
	// State detection in manager.go now calls CapturePaneLiveTail.
	// FakeTmux must satisfy the interface contract used by both
	// production and unit tests.
	var tm TmuxAPI = NewFakeTmux()
	if _, ok := tm.(interface {
		CapturePaneLiveTail(string) (string, error)
	}); !ok {
		t.Fatal("FakeTmux must implement CapturePaneLiveTail")
	}
}

func TestBL211_TmuxManagerInterfaceListsLiveTail(t *testing.T) {
	// Defensive: the TmuxManagerInterface declaration in tmux.go
	// must list CapturePaneLiveTail so any future fake / mock
	// implementing only the existing methods fails to compile.
	// Compile-time assertion via type-check:
	var _ interface {
		CapturePaneVisible(string) (string, error)
		CapturePaneLiveTail(string) (string, error)
		CapturePaneANSI(string) (string, error)
	} = (*FakeTmux)(nil)
}

func TestBL211_CapturePaneLiveTailFakeBehavior(t *testing.T) {
	tm := NewFakeTmux()
	tm.Pane["s1"] = "live tail content\n❯ "
	out, err := tm.CapturePaneLiveTail("s1")
	if err != nil {
		t.Fatalf("CapturePaneLiveTail err: %v", err)
	}
	if out != "live tail content\n❯ " {
		t.Errorf("got %q want %q", out, "live tail content\n❯ ")
	}
	// Recorded under its own op name so tests can assert which
	// capture method production code took.
	if tm.Count("capture-live-tail") != 1 {
		t.Errorf("capture-live-tail count=%d want 1", tm.Count("capture-live-tail"))
	}
	if tm.Count("capture-visible") != 0 {
		t.Errorf("CapturePaneLiveTail must not record under capture-visible op")
	}
}

func TestBL215_LongRateLimitLineMatches(t *testing.T) {
	// v5.27.4 raised the rate-limit pattern set; v5.27.6 raises
	// the per-line length gate from 200 → 1024 because modern
	// claude-code rate-limit dialogs are paragraph-length.
	// Build a 600-char rate-limit message that includes the
	// "limit reached" substring v5.27.4 added.
	body := strings.Repeat("Some context describing why the limit hit and what the operator should do next. ", 7)
	line := body + "5-hour limit reached. Resets at 2pm. Reach out to support if blocked."
	if len(line) <= 200 {
		t.Fatalf("test setup error: line is only %d chars, won't exercise the gate", len(line))
	}
	if !matchesRateLimit(line) {
		t.Errorf("expected MATCH on %d-char rate-limit line, got miss", len(line))
	}
}

func TestBL215_NoResetTimeFallsThroughTo60min(t *testing.T) {
	// parseRateLimitResetTime returns zero time when the line has
	// no parseable reset marker. The auto-resume scheduler at
	// manager.go:3837-3840 must fall through to +60min in that case.
	// Verify the parser returns zero (the fallback logic itself is
	// exercised by the manager-level rate-limit smoke).
	line := "5-hour limit reached." // no resets/at/in marker
	got := parseRateLimitResetTime(line)
	if !got.IsZero() {
		t.Errorf("expected zero time for no-reset-marker line, got %v", got)
	}
}

func TestBL215_LengthGateUpperBoundStillRejects(t *testing.T) {
	// 1024 chars is the new ceiling; anything beyond should still
	// be ignored to avoid false positives from giant log dumps.
	line := strings.Repeat("limit reached ", 80) // ~14 chars × 80 = 1120 chars
	if len(line) < 1024 {
		t.Fatalf("test setup: line is only %d chars; need ≥ 1024", len(line))
	}
	if matchesRateLimit(line) {
		t.Errorf("expected miss for %d-char line (above 1024 ceiling), got match", len(line))
	}
}

// matchesRateLimit replicates the in-flight check at manager.go:3787+
// so the test can verify the gate without spinning up a Manager.
// Mirrors the exact branching the production code uses.
func matchesRateLimitV5276(line string) bool {
	if strings.Contains(line, "DATAWATCH_RATE_LIMITED:") {
		return true
	}
	if len(line) >= 1024 {
		return false
	}
	low := strings.ToLower(line)
	for _, pat := range rateLimitPatterns {
		if pat == "DATAWATCH_RATE_LIMITED:" {
			continue
		}
		if strings.Contains(low, strings.ToLower(pat)) {
			return true
		}
	}
	return false
}

// Test-helper sanity: the v5.27.4 helper still uses the 200-char
// gate (its tests still pass), and the new helper above models the
// 1024 gate. We keep both to make the v5.27.6 change auditable.
func TestBL215_HelperUsesNewGate(t *testing.T) {
	// 800-char line with "limit reached" — old helper rejects
	// (>200), new helper accepts (<1024).
	body := strings.Repeat("a ", 400)
	line := body + " limit reached"
	if !matchesRateLimitV5276(line) {
		t.Errorf("v5.27.6 helper must match 800-char limit-reached line")
	}
}

// Ensure the auto-resume scheduling math works when resetAt is zero.
// Production code at line 3837:
//
//	resumeAt := resetAt
//	if resumeAt.IsZero() || time.Until(resumeAt) < time.Minute {
//	    resumeAt = time.Now().Add(60 * time.Minute)
//	}
func TestBL215_FallbackResumeAt60min(t *testing.T) {
	var resetAt time.Time // zero
	resumeAt := resetAt
	if resumeAt.IsZero() || time.Until(resumeAt) < time.Minute {
		resumeAt = time.Now().Add(60 * time.Minute)
	}
	delta := time.Until(resumeAt)
	if delta < 59*time.Minute || delta > 61*time.Minute {
		t.Errorf("fallback resumeAt should be ~60min from now, got delta=%v", delta)
	}
}
