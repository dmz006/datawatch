package autonomous

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// installRulesContextConfig sets the installed autonomous.rules_context
// config for the test and restores whatever was there before.
func installRulesContextConfig(t *testing.T, c config.AutonomousRulesContextConfig) {
	t.Helper()
	prev := rulesContextConfigSnapshot()
	SetRulesContextConfig(c)
	t.Cleanup(func() { SetRulesContextConfig(prev) })
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("content\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func TestRulesContextPolicy_ZeroValueDefaults(t *testing.T) {
	p, err := ResolveRulesContext(config.AutonomousRulesContextConfig{}, "", "", "/somewhere/project")
	if err != nil {
		t.Fatalf("zero config must resolve: %v", err)
	}
	want := defaultRulesContext()
	if p.RulesFile != want.RulesFile {
		t.Fatalf("rules_file: got %q, want %q", p.RulesFile, want.RulesFile)
	}
	if !reflect.DeepEqual(p.ContextFiles, want.ContextFiles) {
		t.Fatalf("context_files: got %v, want %v", p.ContextFiles, want.ContextFiles)
	}
	if p.DigestMode != config.RulesContextDigestModeNoTools {
		t.Fatalf("digest_mode: got %q, want %q", p.DigestMode, config.RulesContextDigestModeNoTools)
	}
	if p.DigestMaxBytes != config.RulesContextDigestMaxBytesDefault {
		t.Fatalf("digest_max_bytes: got %d, want %d", p.DigestMaxBytes, config.RulesContextDigestMaxBytesDefault)
	}
}

func TestRulesContextPolicy_LegacyKeysFallback(t *testing.T) {
	// The legacy autonomous.rules_file / autonomous.context_file values
	// (carried on the manager config before rules_context existed) are the
	// fallback layer under the defaults-free global value.
	p, err := ResolveRulesContext(config.AutonomousRulesContextConfig{}, "MY-RULES.md", "MY-CONTEXT.md", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.RulesFile != "MY-RULES.md" {
		t.Fatalf("rules_file: got %q, want MY-RULES.md", p.RulesFile)
	}
	if !reflect.DeepEqual(p.ContextFiles, []string{"MY-CONTEXT.md"}) {
		t.Fatalf("context_files: got %v, want [MY-CONTEXT.md]", p.ContextFiles)
	}
}

func TestRulesContextPolicy_GlobalBeatsLegacy(t *testing.T) {
	p, err := ResolveRulesContext(
		config.AutonomousRulesContextConfig{RulesFile: "GLOBAL-RULES.md"},
		"MY-RULES.md", "MY-CONTEXT.md", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.RulesFile != "GLOBAL-RULES.md" {
		t.Fatalf("rules_file: got %q, want GLOBAL-RULES.md", p.RulesFile)
	}
	// Per-setting fall-through: the global renames only the rules file,
	// so the context file keeps its legacy name rather than the default.
	if !reflect.DeepEqual(p.ContextFiles, []string{"MY-CONTEXT.md"}) {
		t.Fatalf("context_files: got %v, want [MY-CONTEXT.md]", p.ContextFiles)
	}
}

func TestRulesContextPolicy_ProjectOverrideBeatsGlobal(t *testing.T) {
	dir := "/srv/demo"
	cfg := config.AutonomousRulesContextConfig{
		RulesFile:    "GLOBAL-RULES.md",
		ContextFiles: []string{"GLOBAL.md"},
		Projects: []config.AutonomousRulesContextProject{
			{ProjectDir: dir, RulesFile: "DEMO-RULES.md", ContextFiles: []string{"DEMO.md"}, DigestMode: "off"},
		},
	}
	p, err := ResolveRulesContext(cfg, "", "", dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.RulesFile != "DEMO-RULES.md" {
		t.Fatalf("rules_file: got %q, want DEMO-RULES.md", p.RulesFile)
	}
	if !reflect.DeepEqual(p.ContextFiles, []string{"DEMO.md"}) {
		t.Fatalf("context_files: got %v, want [DEMO.md]", p.ContextFiles)
	}
	if p.DigestMode != "off" {
		t.Fatalf("digest_mode: got %q, want off", p.DigestMode)
	}
	// A project the override doesn't name keeps the global values.
	pOther, err := ResolveRulesContext(cfg, "", "", "/srv/other")
	if err != nil {
		t.Fatalf("resolve other: %v", err)
	}
	if pOther.RulesFile != "GLOBAL-RULES.md" || !reflect.DeepEqual(pOther.ContextFiles, []string{"GLOBAL.md"}) {
		t.Fatalf("other project: got %+v, want global values", pOther)
	}
}

func TestRulesContextPolicy_OverrideFallsThroughPerSetting(t *testing.T) {
	dir := "/srv/demo"
	cfg := config.AutonomousRulesContextConfig{
		Projects: []config.AutonomousRulesContextProject{
			{ProjectDir: dir, DigestMode: "off"}, // renames nothing
		},
	}
	p, err := ResolveRulesContext(cfg, "MY-RULES.md", "MY-CONTEXT.md", dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.RulesFile != "MY-RULES.md" || !reflect.DeepEqual(p.ContextFiles, []string{"MY-CONTEXT.md"}) {
		t.Fatalf("unoverridden settings must keep the legacy values: %+v", p)
	}
	if p.DigestMode != "off" {
		t.Fatalf("digest_mode: got %q, want off", p.DigestMode)
	}
}

func TestRulesContextPolicy_ManyContextFiles(t *testing.T) {
	base := t.TempDir()
	writeFiles(t, base, "A.md", "B.md", "C.md")
	cfg := config.AutonomousRulesContextConfig{
		RulesFile: "MY-RULES.md",
		Projects: []config.AutonomousRulesContextProject{
			{ProjectDir: base, ContextFiles: []string{"A.md", "B.md", "C.md"}},
		},
	}
	p, err := ResolveRulesContext(cfg, "", "", base)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !reflect.DeepEqual(p.ContextFiles, []string{"A.md", "B.md", "C.md"}) {
		t.Fatalf("context_files: got %v, want [A.md B.md C.md]", p.ContextFiles)
	}
	// Pointer text joins more than two files as "A, B and C".
	installRulesContextConfig(t, cfg)
	writeFiles(t, base, "MY-RULES.md")
	m := &Manager{cfg: Config{}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: base})
	if want := "MY-RULES.md, A.md, B.md and C.md"; !strings.Contains(got, want) {
		t.Fatalf("suffix %q does not join the file list as %q", got, want)
	}
}

func TestRulesContextPolicy_InstalledConfigDrivesManager(t *testing.T) {
	dirName := t.TempDir()
	writeFiles(t, dirName, "MY-RULES.md", "STATE.md", "NOTES.md")
	installRulesContextConfig(t, config.AutonomousRulesContextConfig{
		Projects: []config.AutonomousRulesContextProject{
			{ProjectDir: dirName, RulesFile: "MY-RULES.md", ContextFiles: []string{"STATE.md", "NOTES.md"}},
		},
	})
	m := &Manager{cfg: Config{}}
	got := m.presentRulesContextFiles(&PRD{ProjectDir: dirName})
	if want := "MY-RULES.md, STATE.md and NOTES.md"; got != want {
		t.Fatalf("presentRulesContextFiles: got %q, want %q", got, want)
	}
}

func TestRulesContextPolicy_InvalidConfigFallsBackToDefaults(t *testing.T) {
	// RulesContextFor must never fail the caller: an invalid installed
	// config resolves to the defaults, exactly like ChecksPolicyFor does.
	installRulesContextConfig(t, config.AutonomousRulesContextConfig{RulesFile: "/etc/passwd"})
	p := RulesContextFor("/somewhere/project")
	if p.RulesFile != "AGENT.md" || !reflect.DeepEqual(p.ContextFiles, []string{"CONTEXT.md"}) {
		t.Fatalf("fallback: got %+v, want the defaults", p)
	}
}

func TestRulesContextPolicy_InvalidResolveIsAnError(t *testing.T) {
	_, err := ResolveRulesContext(config.AutonomousRulesContextConfig{ContextFiles: []string{"../UP.md"}}, "", "", "")
	if err == nil {
		t.Fatal("ResolveRulesContext must reject a parent-escaping context file")
	}
}
