// B98 — tests for the aider pane-log usage-tracking helpers.

package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAiderTokenCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"610", 610, true},
		{"0", 0, true},
		{"1.2k", 1200, true},
		{"9.9k", 9900, true},
		{"15k", 15000, true},
		{"not-a-number", 0, false},
	}
	for _, c := range cases {
		got, ok := parseAiderTokenCount(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseAiderTokenCount(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestAiderTokensRe_MatchesRealCapturedLine(t *testing.T) {
	// Captured verbatim from a real tmux pipe-pane log of `aider
	// --model ollama_chat/qwen3:1.7b --yes --message 'say hi only'`.
	line := "Tokens: 767 sent, 727 received."
	m := aiderTokensRe.FindStringSubmatch(line)
	if m == nil || m[1] != "767" || m[2] != "727" {
		t.Fatalf("got %v, want sent=767 received=727", m)
	}
}

func TestAiderTokensRe_MatchesKFormat(t *testing.T) {
	// format_tokens() (aider/utils.py) switches to "X.Yk" at 1,000 and
	// "Nk" (no decimal) at 10,000 -- confirmed by reading aider 0.86.2's
	// own source (aider/coders/base_coder.py ~line 2023).
	line := "Tokens: 1.2k sent, 15k received."
	m := aiderTokensRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatal("no match")
	}
	sent, ok1 := parseAiderTokenCount(m[1])
	received, ok2 := parseAiderTokenCount(m[2])
	if !ok1 || !ok2 || sent != 1200 || received != 15000 {
		t.Fatalf("got sent=%d(%v) received=%d(%v), want 1200/true 15000/true", sent, ok1, received, ok2)
	}
}

func TestAiderTokensRe_ToleratesCacheSegments(t *testing.T) {
	line := "Tokens: 610 sent, 50 cache write, 10 cache hit, 146 received."
	m := aiderTokensRe.FindStringSubmatch(line)
	if m == nil || m[1] != "610" || m[2] != "146" {
		t.Fatalf("got %v, want sent=610 received=146 (cache segments ignored)", m)
	}
}

func TestScanAiderUsageOnce_SumsOnlyNewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pane.log")
	f, err := os.Create(path) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("Model: ollama_chat/qwen3:1.7b\nTokens: 610 sent, 146 received.\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	const sessID = "sess-aider-sums-test"
	t.Cleanup(func() { aiderUsageLinesRead.Delete(sessID) })

	var gotIn, gotOut, calls int
	report := func(_ string, tokensIn, tokensOut, linesRead int) {
		calls++
		gotIn += tokensIn
		gotOut += tokensOut
	}

	scanAiderUsageOnce(path, sessID, report)
	if calls != 1 || gotIn != 610 || gotOut != 146 {
		t.Fatalf("first scan: calls=%d in=%d out=%d, want 1/610/146", calls, gotIn, gotOut)
	}

	// No new lines -- must not double-count.
	scanAiderUsageOnce(path, sessID, report)
	if calls != 1 {
		t.Fatalf("second scan with no new data should not call report again: calls=%d", calls)
	}

	// Append a second turn's report.
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("Tokens: 1.2k sent, 300 received.\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	scanAiderUsageOnce(path, sessID, report)
	if calls != 2 || gotIn != 1810 || gotOut != 446 {
		t.Fatalf("after append: calls=%d in=%d out=%d, want 2/1810/446 (610+1200, 146+300)", calls, gotIn, gotOut)
	}
}

func TestScanAiderUsageOnce_MissingFileIsNoop(t *testing.T) {
	called := false
	scanAiderUsageOnce(filepath.Join(t.TempDir(), "does-not-exist.log"), "sess-aider-missing-test", func(string, int, int, int) { called = true })
	if called {
		t.Error("a missing log file must not call report")
	}
}

func TestScanAiderUsageOnce_StripsANSIBeforeMatching(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pane.log")
	f, err := os.Create(path) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	// SGR color wrapping the whole line, as a defensive case even though
	// the real captured line (see TestAiderTokensRe_MatchesRealCapturedLine)
	// was plain text.
	if _, err := f.WriteString("\x1b[32mTokens: 610 sent, 146 received.\x1b[0m\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	const sessID = "sess-aider-ansi-test"
	t.Cleanup(func() { aiderUsageLinesRead.Delete(sessID) })
	var gotIn, gotOut int
	scanAiderUsageOnce(path, sessID, func(_ string, tokensIn, tokensOut, linesRead int) {
		gotIn, gotOut = tokensIn, tokensOut
	})
	if gotIn != 610 || gotOut != 146 {
		t.Fatalf("got in=%d out=%d, want 610/146 (ANSI-wrapped line must still match)", gotIn, gotOut)
	}
}
