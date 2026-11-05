package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesDigestKeepsHeadingsAndMandatoryLines(t *testing.T) {
	content := `# General

Intro prose about the layout.

You must keep the log.
Mandatory: keep build artifacts around.
### Detail

This is ordinary text describing a workflow.
- Never delete the cache.
- This step is operator-required.
- These rules carry no exceptions.
`
	d := BuildRulesDigest([]byte(content), 4096)
	again := BuildRulesDigest([]byte(content), 4096)
	if d != again {
		t.Fatal("digest is not deterministic")
	}
	if !strings.Contains(d, "Rules digest (generated, 2 sections, 5 mandatory lines):") {
		t.Fatalf("bad header in digest:\n%s", d)
	}
	for _, want := range []string{
		"General",
		"Detail",
		"You must keep the log.",
		"Mandatory: keep build artifacts around.",
		"Never delete the cache.",
		"This step is operator-required.",
		"These rules carry no exceptions.",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("digest missing %q:\n%s", want, d)
		}
	}
	for _, absent := range []string{
		"Intro prose about the layout.",
		"ordinary text describing a workflow",
	} {
		if strings.Contains(d, absent) {
			t.Errorf("digest kept ordinary prose %q:", absent)
		}
	}
}

func TestRulesDigestSkipsCodeFences(t *testing.T) {
	content := "# Setup\n\nMUST read this before starting.\n\n" +
		"```\n" +
		"must be skipped inside the fence\n" +
		"# Heading inside a fence\n" +
		"```\n" +
		"\n" +
		"Always verify the build.\n"
	d := BuildRulesDigest([]byte(content), 4096)
	if !strings.Contains(d, "MUST read this before starting.") {
		t.Errorf("missing kept line outside fence:\n%s", d)
	}
	if !strings.Contains(d, "Always verify the build.") {
		t.Errorf("missing kept line after fence:\n%s", d)
	}
	if strings.Contains(d, "inside the fence") {
		t.Fatalf("fenced content leaked into digest:\n%s", d)
	}
	if got := RulesDigestSections([]byte(content)); len(got) != 1 || got[0] != "Setup" {
		t.Fatalf("sections = %v, want [Setup]", got)
	}
}

func TestRulesDigestPreamble(t *testing.T) {
	d := BuildRulesDigest([]byte("This must happen first\n"), 1000)
	lines := strings.Split(d, "\n")
	if len(lines) != 3 {
		t.Fatalf("want header, preamble, one line; got %d lines:\n%s", len(lines), d)
	}
	if !strings.HasSuffix(lines[0], "0 sections, 1 mandatory lines):") {
		t.Errorf("bad header: %q", lines[0])
	}
	if lines[1] != "(preamble)" || lines[2] != "  This must happen first" {
		t.Errorf("bad preamble block: %q / %q", lines[1], lines[2])
	}
}

func TestRulesDigestLongLineClipped(t *testing.T) {
	long := strings.Repeat("a", 300) + " MUST be clipped."
	d := BuildRulesDigest([]byte("# H\n"+long+"\n"), 4096)
	found := false
	for _, line := range strings.Split(d, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "...") {
			found = true
			if len(trimmed) != rulesDigestLineCap {
				t.Errorf("clipped line is %d bytes, want %d", len(trimmed), rulesDigestLineCap)
			}
		}
		if len(trimmed) > rulesDigestLineCap && !strings.HasPrefix(line, "Rules digest") {
			t.Errorf("line over cap: %d bytes", len(trimmed))
		}
	}
	if !found {
		t.Fatalf("no clipped line in digest:\n%s", d)
	}
}

func TestRulesDigestCapDropsTailLinesThenMarker(t *testing.T) {
	content := "# One very long heading name that is indeed quite long\n" +
		"Alpha line that must stay.\n" +
		"# Another long heading name that stretches out quite a lot\n" +
		"Bravo line that always stays.\n" +
		"Charlie line that is required.\n"

	// Full digest is 253 bytes; at 200 the tail line drops so it fits
	// without the marker.
	d := BuildRulesDigest([]byte(content), 200)
	if len(d) > 200 {
		t.Fatalf("digest is %d bytes over cap 200:\n%s", len(d), d)
	}
	if !strings.Contains(d, "Alpha line that must stay.") || strings.Contains(d, "Charlie line") {
		t.Fatalf("tail lines were not dropped from the end:\n%s", d)
	}
	if strings.Contains(d, "digest truncated") {
		t.Fatalf("marker present though it fit after dropping:\n%s", d)
	}

	// Even with no mandatory lines left, the headings alone overflow 120
	// bytes: cut at a line boundary and append the truncation note.
	small := BuildRulesDigest([]byte(content), 120)
	if len(small) > 120 {
		t.Fatalf("digest is %d bytes over cap 120:\n%s", len(small), small)
	}
	if !strings.HasSuffix(small, "[digest truncated at 120 bytes]") {
		t.Fatalf("missing truncation note:\n%s", small)
	}
}

