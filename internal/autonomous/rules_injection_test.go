package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const injRulesBody = "# Rules\n\nYou must run the tests.\nplain prose line\n\n## Docs\n\nAlways update the docs.\n"

func injWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func injProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	injWrite(t, dir, "AGENT.md", injRulesBody)
	injWrite(t, dir, "CONTEXT.md", "# Context\n\n## State\n\nNever trust this line mention.\n")
	return dir
}

func injMode(t *testing.T, mode string, maxBytes int) {
	t.Helper()
	installRulesContextConfig(t, config.AutonomousRulesContextConfig{DigestMode: mode, DigestMaxBytes: maxBytes})
}

func TestRulesInjectionPlannerFilePresent(t *testing.T) {
	injMode(t, "", 0)
	dir := injProject(t)
	m := &Manager{cfg: Config{}}
	got, injected, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, "claude-code")
	if !strings.Contains(got, "AGENT.md and CONTEXT.md") || !strings.Contains(got, "Follow them") {
		t.Fatalf("pointer must name both files and keep the follow sentence: %q", got)
	}
	if injected || strings.Contains(got, rulesDigestHeader) {
		t.Fatalf("tooled backend in default mode gets no digest: %q", got)
	}
}

func TestRulesInjectionPlannerFileMissing(t *testing.T) {
	injMode(t, "", 0)
	dir := t.TempDir()
	injWrite(t, dir, "AGENT.md", injRulesBody)
	m := &Manager{cfg: Config{}}
	got, _, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, "claude-code")
	if !strings.Contains(got, "Context file CONTEXT.md was configured and not found") {
		t.Fatalf("missing file needs an explicit line: %q", got)
	}
	if strings.Count(got, "not found") != 1 {
		t.Fatalf("one line per missing file: %q", got)
	}
}

func TestRulesInjectionRenamedPerProject(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		injWrite(t, d, "AGENT.md", injRulesBody)
		injWrite(t, d, "DATAWATCH-CONTEXT.md", "# C\n")
	}
	installRulesContextConfig(t, config.AutonomousRulesContextConfig{
		Projects: []config.AutonomousRulesContextProject{{ProjectDir: a, ContextFiles: []string{"DATAWATCH-CONTEXT.md"}}},
	})
	m := &Manager{cfg: Config{}}
	ga, _, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: a}, "claude-code")
	gb, _, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: b}, "claude-code")
	if !strings.Contains(ga, "DATAWATCH-CONTEXT.md") {
		t.Fatalf("renamed project must name its file: %q", ga)
	}
	if strings.Contains(gb, "read AGENT.md and DATAWATCH-CONTEXT.md") || strings.Contains(gb, "DATAWATCH-CONTEXT.md was") {
		t.Fatalf("other project must not see the rename: %q", gb)
	}
}

func TestRulesInjectionNoToolsBackendGetsDigestWithinCap(t *testing.T) {
	injMode(t, "", 600)
	dir := injProject(t)
	m := &Manager{cfg: Config{}}
	got, injected, n := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, "ollama")
	if !injected || !strings.Contains(got, rulesDigestHeader) {
		t.Fatalf("no-tools backend must get the digest: %q", got)
	}
	if n <= 0 || n > 600 {
		t.Fatalf("digest bytes %d outside cap 600", n)
	}
	if !strings.Contains(got, "You must run the tests.") || strings.Contains(got, "plain prose line") {
		t.Fatalf("digest keeps mandatory lines only: %q", got)
	}
	if !strings.Contains(got, "CONTEXT.md headings:") || strings.Contains(got, "Never trust this line mention") {
		t.Fatalf("context files contribute headings only: %q", got)
	}
}

func TestRulesInjectionDigestCapTight(t *testing.T) {
	injMode(t, "all", 512)
	dir := t.TempDir()
	injWrite(t, dir, "AGENT.md", strings.Repeat("## Heading\nYou must do a thing here.\n", 200))
	m := &Manager{cfg: Config{}}
	_, injected, n := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, "ollama")
	if !injected || n > 512 {
		t.Fatalf("injected=%v bytes=%d, want within 512", injected, n)
	}
}

