package session

// BL406 Phase 1 — tests for the built-in per-backend rate-limit line table
// (rate_limit_lines.go) and the flat list it contributes to the
// patterns_version fingerprint.
//
// Coverage:
//   - TestBuiltinRateLimitFixtureDetection pins provenance bidirectionally:
//     every verbatim line in testdata/rate_limit_lines/<family>.txt is
//     detected, and every table phrase appears verbatim in the recorded
//     lines of that fixture. The fixture (and its README) is the source
//     of truth.
//   - TestBuiltinRateLimitAnchoredMatching pins the line-start (anchored)
//     matching rules, the shared marker (every backend), per-backend
//     scoping, and the leading-whitespace trim.
//   - TestBuiltinFlatFingerprintEncodesBackend pins issue: moving a phrase
//     between backends must change the patterns_version digest, because
//     writePatterns sorts and de-duplicates the flat list and would
//     otherwise erase the association.
//   - TestRateLimitDefaultWiringAndScoping drives processOutputLine with
//     the config package's own defaults (DefaultConfig.GetDetection) so
//     the detection assertion runs against the real wiring, end to end.
//   - TestRateLimitOperatorFlatListKeepsSubstringBehavior pins the
//     operator-configured list's classic flat-substring semantics (all
//     backends, anywhere on the line), including legacy phrasings that no
//     captured line leads and therefore are not built in.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fixtureDir is the canonical captured-line fixture. Paths are relative to
// the test's working directory (this package's directory).
const fixtureDir = "../../testdata/rate_limit_lines"

// readFixtureLines returns the non-empty lines of a captured-line fixture,
// exactly as recorded (surrounding whitespace preserved per line so the
// trim behavior under test stays visible).
func readFixtureLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if l == "" || l == "\r" {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		t.Fatalf("fixture %s contains no lines", name)
	}
	return lines
}

func TestBuiltinRateLimitFixtureDetection(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	fixtureFamilies := map[string][]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		family := strings.TrimSuffix(e.Name(), ".txt")
		fixtureFamilies[family] = readFixtureLines(t, e.Name())
	}
	if _, ok := fixtureFamilies["claude-code"]; !ok {
		t.Fatal("expected testdata/rate_limit_lines/claude-code.txt to exist")
	}

	for family, lines := range fixtureFamilies {
		phrases := builtinRateLimitPhrases[family]
		if len(phrases) == 0 {
			t.Fatalf("family %s has a fixture but no built-in phrases", family)
		}

		// Forward: every recorded line is a detection for its family.
		for _, line := range lines {
			if !matchesBuiltinRateLimitLines(family, line) {
				t.Errorf("%s: recorded fixture line must be detected, got miss on %q", family, line)
			}
		}

		// Reverse: every table phrase must appear verbatim (case-
		// insensitively) in the recorded lines — the fixture is the
		// provenance source, the table the derived detection input.
		text := strings.ToLower(strings.Join(lines, "\n"))
		for _, phrase := range phrases {
			if !strings.Contains(text, strings.ToLower(phrase)) {
				t.Errorf("%s: table phrase %q has no recorded fixture line containing it (provenance gap)", family, phrase)
			}
		}
	}

	// Every non-empty table family must have a fixture file; a family with
	// recorded lines but no fixture entries is an inconsistency either way.
	for family := range builtinRateLimitPhrases {
		if _, ok := fixtureFamilies[family]; !ok && len(builtinRateLimitPhrases[family]) > 0 {
			t.Errorf("family %s has table phrases but no fixture file under testdata/rate_limit_lines/", family)
		}
	}
}