func TestRulesDigestEmptyFile(t *testing.T) {
	if got := BuildRulesDigest(nil, 4096); got != "" {
		t.Fatalf("empty content digest = %q, want empty", got)
	}
	if got := BuildRulesDigest([]byte(""), 4096); got != "" {
		t.Fatalf("empty content digest = %q, want empty", got)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var m Manager
	sum := sha256.Sum256(nil)
	d, sha, size, err := m.rulesDigestFor(dir, "AGENT.md", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if d != "" || size != 0 || sha != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest=%q sha=%s size=%d", d, sha, size)
	}
}

func TestRulesDigestMaxBytesZero(t *testing.T) {
	content := "# H\nMUST keep this.\n"
	for _, maxBytes := range []int{0, -10} {
		if got := BuildRulesDigest([]byte(content), maxBytes); got != "" {
			t.Fatalf("maxBytes=%d: digest = %q, want empty", maxBytes, got)
		}
	}
}

func TestRulesDigestRejectsParentEscape(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	target := filepath.Join(dirB, "AGENT.md")
	if err := os.WriteFile(target, []byte("evil MUST win\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "../" + filepath.Base(dirB) + "/AGENT.md"
	var m Manager
	if _, _, _, err := m.rulesDigestFor(dirA, name, 4096); err == nil {
		t.Fatalf("file name %q escaping the project dir was accepted", name)
	}
}

func TestRulesDigestSectionsReturnsHeadingTexts(t *testing.T) {
	content := "# General\n\nRules prose.\n\n## Planning\n\n### Sub detail\n\n\n"
	got := RulesDigestSections([]byte(content))
	want := []string{"General", "Planning", "Sub detail"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if RulesDigestSections([]byte("no headings here\n")) != nil {
		t.Fatal("want nil for a file without headings")
	}
}

func TestRulesDigestCacheHitAndRebuild(t *testing.T) {
	rulesDigestBuildsReset()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENT.md")
	first := "# H\nAlpha MUST hold.\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	var m Manager
	d1, sha1, size1, err := m.rulesDigestFor(dir, "AGENT.md", 2048)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(first))
	if size1 != len(first) || sha1 != hex.EncodeToString(sum[:]) {
		t.Fatalf("size=%d sha=%s", size1, sha1)
	}
	if !strings.Contains(d1, "Alpha MUST hold.") {
		t.Fatalf("bad digest:\n%s", d1)
	}
	if got := rulesDigestBuildsNow(); got != 1 {
		t.Fatalf("builds after first read = %d, want 1", got)
	}
	d2, _, _, err := m.rulesDigestFor(dir, "AGENT.md", 2048)
	if err != nil {
		t.Fatal(err)
	}
	if d2 != d1 {
		t.Fatal("cached digest differs from the first build")
	}
	if got := rulesDigestBuildsNow(); got != 1 {
		t.Fatalf("builds after cache hit = %d, want still 1", got)
	}
	second := "# H\nBravo MUST rebuild.\n"
	if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	d3, _, _, err := m.rulesDigestFor(dir, "AGENT.md", 2048)
	if err != nil {
		t.Fatal(err)
	}
	if d3 == d1 || !strings.Contains(d3, "Bravo MUST rebuild.") {
		t.Fatalf("changed content was not rebuilt:\n%s", d3)
	}
	if got := rulesDigestBuildsNow(); got != 2 {
		t.Fatalf("builds after content change = %d, want 2", got)
	}
}

func TestRulesDigestCacheBounded(t *testing.T) {
	rulesDigestBuildsReset()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENT.md")
	var m Manager
	var lastKey string
	for i := 1; i <= rulesDigestCacheEntries+6; i++ {
		content := fmt.Sprintf("line %d MUST hold\n", i)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		maxBytes := 200 + i
		if _, _, _, err := m.rulesDigestFor(dir, "AGENT.md", maxBytes); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(content))
		lastKey = hex.EncodeToString(sum[:]) + ":" + fmt.Sprint(maxBytes)
	}
	rulesDigestCacheMu.Lock()
	n := len(rulesDigestCache)
	_, latest := rulesDigestCache[lastKey]
	oldestSum := sha256.Sum256([]byte("line 1 MUST hold\n"))
	_, oldest := rulesDigestCache[fmt.Sprintf("%s:201", hex.EncodeToString(oldestSum[:]))]
	rulesDigestCacheMu.Unlock()
	if n > rulesDigestCacheEntries {
		t.Fatalf("cache holds %d entries, bound is %d", n, rulesDigestCacheEntries)
	}
	if !latest {
		t.Fatal("most recent entry was evicted")
	}
	if oldest {
		t.Fatal("oldest entry should have been dropped")
	}
}
