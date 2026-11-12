package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRulesContextConfig_ZeroValueIsValid(t *testing.T) {
	var c AutonomousRulesContextConfig
	if err := c.Validate(); err != nil {
		t.Fatalf("zero value must be valid: %v", err)
	}
}

func TestRulesContextConfig_ValidShape(t *testing.T) {
	c := AutonomousRulesContextConfig{
		RulesFile:      "MY-RULES.md",
		ContextFiles:   []string{"STATE.md", "ROADMAP.md"},
		DigestMode:     "no_tools",
		DigestMaxBytes: 8192,
		Projects: []AutonomousRulesContextProject{
			{ProjectDir: "/srv/demo", RulesFile: "DEMO-RULES.md"},
			{ProjectDir: "/srv/other", ContextFiles: []string{"NOTES.md"}, DigestMode: "off", DigestMaxBytes: 512},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestRulesContextConfig_RejectAbsoluteRulesFile(t *testing.T) {
	c := AutonomousRulesContextConfig{RulesFile: "/etc/AGENT.md"}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "rules_file") {
		t.Fatalf("want an absolute-path error naming rules_file, got %v", err)
	}
}

func TestRulesContextConfig_RejectParentDirectoryRulesFile(t *testing.T) {
	c := AutonomousRulesContextConfig{RulesFile: "../AGENT.md"}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "..") {
		t.Fatalf("want a ..-escape error, got %v", err)
	}
}

func TestRulesContextConfig_RejectNestedDotDot(t *testing.T) {
	c := AutonomousRulesContextConfig{RulesFile: "docs/../secret.md"}
	if err := c.Validate(); err == nil {
		t.Fatal("want an error for a nested ../ escape, got nil")
	}
}

func TestRulesContextConfig_RejectEmptyContextFile(t *testing.T) {
	c := AutonomousRulesContextConfig{ContextFiles: []string{"STATE.md", "  "}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "context_files[1]") {
		t.Fatalf("want an empty-context-file error naming index 1, got %v", err)
	}
}

func TestRulesContextConfig_RejectTooManyContextFiles(t *testing.T) {
	files := make([]string, 0, MaxRulesContextFiles+1)
	for i := 0; i < MaxRulesContextFiles+1; i++ {
		files = append(files, "F.md")
	}
	c := AutonomousRulesContextConfig{ContextFiles: files}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("want a list-length error, got %v", err)
	}
}

func TestRulesContextConfig_AllowMaxContextFiles(t *testing.T) {
	files := make([]string, 0, MaxRulesContextFiles)
	for i := 0; i < MaxRulesContextFiles; i++ {
		files = append(files, "F.md")
	}
	if err := (AutonomousRulesContextConfig{ContextFiles: files}).Validate(); err != nil {
		t.Fatalf("%d files is at the bound and must be valid: %v", MaxRulesContextFiles, err)
	}
}

func TestRulesContextConfig_RejectUnknownDigestMode(t *testing.T) {
	c := AutonomousRulesContextConfig{DigestMode: "yolo"}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "digest mode") {
		t.Fatalf("want a digest-mode error, got %v", err)
	}
}

func TestRulesContextConfig_DigestModeCaseInsensitive(t *testing.T) {
	c := AutonomousRulesContextConfig{DigestMode: "NO_TOOLS"}
	if err := c.Validate(); err != nil {
		t.Fatalf("mode is case-insensitive: %v", err)
	}
	if got := NormalizeRulesContextMode(c.DigestMode); got != RulesContextDigestModeNoTools {
		t.Fatalf("normalize NO_TOOLS = %q, want %q", got, RulesContextDigestModeNoTools)
	}
}

func TestRulesContextConfig_RejectDigestMaxBytesBelowBound(t *testing.T) {
	c := AutonomousRulesContextConfig{DigestMaxBytes: RulesContextDigestMaxBytesMin - 1}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("want a range error, got %v", err)
	}
}

func TestRulesContextConfig_RejectDigestMaxBytesAboveBound(t *testing.T) {
	c := AutonomousRulesContextConfig{DigestMaxBytes: RulesContextDigestMaxBytesMax + 1}
	if err := c.Validate(); err == nil {
		t.Fatal("want a range error, got nil")
	}
}

func TestRulesContextConfig_DigestMaxBytesBoundsInclusive(t *testing.T) {
	if err := (AutonomousRulesContextConfig{DigestMaxBytes: RulesContextDigestMaxBytesMin}).Validate(); err != nil {
		t.Fatalf("lower bound must be allowed: %v", err)
	}
	if err := (AutonomousRulesContextConfig{DigestMaxBytes: RulesContextDigestMaxBytesMax}).Validate(); err != nil {
		t.Fatalf("upper bound must be allowed: %v", err)
	}
}

func TestRulesContextConfig_RejectProjectWithoutDir(t *testing.T) {
	c := AutonomousRulesContextConfig{Projects: []AutonomousRulesContextProject{{RulesFile: "X.md"}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "project_dir") {
		t.Fatalf("want a project_dir error, got %v", err)
	}
}

func TestRulesContextConfig_RejectProjectBadDigestMode(t *testing.T) {
	c := AutonomousRulesContextConfig{Projects: []AutonomousRulesContextProject{{ProjectDir: "/x", DigestMode: "sneaky"}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "digest mode") {
		t.Fatalf("want a digest-mode error in the project entry, got %v", err)
	}
}

func TestRulesContextConfig_YamlRoundTrip(t *testing.T) {
	in := AutonomousRulesContextConfig{
		RulesFile:      "MY-RULES.md",
		ContextFiles:   []string{"STATE.md", "ROADMAP.md"},
		DigestMode:     "off",
		DigestMaxBytes: 2048,
		Projects: []AutonomousRulesContextProject{
			{ProjectDir: "/srv/demo", RulesFile: "DEMO-RULES.md", DigestMode: "all"},
		},
	}
	out, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AutonomousRulesContextConfig
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RulesFile != in.RulesFile {
		t.Fatalf("rules_file: got %q, want %q", got.RulesFile, in.RulesFile)
	}
	if len(got.ContextFiles) != 2 || got.ContextFiles[0] != "STATE.md" || got.ContextFiles[1] != "ROADMAP.md" {
		t.Fatalf("context_files: got %v, want %v", got.ContextFiles, in.ContextFiles)
	}
	if got.DigestMode != "off" {
		t.Fatalf("digest_mode: got %q, want off", got.DigestMode)
	}
	if got.DigestMaxBytes != 2048 {
		t.Fatalf("digest_max_bytes: got %d, want 2048", got.DigestMaxBytes)
	}
	if len(got.Projects) != 1 || got.Projects[0].ProjectDir != "/srv/demo" || got.Projects[0].RulesFile != "DEMO-RULES.md" {
		t.Fatalf("projects: got %+v", got.Projects)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("round-tripped value must stay valid: %v", err)
	}
}