func TestBuiltinRateLimitAnchoredMatching(t *testing.T) {
	hits := []struct {
		family string
		line   string
	}{
		{"claude-code", "You've hit your limit"},
		{"claude-code", "⎿  You've hit your limit · resets 10pm (America/New_York)"},
		{"claude-code", "5-hour limit reached ∙ resets 2pm"},
		{"claude-code", "Weekly limit reached"},
		{"claude-code", "Hit weekly limit"},
		{"claude-code", "Opus limit reached"},
		{"claude-code", "Sonnet limit reached"},
		{"claude-code", "You're out of extra usage · resets 11:50am (America/New_York)"},
		{"claude-code", "datawatch_rate_limited: resets 2026-10-28T16:00:00Z"},
		// The shared marker and a real notice are both indented in
		// operator-reported renders; surrounding whitespace must be ignored
		// before the line-start check.
		{"claude-code", "  DATAWATCH_RATE_LIMITED: resets in 2h"},
		{"claude-code", "  You've hit your limit · resets 10pm"},
	}
	for _, tc := range hits {
		if !matchesBuiltinRateLimitLines(tc.family, tc.line) {
			t.Errorf("%s: expected detection on %q", tc.family, tc.line)
		}
	}

	// The protocol marker is a rule for EVERY backend, including families
	// with no built-in phrases yet.
	for _, family := range []string{"pi-prompt", "opencode", "opencode-acp", "opencode-prompt", "ollama"} {
		if !matchesBuiltinRateLimitLines(family, "  DATAWATCH_RATE_LIMITED: resets in 2h") {
			t.Errorf("%s: shared protocol marker must fire for every backend", family)
		}
	}

	misses := []struct {
		family string
		line   string
	}{
		// Phrases quoted mid-sentence are never a detection, for any
		// backend.
		{"claude-code", "I checked what the security workflow prints; it outputs You've hit your limit and then sleeps"},
		{"claude-code", "task says: 'you've hit your limit' is logged to notes.md"},
		{"claude-code", "quota: 42 requests remaining"},
		// Per-backend scoping: a claude-code phrase must not fire for a
		// backend whose table is empty.
		{"pi-prompt", "You've hit your limit"},
		{"opencode", "5-hour limit reached ∙ resets 2pm"},
		// Similar-sounding lines that no table phrase leads.
		{"claude-code", "Your limit resets in a few days"},
		{"claude-code", "The DATAWATCH_RATE_LIMITED marker is documented in templates"},
	}
	for _, tc := range misses {
		if matchesBuiltinRateLimitLines(tc.family, tc.line) {
			t.Errorf("%s: expected no detection on %q", tc.family, tc.line)
		}
	}
}

// TestBuiltinFlatFingerprintEncodesBackend guards the patterns_version
// contract: writePatterns sorts and de-duplicates, so the flat list must
// encode each entry's backend family — reassigning a phrase between
// backends changes the digest even though the multiset of bare phrases
// and families is unchanged.
func TestBuiltinFlatFingerprintEncodesBackend(t *testing.T) {
	base := fingerprintPatterns(builtinRateLimitPatternsFlat())
	if base == "" {
		t.Fatal("base digest empty")
	}

	// Move the last claude-code phrase to another backend.
	family := "claude-code"
	src := builtinRateLimitPhrases[family]
	dst := "opencode"
	if len(src) == 0 || len(builtinRateLimitPhrases[dst]) != 0 {
		t.Fatal("test precondition: claude-code phrases non-empty and opencode empty")
	}
	moved := src[len(src)-1]
	orig := make([]string, len(src))
	copy(orig, src)
	defer func() {
		restored := make([]string, len(orig))
		copy(restored, orig)
		builtinRateLimitPhrases[family] = restored
		builtinRateLimitPhrases[dst] = nil
	}()
	builtinRateLimitPhrases[family] = src[:len(src)-1]
	builtinRateLimitPhrases[dst] = []string{moved}
	if got := fingerprintPatterns(builtinRateLimitPatternsFlat()); got == base {
		t.Error("moving a phrase between backends must change the flat fingerprint")
	}

	// Add a new phrase for some backend.
	builtinRateLimitPhrases["ollama"] = []string{"slot full — all busy"}
	if got := fingerprintPatterns(builtinRateLimitPatternsFlat()); got == base {
		t.Error("adding a phrase must change the flat fingerprint")
	}

	// Removing one changes it similarly.
	builtinRateLimitPhrases["ollama"] = nil
	_, hadPrefix := builtinRateLimitLinePrefix["claude-code"]
	if !hadPrefix {
		t.Fatal("test precondition: claude-code gutter prefix present")
	}
}