func TestRulesInjectionModes(t *testing.T) {
	dir := injProject(t)
	m := &Manager{cfg: Config{}}
	cases := []struct {
		mode, backend string
		want          bool
	}{
		{"", "claude-code", false},
		{"no_tools", "pi-prompt", false},
		{"no_tools", "openwebui", true},
		{"all", "claude-code", true},
		{"all", "ollama", true},
		{"off", "ollama", false},
		{"off", "claude-code", false},
	}
	for _, c := range cases {
		injMode(t, c.mode, 0)
		_, injected, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, c.backend)
		if injected != c.want {
			t.Errorf("mode=%q backend=%q: digest=%v want %v", c.mode, c.backend, injected, c.want)
		}
	}
}

func TestRulesInjectionWorkerPrompt(t *testing.T) {
	dir := injProject(t)
	m := &Manager{cfg: Config{}}

	injMode(t, "", 0)
	got := m.withRulesContextBlock("task spec", dir, "claude-code")
	if !strings.HasPrefix(got, "task spec") || !strings.Contains(got, "AGENT.md and CONTEXT.md") {
		t.Fatalf("worker prompt must carry the pointer: %q", got)
	}
	if strings.Contains(got, rulesDigestHeader) {
		t.Fatalf("tooled worker in default mode: no digest: %q", got)
	}
	if again := m.withRulesContextBlock(got, dir, "claude-code"); again != got {
		t.Fatalf("block must not be appended twice")
	}
	if strings.Count(m.withRulesContextBlock("x", dir, "ollama"), rulesDigestHeader) != 1 {
		t.Fatalf("no-tools worker backend gets exactly one digest")
	}
	if got := m.withRulesContextBlock("task spec", "", "ollama"); got != "task spec" {
		t.Fatalf("empty project dir: omitted, got %q", got)
	}
	injMode(t, "off", 0)
	if strings.Contains(m.withRulesContextBlock("x", dir, "ollama"), rulesDigestHeader) {
		t.Fatalf("mode off never injects")
	}
}

func TestRulesInjectionVerifierPrompt(t *testing.T) {
	dir := injProject(t)
	m := &Manager{cfg: Config{}}
	prd := &PRD{ProjectDir: dir}

	injMode(t, "", 0)
	tooled := m.verificationScriptSuffixFor(prd, "claude-code", true)
	if !strings.Contains(tooled, "AGENT.md and CONTEXT.md") || strings.Contains(tooled, rulesDigestHeader) {
		t.Fatalf("tooled verifier: pointer, no digest: %q", tooled)
	}
	headless := m.verificationScriptSuffixFor(prd, "ollama", true)
	if !strings.Contains(headless, "AGENT.md and CONTEXT.md") || !strings.Contains(headless, rulesDigestHeader) {
		t.Fatalf("no-tools verifier: pointer and digest: %q", headless)
	}
	if strings.Contains(m.verificationScriptSuffixFor(prd, "ollama", false), rulesDigestHeader) {
		t.Fatalf("withDigest=false must omit the digest")
	}
	injMode(t, "off", 0)
	if strings.Contains(m.verificationScriptSuffixFor(prd, "ollama", true), rulesDigestHeader) {
		t.Fatalf("mode off never injects")
	}
}

func TestRulesInjectionPlanningRecordDigestInjected(t *testing.T) {
	injMode(t, "all", 0)
	fakeFn := func(req DecomposeRequest) (string, error) { return fakePlanResp, nil }
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeFn)
	if err != nil {
		t.Fatal(err)
	}
	dir := injProject(t)
	prd, err := m.CreatePRD("spec", dir, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("decompose: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	rec := prd.RulesContext
	if rec == nil || !rec.DigestInjected || rec.DigestBytes <= 0 {
		t.Fatalf("planning record must show the digest: %+v", rec)
	}
}
