package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestRepoCIWatchSecurityWorkflowsSurviveSaveAndReload — the security
// workflow list's default must survive a config save and a daemon restart
// (save, then load). Before the fix a nil list was written as an empty list
// and read back as "none", which silently switched the security failure
// pass off in every mode.
func TestRepoCIWatchSecurityWorkflowsSurviveSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	// A fresh default config, saved and reloaded the way the daemon does.
	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save(default): %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	rcw := cfg.Autonomous.Automation.RepoCIWatch
	if !slices.Equal(rcw.SecurityWorkflows, DefaultRepoCIWatchSecurityWorkflows) {
		t.Errorf("reloaded security_workflows = %v, want the default %v", rcw.SecurityWorkflows, DefaultRepoCIWatchSecurityWorkflows)
	}
	if got := rcw.EffectiveSecurityWorkflows(); !slices.Equal(got, DefaultRepoCIWatchSecurityWorkflows) {
		t.Errorf("effective security_workflows after reload = %v, want the default %v", got, DefaultRepoCIWatchSecurityWorkflows)
	}

	// A second save of the reloaded config keeps the list: the default does
	// not decay over repeated saves and restarts.
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save(reloaded): %v", err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatalf("Load after second save: %v", err)
	}
	if got := again.Autonomous.Automation.RepoCIWatch.EffectiveSecurityWorkflows(); !slices.Equal(got, DefaultRepoCIWatchSecurityWorkflows) {
		t.Errorf("effective security_workflows after second save = %v, want the default", got)
	}
}

// TestRepoCIWatchSecurityWorkflowsUnsetFileReadsDefault — a config file that
// never mentions security_workflows reads the default on load.
func TestRepoCIWatchSecurityWorkflowsUnsetFileReadsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  automation:\n    repo_ci_watch:\n      mode: shadow\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.Automation.RepoCIWatch.EffectiveSecurityWorkflows(); !slices.Equal(got, DefaultRepoCIWatchSecurityWorkflows) {
		t.Errorf("unset security_workflows = %v, want the default", got)
	}
}

// TestRepoCIWatchSecurityWorkflowsExplicitListRoundTrip — an operator's
// explicit list, including an explicit empty list (= none), is kept exactly
// across a save and a reload; only an absent key reads the default.
func TestRepoCIWatchSecurityWorkflowsExplicitListRoundTrip(t *testing.T) {
	cases := map[string][]string{
		"explicit list":  {"CodeQL", "secret-scan"},
		"explicit empty": {},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := DefaultConfig()
			cfg.Autonomous.Automation.RepoCIWatch.SecurityWorkflows = append([]string{}, want...)
			if err := Save(cfg, path); err != nil {
				t.Fatalf("Save: %v", err)
			}
			loaded, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := loaded.Autonomous.Automation.RepoCIWatch.EffectiveSecurityWorkflows()
			if len(got) != len(want) || (len(want) > 0 && !slices.Equal(got, want)) {
				t.Errorf("reloaded = %v, want %v", got, want)
			}
			if len(want) == 0 && loaded.Autonomous.Automation.RepoCIWatch.SecurityWorkflows == nil {
				t.Errorf("explicit empty list reloaded as unset (nil); it must stay an explicit none")
			}
		})
	}
}
