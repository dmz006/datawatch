package session

import (
	"testing"
	"time"
)

// now is the shared reference clock for the liveness tests. All "fake" inputs
// are expressed relative to this single instant so the silence-window math is
// deterministic. No tmux, no daemon, no filesystem.
var livenessNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

const livenessWindow = 30 * time.Second

// base returns a LivenessInput pre-filled with the shared clock and a 30 s
// window; table cases override only the fields under test.
func baseLivenessInput(family string) LivenessInput {
	return LivenessInput{
		BackendFamily: family,
		Now:           livenessNow,
		Window:        livenessWindow,
	}
}

// TestBackendLiveness_PrintMode drives the pi-prompt rules: a print-mode
// backend writes nothing until it finishes, so "alive + writing" is a running
// print run, "alive + silent" is a stalled one, and a dead process is always
// stalled.
func TestBackendLiveness_PrintMode(t *testing.T) {
	family := "pi-prompt"
	cases := []struct {
		name         string
		processAlive bool
		lastActivity time.Time // zero = no activity observed
		atPrompt     bool      // ignored for print-mode, but asserted to be ignored
		want         LivenessVerdict
	}{
		{"alive and writing", true, livenessNow.Add(-5 * time.Second), false, LivenessRunning},
		{"alive at activity boundary", true, livenessNow.Add(-livenessWindow), false, LivenessRunning},
		{"alive and silent", true, livenessNow.Add(-livenessWindow - time.Second), false, LivenessStalled},
		{"alive and no activity observed", true, time.Time{}, false, LivenessStalled},
		{"dead while writing", false, livenessNow.Add(-5 * time.Second), false, LivenessStalled},
		{"dead and silent", false, livenessNow.Add(-time.Minute), false, LivenessStalled},
		// atPrompt must not change the print-mode answer: the pane of a print
		// backend never carries a real interactive input prompt.
		{"alive writing with atPrompt ignored", true, livenessNow.Add(-5 * time.Second), true, LivenessRunning},
		{"alive silent with atPrompt ignored", true, livenessNow.Add(-time.Minute), true, LivenessStalled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseLivenessInput(family)
			in.PrintMode = true
			in.ProcessAlive = tc.processAlive
			in.LastActivity = tc.lastActivity
			in.AtPrompt = tc.atPrompt
			if got := ClassifyLiveness(in); got != tc.want {
				t.Errorf("ClassifyLiveness(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestBackendLiveness_Interactive covers the non-claude-code interactive
// families' common rules: dead → stalled, at a real prompt → waiting_input,
// alive and writing → running. Silent-beyond-window is exercised separately
// (TestBackendLiveness_Silent) because its answer depends on the opt-in and
// the claude-code busy-probe exception.
func TestBackendLiveness_Interactive(t *testing.T) {
	families := []string{"pi", "opencode", "opencode-acp", "goose", "gemini", "aider", "ollama", "openwebui"}
	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			cases := []struct {
				name         string
				processAlive bool
				lastActivity time.Time
				atPrompt     bool
				want         LivenessVerdict
			}{
				{"alive and writing", true, livenessNow.Add(-5 * time.Second), false, LivenessRunning},
				{"dead", false, livenessNow.Add(-5 * time.Second), false, LivenessStalled},
				{"at a real prompt", true, livenessNow.Add(-time.Minute), true, LivenessWaitingInput},
				{"at prompt while dead is stalled", false, time.Time{}, true, LivenessStalled},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					in := baseLivenessInput(family)
					in.ProcessAlive = tc.processAlive
					in.LastActivity = tc.lastActivity
					in.AtPrompt = tc.atPrompt
					if got := ClassifyLiveness(in); got != tc.want {
						t.Errorf("ClassifyLiveness(%q) = %v, want %v", tc.name, got, tc.want)
					}
				})
			}
		})
	}
}

// TestBackendLiveness_Silent covers the silent-beyond-window interactive case
// across all required families: claude-code stays running (busy-probe
// semantics), the other families defer to the gap watcher (no opinion) unless
// the caller opts in, in which case they are stalled.
func TestBackendLiveness_Silent(t *testing.T) {
	families := []string{"claude-code", "pi", "opencode", "opencode-acp", "goose", "gemini", "aider", "ollama", "openwebui"}
	silent := livenessNow.Add(-livenessWindow - time.Second)
	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			wantOptIn := LivenessStalled
			wantDefault := LivenessNoOpinion
			if family == "claude-code" {
				// claude-code keeps the busy-probe answer regardless of opt-in.
				wantDefault = LivenessRunning
				wantOptIn = LivenessRunning
			}
			t.Run("silent, no opt-in", func(t *testing.T) {
				in := baseLivenessInput(family)
				in.ProcessAlive = true
				in.LastActivity = silent
				in.StallOthers = false
				if got := ClassifyLiveness(in); got != wantDefault {
					t.Errorf("ClassifyLiveness(%q, silent, opt-in=false) = %v, want %v", family, got, wantDefault)
				}
			})
			t.Run("silent, opt-in", func(t *testing.T) {
				in := baseLivenessInput(family)
				in.ProcessAlive = true
				in.LastActivity = silent
				in.StallOthers = true
				if got := ClassifyLiveness(in); got != wantOptIn {
					t.Errorf("ClassifyLiveness(%q, silent, opt-in=true) = %v, want %v", family, got, wantOptIn)
				}
			})
		})
	}
}

// TestBackendLiveness_WindowDefault pins the default silence window: a
// non-positive window falls back to DefaultLivenessWindow, so an activity
// inside the default window still reads as running and one just past it does
// not.
func TestBackendLiveness_WindowDefault(t *testing.T) {
	if DefaultLivenessWindow <= 0 {
		t.Fatalf("DefaultLivenessWindow = %v, want > 0", DefaultLivenessWindow)
	}
	cases := []struct {
		name         string
		lastActivity time.Time
		want         LivenessVerdict
	}{
		{"within default window", livenessNow.Add(-DefaultLivenessWindow + time.Second), LivenessRunning},
		{"just past default window", livenessNow.Add(-DefaultLivenessWindow - time.Second), LivenessStalled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseLivenessInput("pi-prompt")
			in.PrintMode = true
			in.ProcessAlive = true
			in.LastActivity = tc.lastActivity
			in.Window = 0 // force the default
			if got := ClassifyLiveness(in); got != tc.want {
				t.Errorf("ClassifyLiveness(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
