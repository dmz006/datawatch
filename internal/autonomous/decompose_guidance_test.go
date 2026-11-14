// decompose_guidance_test.go — the decompose-guidance resolver
// (see decompose_guidance.go): literal text vs named file vs the
// block generated from the active decompose-lint rules, in the
// precedence the decompose prompt relies on.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestDecomposeGuidance_UnsetGeneratesFromDefaultRules: with
// decompose_guidance unset, the guidance is generated from the active
// lint rules (the default pack here), one line per rule (id, severity,
// message) — the decomposer is told exactly what the lint hook will
// flag.
func TestDecomposeGuidance_UnsetGeneratesFromDefaultRules(t *testing.T) {
	block := BuildDecomposeGuidance(&config.AutonomousConfig{DecomposeLint: config.DecomposeLintConfig{}})
	for _, want := range []string{
		"runs_hosting_cli (block):",
		"no_verification_script (warn):",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("generated guidance missing %q:\n%s", want, block)
		}
	}
}

// TestDecomposeGuidance_LiteralText: a decompose_guidance value that is
// prose (no path shape) is used verbatim.
func TestDecomposeGuidance_LiteralText(t *testing.T) {
	literal := "Always declare a verification script and keep tasks under one screen of spec."
	block := BuildDecomposeGuidance(&config.AutonomousConfig{
		DecomposeLint:     config.DecomposeLintConfig{},
		DecomposeGuidance: "  " + literal + "  ",
	})
	if block != literal {
		t.Fatalf("guidance = %q, want the literal text %q (trim only, no fallback)", block, literal)
	}
}

// TestDecomposeGuidance_FilePath: a decompose_guidance naming an
// existing readable file loads the file's contents (operator keeps the
// guidance text out of the config file).
func TestDecomposeGuidance_FilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "decompose-guidance.md")
	contents := "Keep task specs under one screen and always declare a verification script.\nNever run the hosting CLI from a task.\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	block := BuildDecomposeGuidance(&config.AutonomousConfig{
		DecomposeLint:     config.DecomposeLintConfig{},
		DecomposeGuidance: path,
	})
	if block != contents {
		t.Fatalf("guidance = %q, want the file contents %q", block, contents)
	}
}

// TestDecomposeGuidance_MissingPathFallsBackToGenerated: a path-shaped
// value with no readable file is a dead path, not prose — the
// generated block stands in instead of the path text.
func TestDecomposeGuidance_MissingPathFallsBackToGenerated(t *testing.T) {
	block := BuildDecomposeGuidance(&config.AutonomousConfig{
		DecomposeLint:     config.DecomposeLintConfig{},
		DecomposeGuidance: "/definitely/not/here/decompose-guidance.md",
	})
	if strings.Contains(block, "/definitely/not/here") {
		t.Fatalf("guidance = %q, want the generated fallback, not the dead path text", block)
	}
	if !strings.Contains(block, "runs_hosting_cli (block):") {
		t.Fatalf("guidance = %q, want the generated block (one line per active rule)", block)
	}
}

// TestDecomposeGuidance_EmptyFileFallsBackToGenerated: an existing but
// empty file names nothing to append — the generated block stands in.
func TestDecomposeGuidance_EmptyFileFallsBackToGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decompose-guidance.md")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	block := BuildDecomposeGuidance(&config.AutonomousConfig{
		DecomposeLint:     config.DecomposeLintConfig{},
		DecomposeGuidance: path,
	})
	if !strings.Contains(block, "runs_hosting_cli (block):") {
		t.Fatalf("guidance = %q, want the generated block for an empty named file", block)
	}
}

// TestDecomposeGuidance_DisabledRuleExcluded: a default rule dropped by
// disabled_rules leaves the generated block — the decomposer is told
// what the lint hook will and will not flag.
func TestDecomposeGuidance_DisabledRuleExcluded(t *testing.T) {
	c := config.AutonomousConfig{
		DecomposeLint: config.DecomposeLintConfig{
			DisabledRules: []string{"runs_hosting_cli"},
		},
	}
	block := BuildDecomposeGuidance(&c)
	if strings.Contains(block, "runs_hosting_cli") {
		t.Fatalf("guidance = %q, want no line for the disabled runs_hosting_cli rule", block)
	}
	if !strings.Contains(block, "no_verification_script (warn):") {
		t.Fatalf("guidance = %q, want the remaining active default rules", block)
	}
}

