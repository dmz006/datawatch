package router

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDeployComm_RunRefusalNamesTheSetting(t *testing.T) {
	_, _, got, _ := deployCommSay(t, 403, config.MergeDeployRunRefusal+"\n", "autonomous deploy run")
	if !strings.Contains(got, "autonomous deploy failed") || !strings.Contains(got, config.MergeDeployRunRefusal) {
		t.Errorf("reply %q, want the refusal", got)
	}
}

// A held supervisor loop job shows the daemon's reason in chat.
func TestDeployComm_HeldLoopJobShowsTheReason(t *testing.T) {
	_, port := laneFillCommDaemon(t, 403, config.MergeDeployRunRefusal+"\n")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor run job=run_merge_until_conflict"), " ")
	if !strings.Contains(got, "run failed") || !strings.Contains(got, config.MergeDeployRunRefusal) {
		t.Errorf("reply %q, want the refusal", got)
	}
}
