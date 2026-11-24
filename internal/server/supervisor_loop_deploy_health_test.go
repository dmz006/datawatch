package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// The health check after a deploy is the operator's command: no command is
// unchecked, exit 0 passes, anything else fails with its first output line.
func TestSupervisorLoopDeployHealth(t *testing.T) {
	cfg := autonomous.DefaultConfig()
	cfg.WorktreeBaseRepo = t.TempDir()
	m, err := autonomous.NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := func(cmd string) (string, string) {
		return supervisorLoopDeployHealth(context.Background(), m, t.TempDir(), config.MergePipelineConfig{}, cmd)
	}
	if h, _ := run(""); h != autonomous.DeployHealthUnchecked {
		t.Errorf("no command: health = %q, want unchecked", h)
	}
	if h, d := run("echo healthy"); h != autonomous.DeployHealthPassed || d != "healthy" {
		t.Errorf("exit 0: health=%q detail=%q, want passed/healthy", h, d)
	}
	if h, d := run("echo not ready; exit 3"); h != autonomous.DeployHealthFailed || d == "" {
		t.Errorf("exit 3: health=%q detail=%q, want failed with a detail", h, d)
	}
	bare, _ := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if h, _ := supervisorLoopDeployHealth(context.Background(), bare, t.TempDir(), config.MergePipelineConfig{}, "exit 0"); h != autonomous.DeployHealthUnchecked {
		t.Errorf("no base repository: health = %q, want unchecked", h)
	}
}
