package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadProjectGateYAML writes body to a temp config file and loads it through
// the real Load path, so the autonomous.project_gate_command validation chain
// runs exactly as it does at daemon startup.
func loadProjectGateYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return Load(path)
}

// projectGateDaemon wraps a daemon-tier autonomous block for the resolver.
func projectGateDaemon(global string) DaemonPolicySource {
	return DaemonPolicySource{Autonomous: AutonomousConfig{ProjectGateCommand: global}}
}

func projectGateStr(s string) *string { return &s }

func TestProjectGateCommandUnset(t *testing.T) {
	cfg, err := loadProjectGateYAML(t, "autonomous:\n  enabled: false\n")
	if err != nil {
		t.Fatalf("Load(no key) = %v, want nil", err)
	}
	if got := cfg.Autonomous.ProjectGateCommand; got != "" {
		t.Fatalf("loaded default = %q, want empty (unset)", got)
	}
	if got := cfg.Autonomous.EffectiveProjectGateCommand(nil); got != "" {
		t.Fatalf("effective with no policy = %q, want empty", got)
	}
	if got := (*AutonomousConfig)(nil).EffectiveProjectGateCommand(nil); got != "" {
		t.Fatalf("nil receiver effective = %q, want empty", got)
	}
	eff := ResolveProjectPolicy(projectGateDaemon(""), nil, AutomatonOverrides{})
	if eff.ProjectGateCommand.Value != "" || eff.ProjectGateCommand.Source != PolicySourceDaemon {
		t.Fatalf("resolved unset = %+v, want empty from daemon", eff.ProjectGateCommand)
	}
}

func TestProjectGateCommandSetGlobally(t *testing.T) {
	cfg, err := loadProjectGateYAML(t, "autonomous:\n  project_gate_command: \"  make test  \"\n")
	if err != nil {
		t.Fatalf("Load = %v, want nil", err)
	}
	if got := cfg.Autonomous.EffectiveProjectGateCommand(nil); got != "make test" {
		t.Fatalf("effective global = %q, want %q (trimmed)", got, "make test")
	}
	// A project with no policy field inherits the global value.
	if got := cfg.Autonomous.EffectiveProjectGateCommand(&ProjectPolicy{}); got != "make test" {
		t.Fatalf("effective with empty policy = %q, want inherited %q", got, "make test")
	}
	eff := ResolveProjectPolicy(projectGateDaemon("make test"), nil, AutomatonOverrides{})
	if eff.ProjectGateCommand.Value != "make test" || eff.ProjectGateCommand.Source != PolicySourceDaemon {
		t.Fatalf("resolved global = %+v, want make test from daemon", eff.ProjectGateCommand)
	}
}

func TestProjectGateCommandProjectOverrideBeatsGlobal(t *testing.T) {
	a := AutonomousConfig{ProjectGateCommand: "make test"}
	policy := &ProjectPolicy{ProjectGateCommand: projectGateStr("npm test")}
	if got := a.EffectiveProjectGateCommand(policy); got != "npm test" {
		t.Fatalf("effective override = %q, want %q", got, "npm test")
	}
	eff := ResolveProjectPolicy(projectGateDaemon("make test"), policy, AutomatonOverrides{})
	if eff.ProjectGateCommand.Value != "npm test" || eff.ProjectGateCommand.Source != PolicySourceProject {
		t.Fatalf("resolved override = %+v, want npm test from project", eff.ProjectGateCommand)
	}
	// An override set with no global value also applies.
	if got := (&AutonomousConfig{}).EffectiveProjectGateCommand(policy); got != "npm test" {
		t.Fatalf("override with unset global = %q, want %q", got, "npm test")
	}
}

