package session

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeCLIRunner is a counting CommandRunner with mutable output/error so
// tests can switch the apparent claude version between calls without any
// binary on PATH. It is only used from a single goroutine per test.
type fakeCLIRunner struct {
	out     string
	err     error
	calls   int
	lastCtx context.Context
}

func (f *fakeCLIRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls++
	f.lastCtx = ctx
	if name != claudeCLIBinaryName {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	return []byte(f.out), f.err
}

func newGuardTestManager(t *testing.T, fake *fakeCLIRunner) *Manager {
	t.Helper()
	m, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	m.SetClaudeCLIVersionRunner(fake.Run)
	return m
}

func absentToolErr() error {
	return &exec.Error{Name: claudeCLIBinaryName, Err: exec.ErrNotFound}
}

// (a) an unchanged version is not stale after a recheck.
func TestGuard_UnchangedVersionNotStaleAfterRecheck(t *testing.T) {
	fake := &fakeCLIRunner{out: "claude 2.0.55 (f742c65)"}
	m := newGuardTestManager(t, fake)

	if _, stale := m.RateLimitPatternsStatus(); !stale {
		t.Fatal("fresh manager with no recheck entry must be stale")
	}

	v1, err := m.RecheckRateLimitPatterns()
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if v1 == "" || len(v1) != patternsVersionLen {
		t.Fatalf("recheck version %q, want %d hex chars", v1, patternsVersionLen)
	}
	// The reported version is now the combined digest (patterns + known
	// tool text), which is not the pattern-only digest.
	if v1 == fingerprintPatterns(m.effectiveRateLimitPatterns()) {
		t.Error("combined version must differ from the pattern-only digest when the tool text is known")
	}

	v2, stale := m.RateLimitPatternsStatus()
	if stale {
		t.Fatalf("unchanged version must not be stale after recheck (version %q)", v2)
	}
	if v2 != v1 {
		t.Errorf("version changed across status read: %q != %q", v2, v1)
	}
}

// (b) a recheck with a different known tool text goes stale and reports a
// new combined version.
func TestGuard_KnownToolChangeGoesStale(t *testing.T) {
	fake := &fakeCLIRunner{out: "claude 2.0.55 (f742c65)"}
	m := newGuardTestManager(t, fake)

	v1, err := m.RecheckRateLimitPatterns()
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := m.RateLimitPatternsStatus(); stale {
		t.Fatal("freshly rechecked manager must not be stale")
	}

	fake.out = "claude 3.1.9 (a1b2c3d)"
	m.RefreshClaudeCLIVersion()

	v2, stale := m.RateLimitPatternsStatus()
	if !stale {
		t.Errorf("switching to a different known tool text must be stale (version %q)", v2)
	}
	if v2 == v1 {
		t.Errorf("reported version must change with the tool text: both %q", v1)
	}
	// The pattern-only digest did not move, only the tool text did.
	if v2 != fingerprintPatternsWithTool(m.effectiveRateLimitPatterns(), fake.out) {
		t.Errorf("combined version %q does not match patterns + new tool text", v2)
	}
}

// (c) an absent tool is recorded as "unknown" and never raises stale by
// itself, in either direction.
func TestGuard_AbsentToolNeverStaleOnItsOwn(t *testing.T) {
	// Unknown-only manager: recheck, then not stale.
	fake := &fakeCLIRunner{err: absentToolErr()}
	m := newGuardTestManager(t, fake)
	if got := m.ClaudeCLIVersion(); got != claudeCLIVersionUnknown {
		t.Fatalf("absent tool: ClaudeCLIVersion() = %q, want %q", got, claudeCLIVersionUnknown)
	}
	if _, stale := m.RateLimitPatternsStatus(); !stale {
		t.Fatal("no recheck entry yet must be stale")
	}
	if _, err := m.RecheckRateLimitPatterns(); err != nil {
		t.Fatal(err)
	}
	version, tool, stale := m.RateLimitGuardStatus()
	if stale {
		t.Fatalf("absent tool must not be stale after recheck (version %q tool %q)", version, tool)
	}
	if tool != claudeCLIVersionUnknown {
		t.Errorf("reported claude tool %q, want %q", tool, claudeCLIVersionUnknown)
	}

	// Recheck made while known, then the tool goes absent: still not stale.
	fake2 := &fakeCLIRunner{out: "claude 2.0.55 (f742c65)"}
	m2 := newGuardTestManager(t, fake2)
	if _, err := m2.RecheckRateLimitPatterns(); err != nil {
		t.Fatal(err)
	}
	fake2.out = ""
	fake2.err = absentToolErr()
	m2.RefreshClaudeCLIVersion()
	if got := m2.ClaudeCLIVersion(); got != claudeCLIVersionUnknown {
		t.Fatalf("tool went absent: ClaudeCLIVersion() = %q, want %q", got, claudeCLIVersionUnknown)
	}
	if _, stale := m2.RateLimitPatternsStatus(); stale {
		t.Error("known recheck followed by the tool going absent must not be stale")
	}
}

// (d) a recheck clears the stale flag raised by a known tool change.
func TestGuard_RecheckClearsStaleToolChange(t *testing.T) {
	fake := &fakeCLIRunner{out: "claude 2.0.55 (f742c65)"}
	m := newGuardTestManager(t, fake)
	if _, err := m.RecheckRateLimitPatterns(); err != nil {
		t.Fatal(err)
	}
	fake.out = "claude 3.1.9 (a1b2c3d)"
	m.RefreshClaudeCLIVersion()
	if _, stale := m.RateLimitPatternsStatus(); !stale {
		t.Fatal("setup: different known tool text must be stale")
	}
	v, err := m.RecheckRateLimitPatterns()
	if err != nil {
		t.Fatal(err)
	}
	if v != fingerprintPatternsWithTool(m.effectiveRateLimitPatterns(), fake.out) {
		t.Errorf("recheck version %q, want combined digest with the new tool text", v)
	}
	if got, stale := m.RateLimitPatternsStatus(); stale || got != v {
		t.Fatalf("after recheck: version=%q stale=%v, want %q stale=false", got, stale, v)
	}
}

// (e) the cache: one run within the TTL, a fresh run after it expires.
func TestGuard_CacheTTL(t *testing.T) {
	fake := &fakeCLIRunner{out: "claude 2.0.55 (f742c65)"}
	m := newGuardTestManager(t, fake)

	base := time.Unix(1_700_000_000, 0)
	cur := base
	m.setClaudeCLIVersionClock(func() time.Time { return cur })

	a := m.ClaudeCLIVersion()
	if a != strings.TrimSpace(fake.out) {
		t.Fatalf("ClaudeCLIVersion() = %q, want %q", a, strings.TrimSpace(fake.out))
	}
	if fake.calls != 1 {
		t.Fatalf("first read: runner called %d times, want 1", fake.calls)
	}
	if b := m.ClaudeCLIVersion(); b != a {
		t.Fatalf("cached read: %q != %q", b, a)
	}
	cur = base.Add(time.Minute) // within the 10-minute TTL
	if c := m.ClaudeCLIVersion(); c != a {
		t.Fatalf("read within TTL: %q != %q", c, a)
	}
	if fake.calls != 1 {
		t.Fatalf("within TTL: runner called %d times, want 1", fake.calls)
	}
	cur = base.Add(claudeCLIVersionCacheTTL + time.Minute)
	if d := m.ClaudeCLIVersion(); d != a {
		t.Fatalf("read after TTL: %q != %q", d, a)
	}
	if fake.calls != 2 {
		t.Fatalf("after TTL expiry: runner called %d times, want 2", fake.calls)
	}
}

// (f) the runner receives a context with a deadline near the command timeout.
func TestGuard_RunnerContextHasDeadline(t *testing.T) {
	fake := &fakeCLIRunner{out: "claude 2.0.55 (f742c65)"}
	m := newGuardTestManager(t, fake)

	before := time.Now()
	m.ClaudeCLIVersion()
	if fake.lastCtx == nil {
		t.Fatal("runner was not called with a context")
	}
	dl, ok := fake.lastCtx.Deadline()
	if !ok {
		t.Fatal("runner context carries no deadline")
	}
	if !dl.After(before) {
		t.Errorf("deadline %s is not in the future relative to %s", dl, before)
	}
	if got := dl.Sub(before); got < time.Second || got > claudeCLIVersionTimeout+time.Minute {
		t.Errorf("deadline span %s is unexpected (start %s, deadline %s)", got, before, dl)
	}
}

// (g) an old single-line recheck entry still reads as not stale when the
// patterns are unchanged and the claude text is unknown.
func TestGuard_OldSingleLineEntryUnknownTool(t *testing.T) {
	fake := &fakeCLIRunner{err: absentToolErr()}
	m := newGuardTestManager(t, fake)

	// Write the pre-tool-format entry: one line, the pattern-only digest.
	patternsOnly := fingerprintPatterns(m.effectiveRateLimitPatterns())
	if err := os.WriteFile(m.patternsRecheckedPath(), []byte(patternsOnly+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	version, tool, stale := m.RateLimitGuardStatus()
	if stale {
		t.Fatalf("old single-line entry, unchanged patterns, unknown tool must not be stale (version %q tool %q)", version, tool)
	}
	if tool != claudeCLIVersionUnknown {
		t.Errorf("absent tool: reported text %q, want %q", tool, claudeCLIVersionUnknown)
	}
	want := fingerprintPatternsWithTool(m.effectiveRateLimitPatterns(), claudeCLIVersionUnknown)
	if version != want {
		t.Errorf("version %q, want %q (unknown tool collapses to the pattern-only digest)", version, want)
	}
}

// When the claude tool text is unknown (or empty) the combined digest must
// equal the pattern-only digest, so old entries stay comparable.
func TestFingerprintUnknownToolEqualsPatternsOnly(t *testing.T) {
	patterns := []string{"rate limit", "usage limit hit", "try again later"}
	base := fingerprintPatterns(patterns)
	for _, text := range []string{claudeCLIVersionUnknown, ""} {
		if got := fingerprintPatternsWithTool(patterns, text); got != base {
			t.Errorf("tool %q: combined %q != pattern-only %q", text, got, base)
		}
	}
	if got := fingerprintPatternsWithTool(patterns, "claude 2.0.55"); got == base {
		t.Error("known tool text must change the digest")
	}
}
