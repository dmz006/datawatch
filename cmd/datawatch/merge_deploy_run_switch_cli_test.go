package main

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDeployCLI_RunRefusalNamesTheSetting(t *testing.T) {
	var rec [2]string
	deployTestDaemon(t, 403, config.MergeDeployRunRefusal+"\n", &rec)
	for _, args := range [][]string{{"run"}, {"run", "--json"}} {
		_, err := deployRun(args...)
		if err == nil || !strings.Contains(err.Error(), config.MergeDeployRunRefusal) {
			t.Errorf("%v: err = %v, want the refusal", args, err)
		}
	}
}

func TestDeployCLI_HelpNamesTheRunSwitch(t *testing.T) {
	if long := newAutonomousDeployCmd().Long; !strings.Contains(long, config.MergeDeployRunKey) {
		t.Errorf("deploy help does not name %s: %q", config.MergeDeployRunKey, long)
	}
}

// A held supervisor loop job fails the command with the daemon's reason, and
// the command help names the switch.
func TestDeployCLI_HeldLoopJobShowsTheReason(t *testing.T) {
	supervisorLoopDaemon(t, 403, config.MergeDeployRunRefusal+"\n")
	_, err := runSupervisorLoopCmd(t, "run", "run_merge_until_conflict")
	if err == nil || !strings.Contains(err.Error(), config.MergeDeployRunRefusal) {
		t.Errorf("err = %v, want the refusal", err)
	}
	if long := newSupervisorLoopRunCmd().Long; !strings.Contains(long, config.MergeDeployRunKey) {
		t.Errorf("run help does not name %s: %q", config.MergeDeployRunKey, long)
	}
}