func TestProjectGateCommandProjectExplicitlyUnsets(t *testing.T) {
	a := AutonomousConfig{ProjectGateCommand: "make test"}
	policy := &ProjectPolicy{ProjectGateCommand: projectGateStr("")}
	if got := a.EffectiveProjectGateCommand(policy); got != "" {
		t.Fatalf("explicit unset = %q, want empty even though global is set", got)
	}
	eff := ResolveProjectPolicy(projectGateDaemon("make test"), policy, AutomatonOverrides{})
	if eff.ProjectGateCommand.Value != "" || eff.ProjectGateCommand.Source != PolicySourceProject {
		t.Fatalf("resolved explicit unset = %+v, want empty from project", eff.ProjectGateCommand)
	}
	// Whitespace-only is the same as unset.
	blank := &ProjectPolicy{ProjectGateCommand: projectGateStr("   ")}
	if got := a.EffectiveProjectGateCommand(blank); got != "" {
		t.Fatalf("whitespace-only override = %q, want empty", got)
	}
	if err := ValidateProjectPolicy(policy, "projects.demo.policy"); err != nil {
		t.Fatalf("explicit empty must validate, got %v", err)
	}
}

func TestProjectGateCommandRefusesTooLong(t *testing.T) {
	long := strings.Repeat("x", ProjectGateCommandMaxBytes+1)
	if _, err := loadProjectGateYAML(t, "autonomous:\n  project_gate_command: "+long+"\n"); err == nil {
		t.Fatal("Load(over-long) = nil, want refusal")
	} else if !strings.Contains(err.Error(), "autonomous.project_gate_command") {
		t.Fatalf("error %q does not name autonomous.project_gate_command", err)
	}
	// Exactly the limit is accepted.
	atLimit := strings.Repeat("x", ProjectGateCommandMaxBytes)
	if err := ValidateProjectGateCommand("autonomous", atLimit); err != nil {
		t.Fatalf("at-limit value refused: %v", err)
	}
	// Trimming happens before the length check.
	padded := "  " + atLimit + "  "
	if err := ValidateProjectGateCommand("autonomous", padded); err != nil {
		t.Fatalf("padded at-limit value refused: %v", err)
	}
	policy := &ProjectPolicy{ProjectGateCommand: projectGateStr(long)}
	err := ValidateProjectPolicy(policy, "projects.demo.policy")
	if err == nil || !strings.Contains(err.Error(), "projects.demo.policy.project_gate_command") {
		t.Fatalf("policy over-long error = %v, want one naming projects.demo.policy.project_gate_command", err)
	}
}

func TestProjectGateCommandRefusesNUL(t *testing.T) {
	if err := ValidateProjectGateCommand("autonomous", "make\x00test"); err == nil ||
		!strings.Contains(err.Error(), "autonomous.project_gate_command") {
		t.Fatalf("NUL error = %v, want refusal naming the key", err)
	}
	if _, err := loadProjectGateYAML(t, "autonomous:\n  project_gate_command: \"make\\0test\"\n"); err == nil {
		t.Fatal("Load(NUL) = nil, want refusal")
	}
	policy := &ProjectPolicy{ProjectGateCommand: projectGateStr("make\x00test")}
	if err := ValidateProjectPolicy(policy, "projects.demo.policy"); err == nil ||
		!strings.Contains(err.Error(), "projects.demo.policy.project_gate_command") {
		t.Fatalf("policy NUL error = %v, want refusal naming the key", err)
	}
}

func TestProjectGateCommandRefusesNewline(t *testing.T) {
	for _, bad := range []string{"make\ntest", "make\rtest", "make\r\ntest"} {
		if err := ValidateProjectGateCommand("autonomous", bad); err == nil ||
			!strings.Contains(err.Error(), "autonomous.project_gate_command") {
			t.Fatalf("newline value %q error = %v, want refusal naming the key", bad, err)
		}
	}
	if _, err := loadProjectGateYAML(t, "autonomous:\n  project_gate_command: \"make\\ntest\"\n"); err == nil {
		t.Fatal("Load(newline) = nil, want refusal")
	}
	policy := &ProjectPolicy{ProjectGateCommand: projectGateStr("make\ntest")}
	if err := ValidateProjectPolicy(policy, "projects.demo.policy"); err == nil ||
		!strings.Contains(err.Error(), "projects.demo.policy.project_gate_command") {
		t.Fatalf("policy newline error = %v, want refusal naming the key", err)
	}
}

func TestProjectGateCommandAcceptsValid(t *testing.T) {
	for _, ok := range []string{"", "make test", "go test ./... && npm run lint", "./scripts/check.sh --fast"} {
		if err := ValidateProjectGateCommand("autonomous", ok); err != nil {
			t.Fatalf("valid value %q refused: %v", ok, err)
		}
	}
}