// TestRateLimitDefaultWiringAndScoping drives processOutputLine with the
// config package's own defaults (DefaultConfig().GetDetection), i.e. the
// real detection wiring an unset operator config produces. It uses a
// terminal (no-structured-channel) backend: sessions with a structured
// channel (claude-code's MCP, opencode-acp) are routed by the first
// branch of processOutputLine and terminate before the line-based
// rate-limit check — so this test pins what the line path actually does
// for the backends it serves.
func TestRateLimitDefaultWiringAndScoping(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	// The config package's own defaults: no operator rate-limit list, so
	// detection must come from the built-in per-backend table.
	mgr.detection = config.DefaultConfig().GetDetection("pi-prompt")

	// The shared protocol marker flips a running pi-prompt session, even
	// indented (trim before the line-start check) — the one built-in
	// rule that applies to backends with no captured phrases.
	marker := newRateLimitTestSession(t, mgr, "rlfl01", "pi-prompt")
	driveRateLimitLine(t, mgr, marker, "  DATAWATCH_RATE_LIMITED: resets in 2h")
	if got, ok := mgr.store.Get(marker.FullID); !ok || got.State != StateRateLimited {
		t.Fatalf("pi-prompt session must detect the shared marker, got ok=%v state=%v", ok, got.State)
	}

	// A claude-code phrase must NOT flip a pi-prompt session under the
	// defaults (per-backend scoping: that table is claude-code's).
	scoped := newRateLimitTestSession(t, mgr, "rlfl02", "pi-prompt")
	driveRateLimitLine(t, mgr, scoped, "You've hit your limit · resets 10pm")
	if got, ok := mgr.store.Get(scoped.FullID); !ok || got.State != StateRunning {
		t.Fatalf("pi-prompt session must stay StateRunning on a claude-code-only phrase, got ok=%v state=%v", ok, got.State)
	}

	// A mid-sentence quote of the marker stays a miss (line-start only).
	quoted := newRateLimitTestSession(t, mgr, "rlfl03", "pi-prompt")
	driveRateLimitLine(t, mgr, quoted, "the marker DATAWATCH_RATE_LIMITED: documented above means throttling")
	if got, ok := mgr.store.Get(quoted.FullID); !ok || got.State != StateRunning {
		t.Fatalf("quoted-marker line must not detect, got ok=%v state=%v", ok, got.State)
	}
}

// TestRateLimitOperatorFlatListKeepsSubstringBehavior pins that an
// operator-configured list keeps the classic flat-substring semantics:
// matched anywhere on the line, for every backend, including phrasings
// that no recorded line leads (and therefore are not in the built-in
// table). Driven on a terminal backend, the one the line path serves.
func TestRateLimitOperatorFlatListKeepsSubstringBehavior(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	// Classic operator-configured flat list (global or per-LLM): classic
	// substring-anywhere semantics, every backend.
	det := config.DefaultConfig().GetDetection("pi-prompt")
	det.RateLimitPatterns = []string{
		"rate limit exceeded",
		"quota exceeded",
		"Approaching usage limit",
	}
	mgr.detection = det

	// Mid-sentence legacy phrasing must still detect (the old seeded
	// defaults, kept only through operator config).
	legacy := newRateLimitTestSession(t, mgr, "rlfp01", "pi-prompt")
	driveRateLimitLine(t, mgr, legacy, "warning: rate limit exceeded, quota reset in 5m")
	if got, ok := mgr.store.Get(legacy.FullID); !ok || got.State != StateRateLimited {
		t.Fatalf("operator flat list must detect legacy phrasing (substring), got ok=%v state=%v", ok, got.State)
	}

	// A phrase from a claude-code capture applies to every backend under
	// an operator list — the flat path is not backend-scoped.
	cross := newRateLimitTestSession(t, mgr, "rlfp02", "pi-prompt")
	detCross := config.DefaultConfig().GetDetection("pi-prompt")
	detCross.RateLimitPatterns = []string{"You've hit your limit"}
	mgr.detection = detCross
	driveRateLimitLine(t, mgr, cross, "api error: You've hit your limit — retry later")
	if got, ok := mgr.store.Get(cross.FullID); !ok || got.State != StateRateLimited {
		t.Fatalf("operator list applies across backends, got ok=%v state=%v", ok, got.State)
	}
}