// TestDecomposeGuidance_OperatorRuleIncluded: an operator rule added to
// the pack shows up in the generated block, merged with the defaults.
func TestDecomposeGuidance_OperatorRuleIncluded(t *testing.T) {
	c := config.AutonomousConfig{
		DecomposeLint: config.DecomposeLintConfig{
			Rules: []config.DecomposeLintRule{{
				ID:       "always_verify",
				Severity: config.DecomposeLintSeverityWarning,
				Message:  "Every task should declare a verification script.",
				Match:    config.DecomposeLintMatch{SpecRegex: "x"},
			}},
		},
	}
	block := BuildDecomposeGuidance(&c)
	if !strings.Contains(block, "always_verify (warn): Every task should declare a verification script.") {
		t.Fatalf("guidance = %q, want the operator rule's line", block)
	}
	if !strings.Contains(block, "runs_hosting_cli (block):") {
		t.Fatalf("guidance = %q, want the default pack's rules alongside the operator's", block)
	}
}

// TestDecomposeGuidance_NoActiveRulesNoBlock: with every rule disabled
// (or no rules at all) and no configured text, the block is empty —
// the decompose prompt is unchanged (no guidance section).
func TestDecomposeGuidance_NoActiveRulesNoBlock(t *testing.T) {
	c := config.AutonomousConfig{
		DecomposeLint: config.DecomposeLintConfig{
			DisabledRules: []string{
				"edits_version_variable",
				"runs_hosting_cli",
				"starts_daemon_outside_sandbox",
				"reads_operator_data_or_config_folders",
				"read_only_but_writes_files",
				"read_only_unverifiable",
				"read_only_task_writes_file",
				"spec_path_outside_allowed_dirs",
				"verify_script_task_files_only",
				"verify_script_docs_without_sync",
				"final_gate_without_script",
				"no_verification_script",
				"unexecutable_verification_script",
				"names_tracker_id",
				"version_number_in_spec",
				"unbounded_load_step",
			},
		},
	}
	if block := BuildDecomposeGuidance(&c); block != "" {
		t.Fatalf("guidance = %q, want empty (no active rule, no configured text)", block)
	}
	if n := len(EffectiveRules(c.DecomposeLint)); n != 0 {
		t.Fatalf("EffectiveRules = %d, want 0 (fixture bug)", n)
	}
}

// TestDecomposeGuidancePrompt_AppendedToDecomposePrompt: the resolved
// block is what the decompose prompt actually carries (wiring of the
// manager's prompt assembly).
func TestDecomposeGuidancePrompt_AppendedToDecomposePrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeGuidance = "GUIDANCE-SENTINEL-XY: keep every task verifiable by its own script."
	const planJSON = `{"title":"Ship it","stories":[{"title":"S","tasks":[{"title":"Task","spec":"Record the handoff note for the operator"}]}]}`
	var prompts []string
	fn := func(req DecomposeRequest) (string, error) {
		prompts = append(prompts, req.Spec)
		return planJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	if len(prompts) != 1 {
		t.Fatalf("decompose prompts = %d, want 1", len(prompts))
	}
	prompt := prompts[0]
	if !strings.Contains(prompt, "GUIDANCE-SENTINEL-XY") {
		t.Fatalf("decompose prompt did not carry the configured guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "--- Decompose guidance ---") {
		t.Fatalf("decompose prompt did not carry the guidance section header:\n%s", prompt)
	}
}

// TestDecomposeGuidancePrompt_GeneratedBlockInPrompt: with no
// configured text and the default pack active, the decompose prompt
// carries the generated block (one line per active rule).
func TestDecomposeGuidancePrompt_GeneratedBlockInPrompt(t *testing.T) {
	cfg := DefaultConfig()
	const planJSON = `{"title":"Ship it","stories":[{"title":"S","tasks":[{"title":"Task","spec":"Record the handoff note for the operator"}]}]}`
	var prompts []string
	fn := func(req DecomposeRequest) (string, error) {
		prompts = append(prompts, req.Spec)
		return planJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("decompose prompts = %d, want 1", len(prompts))
	}
	prompt := prompts[0]
	if !strings.Contains(prompt, "--- Decompose guidance ---") {
		t.Fatalf("decompose prompt did not carry the generated guidance header:\n%s", prompt)
	}
	if !strings.Contains(prompt, "runs_hosting_cli (block):") {
		t.Fatalf("decompose prompt did not carry the generated rule line:\n%s", prompt)
	}
}
